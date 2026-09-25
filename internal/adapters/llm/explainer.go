package llm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/m0ntbl4ck/voltia/internal/domain"
	"github.com/m0ntbl4ck/voltia/internal/ports"
)

// DefaultTimeout is how long one model call may take before the template is used.
const DefaultTimeout = 20 * time.Second

// Generator is the transport to one language model. It sends the two prompts
// and returns the JSON text of the answer; everything else is the Explainer's.
type Generator interface {
	Provider() string
	Model() string
	Generate(ctx context.Context, system, user string) (string, error)
}

// Options are the parts of an Explainer other than its generator.
type Options struct {
	// Fallback writes the text when the model is skipped or fails. Required.
	Fallback ports.Explainer
	// Cache is optional; without it every run asks the model again.
	Cache ports.ExplanationCache
	// Loc is the plant time zone the text is written in. Required.
	Loc     *time.Location
	Timeout time.Duration
	Logger  *slog.Logger
}

// Explainer asks a language model to write the explanation and checks what
// comes back. Whatever goes wrong, the operator still gets the template text.
type Explainer struct {
	gen Generator
	opt Options
}

// New returns an Explainer that uses gen and the options' fallback and cache.
func New(gen Generator, opt Options) *Explainer {
	if opt.Timeout <= 0 {
		opt.Timeout = DefaultTimeout
	}
	if opt.Logger == nil {
		opt.Logger = slog.Default()
	}
	return &Explainer{gen: gen, opt: opt}
}

// Explain returns the model's text for the evidence, from the cache when the
// same evidence was explained before. It falls back to the template when the
// evidence does not need a model, the call fails or times out, or the text
// states a number the evidence does not contain. Fallback text is never
// cached, so the next run tries the model again.
func (e *Explainer) Explain(ctx context.Context, ev domain.Evidence) (domain.Explanation, error) {
	if ev.Type == domain.FalsePositive || ev.Severity == domain.SeverityLow {
		return e.opt.Fallback.Explain(ctx, ev)
	}
	key, err := e.cacheKey(ev)
	if err != nil {
		return domain.Explanation{}, err
	}
	if e.opt.Cache != nil {
		exp, ok, err := e.opt.Cache.CachedExplanation(ctx, key)
		switch {
		case err != nil:
			e.opt.Logger.Warn("explanation cache read failed", "meter", ev.MeterID, "err", err)
		case ok:
			return exp, nil
		}
	}
	exp, err := e.generate(ctx, ev)
	if err != nil {
		if ctx.Err() != nil {
			return domain.Explanation{}, ctx.Err()
		}
		e.opt.Logger.Warn("language model explanation discarded, using the template",
			"meter", ev.MeterID, "provider", e.gen.Provider(), "err", err)
		return e.opt.Fallback.Explain(ctx, ev)
	}
	if e.opt.Cache != nil {
		if err := e.opt.Cache.CacheExplanation(ctx, key, e.gen.Provider(), exp); err != nil {
			e.opt.Logger.Warn("explanation cache write failed", "meter", ev.MeterID, "err", err)
		}
	}
	return exp, nil
}

func (e *Explainer) generate(ctx context.Context, ev domain.Evidence) (domain.Explanation, error) {
	user, err := userPrompt(ev, e.opt.Loc)
	if err != nil {
		return domain.Explanation{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, e.opt.Timeout)
	defer cancel()
	text, err := e.gen.Generate(ctx, SystemPrompt, user)
	if err != nil {
		return domain.Explanation{}, err
	}
	var d draft
	if err := json.Unmarshal([]byte(text), &d); err != nil {
		return domain.Explanation{}, fmt.Errorf("the answer is not the expected JSON: %w", err)
	}
	if err := d.check(); err != nil {
		return domain.Explanation{}, err
	}
	all := strings.Join(append([]string{d.Summary, d.Reason, d.RecommendedAction}, d.InvestigationSteps...), "\n")
	if err := CheckNumbers(all, ev, e.opt.Loc); err != nil {
		return domain.Explanation{}, err
	}
	return domain.Explanation{
		Summary: d.Summary, Reason: d.Reason, RecommendedAction: d.RecommendedAction,
		InvestigationSteps: d.InvestigationSteps, Source: domain.SourceLLM, Model: e.gen.Model(),
	}, nil
}

func (d draft) check() error {
	if strings.TrimSpace(d.Summary) == "" || strings.TrimSpace(d.Reason) == "" || strings.TrimSpace(d.RecommendedAction) == "" {
		return errors.New("the answer leaves a field empty")
	}
	if len(d.InvestigationSteps) == 0 {
		return errors.New("the answer has no investigation steps")
	}
	for _, s := range d.InvestigationSteps {
		if strings.TrimSpace(s) == "" {
			return errors.New("the answer has an empty investigation step")
		}
	}
	return nil
}

// cacheKey identifies the text a model would write: the evidence, the model
// and the prompt. Changing any of them asks again.
func (e *Explainer) cacheKey(ev domain.Evidence) (string, error) {
	raw, err := json.Marshal(ev)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s\x00%s\x00", e.gen.Provider(), e.gen.Model(), PromptVersion)
	h.Write(raw)
	return hex.EncodeToString(h.Sum(nil)), nil
}
