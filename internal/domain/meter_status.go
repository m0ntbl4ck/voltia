package domain

// MeterStatus is the state of a meter, derived from its unresolved anomalies
// and never stored, so there is one source of truth.
type MeterStatus string

const (
	MeterOK       MeterStatus = "ok"
	MeterAlert    MeterStatus = "alert"
	MeterCritical MeterStatus = "critical"
)

// Unresolved reports whether the anomaly still needs attention: acknowledged
// ones count, since an inspection has been ordered but nothing is fixed yet.
func (a Anomaly) Unresolved() bool {
	return a.Status == StatusOpen || a.Status == StatusAcknowledged
}

// StatusOf derives the state of a meter from its anomalies. It is critical
// with a high severity real anomaly, alert with a data quality problem or any
// medium or higher anomaly that is not a false positive, and ok otherwise.
// Resolved and dismissed anomalies are ignored.
func StatusOf(anomalies []Anomaly) MeterStatus {
	status := MeterOK
	for _, a := range anomalies {
		if !a.Unresolved() {
			continue
		}
		switch {
		case a.Type == RealAnomaly && a.Severity == SeverityHigh:
			return MeterCritical
		case a.Type == DataQuality, a.Type != FalsePositive && a.Severity != SeverityLow:
			status = MeterAlert
		}
	}
	return status
}

// SeverityRank orders severities for sorting: high above medium above low,
// and no severity at all below everything.
func SeverityRank(s Severity) int {
	switch s {
	case SeverityHigh:
		return 3
	case SeverityMedium:
		return 2
	case SeverityLow:
		return 1
	}
	return 0
}
