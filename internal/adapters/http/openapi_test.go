package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"

	"github.com/m0ntbl4ck/voltia/internal/analysis/scoring"
	"github.com/m0ntbl4ck/voltia/internal/app"
	"github.com/m0ntbl4ck/voltia/internal/domain"
)

// The contract tests run the real router with fakes and check every response
// against the schema of api/openapi.yaml, so the document cannot drift from the code.

type spec map[string]any

func loadSpec(t *testing.T) spec {
	t.Helper()
	raw, err := os.ReadFile("../../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	// Decoding into the plain map keeps the nested maps plain too.
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return spec(doc)
}

// at walks a path of keys through nested maps.
func (s spec) at(keys ...string) map[string]any {
	var cur any = map[string]any(s)
	for _, k := range keys {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[k]
	}
	m, _ := cur.(map[string]any)
	return m
}

func (s spec) resolve(node map[string]any) map[string]any {
	for {
		ref, ok := node["$ref"].(string)
		if !ok {
			return node
		}
		node = s.at(strings.Split(strings.TrimPrefix(ref, "#/"), "/")...)
		if node == nil {
			panic("unresolved reference " + ref)
		}
	}
}

func toFloat(v any) float64 {
	switch n := v.(type) {
	case int:
		return float64(n)
	case float64:
		return n
	}
	return 0
}

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// validate reports every way value differs from schema. Objects are strict:
// a key that the schema does not list is an error unless additionalProperties says otherwise.
func (s spec) validate(schema map[string]any, value any, path string) []string {
	schema = s.resolve(schema)
	if value == nil {
		if schema["nullable"] == true {
			return nil
		}
		return []string{path + ": null where the schema does not allow it"}
	}
	var errs []string
	if all, ok := schema["allOf"].([]any); ok {
		// An object is checked against the union of its branches, or a branch
		// would reject the keys the others add.
		if obj, isObject := value.(map[string]any); isObject {
			return s.validateObject(schema, obj, path)
		}
		for _, sub := range all {
			errs = append(errs, s.validate(sub.(map[string]any), value, path)...)
		}
		return errs
	}
	if enum, ok := schema["enum"].([]any); ok {
		found := false
		for _, e := range enum {
			if fmt.Sprint(e) == fmt.Sprint(value) {
				found = true
			}
		}
		if !found {
			errs = append(errs, fmt.Sprintf("%s: %v is not one of %v", path, value, enum))
		}
	}
	switch schema["type"] {
	case "object":
		obj, ok := value.(map[string]any)
		if !ok {
			return append(errs, path+": not an object")
		}
		errs = append(errs, s.validateObject(schema, obj, path)...)
	case "array":
		arr, ok := value.([]any)
		if !ok {
			return append(errs, path+": not an array")
		}
		if items, ok := schema["items"].(map[string]any); ok {
			for i, item := range arr {
				errs = append(errs, s.validate(items, item, fmt.Sprintf("%s[%d]", path, i))...)
			}
		}
	case "string":
		str, ok := value.(string)
		if !ok {
			return append(errs, path+": not a string")
		}
		switch schema["format"] {
		case "date-time":
			if _, err := time.Parse(time.RFC3339, str); err != nil {
				errs = append(errs, path+": not an RFC 3339 time: "+str)
			}
		case "uuid":
			if !uuidRe.MatchString(str) {
				errs = append(errs, path+": not a uuid: "+str)
			}
		}
		if max, ok := schema["maxLength"]; ok && float64(len([]rune(str))) > toFloat(max) {
			errs = append(errs, path+": longer than maxLength")
		}
	case "integer", "number":
		n, ok := value.(float64)
		if !ok {
			return append(errs, path+": not a number")
		}
		if schema["type"] == "integer" && n != float64(int64(n)) {
			errs = append(errs, fmt.Sprintf("%s: %v is not an integer", path, n))
		}
		if min, ok := schema["minimum"]; ok && n < toFloat(min) {
			errs = append(errs, fmt.Sprintf("%s: %v is below the minimum", path, n))
		}
		if max, ok := schema["maximum"]; ok && n > toFloat(max) {
			errs = append(errs, fmt.Sprintf("%s: %v is above the maximum", path, n))
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			errs = append(errs, path+": not a boolean")
		}
	}
	return errs
}

// properties gathers the declared properties and required keys of a schema and its allOf branches.
func (s spec) properties(schema map[string]any, props map[string]map[string]any, required *[]string) {
	schema = s.resolve(schema)
	if all, ok := schema["allOf"].([]any); ok {
		for _, sub := range all {
			s.properties(sub.(map[string]any), props, required)
		}
	}
	if p, ok := schema["properties"].(map[string]any); ok {
		for k, v := range p {
			props[k] = v.(map[string]any)
		}
	}
	if r, ok := schema["required"].([]any); ok {
		for _, k := range r {
			*required = append(*required, k.(string))
		}
	}
}

func (s spec) validateObject(schema map[string]any, obj map[string]any, path string) []string {
	props := map[string]map[string]any{}
	var required []string
	s.properties(schema, props, &required)
	var errs []string
	for _, k := range required {
		if _, ok := obj[k]; !ok {
			errs = append(errs, path+": missing required "+k)
		}
	}
	extra, hasExtra := schema["additionalProperties"]
	for _, k := range slices.Sorted(maps.Keys(obj)) {
		if sub, ok := props[k]; ok {
			errs = append(errs, s.validate(sub, obj[k], path+"."+k)...)
			continue
		}
		switch e := extra.(type) {
		case map[string]any:
			errs = append(errs, s.validate(e, obj[k], path+"."+k)...)
		case bool:
			if !e {
				errs = append(errs, path+": unexpected key "+k)
			}
		default:
			if !hasExtra {
				errs = append(errs, path+": unexpected key "+k)
			}
		}
	}
	return errs
}

func TestTheValidatorCatchesDrift(t *testing.T) {
	s := loadSpec(t)
	schema := map[string]any{"$ref": "#/components/schemas/Level"}
	cases := map[string]any{
		"a missing key":      map[string]any{"recent": 1.0},
		"an unknown key":     map[string]any{"recent": 1.0, "baseline": 2.0, "extra": 3.0},
		"the wrong type":     map[string]any{"recent": "1", "baseline": 2.0},
		"a null":             map[string]any{"recent": nil, "baseline": 2.0},
		"not even an object": []any{},
	}
	for name, v := range cases {
		if errs := s.validate(schema, v, "$"); len(errs) == 0 {
			t.Errorf("%s was accepted", name)
		}
	}
	if errs := s.validate(schema, map[string]any{"recent": 1.0, "baseline": 2.0}, "$"); len(errs) != 0 {
		t.Errorf("a valid value was rejected: %v", errs)
	}
}

// --- fixtures -------------------------------------------------------------

func contractAnomaly(status domain.AnomalyStatus) domain.Anomaly {
	start := time.Date(2026, 9, 12, 14, 0, 0, 0, bogotaLoc())
	confidence, _ := json.Marshal(scoring.ConfidenceBreakdown{DetectorAgreement: 0.75, SignalStrength: 1, ClassificationClarity: 1, DataIntegrity: 1, IntegrityApplies: true})
	priority, _ := json.Marshal(scoring.PriorityBreakdown{Severity: 40, Type: 30, Impact: 15, Recency: 15})
	return domain.Anomaly{
		ID: anomalyIDValue, MeterID: "M-109", Type: domain.RealAnomaly, Severity: domain.SeverityHigh, Confidence: 0.988,
		Priority: 100, Status: status, Ongoing: true, EpisodeStart: start, EpisodeEnd: start.Add(58 * time.Hour),
		DetectedAt: start.Add(100 * time.Hour), ConfidenceBreakdown: confidence, PriorityBreakdown: priority,
		Evidence: domain.Evidence{
			MeterID: "M-109", MeterName: "Molino", Location: "Nave 1", Type: domain.RealAnomaly, Severity: domain.SeverityHigh,
			Rule: "no_explaining_event", Confidence: 0.988, EpisodeStart: start.UTC(), EpisodeEnd: start.Add(58 * time.Hour).UTC(),
			DurationH: 58, Ongoing: true, Direction: "UP", VariationPct: 110.5, ExcessKWh: 2825, InvalidReadings: 0,
			Signals: []domain.SignalEvidence{
				{Kind: "PERSISTENT_SHIFT", Variable: domain.Consumption, Start: start.UTC(), End: start.Add(58 * time.Hour).UTC(), Hours: 58, Direction: 1, Observed: 92.8, Expected: 44.1, MeanZ: 21},
				{Kind: "HOURLY_PATTERN", Check: "daily_correlation", Variable: domain.Consumption, Start: start.UTC(), End: start.Add(10 * time.Hour).UTC(), Hours: 10,
					Observed: 98, Expected: 46, MeanZ: 21.8, Metrics: map[string]float64{"correlation": 0.53}},
				{Kind: "ISOLATION_FOREST", Variable: domain.Current, Start: start.UTC(), End: start.Add(58 * time.Hour).UTC(), Hours: 58,
					Observed: 0.75, Expected: 0.6, Attribution: map[domain.Variable]float64{domain.Current: 0.6, domain.Voltage: 0.4}},
			},
			Events: []domain.EventEvidence{{Type: domain.EventUnknown, Timestamp: start.UTC(), Description: "No operational event reported", Role: "NOT_EXPLANATORY"}},
		},
		Explanation: domain.Explanation{
			Summary: "Consumption doubled", Reason: "It is 110% above its baseline.", RecommendedAction: "Send an inspector.",
			InvestigationSteps: []string{"Check the feeder"}, Source: domain.SourceLLM, Model: "gemini-3.8-flash",
		},
		LastAnalysisID: validID,
	}
}

func contractSummary() app.MeterSummary {
	s := sampleSummary()
	s.LastReadingState = "OK"
	return s
}

func contractRun() domain.AnalysisRun {
	start := time.Date(2026, 9, 24, 15, 0, 0, 0, bogotaLoc())
	end := start.Add(3 * time.Second)
	return domain.AnalysisRun{
		ID: validID, Status: domain.RunCompleted, StartedAt: start, FinishedAt: &end,
		Stages: []domain.StageState{
			{Name: "READINGS", Status: domain.StageDone, StartedAt: &start, FinishedAt: &end},
			{Name: "BASELINE", Status: domain.StageRunning, StartedAt: &start},
			{Name: "DETECTION", Status: domain.StagePending},
		},
		Summary: &domain.RunSummary{
			Anomalies: 4, ByType: map[string]int{"REAL_ANOMALY": 1}, BySeverity: map[string]int{"HIGH": 2}, Confidence: 0.989,
			Failures: []domain.MeterFailure{{MeterID: "M-101", Error: "no baseline"}},
		},
	}
}

func contractSeries(withBaseline bool) app.Series {
	at := time.Date(2026, 9, 10, 0, 0, 0, 0, bogotaLoc())
	s := app.Series{Resolution: "hour", Points: []app.Point{{Time: at, ConsumptionKWh: 50, VoltageV: 220, CurrentA: 100, PowerFactor: 0.95, Hours: 1}}}
	if withBaseline {
		var bands [24]app.HourBand
		for h := range bands {
			bands[h] = app.HourBand{Median: 50, Sigma: 2.5}
		}
		s.Baseline = &app.BaselineInfo{
			ReferenceStart: at, ReferenceEnd: at.Add(7 * 24 * time.Hour), BandZ: 3, DailyKWh: 1200,
			Profile: map[domain.Variable][24]app.HourBand{domain.Consumption: bands, domain.Voltage: bands},
		}
	}
	return s
}

// world is the set of fakes one case runs against.
type world struct {
	store     *fakeStore
	meters    *fakeMeters
	dashboard *fakeDashboard
	runs      *fakeRuns
	starter   *fakeStarter
	auth      *fakeAuth
}

func newWorld() *world {
	an := contractAnomaly(domain.StatusOpen)
	acknowledged := contractAnomaly(domain.StatusAcknowledged)
	history := []domain.AnomalyAction{{
		ID: validID, Action: domain.ActionCreateInspectionOrder, Note: "Send Luis", From: domain.StatusOpen,
		To: domain.StatusAcknowledged, UserName: "Ana", CreatedAt: time.Date(2026, 9, 24, 15, 30, 0, 0, bogotaLoc()),
	}}
	confidence := 0.989
	run := contractRun()
	detail := app.MeterDetail{
		MeterSummary: contractSummary(), Voltage: app.Level{Recent: 216.8, Baseline: 219.8},
		Current: app.Level{Recent: 424.7, Baseline: 202.1}, PowerFactor: app.Level{Recent: 0.74, Baseline: 0.94},
		Anomalies: []domain.Anomaly{an},
	}
	return &world{
		store: &fakeStore{list: []domain.Anomaly{an, acknowledged}, one: an, history: history, applied: acknowledged},
		meters: &fakeMeters{
			list:   []app.MeterSummary{contractSummary(), {Meter: domain.Meter{MeterID: "M-101", Name: "Compresor"}, Status: domain.MeterOK}},
			detail: detail, series: contractSeries(true),
			events: []domain.Event{
				{Type: domain.EventScheduledOutage, Timestamp: time.Date(2026, 9, 8, 0, 0, 0, 0, bogotaLoc()), Description: "Outage for 12 hours", Duration: 12 * time.Hour},
				{Type: domain.EventUnknown, Timestamp: time.Date(2026, 9, 12, 14, 0, 0, 0, bogotaLoc()), Description: "None"},
			},
		},
		dashboard: &fakeDashboard{summary: app.Dashboard{
			HasAnalysis: true, LastAnalysis: &run, Meters: app.MeterCounts{Total: 12, OK: 9, Alert: 2, Critical: 1},
			PeriodFrom: "2026-09-01", PeriodTo: "2026-09-14", ConsumptionKWh: 155251, VariationPct: 15.8,
			Daily: []app.DailyKWh{{Date: "2026-09-14", KWh: 12000}}, Anomalies: app.AnomalyCounts{Unresolved: 4, PendingHighPriority: 2},
			Confidence: &confidence, Attention: []domain.Anomaly{an},
		}},
		runs:    &fakeRuns{run: run},
		starter: &fakeStarter{run: domain.AnalysisRun{ID: validID}},
		auth:    newFakeAuth(),
	}
}

func (w *world) handler() http.Handler {
	return NewRouter(Deps{
		Auth: w.auth, Analysis: w.starter, Runs: w.runs, Anomalies: w.store, Meters: w.meters, Dashboard: w.dashboard,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

type contractCase struct {
	route, method, path string
	status              int
	body, contentType   string
	anon                bool
	tweak               func(*world)
}

func (c contractCase) run(t *testing.T, s spec) {
	t.Helper()
	w := newWorld()
	if c.tweak != nil {
		c.tweak(w)
	}
	req := httptest.NewRequest(c.method, "/api/v1"+c.path, strings.NewReader(c.body))
	if c.route == "/healthz" {
		req = httptest.NewRequest(c.method, c.path, nil)
	}
	if c.contentType != "" {
		req.Header.Set("Content-Type", c.contentType)
	}
	if !c.anon {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: validToken})
	}
	rec := httptest.NewRecorder()
	w.handler().ServeHTTP(rec, req)
	name := fmt.Sprintf("%s %s -> %d", c.method, c.route, c.status)

	if rec.Code != c.status {
		t.Errorf("%s: the router answered %d: %s", name, rec.Code, rec.Body)
		return
	}
	responses := s.at("paths", c.route, strings.ToLower(c.method), "responses")
	doc := responses[fmt.Sprint(c.status)]
	if doc == nil {
		t.Errorf("%s: the status is not documented", name)
		return
	}
	resp := s.resolve(doc.(map[string]any))
	content, _ := resp["content"].(map[string]any)
	if len(content) == 0 {
		if rec.Body.Len() != 0 {
			t.Errorf("%s: documented without a body but sent %q", name, rec.Body)
		}
		return
	}
	mediaType, _, _ := strings.Cut(rec.Header().Get("Content-Type"), ";")
	media, ok := content[mediaType].(map[string]any)
	if !ok {
		t.Errorf("%s: sent %q, documented %v", name, mediaType, slices.Sorted(maps.Keys(content)))
		return
	}
	schema, _ := media["schema"].(map[string]any)
	if mediaType == "text/plain" || schema == nil {
		return
	}
	var body any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Errorf("%s: the body is not JSON: %v", name, err)
		return
	}
	for _, e := range s.validate(schema, body, "$") {
		t.Errorf("%s: %s", name, e)
	}
}

func failWith(err error) func(*world) {
	return func(w *world) {
		w.store.getErr, w.store.applyErr, w.store.listErr = err, err, err
		w.meters.err = err
		w.runs.err = err
		w.starter.err = err
		w.dashboard.err = err
	}
}

// authBoom makes the login and the session check fail.
func authBoom(w *world) {
	w.auth.loginErr = errors.New("boom")
	w.auth.tokenErr = errors.New("boom")
}

const (
	loginBody  = `{"email":"demo@voltia.local","password":"right-password"}`
	jsonType   = "application/json"
	anomalyURL = "/anomalies/" + anomalyIDValue
)

func contractCases() []contractCase {
	notFound, boom := failWith(domain.ErrNotFound), failWith(errors.New("boom"))
	return []contractCase{
		{route: "/healthz", method: "GET", path: "/healthz", status: 200, anon: true},

		{route: "/auth/login", method: "POST", path: "/auth/login", status: 200, body: loginBody, contentType: jsonType, anon: true},
		{route: "/auth/login", method: "POST", path: "/auth/login", status: 400, body: `{"email":`, contentType: jsonType, anon: true},
		{route: "/auth/login", method: "POST", path: "/auth/login", status: 401, body: `{"email":"x@y.z","password":"nope"}`, contentType: jsonType, anon: true},
		{route: "/auth/login", method: "POST", path: "/auth/login", status: 415, body: loginBody, anon: true},
		{route: "/auth/login", method: "POST", path: "/auth/login", status: 500, body: loginBody, contentType: jsonType, anon: true, tweak: authBoom},
		{route: "/auth/logout", method: "POST", path: "/auth/logout", status: 204, anon: true},
		{route: "/auth/me", method: "GET", path: "/auth/me", status: 200},
		{route: "/auth/me", method: "GET", path: "/auth/me", status: 500, tweak: authBoom},

		{route: "/dashboard/summary", method: "GET", path: "/dashboard/summary", status: 200},
		{route: "/dashboard/summary", method: "GET", path: "/dashboard/summary", status: 200, tweak: func(w *world) {
			w.dashboard.summary = app.Dashboard{Meters: app.MeterCounts{Total: 12, OK: 12}}
		}},
		{route: "/dashboard/summary", method: "GET", path: "/dashboard/summary", status: 500, tweak: boom},

		{route: "/meters", method: "GET", path: "/meters", status: 200},
		{route: "/meters", method: "GET", path: "/meters?status=broken", status: 400},
		{route: "/meters", method: "GET", path: "/meters", status: 500, tweak: boom},
		{route: "/meters/{meter_id}", method: "GET", path: "/meters/M-109", status: 200},
		{route: "/meters/{meter_id}", method: "GET", path: "/meters/M-109", status: 200, tweak: func(w *world) {
			w.meters.detail.HasBaseline = false
			w.meters.detail.Anomalies = nil
		}},
		{route: "/meters/{meter_id}", method: "GET", path: "/meters/M-999", status: 404, tweak: notFound},
		{route: "/meters/{meter_id}", method: "GET", path: "/meters/M-109", status: 500, tweak: boom},
		{route: "/meters/{meter_id}/readings", method: "GET", path: "/meters/M-109/readings?include=baseline", status: 200},
		{route: "/meters/{meter_id}/readings", method: "GET", path: "/meters/M-109/readings?resolution=day", status: 200, tweak: func(w *world) {
			w.meters.series = contractSeries(false)
		}},
		{route: "/meters/{meter_id}/readings", method: "GET", path: "/meters/M-109/readings?resolution=week", status: 400},
		{route: "/meters/{meter_id}/readings", method: "GET", path: "/meters/M-999/readings", status: 404, tweak: notFound},
		{route: "/meters/{meter_id}/readings", method: "GET", path: "/meters/M-109/readings", status: 500, tweak: boom},
		{route: "/meters/{meter_id}/events", method: "GET", path: "/meters/M-106/events", status: 200},
		{route: "/meters/{meter_id}/events", method: "GET", path: "/meters/M-999/events", status: 404, tweak: notFound},
		{route: "/meters/{meter_id}/events", method: "GET", path: "/meters/M-106/events", status: 500, tweak: boom},

		{route: "/anomalies", method: "GET", path: "/anomalies", status: 200},
		{route: "/anomalies", method: "GET", path: "/anomalies?severity=CRITICAL", status: 400},
		{route: "/anomalies", method: "GET", path: "/anomalies", status: 500, tweak: boom},
		{route: "/anomalies/{id}", method: "GET", path: anomalyURL, status: 200},
		{route: "/anomalies/{id}", method: "GET", path: "/anomalies/not-an-id", status: 404},
		{route: "/anomalies/{id}", method: "GET", path: anomalyURL, status: 500, tweak: boom},
		{route: "/anomalies/{id}/actions", method: "POST", path: anomalyURL + "/actions", status: 200, body: `{"action":"CREATE_INSPECTION_ORDER","note":"Send Luis"}`, contentType: jsonType},
		{route: "/anomalies/{id}/actions", method: "POST", path: anomalyURL + "/actions", status: 400, body: `{"action":"EXPLODE"}`, contentType: jsonType},
		{route: "/anomalies/{id}/actions", method: "POST", path: anomalyURL + "/actions", status: 404, body: `{"action":"DISMISS"}`, contentType: jsonType, tweak: notFound},
		{route: "/anomalies/{id}/actions", method: "POST", path: anomalyURL + "/actions", status: 409, body: `{"action":"DISMISS"}`, contentType: jsonType, tweak: failWith(domain.ErrInvalidTransition)},
		{route: "/anomalies/{id}/actions", method: "POST", path: anomalyURL + "/actions", status: 415, body: `action=DISMISS`},
		{route: "/anomalies/{id}/actions", method: "POST", path: anomalyURL + "/actions", status: 500, body: `{"action":"DISMISS"}`, contentType: jsonType, tweak: boom},

		{route: "/ai/analyze", method: "POST", path: "/ai/analyze", status: 202},
		{route: "/ai/analyze", method: "POST", path: "/ai/analyze", status: 500, tweak: boom},
		{route: "/ai/analysis/latest", method: "GET", path: "/ai/analysis/latest", status: 200},
		{route: "/ai/analysis/latest", method: "GET", path: "/ai/analysis/latest", status: 404, tweak: notFound},
		{route: "/ai/analysis/{id}", method: "GET", path: "/ai/analysis/" + validID, status: 200},
		{route: "/ai/analysis/{id}", method: "GET", path: "/ai/analysis/" + validID, status: 404, tweak: notFound},
		{route: "/ai/analysis/{id}", method: "GET", path: "/ai/analysis/" + validID, status: 500, tweak: boom},
	}
}

// operations lists every documented path and method.
func (s spec) operations() [][2]string {
	var out [][2]string
	for path, item := range s.at("paths") {
		for method := range item.(map[string]any) {
			if method == "get" || method == "post" {
				out = append(out, [2]string{path, strings.ToUpper(method)})
			}
		}
	}
	slices.SortFunc(out, func(a, b [2]string) int { return strings.Compare(a[0]+a[1], b[0]+b[1]) })
	return out
}

func TestResponsesMatchTheOpenAPIDocument(t *testing.T) {
	s := loadSpec(t)
	hit := map[string]bool{}
	for _, c := range contractCases() {
		c.run(t, s)
		hit[fmt.Sprintf("%s %s %d", c.method, c.route, c.status)] = true
	}
	// Every protected operation must answer 401 without a session, as documented.
	for _, op := range s.operations() {
		path, method := op[0], op[1]
		if s.at("paths", path, strings.ToLower(method))["security"] != nil || path == "/healthz" {
			continue
		}
		concrete := strings.NewReplacer("{meter_id}", "M-109", "{id}", validID).Replace(path)
		c := contractCase{route: path, method: method, path: concrete, status: 401, anon: true}
		c.run(t, s)
		hit[fmt.Sprintf("%s %s 401", method, path)] = true
	}
	// Every documented success must have been exercised.
	for _, op := range s.operations() {
		for status := range s.at("paths", op[0], strings.ToLower(op[1]), "responses") {
			if status[0] == '2' && !hit[fmt.Sprintf("%s %s %s", op[1], op[0], status)] {
				t.Errorf("%s %s documents a %s that no contract case exercises", op[1], op[0], status)
			}
		}
	}
}

func TestEveryRouteIsDocumentedAndEveryDocumentedOperationExists(t *testing.T) {
	s := loadSpec(t)
	documented := map[string]bool{}
	for _, op := range s.operations() {
		documented[op[1]+" "+op[0]] = true
	}
	routes, ok := newWorld().handler().(chi.Routes)
	if !ok {
		t.Fatal("the router is not a chi router")
	}
	registered := map[string]bool{}
	err := chi.Walk(routes, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		route = strings.TrimSuffix(route, "/")
		switch {
		case route == "/healthz":
		case strings.HasPrefix(route, "/api/v1/"):
			route = strings.TrimPrefix(route, "/api/v1")
		default:
			return nil // the documentation itself and the static assets
		}
		registered[method+" "+route] = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for r := range registered {
		if !documented[r] {
			t.Errorf("%s is served but not documented", r)
		}
	}
	for d := range documented {
		if !registered[d] {
			t.Errorf("%s is documented but not served", d)
		}
	}
}
