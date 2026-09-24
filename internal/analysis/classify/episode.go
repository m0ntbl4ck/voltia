package classify

import (
	"slices"
	"sort"
	"time"

	"github.com/m0ntbl4ck/voltia/internal/analysis/detectors"
	"github.com/m0ntbl4ck/voltia/internal/domain"
)

type Config struct {
	// MergeGap is the quiet stretch that separates two episodes. Signals closer
	// than this belong to the same episode.
	MergeGap time.Duration
	// EventWindow is how far from the start of an episode an event may sit and
	// still be related to it.
	EventWindow time.Duration
	// OutageMargin is the slack allowed when an outage is compared with the
	// duration of the scheduled event.
	OutageMargin time.Duration
}

func DefaultConfig() Config {
	return Config{MergeGap: 6 * time.Hour, EventWindow: 6 * time.Hour, OutageMargin: time.Hour}
}

// Episode is every signal of one meter that overlaps or sits close together.
type Episode struct {
	MeterID string
	Start   time.Time
	// End is the start of the last hour covered.
	End time.Time
	// Open is true when the episode reaches the last reading and may still go on.
	Open    bool
	Signals []detectors.Signal
}

// Duration is the time the episode covers, counting the whole last hour.
func (e Episode) Duration() time.Duration { return e.End.Sub(e.Start) + time.Hour }

func (e Episode) Has(kinds ...detectors.Kind) bool {
	return slices.ContainsFunc(e.Signals, func(s detectors.Signal) bool { return slices.Contains(kinds, s.Kind) })
}

// Direction is the side of the longest consumption shift: +1 above the
// baseline, -1 below, 0 when consumption did not move for hours.
func (e Episode) Direction() int {
	best, hours := 0, 0
	for _, s := range e.Signals {
		if s.Variable != domain.Consumption || (s.Kind != detectors.KindPersistentShift && s.Kind != detectors.KindSpike) {
			continue
		}
		if s.Hours > hours {
			best, hours = s.Direction, s.Hours
		}
	}
	return best
}

// Group joins the signals of each meter into episodes, ordered by meter and start.
func Group(signals []detectors.Signal, cfg Config) []Episode {
	byMeter := map[string][]detectors.Signal{}
	for _, s := range signals {
		byMeter[s.MeterID] = append(byMeter[s.MeterID], s)
	}
	meters := make([]string, 0, len(byMeter))
	for m := range byMeter {
		meters = append(meters, m)
	}
	sort.Strings(meters)

	var out []Episode
	for _, meter := range meters {
		sigs := byMeter[meter]
		slices.SortStableFunc(sigs, func(a, b detectors.Signal) int { return a.Start.Compare(b.Start) })
		current := newEpisode(sigs[0])
		for _, s := range sigs[1:] {
			if s.Start.Sub(current.End) < cfg.MergeGap {
				current.add(s)
				continue
			}
			out = append(out, current)
			current = newEpisode(s)
		}
		out = append(out, current)
	}
	return out
}

func newEpisode(s detectors.Signal) Episode {
	return Episode{MeterID: s.MeterID, Start: s.Start, End: s.End, Open: s.Open, Signals: []detectors.Signal{s}}
}

func (e *Episode) add(s detectors.Signal) {
	e.Signals = append(e.Signals, s)
	if s.End.After(e.End) {
		e.End = s.End
		e.Open = s.Open
	} else if s.End.Equal(e.End) {
		e.Open = e.Open || s.Open
	}
}
