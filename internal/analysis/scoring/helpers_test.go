package scoring

import (
	"math"
	"testing"
	"time"

	"github.com/m0ntbl4ck/voltia/internal/analysis/classify"
	"github.com/m0ntbl4ck/voltia/internal/analysis/detectors"
	"github.com/m0ntbl4ck/voltia/internal/domain"
)

var t0 = time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)

// shift builds a persistent shift of one variable: the mean observed and
// expected values, the mean z-score and its length in hours.
func shift(v domain.Variable, hours, dir int, observed, expected, z float64) detectors.Signal {
	return detectors.Signal{
		Kind: detectors.KindPersistentShift, Variable: v, Start: t0, End: t0.Add(time.Duration(hours-1) * time.Hour),
		Hours: hours, Direction: dir, Observed: observed, Expected: expected, MeanZ: z,
	}
}

// consumption builds a consumption shift whose variation is the given percent.
func consumption(hours, dir int, variationPct float64) detectors.Signal {
	return shift(domain.Consumption, hours, dir, 100*(1+float64(dir)*variationPct/100), 100, 10*float64(dir))
}

func powerFactorDrop(hours int) detectors.Signal {
	s := shift(domain.PowerFactor, hours, -1, 0.7, 0.94, -10)
	s.Kind = detectors.KindElectricalRelation
	return s
}

func invalid(readings int) detectors.Signal {
	return detectors.Signal{Kind: detectors.KindDataQuality, Check: detectors.CheckElectricalJump, Variable: domain.Voltage, Start: t0, End: t0.Add(time.Duration(readings-1) * time.Hour), Hours: readings, MeanZ: 12}
}

// isolation builds the isolation forest signal that backs up an episode.
func isolation(hours int, score float64) detectors.Signal {
	return detectors.Signal{
		Kind: detectors.KindIsolationForest, Variable: domain.Consumption, Start: t0, End: t0.Add(time.Duration(hours-1) * time.Hour),
		Hours: hours, Observed: score, Expected: 0.6,
	}
}

// result builds a classified episode of the given length.
func result(typ domain.AnomalyType, rule classify.Rule, hours int, open bool, events []classify.EventLink, signals ...detectors.Signal) classify.Result {
	return classify.Result{
		Type: typ, Rule: rule, Events: events,
		Episode: classify.Episode{MeterID: "M-T", Start: t0, End: t0.Add(time.Duration(hours-1) * time.Hour), Open: open, Signals: signals},
	}
}

func link(typ domain.EventType, role classify.EventRole, offset float64, duration time.Duration) classify.EventLink {
	return classify.EventLink{Event: domain.Event{Type: typ, Duration: duration}, Role: role, OffsetHours: offset}
}

func near(t *testing.T, name string, got, want, tol float64) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Errorf("%s = %v, want %v (+-%v)", name, got, want, tol)
	}
}
