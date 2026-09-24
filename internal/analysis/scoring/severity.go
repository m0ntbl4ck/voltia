package scoring

import (
	"github.com/m0ntbl4ck/voltia/internal/analysis/classify"
	"github.com/m0ntbl4ck/voltia/internal/analysis/detectors"
	"github.com/m0ntbl4ck/voltia/internal/domain"
)

// severity follows the matrix of type by magnitude. An explainable anomaly is
// never high and a false positive is always low.
func severity(res classify.Result, ev evidence, cfg Config) domain.Severity {
	switch res.Type {
	case domain.FalsePositive:
		return domain.SeverityLow
	case domain.DataQuality:
		switch {
		case ev.invalidReadings >= cfg.HighReadings || res.Episode.Open:
			return domain.SeverityHigh
		case ev.invalidReadings >= cfg.MediumReadings:
			return domain.SeverityMedium
		}
		return domain.SeverityLow
	case domain.RealAnomaly:
		if ev.variationPct >= cfg.HighVariationPct || res.Episode.Has(detectors.KindElectricalRelation) {
			return domain.SeverityHigh
		}
	}
	if ev.variationPct >= cfg.MediumVariationPct {
		return domain.SeverityMedium
	}
	return domain.SeverityLow
}
