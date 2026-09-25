package app

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/m0ntbl4ck/voltia/internal/analysis"
	"github.com/m0ntbl4ck/voltia/internal/analysis/detectors"
	"github.com/m0ntbl4ck/voltia/internal/domain"
)

// Fingerprint identifies an anomaly across runs: the same meter, type and
// episode start is the same anomaly, so a re-run updates it instead of
// adding a copy and losing its status and history.
func Fingerprint(meterID string, typ domain.AnomalyType, episodeStart time.Time) string {
	return fmt.Sprintf("%s|%s|%s", meterID, typ, episodeStart.UTC().Format(time.RFC3339))
}

// BuildEvidence gathers what the engine found about one anomaly.
func BuildEvidence(a analysis.Anomaly, meter domain.Meter) domain.Evidence {
	ep := a.Episode
	ev := domain.Evidence{
		MeterID:      ep.MeterID,
		MeterName:    meter.Name,
		Location:     meter.Location,
		Type:         a.Type,
		Severity:     a.Score.Severity,
		Rule:         string(a.Rule),
		Confidence:   a.Score.Confidence,
		EpisodeStart: ep.Start.UTC(),
		EpisodeEnd:   ep.End.UTC(),
		DurationH:    int(ep.Duration().Hours()),
		Ongoing:      ep.Open,
		Direction:    directionName(a.Direction),
		VariationPct: a.Score.VariationPct,
		ExcessKWh:    a.Score.ExcessKWh,
		Signals:      make([]domain.SignalEvidence, 0, len(ep.Signals)),
		Events:       make([]domain.EventEvidence, 0, len(a.Events)),
	}
	for _, s := range ep.Signals {
		if s.Kind == detectors.KindDataQuality {
			ev.InvalidReadings += s.Hours
		}
		ev.Signals = append(ev.Signals, domain.SignalEvidence{
			Kind:        string(s.Kind),
			Check:       s.Check,
			Variable:    s.Variable,
			Start:       s.Start.UTC(),
			End:         s.End.UTC(),
			Hours:       s.Hours,
			Direction:   s.Direction,
			Observed:    s.Observed,
			Expected:    s.Expected,
			MeanZ:       s.MeanZ,
			Metrics:     s.Metrics,
			Attribution: s.Attribution,
		})
	}
	for _, l := range a.Events {
		ev.Events = append(ev.Events, domain.EventEvidence{
			Type:        l.Event.Type,
			Timestamp:   l.Event.Timestamp.UTC(),
			Description: l.Event.Description,
			DurationH:   l.Event.Duration.Hours(),
			Role:        string(l.Role),
			OffsetHours: l.OffsetHours,
		})
	}
	return ev
}

// BuildAnomaly turns an engine result and its explanation into the record
// that gets stored. The status starts OPEN; storing it again keeps whatever
// status the operators have set since.
func BuildAnomaly(a analysis.Anomaly, meter domain.Meter, exp domain.Explanation, runID string) (domain.Anomaly, error) {
	confidence, err := json.Marshal(a.Score.ConfidenceParts)
	if err != nil {
		return domain.Anomaly{}, err
	}
	priority, err := json.Marshal(a.Score.PriorityBreakdown)
	if err != nil {
		return domain.Anomaly{}, err
	}
	return domain.Anomaly{
		MeterID:             a.Episode.MeterID,
		Fingerprint:         Fingerprint(a.Episode.MeterID, a.Type, a.Episode.Start),
		Type:                a.Type,
		Severity:            a.Score.Severity,
		Confidence:          a.Score.Confidence,
		ConfidenceBreakdown: confidence,
		Priority:            a.Score.Priority,
		PriorityBreakdown:   priority,
		EpisodeStart:        a.Episode.Start.UTC(),
		EpisodeEnd:          a.Episode.End.UTC(),
		Ongoing:             a.Episode.Open,
		Evidence:            BuildEvidence(a, meter),
		Explanation:         exp,
		Status:              domain.StatusOpen,
		LastAnalysisID:      runID,
	}, nil
}

func directionName(d int) string {
	switch {
	case d > 0:
		return "UP"
	case d < 0:
		return "DOWN"
	}
	return "NONE"
}
