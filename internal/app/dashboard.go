package app

import (
	"context"
	"errors"
	"sort"

	"github.com/m0ntbl4ck/voltia/internal/domain"
	"github.com/m0ntbl4ck/voltia/internal/ports"
)

// attentionCount is how many anomalies the "needs attention" card shows.
const attentionCount = 3

// MeterCounts is how many meters are in each derived status.
type MeterCounts struct {
	Total, OK, Alert, Critical int
}

// AnomalyCounts separates what needs an operator from what is already handled.
type AnomalyCounts struct {
	// Unresolved are the open and the acknowledged ones.
	Unresolved int
	// PendingHighPriority are the open ones of high severity: nobody has acted on them yet.
	PendingHighPriority int
}

// Dashboard is the home screen: the state of the plant at a glance.
type Dashboard struct {
	// HasAnalysis is false until an analysis has completed, when the screen invites to run one.
	HasAnalysis  bool
	LastAnalysis *domain.AnalysisRun
	Meters       MeterCounts
	// PeriodFrom and PeriodTo are the first and last complete day with readings.
	PeriodFrom, PeriodTo string
	ConsumptionKWh       float64
	// Daily is the consumption of the whole plant, day by day.
	Daily []DailyKWh
	// VariationPct compares the recent daily consumption of the plant with its baseline.
	VariationPct float64
	Anomalies    AnomalyCounts
	// Confidence is the aggregate of the last analysis, or nil without one.
	Confidence *float64
	// Attention are the open anomalies to look at first.
	Attention []domain.Anomaly
}

type meterLister interface {
	Meters(ctx context.Context, f MeterFilter) ([]MeterSummary, error)
}

// DashboardService builds the home screen from the meters, the anomalies and the last analysis.
type DashboardService struct {
	meters    meterLister
	anomalies ports.AnomalyReader
	runs      ports.Runs
}

func NewDashboardService(meters meterLister, anomalies ports.AnomalyReader, runs ports.Runs) *DashboardService {
	return &DashboardService{meters: meters, anomalies: anomalies, runs: runs}
}

// Summary builds the dashboard as it is now.
func (s *DashboardService) Summary(ctx context.Context) (Dashboard, error) {
	meters, err := s.meters.Meters(ctx, MeterFilter{})
	if err != nil {
		return Dashboard{}, err
	}
	anomalies, err := s.anomalies.Anomalies(ctx, domain.AnomalyFilter{})
	if err != nil {
		return Dashboard{}, err
	}
	var d Dashboard
	d.Meters = countMeters(meters)
	d.Daily = plantDaily(meters)
	for _, day := range d.Daily {
		d.ConsumptionKWh += day.KWh
	}
	if n := len(d.Daily); n > 0 {
		d.PeriodFrom, d.PeriodTo = d.Daily[0].Date, d.Daily[n-1].Date
	}
	d.VariationPct = plantVariation(meters)

	for _, a := range anomalies {
		if a.Unresolved() {
			d.Anomalies.Unresolved++
		}
		if a.Status != domain.StatusOpen {
			continue
		}
		if a.Severity == domain.SeverityHigh {
			d.Anomalies.PendingHighPriority++
		}
		if len(d.Attention) < attentionCount {
			d.Attention = append(d.Attention, a)
		}
	}

	last, err := s.runs.LatestCompletedRun(ctx)
	switch {
	case errors.Is(err, domain.ErrNotFound):
	case err != nil:
		return Dashboard{}, err
	default:
		d.HasAnalysis, d.LastAnalysis = true, &last
		if last.Summary != nil {
			c := last.Summary.Confidence
			d.Confidence = &c
		}
	}
	return d, nil
}

func countMeters(meters []MeterSummary) MeterCounts {
	c := MeterCounts{Total: len(meters)}
	for _, m := range meters {
		switch m.Status {
		case domain.MeterCritical:
			c.Critical++
		case domain.MeterAlert:
			c.Alert++
		default:
			c.OK++
		}
	}
	return c
}

// plantDaily adds up the daily consumption of every meter, oldest day first.
func plantDaily(meters []MeterSummary) []DailyKWh {
	byDate := map[string]float64{}
	for _, m := range meters {
		for _, d := range m.Daily {
			byDate[d.Date] += d.KWh
		}
	}
	out := make([]DailyKWh, 0, len(byDate))
	for date, kwh := range byDate {
		out = append(out, DailyKWh{Date: date, KWh: kwh})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out
}

// plantVariation compares the recent and baseline daily totals. A meter with
// no baseline reports zero for both, so it adds nothing to either.
func plantVariation(meters []MeterSummary) float64 {
	var recent, base float64
	for _, m := range meters {
		recent += m.RecentDailyKWh
		base += m.BaselineDailyKWh
	}
	if base == 0 {
		return 0
	}
	return (recent - base) / base * 100
}
