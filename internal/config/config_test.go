package config

import (
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("PORT", "")
	t.Setenv("PLANT_TZ", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != "8080" {
		t.Errorf("Port = %q, want 8080", cfg.Port)
	}
	if cfg.PlantTZ.String() != "America/Bogota" {
		t.Errorf("PlantTZ = %q, want America/Bogota", cfg.PlantTZ)
	}
	if cfg.DatabaseURL != "postgres://x" {
		t.Errorf("DatabaseURL = %q", cfg.DatabaseURL)
	}
	if cfg.StageDelay != 300*time.Millisecond {
		t.Errorf("StageDelay = %v, want 300ms", cfg.StageDelay)
	}
}

func TestLoadOverrides(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("PORT", "9000")
	t.Setenv("PLANT_TZ", "UTC")
	t.Setenv("ANALYSIS_STAGE_DELAY", "0s")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != "9000" || cfg.PlantTZ.String() != "UTC" || cfg.StageDelay != 0 {
		t.Errorf("got port %q, tz %q and delay %v", cfg.Port, cfg.PlantTZ, cfg.StageDelay)
	}
}

func TestLoadRequiresDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	if _, err := Load(); err == nil {
		t.Error("expected an error without DATABASE_URL")
	}
}

func TestLoadRejectsUnknownTimezone(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("PLANT_TZ", "Mars/Olympus")
	if _, err := Load(); err == nil {
		t.Error("expected an error for an unknown timezone")
	}
}

func TestLoadRejectsABadStageDelay(t *testing.T) {
	for _, bad := range []string{"soon", "-1s", "300"} {
		t.Setenv("DATABASE_URL", "postgres://x")
		t.Setenv("ANALYSIS_STAGE_DELAY", bad)
		if _, err := Load(); err == nil {
			t.Errorf("ANALYSIS_STAGE_DELAY=%q was accepted", bad)
		}
	}
}
