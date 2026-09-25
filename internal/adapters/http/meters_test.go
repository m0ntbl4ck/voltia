package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/m0ntbl4ck/voltia/internal/app"
	"github.com/m0ntbl4ck/voltia/internal/domain"
)

type fakeMeters struct {
	list   []app.MeterSummary
	detail app.MeterDetail
	series app.Series
	events []domain.Event
	err    error

	gotFilter  app.MeterFilter
	gotID      string
	gotQuery   app.ReadingsQuery
	listCalls  int
	otherCalls int
}

func (f *fakeMeters) Meters(_ context.Context, filter app.MeterFilter) ([]app.MeterSummary, error) {
	f.listCalls++
	f.gotFilter = filter
	return f.list, f.err
}

func (f *fakeMeters) Meter(_ context.Context, id string) (app.MeterDetail, error) {
	f.otherCalls++
	f.gotID = id
	return f.detail, f.err
}

func (f *fakeMeters) Readings(_ context.Context, id string, q app.ReadingsQuery) (app.Series, error) {
	f.otherCalls++
	f.gotID, f.gotQuery = id, q
	return f.series, f.err
}

func (f *fakeMeters) Events(_ context.Context, id string) ([]domain.Event, error) {
	f.otherCalls++
	f.gotID = id
	return f.events, f.err
}

func (f *fakeMeters) Location() *time.Location { return bogotaLoc() }

func serverWithMeters(m *fakeMeters) http.Handler {
	return NewRouter(Deps{
		Auth: newFakeAuth(), Analysis: &fakeStarter{}, Runs: &fakeRuns{}, Anomalies: &fakeStore{}, Meters: m,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

func sampleSummary() app.MeterSummary {
	last := time.Date(2026, 9, 14, 23, 0, 0, 0, bogotaLoc())
	return app.MeterSummary{
		Meter:  domain.Meter{MeterID: "M-109", Name: "Molino", Location: "Nave 1, zona de molienda"},
		Status: domain.MeterCritical, HasBaseline: true, RecentDailyKWh: 2201.5, BaselineDailyKWh: 1048.2, VariationPct: 110,
		LastReading: last, LastReadingState: "WARNING", Unresolved: 1, TopSeverity: domain.SeverityHigh,
		Daily: []app.DailyKWh{{Date: "2026-09-13", KWh: 2100}, {Date: "2026-09-14", KWh: 2300}},
	}
}

func TestMeterListShape(t *testing.T) {
	rec := do(serverWithMeters(&fakeMeters{list: []app.MeterSummary{sampleSummary()}}), http.MethodGet, "/api/v1/meters")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	var list []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list) != 1 {
		t.Fatalf("body = %s (%v)", rec.Body, err)
	}
	m := list[0]
	want := map[string]any{
		"meter_id": "M-109", "name": "Molino", "location": "Nave 1, zona de molienda", "status": "critical",
		"has_baseline": true, "recent_daily_kwh": 2201.5, "baseline_daily_kwh": 1048.2, "variation_pct": 110.0,
		"last_reading_at": "2026-09-15T04:00:00Z", "last_reading_status": "WARNING", "open_anomalies": 1.0, "top_severity": "HIGH",
	}
	for k, v := range want {
		if m[k] != v {
			t.Errorf("%s = %v, want %v", k, m[k], v)
		}
	}
	daily, _ := m["daily"].([]any)
	if len(daily) != 2 || daily[1].(map[string]any)["date"] != "2026-09-14" || daily[1].(map[string]any)["kwh"] != 2300.0 {
		t.Errorf("daily = %v", m["daily"])
	}
}

func TestMeterWithNothingToReportUsesNulls(t *testing.T) {
	quiet := app.MeterSummary{Meter: domain.Meter{MeterID: "M-101"}, Status: domain.MeterOK}
	rec := do(serverWithMeters(&fakeMeters{list: []app.MeterSummary{quiet}}), http.MethodGet, "/api/v1/meters")
	var list []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	m := list[0]
	if m["top_severity"] != nil || m["last_reading_at"] != nil {
		t.Errorf("top_severity %v, last_reading_at %v; want null", m["top_severity"], m["last_reading_at"])
	}
	if d, ok := m["daily"].([]any); !ok || len(d) != 0 {
		t.Errorf("daily = %v, want []", m["daily"])
	}
}

func TestEmptyMeterListIsAnArray(t *testing.T) {
	rec := do(serverWithMeters(&fakeMeters{}), http.MethodGet, "/api/v1/meters")
	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Errorf("body = %q", rec.Body)
	}
}

func TestMeterListParsesItsFilters(t *testing.T) {
	asc, desc := true, false
	tests := []struct {
		query string
		want  app.MeterFilter
	}{
		{"", app.MeterFilter{}},
		{"status=critical", app.MeterFilter{Statuses: []domain.MeterStatus{domain.MeterCritical}}},
		{"status=OK,Alert", app.MeterFilter{Statuses: []domain.MeterStatus{domain.MeterOK, domain.MeterAlert}}},
		{"status=ok,%20alert", app.MeterFilter{Statuses: []domain.MeterStatus{domain.MeterOK, domain.MeterAlert}}},
		{"q=molino", app.MeterFilter{Query: "molino"}},
		{"sort=variation", app.MeterFilter{Sort: "variation"}},
		{"sort=consumption&order=asc", app.MeterFilter{Sort: "consumption", Ascending: &asc}},
		{"sort=severity&order=desc", app.MeterFilter{Sort: "severity", Ascending: &desc}},
	}
	for _, tt := range tests {
		m := &fakeMeters{}
		rec := do(serverWithMeters(m), http.MethodGet, "/api/v1/meters?"+tt.query)
		if rec.Code != http.StatusOK {
			t.Errorf("%q: status %d", tt.query, rec.Code)
			continue
		}
		g := m.gotFilter
		if g.Query != tt.want.Query || g.Sort != tt.want.Sort || len(g.Statuses) != len(tt.want.Statuses) ||
			(g.Ascending == nil) != (tt.want.Ascending == nil) || (g.Ascending != nil && *g.Ascending != *tt.want.Ascending) {
			t.Errorf("%q: filter = %+v, want %+v", tt.query, g, tt.want)
		}
		for i := range g.Statuses {
			if g.Statuses[i] != tt.want.Statuses[i] {
				t.Errorf("%q: statuses = %v, want %v", tt.query, g.Statuses, tt.want.Statuses)
			}
		}
	}
}

func TestMeterListRejectsBadFilters(t *testing.T) {
	for _, q := range []string{
		"status=broken", "status=ok,broken", "status=", "sort=name", "order=sideways", "sort=variation&order=up",
		"q=" + strings.Repeat("a", 101),
	} {
		m := &fakeMeters{}
		rec := do(serverWithMeters(m), http.MethodGet, "/api/v1/meters?"+q)
		// An empty status is the same as no filter, not an error.
		if q == "status=" {
			if rec.Code != http.StatusOK {
				t.Errorf("%q: status %d, want 200", q, rec.Code)
			}
			continue
		}
		if rec.Code != http.StatusBadRequest || rec.Header().Get("Content-Type") != "application/problem+json" || m.listCalls != 0 {
			t.Errorf("%q: status %d, type %q, %d calls", q, rec.Code, rec.Header().Get("Content-Type"), m.listCalls)
		}
	}
}

func TestMeterDetail(t *testing.T) {
	detail := app.MeterDetail{
		MeterSummary: sampleSummary(),
		Voltage:      app.Level{Recent: 216.8, Baseline: 219.8},
		Current:      app.Level{Recent: 424.7, Baseline: 202.1},
		PowerFactor:  app.Level{Recent: 0.74, Baseline: 0.94},
		Anomalies:    []domain.Anomaly{sampleDomainAnomaly()},
	}
	rec := do(serverWithMeters(&fakeMeters{detail: detail}), http.MethodGet, "/api/v1/meters/M-109")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	d := decodeMap(t, rec)
	if d["meter_id"] != "M-109" || d["status"] != "critical" {
		t.Errorf("summary fields missing: %v %v", d["meter_id"], d["status"])
	}
	el, _ := d["electrical"].(map[string]any)
	cur, _ := el["current_a"].(map[string]any)
	pf, _ := el["power_factor"].(map[string]any)
	if cur["recent"] != 424.7 || cur["baseline"] != 202.1 || pf["recent"] != 0.74 || pf["baseline"] != 0.94 {
		t.Errorf("electrical = %v", d["electrical"])
	}
	anomalies, _ := d["anomalies"].([]any)
	if len(anomalies) != 1 || anomalies[0].(map[string]any)["meter_id"] != "M-109" || anomalies[0].(map[string]any)["anomaly"] != "Consumption doubled" {
		t.Errorf("anomalies = %v", d["anomalies"])
	}
}

func TestMeterDetailWithoutABaselineHasNullElectrical(t *testing.T) {
	sum := sampleSummary()
	sum.HasBaseline = false
	rec := do(serverWithMeters(&fakeMeters{detail: app.MeterDetail{MeterSummary: sum}}), http.MethodGet, "/api/v1/meters/M-109")
	d := decodeMap(t, rec)
	if d["electrical"] != nil {
		t.Errorf("electrical = %v, want null", d["electrical"])
	}
	if a, ok := d["anomalies"].([]any); !ok || len(a) != 0 {
		t.Errorf("anomalies = %v, want []", d["anomalies"])
	}
}

func TestReadingsSeries(t *testing.T) {
	at := time.Date(2026, 9, 10, 0, 0, 0, 0, bogotaLoc())
	var profile [24]app.HourBand
	for h := range profile {
		profile[h] = app.HourBand{Median: float64(h) + 1, Sigma: 0.5}
	}
	m := &fakeMeters{series: app.Series{
		Resolution: "hour",
		Points:     []app.Point{{Time: at, ConsumptionKWh: 50, VoltageV: 220, CurrentA: 100, PowerFactor: 0.95, Hours: 1}},
		Baseline: &app.BaselineInfo{
			ReferenceStart: at, ReferenceEnd: at.Add(7 * 24 * time.Hour), BandZ: 3, DailyKWh: 1200,
			Profile: map[domain.Variable][24]app.HourBand{domain.Consumption: profile},
		},
	}}
	rec := do(serverWithMeters(m), http.MethodGet, "/api/v1/meters/M-109/readings?include=baseline")
	if rec.Code != http.StatusOK || m.gotID != "M-109" || !m.gotQuery.IncludeBaseline {
		t.Fatalf("status %d, id %q, query %+v", rec.Code, m.gotID, m.gotQuery)
	}
	d := decodeMap(t, rec)
	if d["meter_id"] != "M-109" || d["resolution"] != "hour" {
		t.Errorf("meter %v resolution %v", d["meter_id"], d["resolution"])
	}
	p := d["points"].([]any)[0].(map[string]any)
	if p["timestamp"] != "2026-09-10T05:00:00Z" || p["consumption_kwh"] != 50.0 || p["voltage_v"] != 220.0 ||
		p["current_a"] != 100.0 || p["power_factor"] != 0.95 || p["hours"] != 1.0 {
		t.Errorf("point = %v", p)
	}
	b := d["baseline"].(map[string]any)
	if b["band_z"] != 3.0 || b["daily_kwh"] != 1200.0 || b["reference_start"] != "2026-09-10T05:00:00Z" || b["reference_end"] != "2026-09-17T05:00:00Z" {
		t.Errorf("baseline = %v", b)
	}
	bands := b["profile"].(map[string]any)["consumption_kwh"].([]any)
	if len(bands) != 24 || bands[5].(map[string]any)["hour"] != 5.0 || bands[5].(map[string]any)["median"] != 6.0 || bands[5].(map[string]any)["sigma"] != 0.5 {
		t.Errorf("profile = %v", bands[5])
	}
}

func TestReadingsWithoutTheBaselineHaveNullBaseline(t *testing.T) {
	rec := do(serverWithMeters(&fakeMeters{series: app.Series{Resolution: "day"}}), http.MethodGet, "/api/v1/meters/M-109/readings")
	d := decodeMap(t, rec)
	if d["baseline"] != nil {
		t.Errorf("baseline = %v, want null", d["baseline"])
	}
	if pts, ok := d["points"].([]any); !ok || len(pts) != 0 {
		t.Errorf("points = %v, want []", d["points"])
	}
}

func TestReadingsParseTheirQuery(t *testing.T) {
	m := &fakeMeters{}
	do(serverWithMeters(m), http.MethodGet, "/api/v1/meters/M-104/readings?from=2026-09-10&to=2026-09-12T00:00:00-05:00&resolution=day")
	wantFrom := time.Date(2026, 9, 10, 0, 0, 0, 0, bogotaLoc())
	wantTo := time.Date(2026, 9, 12, 5, 0, 0, 0, time.UTC)
	if !m.gotQuery.From.Equal(wantFrom) || !m.gotQuery.To.Equal(wantTo) || m.gotQuery.Resolution != "day" || m.gotQuery.IncludeBaseline {
		t.Errorf("query = %+v, want from %v to %v by day", m.gotQuery, wantFrom, wantTo)
	}
	if m.gotQuery.From.UTC().Hour() != 5 {
		t.Errorf("a bare date should be midnight in the plant zone (05:00 UTC), got %v", m.gotQuery.From.UTC())
	}
	empty := &fakeMeters{}
	do(serverWithMeters(empty), http.MethodGet, "/api/v1/meters/M-104/readings")
	if !empty.gotQuery.From.IsZero() || !empty.gotQuery.To.IsZero() || empty.gotQuery.Resolution != "" {
		t.Errorf("an empty query = %+v", empty.gotQuery)
	}
}

func TestReadingsRejectBadParameters(t *testing.T) {
	for _, q := range []string{
		"from=yesterday", "to=2026-13-40", "from=2026-09-10T10:00", "resolution=week", "include=everything", "include=baseline,extra",
	} {
		m := &fakeMeters{}
		rec := do(serverWithMeters(m), http.MethodGet, "/api/v1/meters/M-104/readings?"+q)
		if rec.Code != http.StatusBadRequest || m.otherCalls != 0 {
			t.Errorf("%q: status %d, %d calls", q, rec.Code, m.otherCalls)
		}
	}
	for name, err := range map[string]error{"bad range": app.ErrBadRange, "bad resolution": app.ErrBadResolution} {
		rec := do(serverWithMeters(&fakeMeters{err: err}), http.MethodGet, "/api/v1/meters/M-104/readings")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s from the service: status %d, want 400", name, rec.Code)
		}
	}
}

func TestEventsOfAMeter(t *testing.T) {
	m := &fakeMeters{events: []domain.Event{
		{Type: domain.EventScheduledOutage, Timestamp: time.Date(2026, 9, 8, 0, 0, 0, 0, bogotaLoc()), Description: "Scheduled maintenance outage for 12 hours", Duration: 12 * time.Hour},
		{Type: domain.EventUnknown, Timestamp: time.Date(2026, 9, 12, 14, 0, 0, 0, bogotaLoc()), Description: "No operational event reported"},
	}}
	rec := do(serverWithMeters(m), http.MethodGet, "/api/v1/meters/M-106/events")
	var list []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list) != 2 {
		t.Fatalf("body = %s (%v)", rec.Body, err)
	}
	if list[0]["type"] != "SCHEDULED_OUTAGE" || list[0]["timestamp"] != "2026-09-08T05:00:00Z" || list[0]["duration_hours"] != 12.0 ||
		list[0]["description"] != "Scheduled maintenance outage for 12 hours" {
		t.Errorf("first event = %v", list[0])
	}
	if list[1]["duration_hours"] != nil {
		t.Errorf("an event with no duration = %v, want null", list[1]["duration_hours"])
	}
	if m.gotID != "M-106" {
		t.Errorf("looked up %q", m.gotID)
	}
	none := do(serverWithMeters(&fakeMeters{}), http.MethodGet, "/api/v1/meters/M-101/events")
	if strings.TrimSpace(none.Body.String()) != "[]" {
		t.Errorf("no events = %q, want []", none.Body)
	}
}

func TestMeterErrors(t *testing.T) {
	paths := []string{"/api/v1/meters/M-109", "/api/v1/meters/M-109/readings", "/api/v1/meters/M-109/events"}
	for _, p := range paths {
		notFound := do(serverWithMeters(&fakeMeters{err: domain.ErrNotFound}), http.MethodGet, p)
		if notFound.Code != http.StatusNotFound || notFound.Header().Get("Content-Type") != "application/problem+json" {
			t.Errorf("%s unknown meter: status %d", p, notFound.Code)
		}
		failed := do(serverWithMeters(&fakeMeters{err: errors.New("pq: table meters is broken")}), http.MethodGet, p)
		if failed.Code != http.StatusInternalServerError || strings.Contains(failed.Body.String(), "broken") {
			t.Errorf("%s store failure: status %d, body %s", p, failed.Code, failed.Body)
		}
	}
	list := do(serverWithMeters(&fakeMeters{err: errors.New("pq: down")}), http.MethodGet, "/api/v1/meters")
	if list.Code != http.StatusInternalServerError || strings.Contains(list.Body.String(), "down") {
		t.Errorf("list failure: status %d, body %s", list.Code, list.Body)
	}
}

func TestMeterCodesThatCannotExistAreNotFoundWithoutAsking(t *testing.T) {
	for _, code := range []string{"M%20109", "M.109", strings.Repeat("M", 33), "%27%20OR%201=1", "M-109%2F..%2Fx"} {
		for _, suffix := range []string{"", "/readings", "/events"} {
			m := &fakeMeters{}
			rec := do(serverWithMeters(m), http.MethodGet, "/api/v1/meters/"+code+suffix)
			if rec.Code != http.StatusNotFound || m.otherCalls != 0 {
				t.Errorf("%s%s: status %d, %d calls", code, suffix, rec.Code, m.otherCalls)
			}
		}
	}
}

func TestMeterRoutesNeedASession(t *testing.T) {
	m := &fakeMeters{}
	h := serverWithMeters(m)
	for _, path := range []string{"/api/v1/meters", "/api/v1/meters/M-109", "/api/v1/meters/M-109/readings", "/api/v1/meters/M-109/events"} {
		if rec := doAnon(h, http.MethodGet, path); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s without a session: %d", path, rec.Code)
		}
	}
	if m.listCalls+m.otherCalls != 0 {
		t.Error("a handler ran without a session")
	}
}
