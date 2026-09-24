package seed

import (
	"fmt"
	"io"
	"io/fs"
	"time"

	"github.com/m0ntbl4ck/voltia/internal/domain"
)

// Dataset is the full content the seed loads into the database.
type Dataset struct {
	Meters   []domain.Meter
	Readings []domain.Reading
	Events   []domain.Event
}

// Load reads meters.yaml, readings.csv and events.csv from fsys, and fails
// when a reading or an event names a meter that meters.yaml does not list.
func Load(fsys fs.FS, loc *time.Location) (Dataset, error) {
	var ds Dataset
	var err error
	if ds.Meters, err = readFile(fsys, "meters.yaml", ParseMeters); err != nil {
		return Dataset{}, err
	}
	if ds.Readings, err = readFile(fsys, "readings.csv", func(r io.Reader) ([]domain.Reading, error) {
		return ParseReadings(r, loc)
	}); err != nil {
		return Dataset{}, err
	}
	if ds.Events, err = readFile(fsys, "events.csv", func(r io.Reader) ([]domain.Event, error) {
		return ParseEvents(r, loc)
	}); err != nil {
		return Dataset{}, err
	}

	known := make(map[string]bool, len(ds.Meters))
	for _, m := range ds.Meters {
		known[m.MeterID] = true
	}
	for i, r := range ds.Readings {
		if !known[r.MeterID] {
			return Dataset{}, fmt.Errorf("readings row %d: meter %q is not in meters.yaml", i+2, r.MeterID)
		}
	}
	for i, e := range ds.Events {
		if !known[e.MeterID] {
			return Dataset{}, fmt.Errorf("events row %d: meter %q is not in meters.yaml", i+2, e.MeterID)
		}
	}
	return ds, nil
}

func readFile[T any](fsys fs.FS, name string, parse func(io.Reader) (T, error)) (T, error) {
	var zero T
	f, err := fsys.Open(name)
	if err != nil {
		return zero, err
	}
	defer f.Close()
	out, err := parse(f)
	if err != nil {
		return zero, fmt.Errorf("%s: %w", name, err)
	}
	return out, nil
}
