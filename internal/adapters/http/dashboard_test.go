package httpapi

import (
	"context"
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

type fakeDashboard struct {
	summary app.Dashboard
	err     error
	calls   int
}

func (f *fakeDashboard) Summary(context.Context) (app.Dashboard, error) {
	f.calls++
	return f.summary, f.err
}

func serverWithDashboard(d *fakeDashboard) http.Handler {
	return NewRouter(Deps{
		Auth: newFakeAuth(), Analysis: &fakeStarter{}, Runs: &fakeRuns{}, Anomalies: &fakeStore{},
		Meters: &fakeMeters{}, Dashboard: d, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

func TestDashboardBeforeAnyAnalysis(t *testing.T) {
	empty := app.Dashboard{Meters: app.MeterCounts{Total: 12, OK: 12}, PeriodFrom: "2026-09-01", PeriodTo: "2026-09-14", ConsumptionKWh: 172000.5}
	rec := do(serverWithDashboard(&fakeDashboard{summary: empty}), http.MethodGet, "/api/v1/dashboard/summary")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	d := decodeMap(t, rec)
	if d["has_analysis"] != false || d["last_analysis"] != nil {
		t.Errorf("has_analysis %v, last_analysis %v", d["has_analysis"], d["last_analysis"])
	}
	k := d["kpis"].(map[string]any)
	if k["confidence"] != nil || k["period_consumption_kwh"] != 172000.5 || k["unresolved_anomalies"] != 0.0 || k["pending_high_priority"] != 0.0 {
		t.Errorf("kpis = %v", k)
	}
	if m := k["meters"].(map[string]any); m["total"] != 12.0 || m["ok"] != 12.0 || m["alert"] != 0.0 || m["critical"] != 0.0 {
		t.Errorf("meters = %v", m)
	}
	if a, ok := d["attention"].([]any); !ok || len(a) != 0 {
		t.Errorf("attention = %v, want []", d["attention"])
	}
	p := d["period"].(map[string]any)
	if p["from"] != "2026-09-01" || p["to"] != "2026-09-14" {
		t.Errorf("period = %v", p)
	}
	if daily, ok := p["daily"].([]any); !ok || len(daily) != 0 {
		t.Errorf("daily = %v, want []", p["daily"])
	}
}

func TestDashboardAfterAnAnalysis(t *testing.T) {
	done := time.Date(2026, 9, 24, 15, 0, 5, 0, bogotaLoc())
	confidence := 0.989
	run := domain.AnalysisRun{
		ID: validID, Status: domain.RunCompleted, StartedAt: done.Add(-5 * time.Second), FinishedAt: &done,
		Stages:  []domain.StageState{{Name: "READINGS", Status: domain.StageDone}},
		Summary: &domain.RunSummary{Anomalies: 4, Confidence: confidence},
	}
	summary := app.Dashboard{
		HasAnalysis: true, LastAnalysis: &run, Meters: app.MeterCounts{Total: 12, OK: 9, Alert: 2, Critical: 1},
		PeriodFrom: "2026-09-01", PeriodTo: "2026-09-14", ConsumptionKWh: 172000, VariationPct: 6.4,
		Daily:     []app.DailyKWh{{Date: "2026-09-13", KWh: 12000}, {Date: "2026-09-14", KWh: 12500}},
		Anomalies: app.AnomalyCounts{Unresolved: 4, PendingHighPriority: 2}, Confidence: &confidence,
		Attention: []domain.Anomaly{sampleDomainAnomaly()},
	}
	rec := do(serverWithDashboard(&fakeDashboard{summary: summary}), http.MethodGet, "/api/v1/dashboard/summary")
	d := decodeMap(t, rec)

	if d["has_analysis"] != true {
		t.Errorf("has_analysis = %v", d["has_analysis"])
	}
	last := d["last_analysis"].(map[string]any)
	if last["id"] != validID || last["status"] != "COMPLETED" || last["finished_at"] != "2026-09-24T20:00:05Z" || last["progress"] != 1.0 {
		t.Errorf("last_analysis = %v", last)
	}
	k := d["kpis"].(map[string]any)
	want := map[string]any{"confidence": 0.989, "variation_pct": 6.4, "unresolved_anomalies": 4.0, "pending_high_priority": 2.0}
	for key, v := range want {
		if k[key] != v {
			t.Errorf("%s = %v, want %v", key, k[key], v)
		}
	}
	if m := k["meters"].(map[string]any); m["critical"] != 1.0 || m["alert"] != 2.0 || m["ok"] != 9.0 {
		t.Errorf("meters = %v", m)
	}
	daily := d["period"].(map[string]any)["daily"].([]any)
	if len(daily) != 2 || daily[1].(map[string]any)["date"] != "2026-09-14" || daily[1].(map[string]any)["kwh"] != 12500.0 {
		t.Errorf("daily = %v", daily)
	}
	attention := d["attention"].([]any)
	if len(attention) != 1 || attention[0].(map[string]any)["meter_id"] != "M-109" || attention[0].(map[string]any)["recommended_next_action"] != "CREATE_INSPECTION_ORDER" {
		t.Errorf("attention = %v", attention)
	}
}

func TestDashboardFailureIsA500WithoutTheCause(t *testing.T) {
	rec := do(serverWithDashboard(&fakeDashboard{err: errors.New("pq: connection refused at 10.0.0.5")}), http.MethodGet, "/api/v1/dashboard/summary")
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "10.0.0.5") ||
		rec.Header().Get("Content-Type") != "application/problem+json" {
		t.Errorf("status %d, type %q, body %s", rec.Code, rec.Header().Get("Content-Type"), rec.Body)
	}
}

func TestDashboardNeedsASession(t *testing.T) {
	d := &fakeDashboard{}
	if rec := doAnon(serverWithDashboard(d), http.MethodGet, "/api/v1/dashboard/summary"); rec.Code != http.StatusUnauthorized || d.calls != 0 {
		t.Errorf("status %d, %d calls", rec.Code, d.calls)
	}
}
