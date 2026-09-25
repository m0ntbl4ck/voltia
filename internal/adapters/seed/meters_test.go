package seed

import (
	"os"
	"strings"
	"testing"

	"github.com/m0ntbl4ck/voltia/internal/domain"
)

func TestParseMeters(t *testing.T) {
	yaml := "meters:\n  - meter_id: M-101\n    name: Compresor\n    location: Nave 1\n"
	got, err := ParseMeters(strings.NewReader(yaml))
	if err != nil {
		t.Fatal(err)
	}
	want := domain.Meter{MeterID: "M-101", Name: "Compresor", Location: "Nave 1"}
	if len(got) != 1 || got[0] != want {
		t.Errorf("got %+v, want [%+v]", got, want)
	}
}

func TestParseMetersRejectsBadFiles(t *testing.T) {
	entry := "  - meter_id: M-101\n    name: a\n    location: b\n"
	cases := map[string]string{
		"empty":           "",
		"no meters":       "meters: []\n",
		"missing name":    "meters:\n  - meter_id: M-101\n    location: b\n",
		"missing id":      "meters:\n  - name: a\n    location: b\n",
		"duplicate id":    "meters:\n" + entry + entry,
		"unknown field":   "meters:\n" + entry + "    floor: 3\n",
		"not a yaml list": "meters: nope\n",
	}
	for name, yaml := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseMeters(strings.NewReader(yaml)); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestShippedMetersFile(t *testing.T) {
	f, err := os.Open("../../../data/meters.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got, err := ParseMeters(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 12 {
		t.Fatalf("got %d meters, want 12", len(got))
	}
	if got[0].MeterID != "M-101" || got[11].MeterID != "M-112" {
		t.Errorf("ids run from %s to %s, want M-101 to M-112", got[0].MeterID, got[11].MeterID)
	}
}
