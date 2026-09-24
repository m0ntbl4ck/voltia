package iforest

import (
	"errors"
	"fmt"
	"math"
	"slices"

	"github.com/m0ntbl4ck/voltia/internal/analysis/baseline"
	"github.com/m0ntbl4ck/voltia/internal/analysis/classify"
	"github.com/m0ntbl4ck/voltia/internal/analysis/detectors"
	"github.com/m0ntbl4ck/voltia/internal/domain"
)

// Features are the variables the forest looks at, in the order of a row: the
// four robust z-scores and the physical ratio.
var Features = []domain.Variable{
	domain.Consumption, domain.Voltage, domain.Current, domain.PowerFactor, domain.PhysicalRatio,
}

// minRatio keeps the logarithm finite for a reading that reports no consumption.
const minRatio = 0.01

// ModelConfig adds to the forest settings what counts as unusual.
type ModelConfig struct {
	Forest Config
	// Threshold is the score from which a reading is unusual. Ordinary
	// readings score around 0.5 or below, though a few of them brush 0.6 or a
	// bit more, so one unusual hour proves nothing.
	Threshold float64
	// MinShare is the fraction of an episode's readings that must be unusual
	// for the forest to back the episode up. Ordinary hours pass the threshold
	// about 5% of the time in the healthy meters of the dataset, so 0.3 keeps
	// noise out with room to spare on a thin episode.
	MinShare float64
}

func DefaultModelConfig() ModelConfig {
	return ModelConfig{Forest: DefaultConfig(), Threshold: 0.6, MinShare: 0.3}
}

// Model is a forest that learned how one meter looks when nothing is wrong.
type Model struct {
	meter  baseline.Meter
	forest *Forest
	cfg    ModelConfig
}

// Train fits a forest on the reference readings of one meter, described
// against that meter's own baseline.
func Train(m baseline.Meter, reference []domain.Reading, cfg ModelConfig) (Model, error) {
	if len(reference) < 2 {
		return Model{}, errors.New("need at least 2 reference readings")
	}
	rows := make([][]float64, len(reference))
	for i, r := range reference {
		rows[i] = features(m, r)
	}
	forest, err := Fit(rows, cfg.Forest)
	if err != nil {
		return Model{}, fmt.Errorf("meter %s: %w", m.MeterID, err)
	}
	return Model{meter: m, forest: forest, cfg: cfg}, nil
}

// features is one row: how far each variable sits from the meter's baseline,
// and the log of the physical ratio.
func features(m baseline.Meter, r domain.Reading) []float64 {
	row := make([]float64, len(Features))
	for i, v := range Features {
		if v == domain.PhysicalRatio {
			row[i] = math.Log(math.Max(r.Value(v), minRatio))
			continue
		}
		row[i] = m.Z(r, v)
	}
	return row
}

// Score is the isolation score of one reading.
func (m Model) Score(r domain.Reading) float64 {
	return m.forest.Score(features(m.meter, r))
}

// Corroborate scores the readings that fall inside the episode. When at least
// MinShare of them reach the threshold it returns the signal that backs the
// episode up: it spans the first to the last unusual hour, Hours counts them,
// Observed is the highest score, Expected the threshold, and Attribution and
// Variable say which variables isolated them. It never creates an episode of
// its own.
func (m Model) Corroborate(ep classify.Episode, readings []domain.Reading) (detectors.Signal, bool) {
	var flagged []domain.Reading
	var top float64
	scored := 0
	for _, r := range readings {
		if r.Timestamp.Before(ep.Start) || r.Timestamp.After(ep.End) {
			continue
		}
		scored++
		if s := m.Score(r); s >= m.cfg.Threshold {
			flagged = append(flagged, r)
			top = math.Max(top, s)
		}
	}
	if len(flagged) == 0 || float64(len(flagged)) < m.cfg.MinShare*float64(scored) {
		return detectors.Signal{}, false
	}
	slices.SortFunc(flagged, func(a, b domain.Reading) int { return a.Timestamp.Compare(b.Timestamp) })

	shares := make([]float64, len(Features))
	for _, r := range flagged {
		for i, s := range m.forest.Attribute(features(m.meter, r)) {
			shares[i] += s / float64(len(flagged))
		}
	}
	attribution := make(map[domain.Variable]float64, len(Features))
	best := 0
	for i, v := range Features {
		attribution[v] = shares[i]
		if shares[i] > shares[best] {
			best = i
		}
	}

	last := flagged[len(flagged)-1].Timestamp
	return detectors.Signal{
		Kind:        detectors.KindIsolationForest,
		MeterID:     ep.MeterID,
		Variable:    Features[best],
		Start:       flagged[0].Timestamp,
		End:         last,
		Hours:       len(flagged),
		Observed:    top,
		Expected:    m.cfg.Threshold,
		Attribution: attribution,
		Open:        ep.Open && last.Equal(ep.End),
	}, true
}
