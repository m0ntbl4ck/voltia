package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/m0ntbl4ck/voltia/internal/domain"
)

// Inserted counts the rows a seed run added. Rows that were already there
// are skipped and do not count.
type Inserted struct {
	Meters, Readings, Events int
}

// Seed stores the dataset in one transaction. It is idempotent: running it
// again over the same data inserts nothing.
func Seed(ctx context.Context, db *sql.DB, meters []domain.Meter, readings []domain.Reading, events []domain.Event) (Inserted, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Inserted{}, err
	}
	defer tx.Rollback()

	var out Inserted
	if out.Meters, err = insertMeters(ctx, tx, meters); err != nil {
		return Inserted{}, fmt.Errorf("seed meters: %w", err)
	}
	if out.Readings, err = insertReadings(ctx, tx, readings); err != nil {
		return Inserted{}, fmt.Errorf("seed readings: %w", err)
	}
	if out.Events, err = insertEvents(ctx, tx, events); err != nil {
		return Inserted{}, fmt.Errorf("seed events: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Inserted{}, err
	}
	return out, nil
}

func insertMeters(ctx context.Context, tx *sql.Tx, meters []domain.Meter) (int, error) {
	ids, names, locations := make([]string, len(meters)), make([]string, len(meters)), make([]string, len(meters))
	for i, m := range meters {
		ids[i], names[i], locations[i] = m.MeterID, m.Name, m.Location
	}
	return rowsAffected(tx.ExecContext(ctx, `
		INSERT INTO meters (meter_id, name, location)
		SELECT * FROM unnest($1::text[], $2::text[], $3::text[])
		ON CONFLICT (meter_id) DO NOTHING`, ids, names, locations))
}

func insertReadings(ctx context.Context, tx *sql.Tx, readings []domain.Reading) (int, error) {
	n := len(readings)
	ids, ts, status := make([]string, n), make([]time.Time, n), make([]string, n)
	kwh, volts, amps, pf := make([]float64, n), make([]float64, n), make([]float64, n), make([]float64, n)
	for i, r := range readings {
		ids[i], ts[i], status[i] = r.MeterID, r.Timestamp, r.Status
		kwh[i], volts[i], amps[i], pf[i] = r.ConsumptionKWh, r.VoltageV, r.CurrentA, r.PowerFactor
	}
	return rowsAffected(tx.ExecContext(ctx, `
		INSERT INTO readings (meter_id, ts, consumption_kwh, voltage_v, current_a, power_factor, status)
		SELECT * FROM unnest($1::text[], $2::timestamptz[], $3::float8[], $4::float8[], $5::float8[], $6::float8[], $7::text[])
		ON CONFLICT (meter_id, ts) DO NOTHING`, ids, ts, kwh, volts, amps, pf, status))
}

func insertEvents(ctx context.Context, tx *sql.Tx, events []domain.Event) (int, error) {
	n := len(events)
	ids, ts, types, descriptions := make([]string, n), make([]time.Time, n), make([]string, n), make([]string, n)
	for i, e := range events {
		ids[i], ts[i], types[i], descriptions[i] = e.MeterID, e.Timestamp, string(e.Type), e.Description
	}
	return rowsAffected(tx.ExecContext(ctx, `
		INSERT INTO events (meter_id, ts, type, description)
		SELECT * FROM unnest($1::text[], $2::timestamptz[], $3::text[], $4::text[])
		ON CONFLICT (meter_id, ts, type) DO NOTHING`, ids, ts, types, descriptions))
}

func rowsAffected(res sql.Result, err error) (int, error) {
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}
