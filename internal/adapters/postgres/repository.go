package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/m0ntbl4ck/voltia/internal/adapters/postgres/sqlcgen"
	"github.com/m0ntbl4ck/voltia/internal/domain"
	"github.com/m0ntbl4ck/voltia/internal/ports"
)

// Repository stores and reads meters, readings, events, analysis runs and
// anomalies. Timestamps come back in loc, the plant time zone, so a reading's
// Hour() is the one operators see.
type Repository struct {
	db  *sql.DB
	q   *sqlcgen.Queries
	loc *time.Location
}

func NewRepository(db *sql.DB, loc *time.Location) *Repository {
	return &Repository{db: db, q: sqlcgen.New(db), loc: loc}
}

// Meters lists every meter ordered by meter_id.
func (r *Repository) Meters(ctx context.Context) ([]domain.Meter, error) {
	rows, err := r.q.ListMeters(ctx)
	if err != nil {
		return nil, err
	}
	return mapAll(rows, toMeter), nil
}

// Meter returns domain.ErrNotFound when meterID does not exist.
func (r *Repository) Meter(ctx context.Context, meterID string) (domain.Meter, error) {
	row, err := r.q.GetMeter(ctx, meterID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Meter{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Meter{}, err
	}
	return toMeter(row), nil
}

// Readings lists every reading ordered by meter and time.
func (r *Repository) Readings(ctx context.Context) ([]domain.Reading, error) {
	rows, err := r.q.ListReadings(ctx)
	if err != nil {
		return nil, err
	}
	return mapAll(rows, r.toReading), nil
}

// MeterReadings lists one meter's readings in [from, to), oldest first.
func (r *Repository) MeterReadings(ctx context.Context, meterID string, from, to time.Time) ([]domain.Reading, error) {
	rows, err := r.q.ListMeterReadings(ctx, sqlcgen.ListMeterReadingsParams{
		MeterID: meterID, FromTs: from, ToTs: to,
	})
	if err != nil {
		return nil, err
	}
	return mapAll(rows, r.toReading), nil
}

// Events lists every event ordered by time.
func (r *Repository) Events(ctx context.Context) ([]domain.Event, error) {
	rows, err := r.q.ListEvents(ctx)
	if err != nil {
		return nil, err
	}
	return mapAll(rows, r.toEvent), nil
}

// MeterEvents lists one meter's events, oldest first.
func (r *Repository) MeterEvents(ctx context.Context, meterID string) ([]domain.Event, error) {
	rows, err := r.q.ListMeterEvents(ctx, meterID)
	if err != nil {
		return nil, err
	}
	return mapAll(rows, r.toEvent), nil
}

func toMeter(m sqlcgen.Meter) domain.Meter {
	return domain.Meter{MeterID: m.MeterID, Name: m.Name, Location: m.Location}
}

func (r *Repository) toReading(x sqlcgen.Reading) domain.Reading {
	return domain.Reading{
		MeterID:        x.MeterID,
		Timestamp:      x.Ts.In(r.loc),
		ConsumptionKWh: x.ConsumptionKwh,
		VoltageV:       x.VoltageV,
		CurrentA:       x.CurrentA,
		PowerFactor:    x.PowerFactor,
		Status:         x.Status,
	}
}

func (r *Repository) toEvent(e sqlcgen.Event) domain.Event {
	return domain.Event{
		MeterID:     e.MeterID,
		Timestamp:   e.Ts.In(r.loc),
		Type:        domain.EventType(e.Type),
		Description: e.Description,
		Duration:    domain.EventDuration(e.Description),
	}
}

func mapAll[In, Out any](in []In, f func(In) Out) []Out {
	out := make([]Out, len(in))
	for i, v := range in {
		out[i] = f(v)
	}
	return out
}

var (
	_ ports.Source    = (*Repository)(nil)
	_ ports.Runs      = (*Repository)(nil)
	_ ports.Anomalies = (*Repository)(nil)
	_ ports.Users     = (*Repository)(nil)
)
