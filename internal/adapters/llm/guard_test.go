package llm

import (
	"strings"
	"testing"
	"time"

	"github.com/m0ntbl4ck/voltia/internal/domain"
)

func plantTime(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/Bogota")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func surge() domain.Evidence {
	return domain.Evidence{
		MeterID:      "M-109",
		Confidence:   0.988,
		EpisodeStart: time.Date(2026, 9, 12, 19, 0, 0, 0, time.UTC), // 14:00 in Bogota
		EpisodeEnd:   time.Date(2026, 9, 15, 4, 0, 0, 0, time.UTC),
		DurationH:    58,
		VariationPct: 110.5,
		ExcessKWh:    2825.04,
		Signals: []domain.SignalEvidence{
			{Variable: domain.Consumption, Hours: 58, Observed: 92.772, Expected: 44.066, MeanZ: 21.03},
			{Variable: domain.PowerFactor, Hours: 58, Observed: 0.74, Expected: 0.94, MeanZ: -10.2},
		},
	}
}

func TestCheckNumbersAcceptsWhatTheEvidenceHolds(t *testing.T) {
	loc := plantTime(t)
	texts := []string{
		"El consumo subió un 110,5% y suma 2.825 kWh de exceso.",
		"Pasó de 44,1 kWh a 92,8 kWh durante 58 horas.",
		"El factor de potencia bajó de 0,94 a 0,74.",
		"Desde el 12 de septiembre a las 14:00 en M-109.",
		"La confianza es del 99%.",
		"Bajó -10,2 desviaciones.",
		"Sin cifras en este texto.",
		"The excess is 2,825.0 kWh and 110.5 percent.",
	}
	for _, text := range texts {
		if err := CheckNumbers(text, surge(), loc); err != nil {
			t.Errorf("%q: %v", text, err)
		}
	}
}

func TestCheckNumbersRejectsWhatItInvents(t *testing.T) {
	loc := plantTime(t)
	tests := []struct {
		name, text, offender string
	}{
		{"wrong figure", "El consumo subió un 120,5%.", "120,5"},
		{"rounded past its own decimals", "Subió un 110,6%.", "110,6"},
		{"invented duration", "Lleva 3 días así.", "3"},
		{"wrong hour", "Empezó a las 15:00.", "15"},
		{"wrong day", "El 13 de septiembre.", "13"},
		{"wrong grouped figure", "Exceso de 2.830 kWh.", "2.830"},
		{"a meter code does not hide a number next to it", "En M-109, 7 alarmas.", "7"},
	}
	for _, tt := range tests {
		err := CheckNumbers(tt.text, surge(), loc)
		if err == nil {
			t.Errorf("%s: %q was accepted", tt.name, tt.text)
			continue
		}
		if !strings.Contains(err.Error(), tt.offender) {
			t.Errorf("%s: error %q does not name %q", tt.name, err, tt.offender)
		}
	}
}

func TestCheckNumbersReadsHoursInThePlantZone(t *testing.T) {
	// 19:00 UTC is 14:00 in Bogota: the text speaks plant time.
	if err := CheckNumbers("A las 14:00.", surge(), plantTime(t)); err != nil {
		t.Error(err)
	}
	if err := CheckNumbers("A las 19:00.", surge(), plantTime(t)); err == nil {
		t.Error("the UTC hour was accepted as plant time")
	}
}

func TestCheckNumbersReportsEveryOffender(t *testing.T) {
	err := CheckNumbers("Subió 130,5% en 9 horas.", surge(), plantTime(t))
	if err == nil || !strings.Contains(err.Error(), "130,5") || !strings.Contains(err.Error(), "9") {
		t.Errorf("error = %v, want both 130,5 and 9", err)
	}
}

func TestParseNumber(t *testing.T) {
	tests := []struct {
		in       string
		value    float64
		decimals int
	}{
		{"110,5", 110.5, 1},
		{"0,74", 0.74, 2},
		{"2.825", 2825, 0},
		{"1.234.567", 1234567, 0},
		{"2.825,5", 2825.5, 1},
		{"2,825.5", 2825.5, 1},
		{"0.74", 0.74, 2},
		{"58", 58, 0},
		{"14", 14, 0},
	}
	for _, tt := range tests {
		v, d := parseNumber(tt.in)
		if v != tt.value || d != tt.decimals {
			t.Errorf("parseNumber(%q) = %v, %d; want %v, %d", tt.in, v, d, tt.value, tt.decimals)
		}
	}
}
