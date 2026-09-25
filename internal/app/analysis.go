package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/m0ntbl4ck/voltia/internal/analysis"
	"github.com/m0ntbl4ck/voltia/internal/domain"
	"github.com/m0ntbl4ck/voltia/internal/ports"
)

// The two stages after the engine's five. They belong to the run, not to the
// engine: the explainer writes the text and the results are stored.
const (
	StageExplanation    = "EXPLANATION"
	StageRecommendation = "RECOMMENDATION"
)

// explainWorkers bounds how many anomalies are explained at once.
const explainWorkers = 4

// AnalysisOptions tunes how runs are executed.
type AnalysisOptions struct {
	Config analysis.Config
	// StageDelay pauses after each stage so the progress can be followed on screen.
	StageDelay time.Duration
	Logger     *slog.Logger
}

// AnalysisService starts analysis runs and follows them in the background.
type AnalysisService struct {
	source    ports.Source
	runs      ports.Runs
	anomalies ports.Anomalies
	explainer ports.Explainer
	opts      AnalysisOptions

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	// mu makes "is a run active? if not, start one" a single step.
	mu sync.Mutex
}

func NewAnalysisService(source ports.Source, runs ports.Runs, anomalies ports.Anomalies, explainer ports.Explainer, opts AnalysisOptions) *AnalysisService {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &AnalysisService{
		source: source, runs: runs, anomalies: anomalies, explainer: explainer,
		opts: opts, ctx: ctx, cancel: cancel,
	}
}

// Start begins a run and returns it while it is still PENDING. When one is
// already going it returns that one instead of starting another.
func (s *AnalysisService) Start(ctx context.Context) (domain.AnalysisRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil {
		return domain.AnalysisRun{}, errors.New("analysis service is closed")
	}
	active, err := s.runs.ActiveRun(ctx)
	if err == nil {
		return active, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return domain.AnalysisRun{}, err
	}
	params, err := json.Marshal(s.opts.Config)
	if err != nil {
		return domain.AnalysisRun{}, err
	}
	run, err := s.runs.CreateRun(ctx, params, initialStages())
	if err != nil {
		return domain.AnalysisRun{}, err
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.execute(run)
	}()
	return run, nil
}

// Recover closes the runs a stopped process left active, so they do not
// block new ones. Call it once at startup.
func (s *AnalysisService) Recover(ctx context.Context) error {
	for {
		run, err := s.runs.ActiveRun(ctx)
		if errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		summary := domain.RunSummary{Error: "interrupted by a restart"}
		if err := s.runs.FinishRun(ctx, run.ID, domain.RunFailed, run.Stages, summary); err != nil {
			return err
		}
	}
}

// Close cancels the runs in progress and waits for them to record how they ended.
func (s *AnalysisService) Close() {
	s.cancel()
	s.wg.Wait()
}

func (s *AnalysisService) execute(run domain.AnalysisRun) {
	tr := &tracker{svc: s, id: run.ID, stages: slices.Clone(run.Stages)}
	summary, err := s.pipeline(s.ctx, tr)

	// The run must record how it ended even when the service was cancelled.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(s.ctx), 5*time.Second)
	defer cancel()
	status := domain.RunCompleted
	if err != nil {
		msg := err.Error()
		if errors.Is(err, context.Canceled) {
			msg = "interrupted because the server was shutting down"
		}
		s.opts.Logger.Error("analysis run failed", "run", run.ID, "error", msg)
		status, summary = domain.RunFailed, domain.RunSummary{Error: msg}
	}
	if err := s.runs.FinishRun(ctx, run.ID, status, tr.stages, summary); err != nil {
		s.opts.Logger.Error("could not close the analysis run", "run", run.ID, "error", err)
	}
}

func (s *AnalysisService) pipeline(ctx context.Context, tr *tracker) (domain.RunSummary, error) {
	tr.save(ctx, "")
	meters, err := s.source.Meters(ctx)
	if err != nil {
		return domain.RunSummary{}, fmt.Errorf("load meters: %w", err)
	}
	readings, err := s.source.Readings(ctx)
	if err != nil {
		return domain.RunSummary{}, fmt.Errorf("load readings: %w", err)
	}
	events, err := s.source.Events(ctx)
	if err != nil {
		return domain.RunSummary{}, fmt.Errorf("load events: %w", err)
	}

	report, err := analysis.Run(ctx, analysis.Input{Readings: readings, Events: events}, s.opts.Config,
		func(p analysis.Progress) {
			if p.Done {
				tr.finish(ctx, string(p.Stage))
			} else {
				tr.start(ctx, string(p.Stage))
			}
		})
	if err != nil {
		return domain.RunSummary{}, err
	}

	byID := make(map[string]domain.Meter, len(meters))
	for _, m := range meters {
		byID[m.MeterID] = m
	}
	evidence := make([]domain.Evidence, len(report.Anomalies))
	for i, a := range report.Anomalies {
		evidence[i] = BuildEvidence(a, byID[a.Episode.MeterID])
	}

	tr.start(ctx, StageExplanation)
	explanations, err := s.explainAll(ctx, evidence)
	if err != nil {
		return domain.RunSummary{}, err
	}
	tr.finish(ctx, StageExplanation)

	tr.start(ctx, StageRecommendation)
	records := make([]domain.Anomaly, len(report.Anomalies))
	for i, a := range report.Anomalies {
		if records[i], err = BuildAnomaly(a, byID[a.Episode.MeterID], explanations[i], tr.id); err != nil {
			return domain.RunSummary{}, err
		}
	}
	if err := s.anomalies.UpsertAnomalies(ctx, records); err != nil {
		return domain.RunSummary{}, fmt.Errorf("store anomalies: %w", err)
	}
	tr.finish(ctx, StageRecommendation)

	return summarize(report), nil
}

// explainAll explains every anomaly, a few at a time, keeping their order.
func (s *AnalysisService) explainAll(ctx context.Context, evidence []domain.Evidence) ([]domain.Explanation, error) {
	out := make([]domain.Explanation, len(evidence))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(explainWorkers)
	for i := range evidence {
		g.Go(func() error {
			exp, err := s.explainer.Explain(gctx, evidence[i])
			if err != nil {
				return fmt.Errorf("explain %s: %w", evidence[i].MeterID, err)
			}
			out[i] = exp
			return nil
		})
	}
	return out, g.Wait()
}

func summarize(r analysis.Report) domain.RunSummary {
	s := domain.RunSummary{
		Anomalies:  len(r.Anomalies),
		ByType:     map[string]int{},
		BySeverity: map[string]int{},
		Confidence: r.Confidence,
		Failures:   []domain.MeterFailure{},
	}
	for _, a := range r.Anomalies {
		s.ByType[string(a.Type)]++
		s.BySeverity[string(a.Score.Severity)]++
	}
	for _, f := range r.Failures {
		s.Failures = append(s.Failures, domain.MeterFailure{MeterID: f.MeterID, Error: f.Err.Error()})
	}
	return s
}

func initialStages() []domain.StageState {
	names := make([]string, 0, len(analysis.Stages)+2)
	for _, st := range analysis.Stages {
		names = append(names, string(st))
	}
	names = append(names, StageExplanation, StageRecommendation)
	stages := make([]domain.StageState, len(names))
	for i, n := range names {
		stages[i] = domain.StageState{Name: n, Status: domain.StagePending}
	}
	return stages
}

// tracker records the progress of one run as its stages start and finish.
type tracker struct {
	svc    *AnalysisService
	id     string
	stages []domain.StageState
}

func (t *tracker) start(ctx context.Context, name string) {
	now := time.Now()
	if st := t.stage(name); st != nil {
		st.Status, st.StartedAt = domain.StageRunning, &now
	}
	t.save(ctx, name)
}

func (t *tracker) finish(ctx context.Context, name string) {
	now := time.Now()
	if st := t.stage(name); st != nil {
		st.Status, st.FinishedAt = domain.StageDone, &now
	}
	t.save(ctx, "")
	select {
	case <-time.After(t.svc.opts.StageDelay):
	case <-ctx.Done():
	}
}

func (t *tracker) save(ctx context.Context, current string) {
	if err := t.svc.runs.SaveProgress(ctx, t.id, domain.RunRunning, current, t.stages); err != nil {
		t.svc.opts.Logger.Warn("could not save the run progress", "run", t.id, "error", err)
	}
}

func (t *tracker) stage(name string) *domain.StageState {
	for i := range t.stages {
		if t.stages[i].Name == name {
			return &t.stages[i]
		}
	}
	return nil
}
