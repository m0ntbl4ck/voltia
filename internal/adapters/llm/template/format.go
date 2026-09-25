package template

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/m0ntbl4ck/voltia/internal/domain"
)

var months = [...]string{
	"enero", "febrero", "marzo", "abril", "mayo", "junio",
	"julio", "agosto", "septiembre", "octubre", "noviembre", "diciembre",
}

// num writes v the way the interface does in es-CO: a dot groups thousands
// and a comma marks the decimals, as in 2.825 or 110,5.
func num(v float64, decimals int) string {
	s := strconv.FormatFloat(math.Abs(v), 'f', decimals, 64)
	whole, frac, hasFrac := strings.Cut(s, ".")
	var b strings.Builder
	for i, c := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(c)
	}
	if hasFrac {
		b.WriteByte(',')
		b.WriteString(frac)
	}
	return b.String()
}

// moment is a date and hour in the plant's time zone: "12 de septiembre a las 14:00".
func moment(t time.Time, loc *time.Location) string {
	t = t.In(loc)
	return strconv.Itoa(t.Day()) + " de " + months[t.Month()-1] + " a las " + t.Format("15:04")
}

// hours writes a count of hours with its unit.
func hours(h float64) string {
	unit := " horas"
	if math.Abs(h-1) < 1e-9 {
		unit = " hora"
	}
	if h == math.Trunc(h) {
		return num(h, 0) + unit
	}
	return num(h, 1) + unit
}

// measure is how one variable is named, in what unit, and to how many decimals.
type measure struct {
	label    string
	unit     string
	decimals int
}

func measureOf(v domain.Variable) measure {
	switch v {
	case domain.Consumption:
		return measure{"el consumo", " kWh", 1}
	case domain.Voltage:
		return measure{"el voltaje", " V", 1}
	case domain.Current:
		return measure{"la corriente", " A", 1}
	case domain.PowerFactor:
		return measure{"el factor de potencia", "", 2}
	}
	return measure{string(v), "", 2}
}

func (m measure) value(v float64) string { return num(v, m.decimals) + m.unit }

// sentence upper-cases the first letter so a label can open a sentence.
func sentence(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// moved is the verb for a change in direction d.
func moved(d int) string {
	switch {
	case d > 0:
		return "subió"
	case d < 0:
		return "bajó"
	}
	return "cambió"
}
