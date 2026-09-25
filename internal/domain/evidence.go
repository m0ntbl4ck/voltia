package domain

import (
	"encoding/json"
	"time"
)

// Evidence is everything known about an anomaly: the numbers the engine
// measured and how it reached its decision. It is what gets stored, shown, and
// handed to the explainer, which may not say anything it does not contain.
type Evidence struct {
	MeterID      string      `json:"meter_id"`
	MeterName    string      `json:"meter_name"`
	Location     string      `json:"location"`
	Type         AnomalyType `json:"type"`
	Severity     Severity    `json:"severity"`
	Rule         string      `json:"rule"`
	Confidence   float64     `json:"confidence"`
	EpisodeStart time.Time   `json:"episode_start"`
	EpisodeEnd   time.Time   `json:"episode_end"`
	DurationH    int         `json:"duration_hours"`
	Ongoing      bool        `json:"ongoing"`
	// Direction is UP, DOWN or NONE, the side of the consumption shift.
	Direction    string  `json:"direction"`
	VariationPct float64 `json:"variation_pct"`
	ExcessKWh    float64 `json:"excess_kwh"`
	// InvalidReadings counts the readings the data quality detector flagged.
	InvalidReadings int              `json:"invalid_readings"`
	Signals         []SignalEvidence `json:"signals"`
	Events          []EventEvidence  `json:"events"`
}

// SignalEvidence is one detector finding behind an anomaly.
type SignalEvidence struct {
	Kind        string               `json:"kind"`
	Check       string               `json:"check,omitempty"`
	Variable    Variable             `json:"variable"`
	Start       time.Time            `json:"start"`
	End         time.Time            `json:"end"`
	Hours       int                  `json:"hours"`
	Direction   int                  `json:"direction"`
	Observed    float64              `json:"observed"`
	Expected    float64              `json:"expected"`
	MeanZ       float64              `json:"mean_z"`
	Metrics     map[string]float64   `json:"metrics,omitempty"`
	Attribution map[Variable]float64 `json:"attribution,omitempty"`
}

// EventEvidence is a reported event near the episode and what it did for it.
type EventEvidence struct {
	Type        EventType `json:"type"`
	Timestamp   time.Time `json:"timestamp"`
	Description string    `json:"description"`
	DurationH   float64   `json:"duration_hours"`
	Role        string    `json:"role"`
	OffsetHours float64   `json:"offset_hours"`
}

// Explanation is the text an operator reads about an anomaly.
type Explanation struct {
	Summary            string
	Reason             string
	RecommendedAction  string
	InvestigationSteps []string
	Source             ExplanationSource
	// Model names the language model when Source is LLM.
	Model string
}

// AnomalyFilter narrows a listing of anomalies. A zero field matches everything.
type AnomalyFilter struct {
	Type     AnomalyType
	Severity Severity
	Status   AnomalyStatus
	MeterID  string
}

// Anomaly is a stored finding. Breakdowns and evidence travel as JSON because
// their shape belongs to the engine and they are always read whole.
type Anomaly struct {
	ID                  string
	MeterID             string
	Fingerprint         string
	Type                AnomalyType
	Severity            Severity
	Confidence          float64
	ConfidenceBreakdown json.RawMessage
	Priority            int
	PriorityBreakdown   json.RawMessage
	EpisodeStart        time.Time
	EpisodeEnd          time.Time
	Ongoing             bool
	Evidence            Evidence
	Explanation         Explanation
	Status              AnomalyStatus
	DetectedAt          time.Time
	LastAnalysisID      string
}
