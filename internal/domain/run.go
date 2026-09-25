package domain

import (
	"encoding/json"
	"time"
)

// RunStatus is the state of an analysis run.
type RunStatus string

const (
	RunPending   RunStatus = "PENDING"
	RunRunning   RunStatus = "RUNNING"
	RunCompleted RunStatus = "COMPLETED"
	RunFailed    RunStatus = "FAILED"
)

// Active reports whether the run is still going.
func (s RunStatus) Active() bool { return s == RunPending || s == RunRunning }

// StageStatus is where one stage of a run stands.
type StageStatus string

const (
	StagePending StageStatus = "PENDING"
	StageRunning StageStatus = "RUNNING"
	StageDone    StageStatus = "DONE"
)

// StageState is the progress of one stage, as the interface shows it.
type StageState struct {
	Name       string      `json:"name"`
	Status     StageStatus `json:"status"`
	StartedAt  *time.Time  `json:"started_at,omitempty"`
	FinishedAt *time.Time  `json:"finished_at,omitempty"`
}

// MeterFailure is a meter a run could not analyse.
type MeterFailure struct {
	MeterID string `json:"meter_id"`
	Error   string `json:"error"`
}

// RunSummary is what a finished run reports. Error is set only on a failed run.
type RunSummary struct {
	Anomalies  int            `json:"anomalies"`
	ByType     map[string]int `json:"by_type"`
	BySeverity map[string]int `json:"by_severity"`
	Confidence float64        `json:"confidence"`
	Failures   []MeterFailure `json:"failures"`
	Error      string         `json:"error,omitempty"`
}

// AnalysisRun is one execution of the analysis pipeline.
type AnalysisRun struct {
	ID           string
	Status       RunStatus
	CurrentStage string
	Stages       []StageState
	Summary      *RunSummary
	// Params holds the thresholds the run used, so it can be reproduced.
	Params     json.RawMessage
	StartedAt  time.Time
	FinishedAt *time.Time
}
