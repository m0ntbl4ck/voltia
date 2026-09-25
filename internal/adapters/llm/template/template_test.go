package template

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/m0ntbl4ck/voltia/internal/adapters/llm"
	"github.com/m0ntbl4ck/voltia/internal/domain"
)

func explain(t *testing.T, ev domain.Evidence) domain.Explanation {
	t.Helper()
	exp, err := New(plantTime(t)).Explain(context.Background(), ev)
	if err != nil {
		t.Fatal(err)
	}
	return exp
}

func fullText(exp domain.Explanation) string {
	return strings.Join(append([]string{exp.Summary, exp.Reason, exp.RecommendedAction}, exp.InvestigationSteps...), "\n")
}

func TestDatasetExplanationsSayOnlyWhatTheEvidenceHolds(t *testing.T) {
	loc := plantTime(t)
	for id, ev := range datasetEvidence(t) {
		exp := explain(t, ev)
		if err := llm.CheckNumbers(fullText(exp), ev, loc); err != nil {
			t.Errorf("%s: %v\n%s", id, err, fullText(exp))
		}
		if exp.Source != domain.SourceTemplate || exp.Model != "" {
			t.Errorf("%s: source %q model %q", id, exp.Source, exp.Model)
		}
		if exp.Summary == "" || exp.Reason == "" || exp.RecommendedAction == "" || len(exp.InvestigationSteps) == 0 {
			t.Errorf("%s: incomplete explanation %+v", id, exp)
		}
		if strings.ContainsAny(fullText(exp), "—–") {
			t.Errorf("%s: the text has a long dash", id)
		}
		if again := explain(t, ev); fullText(again) != fullText(exp) {
			t.Errorf("%s: two calls gave different text", id)
		}
	}
}

func TestSurgeWithoutAnExplainingEvent(t *testing.T) {
	exp := explain(t, datasetEvidence(t)["M-109"])
	for _, want := range []string{
		"Molino (M-109)", "subió un 110,5%", "12 de septiembre a las 14:00", "58 horas y sigue en curso",
		"92,8 kWh frente a 44,1 kWh esperados (+110,5%)", "de 202,1 A a 424,7 A", "de 0,94 a 0,74",
		"2.825 kWh", "«No operational event reported»", "no explica el cambio",
		"Preguntar al responsable de Nave 1, zona de molienda si hubo un cambio de carga sin reportar",
	} {
		if !strings.Contains(fullText(exp), want) {
			t.Errorf("M-109 text lacks %q:\n%s", want, fullText(exp))
		}
	}
	if !strings.HasPrefix(exp.RecommendedAction, "Crear una orden de inspección") {
		t.Errorf("action = %q", exp.RecommendedAction)
	}
}

func TestDataQualityBlamesTheMeterNotTheLoad(t *testing.T) {
	exp := explain(t, datasetEvidence(t)["M-112"])
	for _, want := range []string{
		"Torre de enfriamiento (M-112)", "16 lecturas incoherentes", "46 horas", "El consumo se mantuvo estable",
		"registró 0,58 y lo esperado era 0,97", "«Intermittent readings and abnormal electrical jumps» lo respalda",
	} {
		if !strings.Contains(fullText(exp), want) {
			t.Errorf("M-112 text lacks %q:\n%s", want, fullText(exp))
		}
	}
	if !strings.HasPrefix(exp.RecommendedAction, "Solicitar la validación del medidor M-112") {
		t.Errorf("action = %q", exp.RecommendedAction)
	}
}

func TestExplainableChangeNamesItsEvent(t *testing.T) {
	exp := explain(t, datasetEvidence(t)["M-104"])
	for _, want := range []string{
		"subió un 46,5%", "«New production line activated»", "en la misma hora en que empezó el cambio", "71,4 kWh frente a 48,8 kWh",
		"Confirmar con el responsable de Nave 2, zona de envasado que «New production line activated»",
		"se mantenga cerca de 71,4 kWh; si sigue subiendo",
	} {
		if !strings.Contains(fullText(exp), want) {
			t.Errorf("M-104 text lacks %q:\n%s", want, fullText(exp))
		}
	}
	if !strings.HasPrefix(exp.RecommendedAction, "Confirmar la operación") {
		t.Errorf("action = %q", exp.RecommendedAction)
	}
}

func TestFalseAlarmSaysTheOutageExplainsIt(t *testing.T) {
	exp := explain(t, datasetEvidence(t)["M-106"])
	for _, want := range []string{
		"bajó un 79,8%", "(-79,8%)", "«Scheduled maintenance outage for 12 hours» (12 horas)", "falsa alarma", "8 de septiembre a las 00:00",
	} {
		if !strings.Contains(fullText(exp), want) {
			t.Errorf("M-106 text lacks %q:\n%s", want, fullText(exp))
		}
	}
	if !strings.HasPrefix(exp.RecommendedAction, "Descartar") {
		t.Errorf("action = %q", exp.RecommendedAction)
	}
}

func TestEveryTypeToleratesSparseEvidence(t *testing.T) {
	loc := plantTime(t)
	start := time.Date(2026, 9, 12, 19, 0, 0, 0, time.UTC)
	for _, typ := range []domain.AnomalyType{domain.RealAnomaly, domain.ExplainableAnomaly, domain.FalsePositive, domain.DataQuality} {
		ev := domain.Evidence{MeterID: "M-200", Type: typ, EpisodeStart: start, EpisodeEnd: start.Add(2 * time.Hour), DurationH: 3, Direction: "NONE"}
		exp := explain(t, ev)
		if exp.Summary == "" || exp.Reason == "" || exp.RecommendedAction == "" {
			t.Errorf("%s: empty text for sparse evidence: %+v", typ, exp)
		}
		if err := llm.CheckNumbers(fullText(exp), ev, loc); err != nil {
			t.Errorf("%s: %v\n%s", typ, err, fullText(exp))
		}
		if !strings.Contains(fullText(exp), "M-200") || strings.Contains(fullText(exp), "()") {
			t.Errorf("%s: a meter with no name should show only its code:\n%s", typ, fullText(exp))
		}
	}
}

func TestRealDropAndItsWording(t *testing.T) {
	start := time.Date(2026, 9, 12, 19, 0, 0, 0, time.UTC)
	ev := domain.Evidence{
		MeterID: "M-201", MeterName: "Compresor", Type: domain.RealAnomaly, Direction: "DOWN", VariationPct: 35.24,
		EpisodeStart: start, EpisodeEnd: start.Add(9 * time.Hour), DurationH: 10, ExcessKWh: 500,
		Signals: []domain.SignalEvidence{
			{Kind: "SPIKE", Variable: domain.Consumption, Hours: 2, Direction: -1, Observed: 12.5, Expected: 30.9, MeanZ: -12},
			{Kind: "PERSISTENT_SHIFT", Variable: domain.Consumption, Hours: 10, Direction: -1, Observed: 20, Expected: 30.9, MeanZ: -8},
		},
	}
	text := fullText(explain(t, ev))
	for _, want := range []string{"bajó un 35,2% bajo su nivel habitual", "(-35,2%)", "(10 horas)", "20,0 kWh frente a 30,9 kWh"} {
		if !strings.Contains(text, want) {
			t.Errorf("text lacks %q:\n%s", want, text)
		}
	}
	for _, unwanted := range []string{"exceso acumulado", "sigue en curso"} {
		if strings.Contains(text, unwanted) {
			t.Errorf("a finished drop should not say %q:\n%s", unwanted, text)
		}
	}
	if !strings.Contains(text, "No hay ningún evento reportado") {
		t.Errorf("no event was reported and the text does not say so:\n%s", text)
	}
}

func TestUnknownTypeIsAnError(t *testing.T) {
	_, err := New(plantTime(t)).Explain(context.Background(), domain.Evidence{Type: "SOMETHING_ELSE"})
	if err == nil {
		t.Error("expected an error for a type without a template")
	}
}

func TestNum(t *testing.T) {
	tests := []struct {
		v        float64
		decimals int
		want     string
	}{
		{110.5, 1, "110,5"},
		{0.74, 2, "0,74"},
		{2825.04, 0, "2.825"},
		{1234567.891, 1, "1.234.567,9"},
		{-79.8, 1, "79,8"},
		{999.96, 1, "1.000,0"},
		{0, 0, "0"},
		{58, 0, "58"},
	}
	for _, tt := range tests {
		if got := num(tt.v, tt.decimals); got != tt.want {
			t.Errorf("num(%v, %d) = %q, want %q", tt.v, tt.decimals, got, tt.want)
		}
	}
}

func TestMomentIsInPlantTime(t *testing.T) {
	got := moment(time.Date(2026, 9, 12, 19, 0, 0, 0, time.UTC), plantTime(t))
	if got != "12 de septiembre a las 14:00" {
		t.Errorf("moment = %q", got)
	}
	got = moment(time.Date(2026, 9, 1, 3, 0, 0, 0, time.UTC), plantTime(t))
	if got != "31 de agosto a las 22:00" {
		t.Errorf("moment across midnight = %q", got)
	}
}

func TestOffsetPhrase(t *testing.T) {
	tests := map[float64]string{
		0:    "en la misma hora en que empezó el cambio",
		0.25: "en la misma hora en que empezó el cambio",
		3:    "3 horas después del inicio",
		1:    "1 hora después del inicio",
		-2:   "2 horas antes del inicio",
		-1.5: "1,5 horas antes del inicio",
	}
	for in, want := range tests {
		if got := offsetPhrase(in); got != want {
			t.Errorf("offsetPhrase(%v) = %q, want %q", in, got, want)
		}
	}
}
