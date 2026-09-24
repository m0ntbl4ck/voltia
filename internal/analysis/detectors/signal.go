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
	}
}
