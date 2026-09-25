package llm

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/m0ntbl4ck/voltia/internal/domain"
)

var (
	// meterID and number are matched in that order: a meter code like M-109
	// is a name, not a figure the evidence has to back up.
	meterID = regexp.MustCompile(`\bM-\d+\b`)
	number  = regexp.MustCompile(`\d+(?:[.,]\d+)*`)
	// thousands matches a dot used to group digits, as in 2.825 or 1.234.567.
	thousands = regexp.MustCompile(`^\d{1,3}(?:\.\d{3})+$`)
)

// CheckNumbers fails when text states a number the evidence does not contain.
// Every figure in an explanation must trace back to a measurement, so a model
// that invents or misremembers one is caught and the template is used instead.
// A number matches when it equals an evidence value rounded to the decimals
// the text shows; sign and percent are ignored. Dates and hours are checked
// in loc, the plant time zone the text is written in.
func CheckNumbers(text string, ev domain.Evidence, loc *time.Location) error {
	allowed, err := evidenceNumbers(ev, loc)
	if err != nil {
		return err
	}
	var missing []string
	for _, tok := range number.FindAllString(meterID.ReplaceAllString(text, ""), -1) {
		value, decimals := parseNumber(tok)
		if !backed(value, decimals, allowed) {
			missing = append(missing, tok)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("numbers not in the evidence: %s", strings.Join(missing, ", "))
	}
	return nil
}

// parseNumber reads a figure written either way (2.825,5 or 2,825.5) and
// returns its value and how many decimals it shows. A lone comma is a decimal
// mark, as in es-CO; a lone dot is one unless it groups thousands.
func parseNumber(tok string) (value float64, decimals int) {
	lastDot, lastComma := strings.LastIndex(tok, "."), strings.LastIndex(tok, ",")
	switch {
	case lastDot >= 0 && lastComma >= 0:
		sep := max(lastDot, lastComma)
		decimals = len(tok) - sep - 1
		tok = strings.NewReplacer(".", "", ",", "").Replace(tok[:sep]) + "." + tok[sep+1:]
	case lastComma >= 0:
		decimals = len(tok) - lastComma - 1
		tok = strings.ReplaceAll(tok, ",", ".")
	case lastDot >= 0 && thousands.MatchString(tok):
		tok = strings.ReplaceAll(tok, ".", "")
	case lastDot >= 0:
		decimals = len(tok) - lastDot - 1
	}
	value, _ = strconv.ParseFloat(tok, 64)
	return value, decimals
}

// backed reports whether some evidence value rounds to n at that many decimals.
func backed(n float64, decimals int, allowed []float64) bool {
	step := math.Pow(10, -float64(decimals))
	for _, v := range allowed {
		if math.Abs(math.Abs(v)-n) <= step/2+1e-9 {
			return true
		}
	}
	return false
}

// evidenceNumbers collects every number in the evidence, plus the day, hour
// and minute of each timestamp in loc, and the confidence as a percentage.
func evidenceNumbers(ev domain.Evidence, loc *time.Location) ([]float64, error) {
	raw, err := json.Marshal(ev)
	if err != nil {
		return nil, err
	}
	var tree any
	if err := json.Unmarshal(raw, &tree); err != nil {
		return nil, err
	}
	out := []float64{0, ev.Confidence * 100}
	walk(tree, loc, &out)
	return out, nil
}

func walk(node any, loc *time.Location, out *[]float64) {
	switch v := node.(type) {
	case float64:
		*out = append(*out, v)
	case string:
		// A zero time is an unset field, not a moment the text may mention.
		if t, err := time.Parse(time.RFC3339, v); err == nil && !t.IsZero() {
			t = t.In(loc)
			*out = append(*out, float64(t.Day()), float64(t.Hour()), float64(t.Minute()))
		}
	case []any:
		for _, x := range v {
			walk(x, loc, out)
		}
	case map[string]any:
		for _, x := range v {
			walk(x, loc, out)
		}
	}
}
