package scoring

import (
	"math"

	"github.com/m0ntbl4ck/voltia/internal/analysis/classify"
	"github.com/m0ntbl4ck/voltia/internal/domain"
)

// PriorityBreakdown shows where the points of a priority came from.
type PriorityBreakdown struct {
	Severity float64 `json:"severity"`
	Type     float64 `json:"type"`
	Impact   float64 `json:"impact"`
	Recency  float64 `json:"recency"`
}

func (b PriorityBreakdown) Total() int {
	return int(math.Round(math.Min(100, b.Severity+b.Type+b.Impact+b.Recency)))
}

func priority(res classify.Result, sev domain.Severity, ev evidence, cfg Config) PriorityBreakdown {
	b := PriorityBreakdown{Severity: cfg.SeverityPoints[sev], Type: cfg.TypePoints[res.Type]}
	if cfg.ImpactFullKWh > 0 {
		b.Impact = cfg.ImpactMaxPoints * math.Min(1, math.Max(0, ev.excessKWh)/cfg.ImpactFullKWh)
	}
	if res.Episode.Open {
		b.Recency = cfg.RecencyPoints
	}
	return b
}
