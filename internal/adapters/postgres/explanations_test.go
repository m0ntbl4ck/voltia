package postgres

import (
	"context"
	"reflect"
	"testing"

	"github.com/m0ntbl4ck/voltia/internal/domain"
)

func TestExplanationCacheRoundTrip(t *testing.T) {
	repo, _, _ := seededRepository(t)
	ctx := context.Background()

	if _, ok, err := repo.CachedExplanation(ctx, "k1"); err != nil || ok {
		t.Fatalf("empty cache = %v, %v; want a miss", ok, err)
	}
	want := domain.Explanation{
		Summary: "resumen", Reason: "razón", RecommendedAction: "acción",
		InvestigationSteps: []string{"uno", "dos"}, Source: domain.SourceLLM, Model: "gemini-x",
	}
	if err := repo.CacheExplanation(ctx, "k1", "gemini", want); err != nil {
		t.Fatal(err)
	}
	got, ok, err := repo.CachedExplanation(ctx, "k1")
	if err != nil || !ok {
		t.Fatalf("CachedExplanation = %v, %v; want a hit", ok, err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestExplanationCacheKeepsTheFirstText(t *testing.T) {
	repo, _, _ := seededRepository(t)
	ctx := context.Background()
	first := domain.Explanation{Summary: "primero", Model: "m"}
	second := domain.Explanation{Summary: "segundo", Model: "m"}
	if err := repo.CacheExplanation(ctx, "k", "gemini", first); err != nil {
		t.Fatal(err)
	}
	if err := repo.CacheExplanation(ctx, "k", "gemini", second); err != nil {
		t.Fatal(err)
	}
	got, _, err := repo.CachedExplanation(ctx, "k")
	if err != nil || got.Summary != "primero" {
		t.Errorf("got %q, %v; want the first text", got.Summary, err)
	}
	if n := count(t, repo.db, "explanation_cache"); n != 1 {
		t.Errorf("%d rows, want 1", n)
	}
}
