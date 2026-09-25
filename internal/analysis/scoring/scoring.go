package scoring

import (
	"github.com/m0ntbl4ck/voltia/internal/analysis/classify"
	"github.com/m0ntbl4ck/voltia/internal/domain"
)

type Band string

const (
	BandHigh       Band = "HIGH"
	BandMediumHigh Band = "MEDIUM_HIGH"
	BandMedium     Band = "MEDIUM"
	BandLow        Band = "LOW"
)

// Score is everything the scoring decides for one classified episode.
type Score struct {
	Severity domain.Severity
	// Priority orders what to investigate first, from 0 to 100.
	Priority          int
	PriorityBreakdown PriorityBreakdown
	Confidence        float64
	ConfidenceBand    Band
	ConfidenceParts   ConfidenceBreakdown
	// VariationPct and ExcessKWh are the evidence behind severity and impact.
	VariationPct float64
	ExcessKWh    float64
}

// Evaluate scores one classified episode.
func Evaluate(res classify.Result, cfg Config) Score {
	ev := gather(res.Episode)
	sev := severity(res, ev, cfg)
	pb := priority(res, sev, ev, cfg)
	conf, parts := confidence(res, cfg.Confidence)
	return Score{
		Severity:          sev,
		Priority:          pb.Total(),
		PriorityBreakdown: pb,
		Confidence:        conf,
		ConfidenceBand:    band(conf, cfg.Confidence),
		ConfidenceParts:   parts,
		VariationPct:      ev.variationPct,
		ExcessKWh:         ev.excessKWh,
	}
}

func band(c float64, cfg ConfidenceConfig) Band {
	switch {
	case c >= cfg.HighBand:
		return BandHigh
	case c >= cfg.MediumHighBand:
		return BandMediumHigh
	case c >= cfg.MediumBand:
		return BandMedium
	}
	return BandLow
}

// AggregateConfidence averages the confidence of several scores, weighing each
// by its severity so that being sure about the serious ones counts more.
func AggregateConfidence(scores []Score, cfg ConfidenceConfig) float64 {
	var sum, weights float64
	for _, s := range scores {
		w := cfg.SeverityWeight[s.Severity]
		sum += w * s.Confidence
		weights += w
	}
	if weights == 0 {
		return 0
	}
	return sum / weights
}
