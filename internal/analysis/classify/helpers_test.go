package classify

import (
	"time"

	"github.com/m0ntbl4ck/voltia/internal/analysis/detectors"
	"github.com/m0ntbl4ck/voltia/internal/domain"
)

var t0 = time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)

func hour(h int) time.Time { return t0.Add(time.Duration(h) * time.Hour) }

// sig builds a signal that starts at hour startHour and lasts hours hours.
func sig(meter string, kind detectors.Kind, v domain.Variable, startHour, hours, dir int) detectors.Signal {
	return detectors.Signal{
		Kind: kind, MeterID: meter, Variable: v,
		Start: hour(startHour), End: hour(startHour + hours - 1), Hours: hours, Direction: dir,
	}
}

func shift(meter string, startHour, hours, dir int) detectors.Signal {
	return sig(meter, detectors.KindPersistentShift, domain.Consumption, startHour, hours, dir)
}

func quality(meter string, startHour int) detectors.Signal {
	s := sig(meter, detectors.KindDataQuality, domain.Voltage, startHour, 1, 1)
	s.Check = detectors.CheckElectricalJump
	return s
}

func powerFactorDrop(meter string, startHour, hours int) detectors.Signal {
	return sig(meter, detectors.KindElectricalRelation, domain.PowerFactor, startHour, hours, -1)
}

func event(meter string, typ domain.EventType, startHour int, duration time.Duration) domain.Event {
	return domain.Event{MeterID: meter, Timestamp: hour(startHour), Type: typ, Description: string(typ), Duration: duration}
}
