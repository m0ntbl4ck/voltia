package domain

import (
	"testing"
	"time"
)

func TestEventDuration(t *testing.T) {
	tests := []struct {
		description string
		want        time.Duration
	}{
		{"Scheduled maintenance outage for 12 hours", 12 * time.Hour},
		{"Short stop for 1 hour", time.Hour},
		{"Stop FOR 3 HOURS", 3 * time.Hour},
		{"New production line activated", 0},
		{"Thanks for 2 hoursx", 0},
		{"", 0},
	}
	for _, tt := range tests {
		if got := EventDuration(tt.description); got != tt.want {
			t.Errorf("EventDuration(%q) = %v, want %v", tt.description, got, tt.want)
		}
	}
}
