package config

import "testing"

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
}

func TestLoadOverrides(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("PORT", "9000")
	t.Setenv("PLANT_TZ", "UTC")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != "9000" || cfg.PlantTZ.String() != "UTC" {
		t.Errorf("got port %q and tz %q", cfg.Port, cfg.PlantTZ)
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
