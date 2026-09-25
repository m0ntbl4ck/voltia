package scoring

import (
	"math"

	"github.com/m0ntbl4ck/voltia/internal/analysis/classify"
	"github.com/m0ntbl4ck/voltia/internal/analysis/detectors"
	"github.com/m0ntbl4ck/voltia/internal/domain"
)

// ConfidenceConfig tunes the confidence index. It measures how strong the
// evidence is, not the odds of being right: there are no labelled cases to
// calibrate against.
type ConfidenceConfig struct {
	WeightAgreement float64
	WeightStrength  float64
	WeightClarity   float64
	WeightIntegrity float64
	// Floor and Ceiling keep the index away from 0 and 1: some doubt always remains.
	Floor   float64
	Ceiling float64
	// StrengthFullZ and StrengthFullHours are the deviation and persistence that
	// earn full marks on signal strength.
	StrengthFullZ     float64
	StrengthFullHours int
	// EventWindowHours is the reach of an event, used to score how well it lines up.
	EventWindowHours float64
	// Band thresholds, from the highest.
	HighBand       float64
	MediumHighBand float64
	MediumBand     float64
	// SeverityWeight weighs each anomaly in the aggregate confidence.
	SeverityWeight map[domain.Severity]float64
}

func DefaultConfidenceConfig() ConfidenceConfig {
	return ConfidenceConfig{
		WeightAgreement: 0.35, WeightStrength: 0.30, WeightClarity: 0.25, WeightIntegrity: 0.10,
		Floor: 0.50, Ceiling: 0.99,
		StrengthFullZ: 8, StrengthFullHours: 12,
		EventWindowHours: 6,
		HighBand:         0.85, MediumHighBand: 0.75, MediumBand: 0.60,
		SeverityWeight: map[domain.Severity]float64{domain.SeverityHigh: 3, domain.SeverityMedium: 2, domain.SeverityLow: 1},
	}
}

// ConfidenceBreakdown holds each component, from 0 to 1.
type ConfidenceBreakdown struct {
	DetectorAgreement     float64 `json:"detector_agreement"`
	SignalStrength        float64 `json:"signal_strength"`
	ClassificationClarity float64 `json:"classification_clarity"`
	DataIntegrity         float64 `json:"data_integrity"`
	// IntegrityApplies is false for data quality episodes, whose bad readings are the evidence itself.
	IntegrityApplies bool `json:"integrity_applies"`
}

func confidence(res classify.Result, cfg ConfidenceConfig) (float64, ConfidenceBreakdown) {
	b := ConfidenceBreakdown{
		DetectorAgreement:     agreement(res.Episode),
		SignalStrength:        strength(res.Episode, cfg),
		ClassificationClarity: clarity(res, cfg),
		IntegrityApplies:      res.Type != domain.DataQuality,
	}
	weights := cfg.WeightAgreement + cfg.WeightStrength + cfg.WeightClarity
	sum := cfg.WeightAgreement*b.DetectorAgreement + cfg.WeightStrength*b.SignalStrength + cfg.WeightClarity*b.ClassificationClarity
	if b.IntegrityApplies {
		b.DataIntegrity = integrity(res.Episode)
		weights += cfg.WeightIntegrity
		sum += cfg.WeightIntegrity * b.DataIntegrity
	}
	return math.Min(cfg.Ceiling, math.Max(cfg.Floor, sum/weights)), b
}

// agreement counts independent sources of evidence: each kind of signal, the
// isolation forest included, plus one when a persistent shift shows up in more
// than one variable.
func agreement(ep classify.Episode) float64 {
	kinds := map[detectors.Kind]bool{}
	shifted := map[domain.Variable]bool{}
	for _, s := range ep.Signals {
		kinds[s.Kind] = true
		if s.Kind == detectors.KindPersistentShift {
			shifted[s.Variable] = true
		}
	}
	sources := len(kinds)
	if len(shifted) >= 2 {
		sources++
	}
	return math.Min(1, 0.5+0.25*float64(sources-1))
}

// strength averages how far the evidence sits from normal and how long it lasted.
func strength(ep classify.Episode, cfg ConfidenceConfig) float64 {
	var maxZ float64
	hours := 0
	for _, s := range ep.Signals {
		if s.Kind == detectors.KindIsolationForest {
			continue // its score is not a z and its hours lie inside the others'
		}
		maxZ = math.Max(maxZ, math.Abs(s.MeanZ))
		if s.Kind == detectors.KindDataQuality {
			hours += s.Hours
		} else if s.Hours > hours {
			hours = s.Hours
		}
	}
	magnitude := math.Min(1, maxZ/cfg.StrengthFullZ)
	persistence := math.Min(1, float64(hours)/float64(cfg.StrengthFullHours))
	return (magnitude + persistence) / 2
}

// clarity scores how clean the decision of the type was.
func clarity(res classify.Result, cfg ConfidenceConfig) float64 {
	explaining := func() (classify.EventLink, bool) {
		for _, l := range res.Events {
			if l.Role == classify.RoleExplains {
				return l, true
			}
		}
		return classify.EventLink{}, false
	}
	alignment := func(l classify.EventLink) float64 {
		return math.Max(0, 1-math.Abs(l.OffsetHours)/cfg.EventWindowHours)
	}

	switch res.Type {
	case domain.DataQuality:
		for _, l := range res.Events {
			if l.Role == classify.RoleCorroborates {
				return 1
			}
		}
		return 0.75
	case domain.FalsePositive:
		l, ok := explaining()
		if !ok {
			return 0.5
		}
		durationFit := 0.7 // an outage that states no length only half proves the match
		if l.Event.Duration > 0 {
			stated := l.Event.Duration.Hours()
			durationFit = math.Max(0, 1-math.Abs(res.Episode.Duration().Hours()-stated)/stated)
		}
		return 0.5*alignment(l) + 0.5*durationFit
	case domain.ExplainableAnomaly:
		l, ok := explaining()
		if !ok {
			return 0.5
		}
		return 0.6*alignment(l) + 0.4 // the electrical readings stayed steady, or it would not be explainable
	}
	switch {
	case res.Rule == classify.RuleElectricalEvidence:
		return 0.8
	case len(res.Events) == 0:
		return 1
	}
	for _, l := range res.Events {
		if l.Role == classify.RoleIncompatible {
			return 0.9
		}
	}
	return 0.95
}

// integrity is the share of the episode's hours not flagged as invalid readings.
func integrity(ep classify.Episode) float64 {
	invalid := 0
	for _, s := range ep.Signals {
		if s.Kind == detectors.KindDataQuality {
			invalid += s.Hours
		}
	}
	return math.Max(0, 1-float64(invalid)/ep.Duration().Hours())
}
