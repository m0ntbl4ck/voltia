package seed

import (
	"strings"
	"testing"
	"time"

	"github.com/m0ntbl4ck/voltia/internal/domain"
)

func bogota(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/Bogota")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func TestParseReadings(t *testing.T) {
	csv := "meter_id,timestamp,consumption_kwh,voltage_v,current_a,power_factor,status\n" +
		"M-101,2026-09-01 06:00:00,28.8,221.64,132.34,0.945,OK\n"
	got, err := ParseReadings(strings.NewReader(csv), bogota(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d readings, want 1", len(got))
	}
	r := got[0]
	want := domain.Reading{MeterID: "M-101", ConsumptionKWh: 28.8, VoltageV: 221.64, CurrentA: 132.34, PowerFactor: 0.945, Status: "OK"}
	r.Timestamp = time.Time{}
	if r != want {
		t.Errorf("got %+v, want %+v", r, want)
	}
	if h := got[0].Timestamp.Hour(); h != 6 {
		t.Errorf("hour = %d, want 6", h)
	}
	if _, off := got[0].Timestamp.Zone(); off != -5*3600 {
		t.Errorf("offset = %d, want -18000", off)
	}
}

func TestParseReadingsRejectsBadRows(t *testing.T) {
	header := "meter_id,timestamp,consumption_kwh,voltage_v,current_a,power_factor,status\n"
	cases := map[string]string{
		"bad timestamp": header + "M-101,yesterday,1,1,1,1,OK\n",
		"bad number":    header + "M-101,2026-09-01 06:00:00,abc,1,1,1,OK\n",
		"empty":         "",
		"missing col":   "meter_id,timestamp\nM-101,2026-09-01 06:00:00\n",
	}
	for name, csv := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseReadings(strings.NewReader(csv), bogota(t)); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestParseEvents(t *testing.T) {
	csv := "meter_id,event_timestamp,event_type,description\n" +
		"M-109,2026-09-12 14:00,UNKNOWN,No operational event reported\n"
	got, err := ParseEvents(strings.NewReader(csv), bogota(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Type != domain.EventUnknown || got[0].MeterID != "M-109" {
		t.Fatalf("unexpected events: %+v", got)
	}
	if got[0].Timestamp.Hour() != 14 || got[0].Timestamp.Day() != 12 {
		t.Errorf("timestamp = %v", got[0].Timestamp)
	}
}
