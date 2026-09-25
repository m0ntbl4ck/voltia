package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/m0ntbl4ck/voltia/internal/domain"
)

const anomalyIDValue = "7d1e4f60-2b3c-4d5e-8f90-a1b2c3d4e5f6"

type actCall struct {
	anomalyID, userID string
	action            domain.Action
	note              string
}

type fakeStore struct {
	list      []domain.Anomaly
	one       domain.Anomaly
	history   []domain.AnomalyAction
	listErr   error
	getErr    error
	actionErr error
	applyErr  error
	applied   domain.Anomaly

	gotFilter   domain.AnomalyFilter
	listCalls   int
	getCalls    int
	actCalls    []actCall
	historyCall int
}

func (f *fakeStore) Anomalies(_ context.Context, filter domain.AnomalyFilter) ([]domain.Anomaly, error) {
	f.listCalls++
	f.gotFilter = filter
	return f.list, f.listErr
}

func (f *fakeStore) Anomaly(context.Context, string) (domain.Anomaly, error) {
	f.getCalls++
	return f.one, f.getErr
}

func (f *fakeStore) AnomalyActions(context.Context, string) ([]domain.AnomalyAction, error) {
	f.historyCall++
	return f.history, f.actionErr
}

func (f *fakeStore) ApplyAction(_ context.Context, id, userID string, action domain.Action, note string) (domain.Anomaly, error) {
	f.actCalls = append(f.actCalls, actCall{id, userID, action, note})
	return f.applied, f.applyErr
}

func sampleDomainAnomaly() domain.Anomaly {
	start := time.Date(2026, 9, 12, 14, 0, 0, 0, bogotaLoc())
	return domain.Anomaly{
		ID: anomalyIDValue, MeterID: "M-109", Type: domain.RealAnomaly, Severity: domain.SeverityHigh,
		Confidence: 0.988, Priority: 100, Status: domain.StatusOpen, Ongoing: true,
		EpisodeStart: start, EpisodeEnd: start.Add(58 * time.Hour), DetectedAt: start.Add(100 * time.Hour),
		ConfidenceBreakdown: json.RawMessage(`{"detector_agreement":0.75}`),
		PriorityBreakdown:   json.RawMessage(`{"impact":15}`),
		Evidence:            domain.Evidence{MeterID: "M-109", MeterName: "Molino", Location: "Nave 1", VariationPct: 110.5},
		Explanation: domain.Explanation{
			Summary: "Consumption doubled", Reason: "It is 110% above its baseline.", RecommendedAction: "Send an inspector.",
			InvestigationSteps: []string{"Check the feeder"}, Source: domain.SourceTemplate,
		},
		LastAnalysisID: validID,
	}
}

func bogotaLoc() *time.Location {
	loc, err := time.LoadLocation("America/Bogota")
	if err != nil {
		panic(err)
	}
	return loc
}

func serverWithStore(store *fakeStore) http.Handler {
	return NewRouter(Deps{
		Auth: newFakeAuth(), Analysis: &fakeStarter{}, Runs: &fakeRuns{}, Anomalies: store,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

func postAs(h http.Handler, path, contentType, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: validToken})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decodeMap(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("body is not a JSON object: %s (%v)", rec.Body, err)
	}
	return m
}

func TestListShowsTheChallengeFieldsAndTheExtras(t *testing.T) {
	store := &fakeStore{list: []domain.Anomaly{sampleDomainAnomaly()}}
	rec := do(serverWithStore(store), http.MethodGet, "/api/v1/anomalies")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	var list []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list) != 1 {
		t.Fatalf("body = %s (%v)", rec.Body, err)
	}
	a := list[0]
	want := map[string]any{
		"meter_id": "M-109", "anomaly": "Consumption doubled", "type": "REAL_ANOMALY", "severity": "HIGH",
		"confidence": 0.988, "reason": "It is 110% above its baseline.", "recommended_action": "Send an inspector.",
		"id": anomalyIDValue, "meter_name": "Molino", "location": "Nave 1", "priority": 100.0, "status": "OPEN",
		"ongoing": true, "episode_start": "2026-09-12T19:00:00Z", "episode_end": "2026-09-15T05:00:00Z",
		"recommended_next_action": "CREATE_INSPECTION_ORDER", "explanation_source": "TEMPLATE", "explanation_model": "",
	}
	for k, v := range want {
		if a[k] != v {
			t.Errorf("%s = %v, want %v", k, a[k], v)
		}
	}
	if actions, _ := a["available_actions"].([]any); len(actions) != 4 {
		t.Errorf("an open anomaly offers %v", a["available_actions"])
	}
	if _, ok := a["evidence"]; ok {
		t.Error("the list carries the full evidence, which belongs to the detail")
	}
}

func TestNextActionDisappearsOnceTheAnomalyIsNotOpen(t *testing.T) {
	ack := sampleDomainAnomaly()
	ack.Status = domain.StatusAcknowledged
	done := sampleDomainAnomaly()
	done.Status = domain.StatusResolved
	rec := do(serverWithStore(&fakeStore{list: []domain.Anomaly{ack, done}}), http.MethodGet, "/api/v1/anomalies")
	var list []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if list[0]["recommended_next_action"] != nil || list[1]["recommended_next_action"] != nil {
		t.Errorf("recommended_next_action = %v, %v; want null once the anomaly is acknowledged or resolved",
			list[0]["recommended_next_action"], list[1]["recommended_next_action"])
	}
	if got := list[0]["available_actions"].([]any); len(got) != 1 || got[0] != "RESOLVE" {
		t.Errorf("acknowledged offers %v, want [RESOLVE]", got)
	}
	if got, ok := list[1]["available_actions"].([]any); !ok || len(got) != 0 {
		t.Errorf("a resolved anomaly offers %v, want an empty list and not null", list[1]["available_actions"])
	}
}

func TestEmptyListIsAnArrayNotNull(t *testing.T) {
	rec := do(serverWithStore(&fakeStore{}), http.MethodGet, "/api/v1/anomalies")
	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Errorf("body = %q, want []", rec.Body)
	}
}

func TestListPassesFiltersToTheStore(t *testing.T) {
	store := &fakeStore{}
	do(serverWithStore(store), http.MethodGet, "/api/v1/anomalies?type=DATA_QUALITY&severity=HIGH&status=OPEN&meter_id=M-112")
	want := domain.AnomalyFilter{Type: domain.DataQuality, Severity: domain.SeverityHigh, Status: domain.StatusOpen, MeterID: "M-112"}
	if store.gotFilter != want {
		t.Errorf("filter = %+v, want %+v", store.gotFilter, want)
	}
}

func TestListRejectsFiltersThatCannotMatch(t *testing.T) {
	for _, q := range []string{
		"type=REAL", "type=real_anomaly", "severity=CRITICAL", "status=CLOSED",
		"meter_id=" + strings.Repeat("M", 33),
	} {
		store := &fakeStore{}
		rec := do(serverWithStore(store), http.MethodGet, "/api/v1/anomalies?"+q)
		if rec.Code != http.StatusBadRequest || rec.Header().Get("Content-Type") != "application/problem+json" {
			t.Errorf("%s: status %d, type %q", q, rec.Code, rec.Header().Get("Content-Type"))
		}
		if store.listCalls != 0 {
			t.Errorf("%s: the store was queried", q)
		}
	}
}

func TestListStoreFailureIsA500WithoutTheCause(t *testing.T) {
	rec := do(serverWithStore(&fakeStore{listErr: errors.New("pq: relation anomalies is locked")}), http.MethodGet, "/api/v1/anomalies")
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "relation") {
		t.Errorf("status %d, body %s", rec.Code, rec.Body)
	}
}

func TestDetailCarriesEvidenceBreakdownsAndHistory(t *testing.T) {
	when := time.Date(2026, 9, 24, 15, 30, 0, 0, bogotaLoc())
	store := &fakeStore{
		one: sampleDomainAnomaly(),
		history: []domain.AnomalyAction{{
			ID: "h1", Action: domain.ActionCreateInspectionOrder, Note: "Luis goes", From: domain.StatusOpen,
			To: domain.StatusAcknowledged, UserName: "Ana", CreatedAt: when,
		}},
	}
	rec := do(serverWithStore(store), http.MethodGet, "/api/v1/anomalies/"+anomalyIDValue)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	d := decodeMap(t, rec)
	if d["id"] != anomalyIDValue || d["meter_id"] != "M-109" {
		t.Errorf("the summary fields are missing from the detail: %v", d["id"])
	}
	if ev, _ := d["evidence"].(map[string]any); ev["meter_name"] != "Molino" || ev["variation_pct"] != 110.5 {
		t.Errorf("evidence = %v", d["evidence"])
	}
	if cb, _ := d["confidence_breakdown"].(map[string]any); cb["detector_agreement"] != 0.75 {
		t.Errorf("confidence_breakdown = %v", d["confidence_breakdown"])
	}
	if pb, _ := d["priority_breakdown"].(map[string]any); pb["impact"] != 15.0 {
		t.Errorf("priority_breakdown = %v", d["priority_breakdown"])
	}
	if steps, _ := d["investigation_steps"].([]any); len(steps) != 1 || steps[0] != "Check the feeder" {
		t.Errorf("investigation_steps = %v", d["investigation_steps"])
	}
	actions, _ := d["actions"].([]any)
	if len(actions) != 1 {
		t.Fatalf("actions = %v", d["actions"])
	}
	first := actions[0].(map[string]any)
	if first["action"] != "CREATE_INSPECTION_ORDER" || first["note"] != "Luis goes" || first["user_name"] != "Ana" ||
		first["from_status"] != "OPEN" || first["to_status"] != "ACKNOWLEDGED" || first["created_at"] != "2026-09-24T20:30:00Z" {
		t.Errorf("history entry = %v", first)
	}
}

func TestDetailWithNoHistoryOrStepsUsesEmptyArrays(t *testing.T) {
	a := sampleDomainAnomaly()
	a.Explanation.InvestigationSteps = nil
	rec := do(serverWithStore(&fakeStore{one: a}), http.MethodGet, "/api/v1/anomalies/"+anomalyIDValue)
	d := decodeMap(t, rec)
	if steps, ok := d["investigation_steps"].([]any); !ok || len(steps) != 0 {
		t.Errorf("investigation_steps = %v, want []", d["investigation_steps"])
	}
	if acts, ok := d["actions"].([]any); !ok || len(acts) != 0 {
		t.Errorf("actions = %v, want []", d["actions"])
	}
}

func TestDetailErrors(t *testing.T) {
	tests := []struct {
		name  string
		path  string
		store *fakeStore
		want  int
		reads bool
	}{
		{"unknown id", "/api/v1/anomalies/" + anomalyIDValue, &fakeStore{getErr: domain.ErrNotFound}, 404, true},
		{"not a uuid", "/api/v1/anomalies/abc", &fakeStore{}, 404, false},
		{"store failure", "/api/v1/anomalies/" + anomalyIDValue, &fakeStore{getErr: errors.New("boom")}, 500, true},
		{"history failure", "/api/v1/anomalies/" + anomalyIDValue, &fakeStore{one: sampleDomainAnomaly(), actionErr: errors.New("boom")}, 500, true},
	}
	for _, tt := range tests {
		rec := do(serverWithStore(tt.store), http.MethodGet, tt.path)
		if rec.Code != tt.want || rec.Header().Get("Content-Type") != "application/problem+json" {
			t.Errorf("%s: status %d, type %q", tt.name, rec.Code, rec.Header().Get("Content-Type"))
		}
		if (tt.store.getCalls > 0) != tt.reads {
			t.Errorf("%s: store read = %v, want %v", tt.name, tt.store.getCalls > 0, tt.reads)
		}
	}
}

func TestActionAppliesAsTheSessionUserAndReturnsTheDetail(t *testing.T) {
	updated := sampleDomainAnomaly()
	updated.Status = domain.StatusAcknowledged
	store := &fakeStore{applied: updated, history: []domain.AnomalyAction{{ID: "h1", Action: domain.ActionCreateInspectionOrder}}}
	rec := postAs(serverWithStore(store), "/api/v1/anomalies/"+anomalyIDValue+"/actions", "application/json",
		`{"action":"CREATE_INSPECTION_ORDER","note":"  Send Luis to the mill  "}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	if len(store.actCalls) != 1 {
		t.Fatalf("%d actions applied", len(store.actCalls))
	}
	if got, want := store.actCalls[0], (actCall{anomalyIDValue, validID, domain.ActionCreateInspectionOrder, "Send Luis to the mill"}); got != want {
		t.Errorf("applied %+v, want %+v (the user comes from the session, the note is trimmed)", got, want)
	}
	d := decodeMap(t, rec)
	if d["status"] != "ACKNOWLEDGED" || d["recommended_next_action"] != nil {
		t.Errorf("status %v, next action %v", d["status"], d["recommended_next_action"])
	}
	if acts, _ := d["actions"].([]any); len(acts) != 1 {
		t.Errorf("the response lacks the history: %v", d["actions"])
	}
}

func TestActionRejectionsNeverReachTheStore(t *testing.T) {
	path := "/api/v1/anomalies/" + anomalyIDValue + "/actions"
	tests := []struct {
		name, contentType, body string
		want                    int
	}{
		{"unknown action", "application/json", `{"action":"EXPLODE"}`, 400},
		{"lower case action", "application/json", `{"action":"dismiss"}`, 400},
		{"missing action", "application/json", `{"note":"hi"}`, 400},
		{"malformed JSON", "application/json", `{"action":`, 400},
		{"note too long", "application/json", `{"action":"DISMISS","note":"` + strings.Repeat("a", 501) + `"}`, 400},
		{"note too long in multibyte characters", "application/json", `{"action":"DISMISS","note":"` + strings.Repeat("ñ", 501) + `"}`, 400},
		{"form encoding", "application/x-www-form-urlencoded", `action=DISMISS`, 415},
		{"no content type", "", `{"action":"DISMISS"}`, 415},
	}
	for _, tt := range tests {
		store := &fakeStore{}
		rec := postAs(serverWithStore(store), path, tt.contentType, tt.body)
		if rec.Code != tt.want || rec.Header().Get("Content-Type") != "application/problem+json" {
			t.Errorf("%s: status %d, type %q", tt.name, rec.Code, rec.Header().Get("Content-Type"))
		}
		if len(store.actCalls) != 0 {
			t.Errorf("%s: the action reached the store", tt.name)
		}
	}
}

func TestActionAcceptsANoteOfExactlyTheLimit(t *testing.T) {
	store := &fakeStore{applied: sampleDomainAnomaly()}
	rec := postAs(serverWithStore(store), "/api/v1/anomalies/"+anomalyIDValue+"/actions", "application/json; charset=utf-8",
		`{"action":"DISMISS","note":"`+strings.Repeat("ñ", 500)+`"}`)
	if rec.Code != http.StatusOK || len(store.actCalls) != 1 {
		t.Errorf("status %d with %d applied", rec.Code, len(store.actCalls))
	}
}

func TestActionOutcomes(t *testing.T) {
	path := "/api/v1/anomalies/" + anomalyIDValue + "/actions"
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"unknown anomaly", domain.ErrNotFound, 404},
		{"not allowed in this status", domain.ErrInvalidTransition, 409},
		{"store failure", errors.New("pq: deadlock detected"), 500},
	}
	for _, tt := range tests {
		rec := postAs(serverWithStore(&fakeStore{applyErr: tt.err}), path, "application/json", `{"action":"DISMISS"}`)
		if rec.Code != tt.want || rec.Header().Get("Content-Type") != "application/problem+json" {
			t.Errorf("%s: status %d, type %q", tt.name, rec.Code, rec.Header().Get("Content-Type"))
		}
		if strings.Contains(rec.Body.String(), "deadlock") {
			t.Errorf("%s: the body leaks the cause: %s", tt.name, rec.Body)
		}
	}
	store := &fakeStore{}
	rec := postAs(serverWithStore(store), "/api/v1/anomalies/not-an-id/actions", "application/json", `{"action":"DISMISS"}`)
	if rec.Code != http.StatusNotFound || len(store.actCalls) != 0 {
		t.Errorf("a bad id: status %d, %d applied", rec.Code, len(store.actCalls))
	}
}

func TestAnomalyRoutesNeedASession(t *testing.T) {
	store := &fakeStore{list: []domain.Anomaly{sampleDomainAnomaly()}, one: sampleDomainAnomaly()}
	h := serverWithStore(store)
	for _, p := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/anomalies"},
		{http.MethodGet, "/api/v1/anomalies/" + anomalyIDValue},
		{http.MethodPost, "/api/v1/anomalies/" + anomalyIDValue + "/actions"},
	} {
		if rec := doAnon(h, p.method, p.path); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without a session: %d", p.method, p.path, rec.Code)
		}
	}
	if store.listCalls+store.getCalls+len(store.actCalls) != 0 {
		t.Error("a handler ran without a session")
	}
}
