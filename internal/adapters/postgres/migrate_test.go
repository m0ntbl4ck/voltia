package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

// openScratchDB connects to DATABASE_URL inside a throwaway schema, so the
// tests never touch the tables of a developer database.
func openScratchDB(t *testing.T) *sql.DB {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	cfg, err := pgx.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	admin := stdlib.OpenDB(*cfg)
	schema := fmt.Sprintf("migrate_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE SCHEMA " + schema); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	cfg.RuntimeParams["search_path"] = schema
	db := stdlib.OpenDB(*cfg)
	t.Cleanup(func() {
		db.Close()
		admin.Exec("DROP SCHEMA " + schema + " CASCADE")
		admin.Close()
	})
	return db
}

func TestMigrateAppliesAndIsRepeatable(t *testing.T) {
	db := openScratchDB(t)
	ctx := context.Background()
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("second run: %v", err)
	}
	for _, table := range []string{"users", "meters", "readings", "events", "analysis_runs", "anomalies", "anomaly_actions", "explanation_cache"} {
		var n int
		err := db.QueryRow("SELECT count(*) FROM " + table).Scan(&n)
		if err != nil {
			t.Errorf("table %s: %v", table, err)
		}
	}
}

func TestMigrationsRollBackToEmpty(t *testing.T) {
	db := openScratchDB(t)
	ctx := context.Background()
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	provider, err := newProvider(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.DownTo(ctx, 0); err != nil {
		t.Fatalf("down: %v", err)
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("up again: %v", err)
	}
}

func TestSchemaRejectsBadRows(t *testing.T) {
	db := openScratchDB(t)
	if err := Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	mustExec := func(query string, args ...any) error {
		_, err := db.Exec(query, args...)
		return err
	}
	if err := mustExec(`INSERT INTO meters (meter_id, name, location) VALUES ('M-1', 'a', 'b')`); err != nil {
		t.Fatal(err)
	}
	insertReading := `INSERT INTO readings (meter_id, ts, consumption_kwh, voltage_v, current_a, power_factor, status)
		VALUES ($1, '2026-09-01 00:00+00', 1, 220, 10, 0.9, 'OK')`
	if err := mustExec(insertReading, "M-1"); err != nil {
		t.Fatal(err)
	}

	cases := map[string]error{
		"duplicate reading":       mustExec(insertReading, "M-1"),
		"reading for no meter":    mustExec(insertReading, "M-404"),
		"event with unknown type": mustExec(`INSERT INTO events (meter_id, ts, type, description) VALUES ('M-1', now(), 'PARTY', 'x')`),
		"run with unknown status": mustExec(`INSERT INTO analysis_runs (status, params) VALUES ('DONE', '{}')`),
	}
	for name, err := range cases {
		if err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}
