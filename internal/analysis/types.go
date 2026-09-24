package analysis

import (
	"time"

	"github.com/m0ntbl4ck/voltia/internal/analysis/baseline"
	"github.com/m0ntbl4ck/voltia/internal/analysis/classify"
	"github.com/m0ntbl4ck/voltia/internal/analysis/detectors"
	"github.com/m0ntbl4ck/voltia/internal/analysis/iforest"
	"github.com/m0ntbl4ck/voltia/internal/analysis/scoring"
	"github.com/m0ntbl4ck/voltia/internal/domain"
)

// Stage is one step of the pipeline, in the order Run goes through them.
type Stage string

const (
	StageReadings    Stage = "READINGS"
	StageBaseline    Stage = "BASELINE"
	StageDetection   Stage = "DETECTION"
	StageCorrelation Stage = "CORRELATION"
	StageEvents      Stage = "EVENTS"
)

// Stages lists the stages in execution order.
var Stages = []Stage{StageReadings, StageBaseline, StageDetection, StageCorrelation, StageEvents}

// Progress says that a stage has started (Done false) or finished (Done true).
type Progress struct {
	Stage Stage
	Done  bool
}

type reporter struct{ fn func(Progress) }

func (r reporter) start(s Stage) { r.send(Progress{Stage: s}) }
func (r reporter) done(s Stage)  { r.send(Progress{Stage: s, Done: true}) }

func (r reporter) send(p Progress) {
	if r.fn != nil {
		r.fn(p)
	}
}

// Config groups the settings of every piece of the pipeline.
type Config struct {
	Baseline  baseline.Config
	Detectors detectors.Config
	IForest   iforest.ModelConfig
	Classify  classify.Config
	Scoring   scoring.Config
	// EventExclusion is how long readings stay out of the baseline after an
	// event that does not state its own duration.
	EventExclusion time.Duration
}

func DefaultConfig() Config {
	return Config{
		Baseline:       baseline.DefaultConfig(),
		Detectors:      detectors.DefaultConfig(),
		IForest:        iforest.DefaultModelConfig(),
		Classify:       classify.DefaultConfig(),
		Scoring:        scoring.DefaultConfig(),
		EventExclusion: 24 * time.Hour,
	}
}

// Input is everything the pipeline reads.
type Input struct {
	Readings []domain.Reading
	Events   []domain.Event
}

// Anomaly is a classified episode with its score.
type Anomaly struct {
	classify.Result
	Score scoring.Score
}

// MeterFailure is a meter the pipeline could not analyse.
type MeterFailure struct {
	MeterID string
	Err     error
}

// Report is the outcome of one run.
type Report struct {
	// Anomalies are ordered by priority, the first being the one to look at first.
	Anomalies []Anomaly
	Failures  []MeterFailure
	// Confidence is the aggregate over Anomalies, 0 when there are none.
	Confidence float64
}
