package domain

import (
	"regexp"
	"strconv"
	"time"
)

type EventType string

const (
	EventOperationalChange EventType = "OPERATIONAL_CHANGE"
	EventScheduledOutage   EventType = "SCHEDULED_OUTAGE"
	EventDataQuality       EventType = "DATA_QUALITY"
	// EventUnknown means nobody reported a cause. It never explains an anomaly.
	EventUnknown EventType = "UNKNOWN"
)

// Event is an operational fact reported for a meter.
type Event struct {
	MeterID     string
	Timestamp   time.Time
	Type        EventType
	Description string
	// Duration is how long the event lasts, or zero when the source does not say.
	Duration time.Duration
}

// durationPattern finds the length an event states in its description, as in
// "Scheduled maintenance outage for 12 hours".
var durationPattern = regexp.MustCompile(`(?i)\bfor (\d+) hours?\b`)

// EventDuration reads the length an event states in its description, or zero
// when it states none. The source has no duration column, so it lives in the text.
func EventDuration(description string) time.Duration {
	m := durationPattern.FindStringSubmatch(description)
	if m == nil {
		return 0
	}
	hours, err := strconv.Atoi(m[1])
	if err != nil {
		return 0
	}
	return time.Duration(hours) * time.Hour
}
