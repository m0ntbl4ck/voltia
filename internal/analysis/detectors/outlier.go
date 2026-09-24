package detectors

import (
	"github.com/m0ntbl4ck/voltia/internal/analysis/baseline"
	"github.com/m0ntbl4ck/voltia/internal/domain"
)

// DetectOutliers finds single readings far beyond the baseline that neither a
// persistent shift nor a spike of the same variable already explains.
func DetectOutliers(m baseline.Meter, readings []domain.Reading, cfg Config) []Signal {
	s := newSeries(m, readings)
	var out []Signal
	for _, v := range domain.Variables {
		z := s.zScores(v)
		covered := make([]bool, len(z))
		spans := s.shiftSpans(z, cfg)
		if v == domain.Consumption {
			spans = append(spans, s.spikeSpans(z, cfg)...)
		}
		for _, sp := range spans {
			for i := sp.from; i <= sp.to; i++ {
				covered[i] = true
			}
		}
		for i, value := range z {
			if covered[i] || abs(value) <= cfg.OutlierZ {
				continue
			}
			out = append(out, s.signal(KindOutlier, v, span{from: i, to: i, sign: side(value, 0)}, z))
		}
	}
	return out
}
