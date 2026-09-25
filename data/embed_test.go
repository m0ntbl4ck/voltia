package data

import (
	"testing"
	"time"

	"github.com/m0ntbl4ck/voltia/internal/adapters/seed"
)

func TestEmbeddedDatasetLoads(t *testing.T) {
	ds, err := seed.Load(FS, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if len(ds.Meters) != 12 || len(ds.Readings) != 4032 || len(ds.Events) != 4 {
		t.Errorf("got %d meters, %d readings, %d events; want 12, 4032, 4",
			len(ds.Meters), len(ds.Readings), len(ds.Events))
	}
}
