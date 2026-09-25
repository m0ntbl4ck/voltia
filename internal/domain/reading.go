package domain

import "time"

// Reading is one hourly measurement. Timestamp carries the plant time zone,
// so Timestamp.Hour() is the hour the plant operators see.
type Reading struct {
	MeterID        string
	Timestamp      time.Time
	ConsumptionKWh float64
	VoltageV       float64
	CurrentA       float64
	PowerFactor    float64
	Status         string
}

// Variable names one of the four measured quantities.
type Variable string

const (
	Consumption Variable = "consumption_kwh"
	Voltage     Variable = "voltage_v"
	Current     Variable = "current_a"
	PowerFactor Variable = "power_factor"
	// PhysicalRatio is derived, not measured: the consumption over what voltage,
	// current and power factor say it should be, kWh / (V·I·FP / 1000). It sits
	// near 1 on a coherent reading and is not part of Variables.
	PhysicalRatio Variable = "physical_ratio"
)

// Variables are the four measured quantities, the ones a baseline profiles.
var Variables = []Variable{Consumption, Voltage, Current, PowerFactor}

func (r Reading) Value(v Variable) float64 {
	switch v {
	case Consumption:
		return r.ConsumptionKWh
	case Voltage:
		return r.VoltageV
	case Current:
		return r.CurrentA
	case PowerFactor:
		return r.PowerFactor
	case PhysicalRatio:
		return r.physicalRatio()
	}
	return 0
}

// physicalRatio is 0 when the electrical readings give no power to compare with.
func (r Reading) physicalRatio() float64 {
	kw := r.VoltageV * r.CurrentA * r.PowerFactor / 1000
	if kw <= 0 {
		return 0
	}
	return r.ConsumptionKWh / kw
}
