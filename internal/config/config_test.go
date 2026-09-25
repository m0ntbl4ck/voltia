package config

import (
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("JWT_SECRET", "s")
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
	t.Setenv("JWT_SECRET", "s")
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
	t.Setenv("JWT_SECRET", "s")
	t.Setenv("PLANT_TZ", "Mars/Olympus")
	if _, err := Load(); err == nil {
		t.Error("expected an error for an unknown timezone")
	}
}

func TestLoadRejectsABadStageDelay(t *testing.T) {
	for _, bad := range []string{"soon", "-1s", "300"} {
		t.Setenv("DATABASE_URL", "postgres://x")
		t.Setenv("JWT_SECRET", "s")
		t.Setenv("ANALYSIS_STAGE_DELAY", bad)
		if _, err := Load(); err == nil {
			t.Errorf("ANALYSIS_STAGE_DELAY=%q was accepted", bad)
		}
	}
}

func TestLoadRequiresTheJWTSecret(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("JWT_SECRET", "")
	if _, err := Load(); err == nil {
		t.Error("expected an error without JWT_SECRET")
	}
}

func TestLoadReadsSessionAndDemoSettings(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("JWT_SECRET", "s")
	t.Setenv("SESSION_TTL", "")
	t.Setenv("DEMO_EMAIL", "")
	t.Setenv("DEMO_PASSWORD", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SessionTTL != 12*time.Hour || cfg.JWTSecret != "s" || cfg.Demo != (DemoUser{Name: "Demo"}) {
		t.Errorf("defaults = ttl %v, secret %q, demo %+v", cfg.SessionTTL, cfg.JWTSecret, cfg.Demo)
	}

	t.Setenv("SESSION_TTL", "90m")
	t.Setenv("DEMO_EMAIL", "demo@voltia.local")
	t.Setenv("DEMO_PASSWORD", "pw")
	t.Setenv("DEMO_NAME", "Ana")
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SessionTTL != 90*time.Minute || cfg.Demo != (DemoUser{Email: "demo@voltia.local", Name: "Ana", Password: "pw"}) {
		t.Errorf("overrides = ttl %v, demo %+v", cfg.SessionTTL, cfg.Demo)
	}
}

func TestLoadRejectsAHalfConfiguredDemoUserAndABadTTL(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("JWT_SECRET", "s")
	t.Setenv("DEMO_EMAIL", "demo@voltia.local")
	t.Setenv("DEMO_PASSWORD", "")
	if _, err := Load(); err == nil {
		t.Error("a demo email without a password was accepted")
	}
	t.Setenv("DEMO_EMAIL", "")
	t.Setenv("DEMO_PASSWORD", "pw")
	if _, err := Load(); err == nil {
		t.Error("a demo password without an email was accepted")
	}
	t.Setenv("DEMO_PASSWORD", "")
	for _, bad := range []string{"forever", "0s", "-1h"} {
		t.Setenv("SESSION_TTL", bad)
		if _, err := Load(); err == nil {
			t.Errorf("SESSION_TTL=%q was accepted", bad)
		}
	}
}

func TestLoadReadsTheLanguageModelSettings(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("JWT_SECRET", "s")
	t.Setenv("LLM_PROVIDER", "")
	t.Setenv("LLM_MODEL", "")
	t.Setenv("GEMINI_API_KEY", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LLMProvider != "gemini" || cfg.LLMModel != "gemini-3.8-flash" || cfg.GeminiAPIKey != "" {
		t.Errorf("defaults = %q %q %q", cfg.LLMProvider, cfg.LLMModel, cfg.GeminiAPIKey)
	}

	t.Setenv("LLM_PROVIDER", "template")
	t.Setenv("LLM_MODEL", "other-model")
	t.Setenv("GEMINI_API_KEY", "key")
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LLMProvider != "template" || cfg.LLMModel != "other-model" || cfg.GeminiAPIKey != "key" {
		t.Errorf("overrides = %q %q %q", cfg.LLMProvider, cfg.LLMModel, cfg.GeminiAPIKey)
	}
}

func TestLoadRejectsAnUnknownProvider(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("JWT_SECRET", "s")
	t.Setenv("LLM_PROVIDER", "gpt")
	if _, err := Load(); err == nil {
		t.Error("expected an error for an unknown LLM_PROVIDER")
	}
}
