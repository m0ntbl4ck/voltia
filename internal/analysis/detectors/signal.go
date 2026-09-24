package detectors

import (
	"time"

	"github.com/m0ntbl4ck/voltia/internal/domain"
)

type Kind string

const (
	KindPersistentShift    Kind = "PERSISTENT_SHIFT"
	KindSpike              Kind = "SPIKE"
	KindOutlier            Kind = "OUTLIER"
	KindElectricalRelation Kind = "ELECTRICAL_RELATION"
	KindDataQuality        Kind = "DATA_QUALITY"
	KindHourlyPattern      Kind = "HOURLY_PATTERN"
	// KindIsolationForest only backs up an episode the other detectors found.
	KindIsolationForest Kind = "ISOLATION_FOREST"
)

// Signal is one finding with the evidence behind it. Detectors never decide
// what the finding means; classification does.
type Signal struct {
	Kind Kind
	// Check names the rule that fired inside detectors with more than one.
	Check    string
	MeterID  string
	Variable domain.Variable
	Start    time.Time
	End      time.Time
	Hours    int
	// Direction is +1 above the baseline, -1 below, 0 when it has no side.
	Direction int
	// Observed and Expected are means over the signal hours.
	Observed float64
	Expected float64
	MeanZ    float64
	// Metrics are the numbers behind a finding that no other field carries,
	// named by the detector that sets them.
	Metrics map[string]float64
	// Attribution is the share of the finding each variable explains, set only
	// by the isolation forest, whose signal spans several variables at once.
	Attribution map[domain.Variable]float64
	// Open is true when the signal reaches the last reading, so it may still be going on.
	Open bool
}

// Config holds every threshold the detectors use.
type Config struct {
	ShiftZ        float64
	ShiftMinHours int
	SpikeZ        float64
	SpikeMaxHours int
	OutlierZ      float64
	// PowerFactorDrop is how far below its hourly median the power factor must sit.
	PowerFactorDrop float64
	// JumpZ is the deviation of an electrical reading that counts as a jump, and
	// JumpNeighborZ how far its two neighbouring hours may stray for it to stay isolated.
	JumpZ         float64
	JumpNeighborZ float64
	// JumpMaxConsumptionZ caps the consumption deviation: an electrical jump with
	// normal consumption points at the meter, not at the load.
	JumpMaxConsumptionZ float64
	// PatternMinCorrelation is the correlation between a day's consumption and
	// the baseline profile below which the shape of the day is off.
	PatternMinCorrelation float64
	// NightStartHour and NightEndHour bound the night: an hour is at night from
	// NightStartHour on and before NightEndHour, wrapping midnight when the
	// start is later than the end.
	NightStartHour int
	NightEndHour   int
	// NightDayTolerance is how far the night to day consumption ratio may
	// stray from the baseline's, as a fraction of it.
	NightDayTolerance float64
}

func DefaultConfig() Config {
	return Config{
		ShiftZ:              3,
		ShiftMinHours:       6,
		SpikeZ:              4,
		SpikeMaxHours:       5,
		OutlierZ:            5,
		PowerFactorDrop:     0.1,
		JumpZ:               5,
		JumpNeighborZ:       4,
		JumpMaxConsumptionZ: 3,

		PatternMinCorrelation: 0.8,
		NightStartHour:        22,
		NightEndHour:          6,
		NightDayTolerance:     0.2,
	}
}
