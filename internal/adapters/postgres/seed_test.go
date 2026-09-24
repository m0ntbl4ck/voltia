package postgres

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/m0ntbl4ck/voltia/internal/adapters/seed"
	"github.com/m0ntbl4ck/voltia/internal/domain"
)

func migratedDB(t *testing.T) *sql.DB {
	t.Helper()
	db := openScratchDB(t)
	if err := Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return db
}

func count(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSeedShippedDatasetIsIdempotent(t *testing.T) {
	db := migratedDB(t)
	loc, err := time.LoadLocation("America/Bogota")
	if err != nil {
		t.Fatal(err)
	}
	ds, err := seed.Load(os.DirFS("../../../data"), loc)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	first, err := Seed(ctx, db, ds.Meters, ds.Readings, ds.Events)
	if err != nil {
		t.Fatal(err)
	}
	if want := (Inserted{Meters: 12, Readings: 4032, Events: 4}); first != want {
		t.Errorf("first run inserted %+v, want %+v", first, want)
	}
	second, err := Seed(ctx, db, ds.Meters, ds.Readings, ds.Events)
	if err != nil {
		t.Fatal(err)
	}
	if second != (Inserted{}) {
		t.Errorf("second run inserted %+v, want nothing", second)
	}
	if n := count(t, db, "readings"); n != 4032 {
		t.Errorf("readings = %d, want 4032", n)
	}
}

func TestSeedKeepsTheInstantOfEachReading(t *testing.T) {
	db := migratedDB(t)
	loc, err := time.LoadLocation("America/Bogota")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 9, 1, 6, 0, 0, 0, loc)
	_, err = Seed(context.Background(), db,
		[]domain.Meter{{MeterID: "M-1", Name: "a", Location: "b"}},
		[]domain.Reading{{MeterID: "M-1", Timestamp: at, ConsumptionKWh: 28.8, VoltageV: 221.64, CurrentA: 132.34, PowerFactor: 0.945, Status: "OK"}},
		nil)
	if err != nil {
		t.Fatal(err)
	}
	var got time.Time
	var kwh float64
	if err := db.QueryRow("SELECT ts, consumption_kwh FROM readings").Scan(&got, &kwh); err != nil {
		t.Fatal(err)
	}
	if !got.Equal(at) {
		t.Errorf("ts = %v, want the instant %v", got, at)
	}
	if kwh != 28.8 {
		t.Errorf("consumption_kwh = %v, want 28.8", kwh)
	}
}

func TestSeedRollsBackWhenARowIsRejected(t *testing.T) {
	db := migratedDB(t)
	at := time.Date(2026, 9, 1, 6, 0, 0, 0, time.UTC)
	_, err := Seed(context.Background(), db,
		[]domain.Meter{{MeterID: "M-1", Name: "a", Location: "b"}},
		[]domain.Reading{{MeterID: "M-404", Timestamp: at, Status: "OK"}},
		nil)
	if err == nil {
		t.Fatal("a reading for an unknown meter must fail")
	}
	if n := count(t, db, "meters"); n != 0 {
		t.Errorf("meters = %d after a failed seed, want 0", n)
	}
}
