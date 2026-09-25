// Package template explains anomalies with fixed Spanish templates filled from
// the evidence. It is the explainer of last resort: it needs no network and
// says the same thing for the same evidence.
package template

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/m0ntbl4ck/voltia/internal/domain"
)

// Explainer writes explanations from templates.
type Explainer struct {
	loc *time.Location
}

// New returns an explainer that writes dates and hours in loc, the plant time zone.
func New(loc *time.Location) *Explainer { return &Explainer{loc: loc} }

// Explain returns the text for the evidence, or an error for a type it has no template for.
func (e *Explainer) Explain(_ context.Context, ev domain.Evidence) (domain.Explanation, error) {
	w := writer{ev: ev, loc: e.loc}
	var exp domain.Explanation
	switch ev.Type {
	case domain.RealAnomaly:
		exp = w.real()
	case domain.ExplainableAnomaly:
		exp = w.explainable()
	case domain.FalsePositive:
		exp = w.falsePositive()
	case domain.DataQuality:
		exp = w.dataQuality()
	default:
		return domain.Explanation{}, fmt.Errorf("no template for anomaly type %q", ev.Type)
	}
	exp.Source = domain.SourceTemplate
	return exp, nil
}

// writer holds the evidence of one anomaly while its text is composed.
type writer struct {
	ev  domain.Evidence
	loc *time.Location
}

func (w writer) real() domain.Explanation {
	who := w.who()
	var summary string
	switch w.ev.Direction {
	case "UP":
		summary = fmt.Sprintf("El consumo de %s subió un %s sobre su nivel habitual y ningún evento lo explica.", who, w.magnitude())
	case "DOWN":
		summary = fmt.Sprintf("El consumo de %s bajó un %s bajo su nivel habitual y ningún evento lo explica.", who, w.magnitude())
	default:
		summary = fmt.Sprintf("%s muestra una relación eléctrica incoherente y ningún evento la explica.", who)
	}

	parts := []string{w.consumptionSentence(), w.electricalSentences(), w.excessSentence(), w.unexplainedSentence()}
	action := fmt.Sprintf("Crear una orden de inspección para %s.", who)
	if w.ev.Ongoing {
		action = fmt.Sprintf("Crear una orden de inspección para %s: la anomalía sigue en curso.", who)
	}
	return domain.Explanation{
		Summary:            summary,
		Reason:             join(parts),
		RecommendedAction:  action,
		InvestigationSteps: w.electricalSteps(),
	}
}

func (w writer) explainable() domain.Explanation {
	who := w.who()
	link := w.eventWithRole("EXPLAINS")
	summary := fmt.Sprintf("El consumo de %s %s un %s y un evento reportado lo explica.", who, moved(w.dir()), w.magnitude())
	parts := []string{w.consumptionSentence(), w.electricalSentences()}
	steps := w.electricalSteps()
	if link != nil {
		parts = append(parts, fmt.Sprintf("Coincide con el evento «%s», reportado %s.", link.Description, offsetPhrase(link.OffsetHours)))
		steps = append([]string{fmt.Sprintf("Confirmar con el %s que «%s» explica el nuevo nivel de consumo.", w.owner(), link.Description)}, steps...)
	}
	if s := w.consumption(); s != nil {
		steps = append(steps, fmt.Sprintf("Revisar en las próximas lecturas que el consumo se mantenga cerca de %s; si sigue %s, crear una orden de inspección.",
			measureOf(domain.Consumption).value(s.Observed), trend(w.dir())))
	}
	return domain.Explanation{
		Summary:            summary,
		Reason:             join(parts),
		RecommendedAction:  fmt.Sprintf("Confirmar la operación: el cambio de consumo de %s corresponde al evento reportado.", who),
		InvestigationSteps: steps,
	}
}

func (w writer) falsePositive() domain.Explanation {
	who := w.who()
	link := w.eventWithRole("EXPLAINS")
	summary := fmt.Sprintf("El consumo de %s %s un %s, pero coincide con un evento programado y no es una falla.", who, moved(w.dir()), w.magnitude())
	parts := []string{w.consumptionSentence()}
	if link != nil {
		event := fmt.Sprintf("Coincide con el evento «%s»", link.Description)
		if link.DurationH > 0 {
			event += fmt.Sprintf(" (%s)", hours(link.DurationH))
		}
		parts = append(parts, event+", reportado "+offsetPhrase(link.OffsetHours)+", así que el motor lo descarta como falsa alarma.")
	}
	follow := "Confirmar que el consumo vuelve a su nivel habitual cuando termine el evento."
	if !w.ev.Ongoing {
		follow = "Confirmar en las próximas lecturas que el consumo se mantiene en su nivel habitual."
	}
	return domain.Explanation{
		Summary:            summary,
		Reason:             join(parts),
		RecommendedAction:  "Descartar la alerta: el evento reportado explica el cambio.",
		InvestigationSteps: []string{follow},
	}
}

func (w writer) dataQuality() domain.Explanation {
	who := w.who()
	invalid := w.ev.InvalidReadings
	stable := w.consumption() == nil
	summary := fmt.Sprintf("Las lecturas eléctricas de %s son incoherentes.", who)
	reason := fmt.Sprintf("%s registró %d lecturas incoherentes de voltaje, corriente o factor de potencia en %s.",
		who, invalid, hours(float64(w.ev.DurationH)))
	if stable {
		summary = fmt.Sprintf("Las lecturas eléctricas de %s son incoherentes mientras su consumo se mantiene estable: el problema parece estar en el medidor.", who)
		reason += " El consumo se mantuvo estable, así que el problema está en la medición y no en la carga."
	}
	parts := []string{reason, w.worstJumpSentence()}
	if link := w.eventWithRole("CORROBORATES"); link != nil {
		parts = append(parts, fmt.Sprintf("El evento reportado «%s» lo respalda.", link.Description))
	}
	return domain.Explanation{
		Summary:           summary,
		Reason:            join(parts),
		RecommendedAction: fmt.Sprintf("Solicitar la validación del medidor %s: los saltos apuntan al medidor y no a la carga.", w.ev.MeterID),
		InvestigationSteps: []string{
			"Comparar las lecturas del medidor con una medición portátil de voltaje y corriente.",
			fmt.Sprintf("Revisar las conexiones y los transformadores de medida del medidor %s.", w.ev.MeterID),
			"Verificar la comunicación del medidor: las lecturas intermitentes pueden venir del enlace de datos.",
		},
	}
}

// who names the meter: "Molino (M-109)", or just the code when it has no name.
func (w writer) who() string {
	if w.ev.MeterName == "" {
		return w.ev.MeterID
	}
	return fmt.Sprintf("%s (%s)", w.ev.MeterName, w.ev.MeterID)
}

// owner names who to ask about the equipment, without an article.
func (w writer) owner() string {
	if w.ev.Location == "" {
		return "responsable de la planta"
	}
	return "responsable de " + w.ev.Location
}

func (w writer) dir() int {
	switch w.ev.Direction {
	case "UP":
		return 1
	case "DOWN":
		return -1
	}
	return 0
}

// magnitude is the size of the consumption change without its sign, as in "110,5%".
func (w writer) magnitude() string { return num(w.ev.VariationPct, 1) + "%" }

// pct is the consumption change with its sign, as in "+110,5%".
func (w writer) pct() string {
	sign := "+"
	if w.dir() < 0 {
		sign = "-"
	}
	return sign + num(w.ev.VariationPct, 1) + "%"
}

// span says when the episode ran.
func (w writer) span() string {
	length := hours(float64(w.ev.DurationH))
	if w.ev.Ongoing {
		length += " y sigue en curso"
	}
	return "Desde el " + moment(w.ev.EpisodeStart, w.loc) + " (" + length + ")"
}

// consumption is the longest shift or spike of the consumption, or nil.
func (w writer) consumption() *domain.SignalEvidence {
	return w.best(domain.Consumption, "PERSISTENT_SHIFT", "SPIKE")
}

func (w writer) consumptionSentence() string {
	s := w.consumption()
	if s == nil {
		return sentence(w.span()) + "."
	}
	m := measureOf(domain.Consumption)
	return fmt.Sprintf("%s, el consumo horario de %s es de %s frente a %s esperados (%s).",
		w.span(), w.who(), m.value(s.Observed), m.value(s.Expected), w.pct())
}

// electricalSentences reports the current, voltage and power factor shifts.
func (w writer) electricalSentences() string {
	var out []string
	for _, v := range []domain.Variable{domain.Current, domain.Voltage, domain.PowerFactor} {
		s := w.best(v, "PERSISTENT_SHIFT", "ELECTRICAL_RELATION")
		if s == nil {
			continue
		}
		m := measureOf(v)
		out = append(out, fmt.Sprintf("%s %s de %s a %s.", sentence(m.label), moved(s.Direction), m.value(s.Expected), m.value(s.Observed)))
	}
	return strings.Join(out, " ")
}

func (w writer) electricalSteps() []string {
	var out []string
	if s := w.best(domain.Current, "PERSISTENT_SHIFT", "ELECTRICAL_RELATION"); s != nil {
		m := measureOf(domain.Current)
		out = append(out, fmt.Sprintf("Medir la corriente en sitio y compararla con los %s registrados (%s esperados).", m.value(s.Observed), m.value(s.Expected)))
	}
	if s := w.best(domain.PowerFactor, "PERSISTENT_SHIFT", "ELECTRICAL_RELATION"); s != nil {
		m := measureOf(domain.PowerFactor)
		out = append(out, fmt.Sprintf("Revisar el factor de potencia (%s frente a %s esperado): bancos de condensadores y estado del motor.", m.value(s.Observed), m.value(s.Expected)))
	}
	if s := w.best(domain.Voltage, "PERSISTENT_SHIFT", "ELECTRICAL_RELATION"); s != nil {
		m := measureOf(domain.Voltage)
		out = append(out, fmt.Sprintf("Revisar el voltaje de alimentación (%s frente a %s esperados).", m.value(s.Observed), m.value(s.Expected)))
	}
	if w.ev.Type == domain.RealAnomaly {
		out = append(out, fmt.Sprintf("Preguntar al %s si hubo un cambio de carga sin reportar.", w.owner()))
	}
	return out
}

func (w writer) excessSentence() string {
	if w.dir() <= 0 || w.ev.ExcessKWh <= 0 {
		return ""
	}
	return fmt.Sprintf("El exceso acumulado es de %s kWh.", num(w.ev.ExcessKWh, 0))
}

// unexplainedSentence says why no reported event accounts for the change.
func (w writer) unexplainedSentence() string {
	if link := w.eventWithRole("NOT_EXPLANATORY"); link != nil {
		return fmt.Sprintf("El evento reportado cerca del inicio dice «%s» y no explica el cambio.", link.Description)
	}
	return "No hay ningún evento reportado que explique el cambio."
}

// worstJumpSentence gives the most extreme isolated reading as an example.
func (w writer) worstJumpSentence() string {
	var worst *domain.SignalEvidence
	for i := range w.ev.Signals {
		s := &w.ev.Signals[i]
		if s.Kind != "DATA_QUALITY" {
			continue
		}
		if worst == nil || abs(s.MeanZ) > abs(worst.MeanZ) {
			worst = s
		}
	}
	if worst == nil {
		return ""
	}
	m := measureOf(worst.Variable)
	return fmt.Sprintf("El caso más marcado: %s registró %s y lo esperado era %s.", m.label, m.value(worst.Observed), m.value(worst.Expected))
}

// best returns the longest signal of the variable with one of the kinds; a
// tie goes to the one that strays further from the baseline.
func (w writer) best(v domain.Variable, kinds ...string) *domain.SignalEvidence {
	var best *domain.SignalEvidence
	for i := range w.ev.Signals {
		s := &w.ev.Signals[i]
		if s.Variable != v || !slices.Contains(kinds, s.Kind) {
			continue
		}
		if best == nil || s.Hours > best.Hours || (s.Hours == best.Hours && abs(s.MeanZ) > abs(best.MeanZ)) {
			best = s
		}
	}
	return best
}

func (w writer) eventWithRole(role string) *domain.EventEvidence {
	for i := range w.ev.Events {
		if w.ev.Events[i].Role == role {
			return &w.ev.Events[i]
		}
	}
	return nil
}

// offsetPhrase places an event in time against the start of the episode.
func offsetPhrase(offsetHours float64) string {
	switch {
	case offsetHours > -0.5 && offsetHours < 0.5:
		return "en la misma hora en que empezó el cambio"
	case offsetHours > 0:
		return hours(offsetHours) + " después del inicio"
	}
	return hours(-offsetHours) + " antes del inicio"
}

func trend(dir int) string {
	if dir < 0 {
		return "bajando"
	}
	return "subiendo"
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// join links the non-empty parts with a space.
func join(parts []string) string {
	kept := parts[:0:0]
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, " ")
}
