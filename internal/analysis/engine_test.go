package analysis

import (
	"context"
	"errors"
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	"github.com/m0ntbl4ck/voltia/internal/analysis/classify"
	"github.com/m0ntbl4ck/voltia/internal/analysis/detectors"
	"github.com/m0ntbl4ck/voltia/internal/analysis/scoring"
	"github.com/m0ntbl4ck/voltia/internal/domain"
)

var day1 = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

// meterDays builds `days` days of hourly readings for one meter. kwh gives the
// consumption of each day and hour; the electrical variables stay flat.
func meterDays(id string, days int, kwh func(day, hour int) float64) []domain.Reading {
	var out []domain.Reading
	for d := 0; d < days; d++ {
		for h := 0; h < 24; h++ {
			out = append(out, domain.Reading{
				MeterID:        id,
				Timestamp:      day1.AddDate(0, 0, d).Add(time.Duration(h) * time.Hour),
				ConsumptionKWh: kwh(d, h),
				VoltageV:       220,
				CurrentA:       100,
				PowerFactor:    0.9,
			})
		}
	}
	return out
}

func flat(day, hour int) float64 { return 100 }

func TestRunFailsWithoutReadings(t *testing.T) {
	if _, err := Run(context.Background(), Input{}, DefaultConfig(), nil); err == nil {
		t.Error("expected an error for an empty input")
	}
}

// Each loop checks the context on its own, so cancelling at the start of a
// stage that walks through meters or episodes must stop the run before
// anything else is reported.
func TestRunStopsWhenTheContextIsCancelled(t *testing.T) {
	rising := func(day, hour int) float64 {
		if day >= 8 {
			return 300
		}
		return 100
	}
	in := Input{Readings: meterDays("M-1", 10, rising)}
	for _, stage := range []Stage{StageBaseline, StageDetection, StageEvents} {
		t.Run(string(stage), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var after []Progress
			_, err := Run(ctx, in, DefaultConfig(), func(p Progress) {
				if ctx.Err() != nil {
					after = append(after, p)
				}
				if p.Stage == stage && !p.Done {
					cancel()
				}
			})
			if !errors.Is(err, context.Canceled) {
				t.Errorf("err = %v, want context.Canceled", err)
			}
			if len(after) != 0 {
				t.Errorf("progress after the cancellation: %v", after)
			}
		})
	}
}

func TestRunKeepsGoingWhenOneMeterHasNoBaseline(t *testing.T) {
	// The second meter only reports the first twelve hours of each day, so its
	// baseline has no samples for the rest.
	broken := slices.DeleteFunc(meterDays("M-2", 10, flat), func(r domain.Reading) bool { return r.Timestamp.Hour() >= 12 })
	rising := func(day, hour int) float64 {
		if day >= 8 {
			return 300
		}
		return 100
	}
	in := Input{Readings: append(broken, meterDays("M-1", 10, rising)...)}

	report, err := Run(context.Background(), in, DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Failures) != 1 || report.Failures[0].MeterID != "M-2" || report.Failures[0].Err == nil {
		t.Errorf("failures = %v, want one for M-2", report.Failures)
	}
	if len(report.Anomalies) != 1 || report.Anomalies[0].Episode.MeterID != "M-1" {
		t.Errorf("anomalies = %v, want one for M-1", report.Anomalies)
	}
}

func TestRunWithOnlyReferenceDaysFindsNothing(t *testing.T) {
	report, err := Run(context.Background(), Input{Readings: meterDays("M-1", 7, flat)}, DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Anomalies) != 0 || len(report.Failures) != 0 || report.Confidence != 0 {
		t.Errorf("report = %+v, want it empty", report)
	}
}

// Days 1 to 4 run at triple consumption from midnight to 05:00, more than half
// of the reference week, so left in they would define the night as normal and
// flag the ordinary nights that follow.
func TestRunKeepsReportedEventsOutOfTheBaseline(t *testing.T) {
	kwh := func(day, hour int) float64 {
		if day < 4 && hour < 6 {
			return 300
		}
		return 100
	}
	var events []domain.Event
	for d := 0; d < 4; d++ {
		events = append(events, domain.Event{
			MeterID: "M-1", Timestamp: day1.AddDate(0, 0, d), Type: domain.EventOperationalChange, Duration: 6 * time.Hour,
		})
	}
	readings := meterDays("M-1", 10, kwh)

	without, err := Run(context.Background(), Input{Readings: readings}, DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(without.Anomalies) == 0 {
		t.Fatal("without events the nights should be flagged, so the case proves nothing")
	}
	with, err := Run(context.Background(), Input{Readings: readings, Events: events}, DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(with.Anomalies) != 0 {
		t.Errorf("with the events reported, got %d anomalies, want none", len(with.Anomalies))
	}
}

// The outage sits in the reference week and is kept out of the baseline. It
// must not come back as an anomaly when the analysis starts after that week.
func TestRunOnlyAnalysesTheDaysAfterTheReferenceWeek(t *testing.T) {
	kwh := func(day, hour int) float64 {
		if day == 5 && hour < 12 {
			return 0
		}
		return 100
	}
	outage := domain.Event{MeterID: "M-1", Timestamp: day1.AddDate(0, 0, 5), Type: domain.EventScheduledOutage, Duration: 12 * time.Hour}

	report, err := Run(context.Background(), Input{Readings: meterDays("M-1", 10, kwh), Events: []domain.Event{outage}}, DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Anomalies) != 0 {
		t.Errorf("got %d anomalies, want none: the outage belongs to the reference week", len(report.Anomalies))
	}
}

func TestRunGivesTheSameReportForReversedReadings(t *testing.T) {
	rising := func(day, hour int) float64 {
		if day >= 8 {
			return 300
		}
		return 100
	}
	readings := append(meterDays("M-1", 10, rising), meterDays("M-2", 10, flat)...)
	want, err := Run(context.Background(), Input{Readings: readings}, DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(want.Anomalies) == 0 {
		t.Fatal("expected an anomaly to compare")
	}

	slices.Reverse(readings)
	got, err := Run(context.Background(), Input{Readings: readings}, DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Anomalies) != len(want.Anomalies) || got.Anomalies[0].Score != want.Anomalies[0].Score ||
		got.Anomalies[0].Episode.Start != want.Anomalies[0].Episode.Start {
		t.Errorf("reversed input changed the report:\n got %+v\nwant %+v", got.Anomalies[0], want.Anomalies[0])
	}
}

func TestReferenceSkip(t *testing.T) {
	at := func(day, hour int) domain.Reading {
		return domain.Reading{Timestamp: day1.AddDate(0, 0, day).Add(time.Duration(hour) * time.Hour)}
	}
	events := []domain.Event{
		{MeterID: "M-1", Timestamp: at(1, 0).Timestamp, Type: domain.EventScheduledOutage, Duration: 12 * time.Hour},
		{MeterID: "M-1", Timestamp: at(3, 0).Timestamp, Type: domain.EventDataQuality},
		{MeterID: "M-1", Timestamp: at(5, 0).Timestamp, Type: domain.EventUnknown},
		{MeterID: "M-2", Timestamp: at(0, 0).Timestamp, Type: domain.EventScheduledOutage, Duration: 48 * time.Hour},
	}
	skip := referenceSkip("M-1", events, 24*time.Hour)

	cases := []struct {
		name string
		r    domain.Reading
		want bool
	}{
		{"first hour of a stated duration", at(1, 0), true},
		{"last hour of a stated duration", at(1, 11), true},
		{"the hour the duration ends", at(1, 12), false},
		{"before the event", at(0, 23), false},
		{"unstated duration covers the exclusion", at(3, 23), true},
		{"after the exclusion", at(4, 0), false},
		{"unknown event excludes nothing", at(5, 3), false},
		{"another meter's event", at(0, 5), false},
	}
	for _, c := range cases {
		if got := skip(c.r); got != c.want {
			t.Errorf("%s: skip = %v, want %v", c.name, got, c.want)
		}
	}
	if referenceSkip("M-3", events, 24*time.Hour) != nil {
		t.Error("a meter without reported events should not filter anything")
	}
}

func TestSortAnomaliesBreaksTiesTheSameWay(t *testing.T) {
	anomaly := func(meter string, start time.Time, priority int, confidence float64) Anomaly {
		return Anomaly{
			Result: classify.Result{Episode: classify.Episode{MeterID: meter, Start: start}},
			Score:  scoring.Score{Priority: priority, Confidence: confidence},
		}
	}
	got := []Anomaly{
		anomaly("M-2", day1, 50, 0.9),
		anomaly("M-9", day1, 80, 0.5),
		anomaly("M-1", day1.Add(time.Hour), 50, 0.9),
		anomaly("M-1", day1, 50, 0.9),
		anomaly("M-3", day1, 50, 0.95),
	}
	sortAnomalies(got)

	type key struct {
		meter string
		start time.Time
	}
	want := []key{{"M-9", day1}, {"M-3", day1}, {"M-1", day1}, {"M-1", day1.Add(time.Hour)}, {"M-2", day1}}
	for i, w := range want {
		if g := (key{got[i].Episode.MeterID, got[i].Episode.Start}); g != w {
			t.Errorf("position %d = %v, want %v", i, g, w)
		}
	}
}

// noisyDays is meterDays with coherent random noise, which the isolation forest
// needs to have anything to cut. When surge is true, on day 9 from 10:00 to
// 15:00 the consumption triples while voltage, current and power factor stay
// as they were: a single persistent shift, so the forest is the second source.
func noisyDays(id string, days int, surge bool) []domain.Reading {
	rng := rand.New(rand.NewPCG(42, 1))
	var out []domain.Reading
	for d := 0; d < days; d++ {
		for h := 0; h < 24; h++ {
			v := 220 + rng.NormFloat64()
			i := 100 + 3*rng.NormFloat64()
			pf := 0.9 + 0.01*rng.NormFloat64()
			kwh := v * i * pf / 1000 * (1 + 0.03*rng.NormFloat64())
			if surge && d == 8 && h >= 10 && h <= 15 {
				kwh *= 3
			}
			out = append(out, domain.Reading{
				MeterID: id, Timestamp: day1.AddDate(0, 0, d).Add(time.Duration(h) * time.Hour),
				ConsumptionKWh: kwh, VoltageV: v, CurrentA: i, PowerFactor: pf,
			})
		}
	}
	return out
}

func TestRunBacksAnEpisodeUpWithTheIsolationForest(t *testing.T) {
	in := Input{Readings: noisyDays("M-1", 10, true)}
	with, err := Run(context.Background(), in, DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(with.Anomalies) != 1 {
		t.Fatalf("got %d anomalies, want 1", len(with.Anomalies))
	}
	a := with.Anomalies[0]
	if !a.Episode.Has(detectors.KindIsolationForest) {
		t.Fatal("the episode should carry the isolation forest signal")
	}

	// With a threshold no score can reach the forest stays silent: everything
	// else about the anomaly must be the same and only the confidence drops.
	off := DefaultConfig()
	off.IForest.Threshold = 2
	without, err := Run(context.Background(), in, off, nil)
	if err != nil {
		t.Fatal(err)
	}
	b := without.Anomalies[0]
	if b.Episode.Has(detectors.KindIsolationForest) {
		t.Fatal("no score reaches 2, so there should be no forest signal")
	}
	if a.Type != b.Type || a.Rule != b.Rule || a.Score.Severity != b.Score.Severity || a.Score.Priority != b.Score.Priority {
		t.Errorf("the forest changed the decision: %s %s %s %d against %s %s %s %d",
			a.Type, a.Rule, a.Score.Severity, a.Score.Priority, b.Type, b.Rule, b.Score.Severity, b.Score.Priority)
	}
	if a.Score.Confidence <= b.Score.Confidence {
		t.Errorf("confidence %.4f with the forest, %.4f without: it should rise", a.Score.Confidence, b.Score.Confidence)
	}
}

// The forest only backs episodes up, so even a threshold that flags every
// reading must not open one on a healthy meter.
func TestRunNeverOpensAnEpisodeFromTheIsolationForest(t *testing.T) {
	cfg := DefaultConfig()
	cfg.IForest.Threshold = 0
	report, err := Run(context.Background(), Input{Readings: noisyDays("M-1", 10, false)}, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Anomalies) != 0 {
		t.Errorf("got %d anomalies on a healthy meter, want none", len(report.Anomalies))
	}
}

// A forest that cannot be trained is a wrong configuration, not a broken
// meter, so it stops the run instead of being listed as a failure.
func TestRunFailsWhenTheIsolationForestIsMisconfigured(t *testing.T) {
	cfg := DefaultConfig()
	cfg.IForest.Forest.Trees = 0
	report, err := Run(context.Background(), Input{Readings: noisyDays("M-1", 10, false)}, cfg, nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	if len(report.Failures) != 0 {
		t.Errorf("failures = %v, want none", report.Failures)
	}
}

// The forest learns from the same reference readings as the baseline. Days 1
// to 5 carry the same tripled consumption as the breakdown on day 9, each
// covered by an event. Left in the training data they would make the breakdown
// look ordinary and the forest would not back the episode up.
func TestRunTrainsTheIsolationForestWithoutTheEventHours(t *testing.T) {
	readings := noisyDays("M-1", 10, true)
	var events []domain.Event
	for d := 0; d < 5; d++ {
		from := day1.AddDate(0, 0, d).Add(10 * time.Hour)
		events = append(events, domain.Event{MeterID: "M-1", Timestamp: from, Type: domain.EventOperationalChange, Duration: 6 * time.Hour})
		for i, r := range readings {
			if !r.Timestamp.Before(from) && r.Timestamp.Before(from.Add(6*time.Hour)) {
				readings[i].ConsumptionKWh *= 3
			}
		}
	}
	report, err := Run(context.Background(), Input{Readings: readings, Events: events}, DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Anomalies) != 1 {
		t.Fatalf("got %d anomalies, want 1", len(report.Anomalies))
	}
	if !report.Anomalies[0].Episode.Has(detectors.KindIsolationForest) {
		t.Error("the breakdown should still look unusual once the event hours are kept out of the forest")
	}
}

// Consumption triples for the whole second week. A forest that also learned
// from those days would see half of its data at the tripled level and take it
// for normal.
func TestRunTrainsTheIsolationForestOnTheReferenceWeekOnly(t *testing.T) {
	readings := noisyDays("M-1", 14, false)
	for i, r := range readings {
		if !r.Timestamp.Before(day1.AddDate(0, 0, 7)) {
			readings[i].ConsumptionKWh *= 3
		}
	}
	report, err := Run(context.Background(), Input{Readings: readings}, DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Anomalies) != 1 {
		t.Fatalf("got %d anomalies, want 1", len(report.Anomalies))
	}
	if !report.Anomalies[0].Episode.Has(detectors.KindIsolationForest) {
		t.Error("the second week should look unusual against a forest that only saw the first")
	}
}
