package postgres

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/m0ntbl4ck/voltia/internal/adapters/seed"
	"github.com/m0ntbl4ck/voltia/internal/domain"
)

func seededRepository(t *testing.T) (*Repository, seed.Dataset, *time.Location) {
	t.Helper()
	db := migratedDB(t)
	loc, err := time.LoadLocation("America/Bogota")
	if err != nil {
		t.Fatal(err)
	}
	ds, err := seed.Load(os.DirFS("../../../data"), loc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Seed(context.Background(), db, ds.Meters, ds.Readings, ds.Events); err != nil {
		t.Fatal(err)
	}
	return NewRepository(db, loc), ds, loc
}

func TestMeters(t *testing.T) {
	repo, ds, _ := seededRepository(t)
	ctx := context.Background()

	got, err := repo.Meters(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, ds.Meters) {
		t.Errorf("meters differ from the seeded ones:\n got %v\nwant %v", got, ds.Meters)
	}

	one, err := repo.Meter(ctx, "M-104")
	if err != nil {
		t.Fatal(err)
	}
	if one.MeterID != "M-104" || one.Name == "" {
		t.Errorf("Meter(M-104) = %+v", one)
	}
	if _, err := repo.Meter(ctx, "M-999"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Meter(M-999) error = %v, want ErrNotFound", err)
	}
}

func TestReadingsRoundTripInPlantTime(t *testing.T) {
	repo, ds, loc := seededRepository(t)

	got, err := repo.Readings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := slices.Clone(ds.Readings)
	slices.SortFunc(want, func(a, b domain.Reading) int {
		if c := strings.Compare(a.MeterID, b.MeterID); c != 0 {
			return c
		}
		return a.Timestamp.Compare(b.Timestamp)
	})
	if len(got) != len(want) {
		t.Fatalf("got %d readings, want %d", len(got), len(want))
	}
	for i := range want {
		g, w := got[i], want[i]
		if !g.Timestamp.Equal(w.Timestamp) || g.Timestamp.Hour() != w.Timestamp.Hour() ||
			g.Timestamp.Location() != loc {
			t.Fatalf("reading %d timestamp = %v, want %v in %v", i, g.Timestamp, w.Timestamp, loc)
		}
		g.Timestamp, w.Timestamp = time.Time{}, time.Time{}
		if g != w {
			t.Fatalf("reading %d = %+v, want %+v", i, g, w)
		}
	}
}

func TestMeterReadingsRangeExcludesTheUpperBound(t *testing.T) {
	repo, _, loc := seededRepository(t)
	from := time.Date(2026, 9, 10, 0, 0, 0, 0, loc)

	got, err := repo.MeterReadings(context.Background(), "M-104", from, from.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 24 {
		t.Fatalf("got %d readings, want 24", len(got))
	}
	if !got[0].Timestamp.Equal(from) || !got[23].Timestamp.Equal(from.Add(23*time.Hour)) {
		t.Errorf("range runs %v to %v", got[0].Timestamp, got[23].Timestamp)
	}
	for _, r := range got {
		if r.MeterID != "M-104" {
			t.Fatalf("reading of %s in the M-104 range", r.MeterID)
		}
	}
}

func TestEventsCarryPlantTimeAndTheStatedDuration(t *testing.T) {
	repo, ds, loc := seededRepository(t)
	ctx := context.Background()

	all, err := repo.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != len(ds.Events) {
		t.Fatalf("got %d events, want %d", len(all), len(ds.Events))
	}
	if !slices.IsSortedFunc(all, func(a, b domain.Event) int { return a.Timestamp.Compare(b.Timestamp) }) {
		t.Error("events are not ordered by time")
	}

	for _, e := range all {
		if e.MeterID == "M-109" && (e.Timestamp.Hour() != 14 || e.Timestamp.Location() != loc) {
			t.Errorf("M-109 event at %v, want 14:00 in %v", e.Timestamp, loc)
		}
	}

	one, err := repo.MeterEvents(ctx, "M-106")
	if err != nil {
		t.Fatal(err)
	}
	if len(one) != 1 || one[0].Type != domain.EventScheduledOutage || one[0].Duration != 12*time.Hour {
		t.Errorf("MeterEvents(M-106) = %+v", one)
	}
	if none, err := repo.MeterEvents(ctx, "M-101"); err != nil || len(none) != 0 {
		t.Errorf("MeterEvents(M-101) = %v, %v; want no events", none, err)
	}
}
