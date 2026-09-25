package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/m0ntbl4ck/voltia/internal/domain"
)

func TestUsersRoundTrip(t *testing.T) {
	repo, _, _ := seededRepository(t)
	ctx := context.Background()

	saved, err := repo.SaveUser(ctx, "ana@example.com", "Ana", "hash-1")
	if err != nil {
		t.Fatal(err)
	}
	if saved.ID == "" || saved.Email != "ana@example.com" || saved.Name != "Ana" || saved.PasswordHash != "hash-1" {
		t.Errorf("saved = %+v", saved)
	}
	byEmail, err := repo.UserByEmail(ctx, "ana@example.com")
	if err != nil || byEmail != saved {
		t.Errorf("UserByEmail = %+v, %v; want %+v", byEmail, err, saved)
	}
	byID, err := repo.UserByID(ctx, saved.ID)
	if err != nil || byID != saved {
		t.Errorf("UserByID = %+v, %v; want %+v", byID, err, saved)
	}
}

func TestSavingAnExistingEmailUpdatesItInPlace(t *testing.T) {
	repo, _, _ := seededRepository(t)
	ctx := context.Background()
	first, err := repo.SaveUser(ctx, "ana@example.com", "Ana", "hash-1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.SaveUser(ctx, "ana@example.com", "Ana Maria", "hash-2")
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID || second.Name != "Ana Maria" || second.PasswordHash != "hash-2" {
		t.Errorf("second save = %+v, first %+v", second, first)
	}
	if n := count(t, repo.db, "users"); n != 1 {
		t.Errorf("%d users after saving one email twice, want 1", n)
	}
}

func TestUnknownUsersAreNotFound(t *testing.T) {
	repo, _, _ := seededRepository(t)
	ctx := context.Background()
	if _, err := repo.UserByEmail(ctx, "nobody@example.com"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("UserByEmail = %v, want ErrNotFound", err)
	}
	if _, err := repo.UserByID(ctx, missingID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("UserByID = %v, want ErrNotFound", err)
	}
}
