package seed

import (
	"os"
	"strings"
	"testing"
	"testing/fstest"
)

const (
	metersYAML   = "meters:\n  - meter_id: M-101\n    name: a\n    location: b\n"
	readingsHead = "meter_id,timestamp,consumption_kwh,voltage_v,current_a,power_factor,status\n"
	eventsHead   = "meter_id,event_timestamp,event_type,description\n"
)

func dataset(readings, events string) fstest.MapFS {
	return fstest.MapFS{
		"meters.yaml":  {Data: []byte(metersYAML)},
		"readings.csv": {Data: []byte(readingsHead + readings)},
		"events.csv":   {Data: []byte(eventsHead + events)},
	}
}

func TestLoad(t *testing.T) {
	fsys := dataset(
		"M-101,2026-09-01 06:00:00,28.8,221.64,132.34,0.945,OK\n",
		"M-101,2026-09-01 00:00,UNKNOWN,No operational event reported\n",
	)
	ds, err := Load(fsys, bogota(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(ds.Meters) != 1 || len(ds.Readings) != 1 || len(ds.Events) != 1 {
		t.Errorf("got %d meters, %d readings, %d events, want 1 of each", len(ds.Meters), len(ds.Readings), len(ds.Events))
	}
}

func TestLoadRejectsUnknownMeters(t *testing.T) {
	reading := "M-101,2026-09-01 06:00:00,1,1,1,1,OK\n"
	event := "M-101,2026-09-01 00:00,UNKNOWN,x\n"
	cases := map[string]struct {
		fsys fstest.MapFS
		want string
	}{
		"reading": {dataset(reading+strings.ReplaceAll(reading, "M-101", "M-999"), event), `readings row 3: meter "M-999"`},
		"event":   {dataset(reading, event+strings.ReplaceAll(event, "M-101", "M-999")), `events row 3: meter "M-999"`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Load(c.fsys, bogota(t))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error = %v, want it to mention %q", err, c.want)
			}
		})
	}
}

func TestLoadNamesTheBrokenFile(t *testing.T) {
	fsys := dataset("M-101,yesterday,1,1,1,1,OK\n", "")
	_, err := Load(fsys, bogota(t))
	if err == nil || !strings.Contains(err.Error(), "readings.csv") {
		t.Fatalf("error = %v, want it to name readings.csv", err)
	}
	delete(fsys, "events.csv")
	fsys["readings.csv"] = &fstest.MapFile{Data: []byte(readingsHead)}
	if _, err := Load(fsys, bogota(t)); err == nil {
		t.Fatal("a missing file must fail")
	}
}

func TestLoadShippedData(t *testing.T) {
	ds, err := Load(os.DirFS("../../../data"), bogota(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(ds.Meters) != 12 || len(ds.Readings) != 4032 || len(ds.Events) != 4 {
		t.Errorf("got %d meters, %d readings, %d events, want 12, 4032 and 4", len(ds.Meters), len(ds.Readings), len(ds.Events))
	}
}
