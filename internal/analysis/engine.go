package analysis

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/m0ntbl4ck/voltia/internal/analysis/baseline"
	"github.com/m0ntbl4ck/voltia/internal/analysis/classify"
	"github.com/m0ntbl4ck/voltia/internal/analysis/detectors"
	"github.com/m0ntbl4ck/voltia/internal/analysis/iforest"
	"github.com/m0ntbl4ck/voltia/internal/analysis/scoring"
	"github.com/m0ntbl4ck/voltia/internal/domain"
)

// meterReadings is the data of one meter, split around its reference window.
type meterReadings struct {
	id  string
	all []domain.Reading
	// reference holds the readings inside the reference window.
	reference []domain.Reading
	// analysis holds the readings at or after the end of the reference window.
	analysis []domain.Reading
}

// Run analyses every meter in the input and returns the anomalies ordered by
// priority. A meter whose baseline cannot be built is reported in Failures
// and does not stop the others. progress may be nil.
func Run(ctx context.Context, in Input, cfg Config, progress func(Progress)) (Report, error) {
	if len(in.Readings) == 0 {
		return Report{}, errors.New("no readings")
	}
	p := reporter{fn: progress}

	p.start(StageReadings)
	meters := splitByMeter(in.Readings, cfg.Baseline.ReferenceDays)
	analysisReadings := make(map[string][]domain.Reading, len(meters))
	for _, m := range meters {
		analysisReadings[m.id] = m.analysis
	}
	p.done(StageReadings)

	var report Report
	profiles := map[string]baseline.Meter{}
	models := map[string]iforest.Model{}
	p.start(StageBaseline)
	for _, m := range meters {
		if err := ctx.Err(); err != nil {
			return Report{}, err
		}
		skip := referenceSkip(m.id, in.Events, cfg.EventExclusion)
		b, err := baseline.Build(m.id, m.all, skip, cfg.Baseline)
		if err != nil {
			report.Failures = append(report.Failures, MeterFailure{MeterID: m.id, Err: err})
			continue
		}
		// The baseline already has samples for every hour, so a forest that
		// cannot be trained means the configuration is wrong, not the meter.
		model, err := iforest.Train(b, withoutSkipped(m.reference, skip), cfg.IForest)
		if err != nil {
			return Report{}, fmt.Errorf("train isolation forest: %w", err)
		}
		profiles[m.id] = b
		models[m.id] = model
	}
	p.done(StageBaseline)

	var signals []detectors.Signal
	p.start(StageDetection)
	for _, m := range meters {
		if err := ctx.Err(); err != nil {
			return Report{}, err
		}
		if b, ok := profiles[m.id]; ok {
			signals = append(signals, detect(b, m.analysis, cfg.Detectors)...)
		}
	}
	p.done(StageDetection)

	p.start(StageCorrelation)
	episodes := classify.Group(signals, cfg.Classify)
	for i, ep := range episodes {
		model, trained := models[ep.MeterID]
		if !trained {
			continue
		}
		if sig, ok := model.Corroborate(ep, analysisReadings[ep.MeterID]); ok {
			episodes[i].Signals = append(ep.Signals, sig)
		}
	}
	p.done(StageCorrelation)

	p.start(StageEvents)
	scores := make([]scoring.Score, 0, len(episodes))
	for _, ep := range episodes {
		if err := ctx.Err(); err != nil {
			return Report{}, err
		}
		res := classify.Classify(ep, in.Events, cfg.Classify)
		score := scoring.Evaluate(res, cfg.Scoring)
		report.Anomalies = append(report.Anomalies, Anomaly{Result: res, Score: score})
		scores = append(scores, score)
	}
	sortAnomalies(report.Anomalies)
	report.Confidence = scoring.AggregateConfidence(scores, cfg.Scoring.Confidence)
	p.done(StageEvents)

	return report, nil
}

// splitByMeter groups the readings by meter, meters sorted by id, and splits
// each meter around its reference window.
func splitByMeter(readings []domain.Reading, referenceDays int) []meterReadings {
	byID := map[string][]domain.Reading{}
	for _, r := range readings {
		byID[r.MeterID] = append(byID[r.MeterID], r)
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	out := make([]meterReadings, 0, len(ids))
	for _, id := range ids {
		all := byID[id]
		_, end := baseline.ReferenceWindow(all, referenceDays)
		m := meterReadings{id: id, all: all}
		for _, r := range all {
			if r.Timestamp.Before(end) {
				m.reference = append(m.reference, r)
			} else {
				m.analysis = append(m.analysis, r)
			}
		}
		out = append(out, m)
	}
	return out
}

// withoutSkipped returns the readings the filter keeps, or all of them when
// there is no filter.
func withoutSkipped(readings []domain.Reading, skip func(domain.Reading) bool) []domain.Reading {
	if skip == nil {
		return readings
	}
	return slices.DeleteFunc(slices.Clone(readings), skip)
}

func detect(b baseline.Meter, readings []domain.Reading, cfg detectors.Config) []detectors.Signal {
	if len(readings) == 0 {
		return nil
	}
	var out []detectors.Signal
	out = append(out, detectors.DetectPersistentShift(b, readings, cfg)...)
	out = append(out, detectors.DetectSpikes(b, readings, cfg)...)
	out = append(out, detectors.DetectOutliers(b, readings, cfg)...)
	out = append(out, detectors.DetectElectricalRelation(b, readings, cfg)...)
	out = append(out, detectors.DetectDataQuality(b, readings, cfg)...)
	return out
}

// sortAnomalies puts what to investigate first at the top: priority, then
// confidence, then meter and start so that ties come out the same every run.
func sortAnomalies(as []Anomaly) {
	slices.SortStableFunc(as, func(a, b Anomaly) int {
		return cmp.Or(
			cmp.Compare(b.Score.Priority, a.Score.Priority),
			cmp.Compare(b.Score.Confidence, a.Score.Confidence),
			cmp.Compare(a.Episode.MeterID, b.Episode.MeterID),
			a.Episode.Start.Compare(b.Episode.Start),
		)
	})
}

// referenceSkip returns the filter that keeps the readings covered by a
// reported event out of the baseline, or nil when the meter has none. An
// UNKNOWN event says nothing happened, so it excludes nothing.
func referenceSkip(meterID string, events []domain.Event, unstated time.Duration) func(domain.Reading) bool {
	type window struct{ from, to time.Time }
	var windows []window
	for _, e := range events {
		if e.MeterID != meterID || e.Type == domain.EventUnknown {
			continue
		}
		length := e.Duration
		if length == 0 {
			length = unstated
		}
		windows = append(windows, window{e.Timestamp, e.Timestamp.Add(length)})
	}
	if len(windows) == 0 {
		return nil
	}
	return func(r domain.Reading) bool {
		return slices.ContainsFunc(windows, func(w window) bool {
			return !r.Timestamp.Before(w.from) && r.Timestamp.Before(w.to)
		})
	}
}
