package llm

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/m0ntbl4ck/voltia/internal/domain"
)

type fakeGen struct {
	answer string
	err    error
	calls  int
	system string
	user   string
	block  bool
	model  string
}

func (g *fakeGen) Provider() string { return "fake" }
func (g *fakeGen) Model() string {
	if g.model != "" {
		return g.model
	}
	return "fake-1"
}
func (g *fakeGen) Generate(ctx context.Context, system, user string) (string, error) {
	g.calls++
	g.system, g.user = system, user
	if g.block {
		<-ctx.Done()
		return "", ctx.Err()
	}
	return g.answer, g.err
}

type fakeFallback struct{ calls int }

func (f *fakeFallback) Explain(context.Context, domain.Evidence) (domain.Explanation, error) {
	f.calls++
	return domain.Explanation{Summary: "plantilla", Source: domain.SourceTemplate}, nil
}

type fakeCache struct {
	stored   map[string]domain.Explanation
	readErr  error
	writeErr error
	puts     int
}

func newFakeCache() *fakeCache { return &fakeCache{stored: map[string]domain.Explanation{}} }

func (c *fakeCache) CachedExplanation(_ context.Context, key string) (domain.Explanation, bool, error) {
	exp, ok := c.stored[key]
	return exp, ok, c.readErr
}

func (c *fakeCache) CacheExplanation(_ context.Context, key, _ string, exp domain.Explanation) error {
	c.puts++
	if c.writeErr == nil {
		c.stored[key] = exp
	}
	return c.writeErr
}

const goodAnswer = `{
  "summary": "El consumo de M-109 subió un 110,5% desde el 12 de septiembre a las 14:00.",
  "reason": "Llevó 58 horas por encima de lo esperado: 92,8 kWh contra 44,1 kWh.",
  "recommended_action": "Revisar el equipo de M-109 en sitio.",
  "investigation_steps": ["Medir la corriente en el tablero.", "Comparar con el factor de potencia de 0,74."]
}`

func setup(gen *fakeGen) (*Explainer, *fakeFallback, *fakeCache) {
	fb, cache := &fakeFallback{}, newFakeCache()
	e := New(gen, Options{
		Fallback: fb, Cache: cache, Loc: time.FixedZone("plant", -5*3600),
		Timeout: 50 * time.Millisecond, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	return e, fb, cache
}

func TestExplainUsesTheModelText(t *testing.T) {
	gen := &fakeGen{answer: goodAnswer}
	e, fb, _ := setup(gen)
	exp, err := e.Explain(context.Background(), surge())
	if err != nil {
		t.Fatal(err)
	}
	if exp.Source != domain.SourceLLM || exp.Model != "fake-1" || !strings.HasPrefix(exp.Summary, "El consumo de M-109") {
		t.Errorf("explanation = %+v", exp)
	}
	if len(exp.InvestigationSteps) != 2 || fb.calls != 0 {
		t.Errorf("steps = %d, fallback calls = %d", len(exp.InvestigationSteps), fb.calls)
	}
}

func TestExplainSendsTheEvidenceInPlantTime(t *testing.T) {
	gen := &fakeGen{answer: goodAnswer}
	e, _, _ := setup(gen)
	if _, err := e.Explain(context.Background(), surge()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gen.user, "2026-09-12T14:00:00-05:00") || strings.Contains(gen.user, "19:00:00Z") {
		t.Errorf("the prompt does not carry plant time:\n%s", gen.user)
	}
	if gen.system != SystemPrompt {
		t.Error("the system prompt was changed")
	}
}

func TestExplainFallsBackWhenTheTextInventsANumber(t *testing.T) {
	gen := &fakeGen{answer: strings.Replace(goodAnswer, "110,5%", "115%", 1)}
	e, fb, cache := setup(gen)
	exp, err := e.Explain(context.Background(), surge())
	if err != nil {
		t.Fatal(err)
	}
	if exp.Source != domain.SourceTemplate || fb.calls != 1 {
		t.Errorf("source %q, fallback calls %d; want the template", exp.Source, fb.calls)
	}
	if cache.puts != 0 {
		t.Error("fallback text was cached")
	}
}

func TestExplainFallsBackOnBadAnswers(t *testing.T) {
	cases := map[string]*fakeGen{
		"error":       {err: errors.New("429 quota")},
		"not json":    {answer: "Claro, aquí está"},
		"empty field": {answer: `{"summary":"","reason":"r","recommended_action":"a","investigation_steps":["p"]}`},
		"no steps":    {answer: `{"summary":"s","reason":"r","recommended_action":"a","investigation_steps":[]}`},
		"blank step":  {answer: `{"summary":"s","reason":"r","recommended_action":"a","investigation_steps":[" "]}`},
		"timeout":     {block: true},
	}
	for name, gen := range cases {
		e, fb, cache := setup(gen)
		exp, err := e.Explain(context.Background(), surge())
		if err != nil || exp.Source != domain.SourceTemplate || fb.calls != 1 || cache.puts != 0 {
			t.Errorf("%s: %+v, %v, fallback %d, puts %d", name, exp, err, fb.calls, cache.puts)
		}
	}
}

func TestExplainDoesNotAskTheModelAboutFalsePositivesOrLowSeverity(t *testing.T) {
	falsePositive, low := surge(), surge()
	falsePositive.Type = domain.FalsePositive
	low.Severity = domain.SeverityLow
	for _, ev := range []domain.Evidence{falsePositive, low} {
		gen := &fakeGen{answer: goodAnswer}
		e, fb, _ := setup(gen)
		if _, err := e.Explain(context.Background(), ev); err != nil {
			t.Fatal(err)
		}
		if gen.calls != 0 || fb.calls != 1 {
			t.Errorf("%s/%s: model calls %d, fallback calls %d", ev.Type, ev.Severity, gen.calls, fb.calls)
		}
	}
}

func TestExplainServesTheCacheTheSecondTime(t *testing.T) {
	gen := &fakeGen{answer: goodAnswer}
	e, _, cache := setup(gen)
	first, err := e.Explain(context.Background(), surge())
	if err != nil {
		t.Fatal(err)
	}
	second, err := e.Explain(context.Background(), surge())
	if err != nil {
		t.Fatal(err)
	}
	if gen.calls != 1 || cache.puts != 1 || second.Summary != first.Summary {
		t.Errorf("model calls %d, puts %d, same text %v", gen.calls, cache.puts, second.Summary == first.Summary)
	}
}

func TestExplainAsksAgainWhenTheEvidenceChanges(t *testing.T) {
	gen := &fakeGen{answer: goodAnswer}
	e, _, _ := setup(gen)
	changed := surge()
	changed.Confidence = 0.9
	for _, ev := range []domain.Evidence{surge(), changed} {
		if _, err := e.Explain(context.Background(), ev); err != nil {
			t.Fatal(err)
		}
	}
	if gen.calls != 2 {
		t.Errorf("model calls = %d, want 2", gen.calls)
	}
}

func TestExplainSurvivesABrokenCache(t *testing.T) {
	gen := &fakeGen{answer: goodAnswer}
	e, _, cache := setup(gen)
	cache.readErr, cache.writeErr = errors.New("down"), errors.New("down")
	exp, err := e.Explain(context.Background(), surge())
	if err != nil || exp.Source != domain.SourceLLM {
		t.Errorf("got %+v, %v; want the model text", exp, err)
	}
}

func TestExplainStopsWhenTheCallerCancels(t *testing.T) {
	gen := &fakeGen{block: true}
	fb := &fakeFallback{}
	e := New(gen, Options{Fallback: fb, Loc: time.UTC, Timeout: time.Minute, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := e.Explain(ctx, surge()); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if fb.calls != 0 {
		t.Error("a cancelled run kept working through the template")
	}
}

func TestExplainAsksAgainWhenTheModelChanges(t *testing.T) {
	old, next := &fakeGen{answer: goodAnswer}, &fakeGen{answer: goodAnswer, model: "fake-2"}
	e, _, cache := setup(old)
	if _, err := e.Explain(context.Background(), surge()); err != nil {
		t.Fatal(err)
	}
	e2 := New(next, Options{Fallback: &fakeFallback{}, Cache: cache, Loc: time.FixedZone("plant", -5*3600), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	exp, err := e2.Explain(context.Background(), surge())
	if err != nil || next.calls != 1 || exp.Model != "fake-2" {
		t.Errorf("model calls %d, model %q, %v; want a fresh answer from fake-2", next.calls, exp.Model, err)
	}
}
