package template

import (
	"context"
	"testing"
	"time"

	"github.com/m0ntbl4ck/voltia/data"
	"github.com/m0ntbl4ck/voltia/internal/adapters/seed"
	"github.com/m0ntbl4ck/voltia/internal/analysis"
	"github.com/m0ntbl4ck/voltia/internal/app"
	"github.com/m0ntbl4ck/voltia/internal/domain"
)

func plantTime(t testing.TB) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/Bogota")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

// datasetEvidence is the evidence of the four anomalies the engine finds in
// the shipped dataset, keyed by meter.
func datasetEvidence(t *testing.T) map[string]domain.Evidence {
	t.Helper()
	ds, err := seed.Load(data.FS, plantTime(t))
	if err != nil {
		t.Fatal(err)
	}
	report, err := analysis.Run(context.Background(),
		analysis.Input{Readings: ds.Readings, Events: ds.Events}, analysis.DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	meters := map[string]domain.Meter{}
	for _, m := range ds.Meters {
		meters[m.MeterID] = m
	}
	out := map[string]domain.Evidence{}
	for _, a := range report.Anomalies {
		out[a.Episode.MeterID] = app.BuildEvidence(a, meters[a.Episode.MeterID])
	}
	return out
}
