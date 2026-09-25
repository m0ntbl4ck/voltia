package seed

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"time"
	// The embedded tz database lets America/Bogota resolve on images without one.
	_ "time/tzdata"

	"github.com/m0ntbl4ck/voltia/internal/domain"
)

const (
	readingLayout = "2006-01-02 15:04:05"
	eventLayout   = "2006-01-02 15:04"
)

// ParseReadings reads readings.csv and interprets every timestamp in loc.
func ParseReadings(r io.Reader, loc *time.Location) ([]domain.Reading, error) {
	rows, col, err := readTable(r, "meter_id", "timestamp", "consumption_kwh", "voltage_v", "current_a", "power_factor", "status")
	if err != nil {
		return nil, err
	}
	out := make([]domain.Reading, 0, len(rows))
	for i, row := range rows {
		ts, err := time.ParseInLocation(readingLayout, row[col["timestamp"]], loc)
		if err != nil {
			return nil, fmt.Errorf("readings row %d: %w", i+2, err)
		}
		reading := domain.Reading{MeterID: row[col["meter_id"]], Timestamp: ts, Status: row[col["status"]]}
		for name, dst := range map[string]*float64{
			"consumption_kwh": &reading.ConsumptionKWh,
			"voltage_v":       &reading.VoltageV,
			"current_a":       &reading.CurrentA,
			"power_factor":    &reading.PowerFactor,
		} {
			if *dst, err = strconv.ParseFloat(row[col[name]], 64); err != nil {
				return nil, fmt.Errorf("readings row %d, %s: %w", i+2, name, err)
			}
		}
		out = append(out, reading)
	}
	return out, nil
}

// ParseEvents reads events.csv and interprets every timestamp in loc.
func ParseEvents(r io.Reader, loc *time.Location) ([]domain.Event, error) {
	rows, col, err := readTable(r, "meter_id", "event_timestamp", "event_type", "description")
	if err != nil {
		return nil, err
	}
	out := make([]domain.Event, 0, len(rows))
	for i, row := range rows {
		ts, err := time.ParseInLocation(eventLayout, row[col["event_timestamp"]], loc)
		if err != nil {
			return nil, fmt.Errorf("events row %d: %w", i+2, err)
		}
		out = append(out, domain.Event{
			MeterID:     row[col["meter_id"]],
			Timestamp:   ts,
			Type:        domain.EventType(row[col["event_type"]]),
			Description: row[col["description"]],
			Duration:    domain.EventDuration(row[col["description"]]),
		})
	}
	return out, nil
}

// readTable returns the data rows and a column index by header name, failing
// when a required column is missing.
func readTable(r io.Reader, required ...string) ([][]string, map[string]int, error) {
	all, err := csv.NewReader(r).ReadAll()
	if err != nil {
		return nil, nil, err
	}
	if len(all) == 0 {
		return nil, nil, fmt.Errorf("empty file")
	}
	col := make(map[string]int, len(all[0]))
	for i, name := range all[0] {
		col[name] = i
	}
	for _, name := range required {
		if _, ok := col[name]; !ok {
			return nil, nil, fmt.Errorf("missing column %q", name)
		}
	}
	return all[1:], col, nil
}
