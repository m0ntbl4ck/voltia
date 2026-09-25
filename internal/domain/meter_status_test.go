package domain

import "testing"

func anomaly(t AnomalyType, s Severity, st AnomalyStatus) Anomaly {
	return Anomaly{Type: t, Severity: s, Status: st}
}

func TestStatusOf(t *testing.T) {
	tests := []struct {
		name      string
		anomalies []Anomaly
		want      MeterStatus
	}{
		{"no anomalies", nil, MeterOK},
		{"a real high anomaly", []Anomaly{anomaly(RealAnomaly, SeverityHigh, StatusOpen)}, MeterCritical},
		{"a real high anomaly already acknowledged", []Anomaly{anomaly(RealAnomaly, SeverityHigh, StatusAcknowledged)}, MeterCritical},
		{"a real medium anomaly", []Anomaly{anomaly(RealAnomaly, SeverityMedium, StatusOpen)}, MeterAlert},
		{"a real low anomaly", []Anomaly{anomaly(RealAnomaly, SeverityLow, StatusOpen)}, MeterOK},
		{"data quality of any severity", []Anomaly{anomaly(DataQuality, SeverityLow, StatusOpen)}, MeterAlert},
		{"data quality high is an alert, not critical", []Anomaly{anomaly(DataQuality, SeverityHigh, StatusOpen)}, MeterAlert},
		{"an explainable medium anomaly", []Anomaly{anomaly(ExplainableAnomaly, SeverityMedium, StatusOpen)}, MeterAlert},
		{"an explainable low anomaly", []Anomaly{anomaly(ExplainableAnomaly, SeverityLow, StatusOpen)}, MeterOK},
		{"a false positive never raises it", []Anomaly{anomaly(FalsePositive, SeverityHigh, StatusOpen)}, MeterOK},
		{"resolved anomalies are ignored", []Anomaly{anomaly(RealAnomaly, SeverityHigh, StatusResolved)}, MeterOK},
		{"dismissed anomalies are ignored", []Anomaly{anomaly(RealAnomaly, SeverityHigh, StatusDismissed)}, MeterOK},
		{"the worst wins", []Anomaly{
			anomaly(DataQuality, SeverityHigh, StatusOpen),
			anomaly(RealAnomaly, SeverityHigh, StatusOpen),
			anomaly(FalsePositive, SeverityLow, StatusOpen),
		}, MeterCritical},
		{"a critical one that is resolved leaves the alert", []Anomaly{
			anomaly(RealAnomaly, SeverityHigh, StatusResolved),
			anomaly(DataQuality, SeverityLow, StatusOpen),
		}, MeterAlert},
	}
	for _, tt := range tests {
		if got := StatusOf(tt.anomalies); got != tt.want {
			t.Errorf("%s: %s, want %s", tt.name, got, tt.want)
		}
	}
}

func TestUnresolved(t *testing.T) {
	want := map[AnomalyStatus]bool{StatusOpen: true, StatusAcknowledged: true, StatusResolved: false, StatusDismissed: false}
	for st, w := range want {
		if got := (Anomaly{Status: st}).Unresolved(); got != w {
			t.Errorf("%s: Unresolved = %v, want %v", st, got, w)
		}
	}
}

func TestSeverityRankOrdersHighFirst(t *testing.T) {
	if !(SeverityRank(SeverityHigh) > SeverityRank(SeverityMedium) &&
		SeverityRank(SeverityMedium) > SeverityRank(SeverityLow) &&
		SeverityRank(SeverityLow) > SeverityRank("")) {
		t.Error("severities are not ordered high, medium, low, none")
	}
}
