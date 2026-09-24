package domain

import "time"

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
}
