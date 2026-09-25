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

// AnomalyStatus is where an anomaly sits in its life cycle.
type AnomalyStatus string

const (
	StatusOpen         AnomalyStatus = "OPEN"
	StatusAcknowledged AnomalyStatus = "ACKNOWLEDGED"
	StatusResolved     AnomalyStatus = "RESOLVED"
	StatusDismissed    AnomalyStatus = "DISMISSED"
)

// ExplanationSource says who wrote the explanation of an anomaly.
type ExplanationSource string

const (
	SourceTemplate ExplanationSource = "TEMPLATE"
	SourceLLM      ExplanationSource = "LLM"
)
