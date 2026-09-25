package ports

import (
	"context"
	"encoding/json"
	"time"

	"github.com/m0ntbl4ck/voltia/internal/domain"
)

// Source is where an analysis reads its input.
type Source interface {
	Meters(ctx context.Context) ([]domain.Meter, error)
	Readings(ctx context.Context) ([]domain.Reading, error)
	Events(ctx context.Context) ([]domain.Event, error)
}

// MeterReader looks up one meter and its data without loading the rest.
// Meter returns domain.ErrNotFound for an unknown meter.
type MeterReader interface {
	Meter(ctx context.Context, meterID string) (domain.Meter, error)
	// MeterReadings lists the readings in [from, to), oldest first.
	MeterReadings(ctx context.Context, meterID string, from, to time.Time) ([]domain.Reading, error)
	MeterEvents(ctx context.Context, meterID string) ([]domain.Event, error)
}

// AnomalyReader lists stored anomalies, most urgent first.
type AnomalyReader interface {
	Anomalies(ctx context.Context, f domain.AnomalyFilter) ([]domain.Anomaly, error)
}

// Runs keeps the record of every analysis run. Lookups return
// domain.ErrNotFound when nothing matches.
type Runs interface {
	CreateRun(ctx context.Context, params json.RawMessage, stages []domain.StageState) (domain.AnalysisRun, error)
	SaveProgress(ctx context.Context, id string, status domain.RunStatus, current string, stages []domain.StageState) error
	FinishRun(ctx context.Context, id string, status domain.RunStatus, stages []domain.StageState, summary domain.RunSummary) error
	Run(ctx context.Context, id string) (domain.AnalysisRun, error)
	LatestRun(ctx context.Context) (domain.AnalysisRun, error)
	ActiveRun(ctx context.Context) (domain.AnalysisRun, error)
}

// Anomalies stores what the analysis found.
type Anomalies interface {
	// UpsertAnomalies saves the batch as a whole. An anomaly already stored
	// under the same fingerprint is refreshed and keeps its id and status.
	UpsertAnomalies(ctx context.Context, anomalies []domain.Anomaly) error
}

// Users keeps the accounts that can sign in. Lookups return domain.ErrNotFound
// when nothing matches.
type Users interface {
	UserByEmail(ctx context.Context, email string) (domain.User, error)
	UserByID(ctx context.Context, id string) (domain.User, error)
	// SaveUser creates the user, or updates the name and password hash when the email exists.
	SaveUser(ctx context.Context, email, name, passwordHash string) (domain.User, error)
}

// Explainer writes the text an operator reads about an anomaly. It works only
// from the evidence and never changes the type, severity or confidence.
// Implementations fall back to templates on their own, so an error here is
// not something the caller can recover from.
type Explainer interface {
	Explain(ctx context.Context, ev domain.Evidence) (domain.Explanation, error)
}
