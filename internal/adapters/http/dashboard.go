package httpapi

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/m0ntbl4ck/voltia/internal/app"
)

// DashboardAPI builds the home screen.
type DashboardAPI interface {
	Summary(ctx context.Context) (app.Dashboard, error)
}

type dashboardResource struct {
	api DashboardAPI
	log *slog.Logger
}

func (d dashboardResource) routes(r chi.Router) {
	r.Get("/dashboard/summary", d.summary)
}

type meterCountsResponse struct {
	Total    int `json:"total"`
	OK       int `json:"ok"`
	Alert    int `json:"alert"`
	Critical int `json:"critical"`
}

type kpisResponse struct {
	Meters               meterCountsResponse `json:"meters"`
	PeriodConsumptionKWh float64             `json:"period_consumption_kwh"`
	VariationPct         float64             `json:"variation_pct"`
	UnresolvedAnomalies  int                 `json:"unresolved_anomalies"`
	PendingHighPriority  int                 `json:"pending_high_priority"`
	// Confidence is null until an analysis has completed.
	Confidence *float64 `json:"confidence"`
}

type periodResponse struct {
	From  string          `json:"from"`
	To    string          `json:"to"`
	Daily []dailyResponse `json:"daily"`
}

type dashboardResponse struct {
	HasAnalysis  bool              `json:"has_analysis"`
	LastAnalysis *runResponse      `json:"last_analysis"`
	KPIs         kpisResponse      `json:"kpis"`
	Period       periodResponse    `json:"period"`
	Attention    []anomalyResponse `json:"attention"`
}

func (d dashboardResource) summary(w http.ResponseWriter, r *http.Request) {
	s, err := d.api.Summary(r.Context())
	if err != nil {
		serverError(w, d.log, "build dashboard", err)
		return
	}
	resp := dashboardResponse{
		HasAnalysis: s.HasAnalysis,
		KPIs: kpisResponse{
			Meters:               meterCountsResponse(s.Meters),
			PeriodConsumptionKWh: s.ConsumptionKWh,
			VariationPct:         s.VariationPct,
			UnresolvedAnomalies:  s.Anomalies.Unresolved,
			PendingHighPriority:  s.Anomalies.PendingHighPriority,
			Confidence:           s.Confidence,
		},
		Period:    periodResponse{From: s.PeriodFrom, To: s.PeriodTo, Daily: make([]dailyResponse, len(s.Daily))},
		Attention: make([]anomalyResponse, len(s.Attention)),
	}
	if s.LastAnalysis != nil {
		run := toRunResponse(*s.LastAnalysis)
		resp.LastAnalysis = &run
	}
	for i, day := range s.Daily {
		resp.Period.Daily[i] = dailyResponse{Date: day.Date, KWh: day.KWh}
	}
	for i, a := range s.Attention {
		resp.Attention[i] = toAnomalyResponse(a)
	}
	writeJSON(w, http.StatusOK, resp)
}
