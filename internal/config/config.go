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
	return Config{
		Port:        envOr("PORT", "8080"),
		DatabaseURL: url,
		PlantTZ:     loc,
	}, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
