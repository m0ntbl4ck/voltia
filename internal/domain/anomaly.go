package domain

// AnomalyType is what a group of signals turned out to be.
type AnomalyType string

const (
	RealAnomaly        AnomalyType = "REAL_ANOMALY"
	ExplainableAnomaly AnomalyType = "EXPLAINABLE_ANOMALY"
	FalsePositive      AnomalyType = "FALSE_POSITIVE"
	DataQuality        AnomalyType = "DATA_QUALITY"
)

type Severity string

const (
	SeverityHigh   Severity = "HIGH"
	SeverityMedium Severity = "MEDIUM"
	SeverityLow    Severity = "LOW"
)
