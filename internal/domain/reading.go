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
)

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
	}
	return 0
}
