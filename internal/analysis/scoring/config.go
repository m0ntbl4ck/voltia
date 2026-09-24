package scoring

import "github.com/m0ntbl4ck/voltia/internal/domain"

// Config holds every number the scoring uses, so none is buried in the code.
type Config struct {
	// HighVariationPct and MediumVariationPct are the consumption changes, in
	// absolute percent, that raise the severity of a real or explainable anomaly.
	HighVariationPct   float64
	MediumVariationPct float64
	// HighReadings and MediumReadings do the same for data quality, counted in
	// invalid readings.
	HighReadings   int
	MediumReadings int

	SeverityPoints map[domain.Severity]float64
	TypePoints     map[domain.AnomalyType]float64
	// ImpactMaxPoints is what an episode earns once its excess consumption
	// reaches ImpactFullKWh. Less excess earns a proportional share.
	ImpactMaxPoints float64
	ImpactFullKWh   float64
	// RecencyPoints go to an episode that reaches the last reading.
	RecencyPoints float64

	Confidence ConfidenceConfig
}

func DefaultConfig() Config {
	return Config{
		HighVariationPct:   50,
		MediumVariationPct: 20,
		HighReadings:       5,
		MediumReadings:     2,
		SeverityPoints: map[domain.Severity]float64{
			domain.SeverityHigh: 50, domain.SeverityMedium: 25, domain.SeverityLow: 5,
		},
		TypePoints: map[domain.AnomalyType]float64{
			domain.RealAnomaly: 25, domain.DataQuality: 15, domain.ExplainableAnomaly: 5, domain.FalsePositive: 0,
		},
		ImpactMaxPoints: 15,
		ImpactFullKWh:   2500,
		RecencyPoints:   10,
		Confidence:      DefaultConfidenceConfig(),
	}
}
