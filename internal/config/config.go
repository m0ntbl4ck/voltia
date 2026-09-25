// Package config reads the runtime settings from the environment.
package config

import (
	"errors"
	"fmt"
	"os"
	"time"
)

// Config holds the settings the server needs to start.
type Config struct {
	Port        string
	DatabaseURL string
	PlantTZ     *time.Location
	// StageDelay pauses after each analysis stage so the progress is readable on screen.
	StageDelay time.Duration
	// JWTSecret signs the session tokens.
	JWTSecret  string
	SessionTTL time.Duration
	// LLMProvider is gemini or template. Without an API key gemini falls back to template.
	LLMProvider  string
	LLMModel     string
	GeminiAPIKey string
	// Demo is the account created at startup, or the zero value when none is configured.
	Demo DemoUser
}

// DemoUser is an account the server creates or updates on every start.
type DemoUser struct {
	Email    string
	Name     string
	Password string
}

// Load reads the settings from the environment. DATABASE_URL is required;
// the rest have defaults.
func Load() (Config, error) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return Config{}, errors.New("DATABASE_URL is not set")
	}
	tz := envOr("PLANT_TZ", "America/Bogota")
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return Config{}, fmt.Errorf("PLANT_TZ %q: %w", tz, err)
	}
	delay := envOr("ANALYSIS_STAGE_DELAY", "300ms")
	stageDelay, err := time.ParseDuration(delay)
	if err != nil || stageDelay < 0 {
		return Config{}, fmt.Errorf("ANALYSIS_STAGE_DELAY %q must be a duration of zero or more, like 300ms", delay)
	}
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		return Config{}, errors.New("JWT_SECRET is not set; generate one with: openssl rand -hex 32")
	}
	ttl := envOr("SESSION_TTL", "12h")
	sessionTTL, err := time.ParseDuration(ttl)
	if err != nil || sessionTTL <= 0 {
		return Config{}, fmt.Errorf("SESSION_TTL %q must be a positive duration, like 12h", ttl)
	}
	demo := DemoUser{Email: os.Getenv("DEMO_EMAIL"), Name: envOr("DEMO_NAME", "Demo"), Password: os.Getenv("DEMO_PASSWORD")}
	if (demo.Email == "") != (demo.Password == "") {
		return Config{}, errors.New("DEMO_EMAIL and DEMO_PASSWORD must be set together")
	}
	provider := envOr("LLM_PROVIDER", "gemini")
	if provider != "gemini" && provider != "template" {
		return Config{}, fmt.Errorf("LLM_PROVIDER %q must be gemini or template", provider)
	}
	return Config{
		LLMProvider:  provider,
		LLMModel:     envOr("LLM_MODEL", "gemini-3.8-flash"),
		GeminiAPIKey: os.Getenv("GEMINI_API_KEY"),
		Port:         envOr("PORT", "8080"),
		DatabaseURL:  url,
		PlantTZ:      loc,
		StageDelay:   stageDelay,
		JWTSecret:    secret,
		SessionTTL:   sessionTTL,
		Demo:         demo,
	}, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
