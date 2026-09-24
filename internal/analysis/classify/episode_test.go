package classify

import (
	"testing"

	"github.com/m0ntbl4ck/voltia/internal/analysis/detectors"
)

func TestGroupJoinsOverlappingSignals(t *testing.T) {
	got := Group([]detectors.Signal{shift("M-1", 10, 20, 1), shift("M-1", 15, 20, 1)}, DefaultConfig())
	if len(got) != 1 || len(got[0].Signals) != 2 {
		t.Fatalf("got %+v", got)
	}
	if !got[0].Start.Equal(hour(10)) || !got[0].End.Equal(hour(34)) {
		t.Errorf("bounds %v to %v", got[0].Start, got[0].End)
	}
}

func TestGroupJoinsSignalsCloserThanTheGap(t *testing.T) {
	// The first ends at hour 10 and the next starts at hour 15: five hours apart.
	got := Group([]detectors.Signal{shift("M-1", 5, 6, 1), shift("M-1", 15, 3, 1)}, DefaultConfig())
	if len(got) != 1 {
		t.Fatalf("got %d episodes, want 1", len(got))
	}
}

func TestGroupSplitsSignalsAtTheGap(t *testing.T) {
	// The first ends at hour 10 and the next starts at hour 16: exactly six hours apart.
	got := Group([]detectors.Signal{shift("M-1", 5, 6, 1), shift("M-1", 16, 3, 1)}, DefaultConfig())
	if len(got) != 2 {
		t.Fatalf("got %d episodes, want 2", len(got))
	}
}

func TestGroupKeepsMetersApart(t *testing.T) {
	got := Group([]detectors.Signal{shift("M-2", 10, 5, 1), shift("M-1", 10, 5, 1)}, DefaultConfig())
	if len(got) != 2 || got[0].MeterID != "M-1" || got[1].MeterID != "M-2" {
		t.Fatalf("got %+v", got)
	}
}

func TestGroupOrdersSignalsInTime(t *testing.T) {
	got := Group([]detectors.Signal{shift("M-1", 30, 4, 1), shift("M-1", 0, 4, 1), shift("M-1", 12, 4, 1)}, DefaultConfig())
	if len(got) != 3 || !got[0].Start.Equal(hour(0)) || !got[2].Start.Equal(hour(30)) {
		t.Fatalf("got %+v", got)
	}
}

func TestGroupKeepsTheLatestEndAndItsOpenFlag(t *testing.T) {
	long := shift("M-1", 0, 40, 1)
	inside := shift("M-1", 10, 5, 1)
	inside.Open = true // ends earlier than the long signal, so it does not make the episode open
	got := Group([]detectors.Signal{long, inside}, DefaultConfig())
	if len(got) != 1 || got[0].Open || !got[0].End.Equal(hour(39)) {
		t.Fatalf("got %+v", got)
	}
	last := shift("M-1", 30, 10, 1)
	last.Open = true
	got = Group([]detectors.Signal{long, last}, DefaultConfig())
	if !got[0].Open {
		t.Error("an episode ending with an open signal should be open")
	}
}

func TestGroupWithoutSignals(t *testing.T) {
	if got := Group(nil, DefaultConfig()); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestEpisodeDurationCountsTheLastHour(t *testing.T) {
	got := Group([]detectors.Signal{shift("M-1", 4, 12, 1)}, DefaultConfig())
	if got[0].Duration().Hours() != 12 {
		t.Errorf("duration = %v, want 12h", got[0].Duration())
	}
}

func TestEpisodeDirectionFollowsTheLongestConsumptionShift(t *testing.T) {
	ep := Group([]detectors.Signal{shift("M-1", 0, 4, 1), shift("M-1", 4, 9, -1), powerFactorDrop("M-1", 0, 30)}, DefaultConfig())[0]
	if ep.Direction() != -1 {
		t.Errorf("direction = %d, want -1", ep.Direction())
	}
	only := Group([]detectors.Signal{quality("M-1", 3)}, DefaultConfig())[0]
	if only.Direction() != 0 {
		t.Errorf("direction without consumption shift = %d, want 0", only.Direction())
	}
}
