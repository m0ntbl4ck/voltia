package seed

import (
	"fmt"
	"io"

	"gopkg.in/yaml.v3"

	"github.com/m0ntbl4ck/voltia/internal/domain"
)

type metersFile struct {
	Meters []struct {
		MeterID  string `yaml:"meter_id"`
		Name     string `yaml:"name"`
		Location string `yaml:"location"`
	} `yaml:"meters"`
}

// ParseMeters reads meters.yaml. Every meter needs an id, a name and a
// location, and ids cannot repeat.
func ParseMeters(r io.Reader) ([]domain.Meter, error) {
	var file metersFile
	dec := yaml.NewDecoder(r)
	dec.KnownFields(true)
	if err := dec.Decode(&file); err != nil {
		return nil, fmt.Errorf("meters: %w", err)
	}
	if len(file.Meters) == 0 {
		return nil, fmt.Errorf("meters: no meters listed")
	}
	seen := make(map[string]bool, len(file.Meters))
	out := make([]domain.Meter, 0, len(file.Meters))
	for i, m := range file.Meters {
		switch {
		case m.MeterID == "", m.Name == "", m.Location == "":
			return nil, fmt.Errorf("meters entry %d: meter_id, name and location are required", i+1)
		case seen[m.MeterID]:
			return nil, fmt.Errorf("meters entry %d: duplicate meter_id %q", i+1, m.MeterID)
		}
		seen[m.MeterID] = true
		out = append(out, domain.Meter{MeterID: m.MeterID, Name: m.Name, Location: m.Location})
	}
	return out, nil
}
