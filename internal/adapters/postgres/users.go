package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/m0ntbl4ck/voltia/internal/adapters/postgres/sqlcgen"
	"github.com/m0ntbl4ck/voltia/internal/domain"
)

// UserByEmail returns domain.ErrNotFound when no user has that email.
func (r *Repository) UserByEmail(ctx context.Context, email string) (domain.User, error) {
	return oneUser(r.q.GetUserByEmail(ctx, email))
}

// UserByID returns domain.ErrNotFound when no user has that id.
func (r *Repository) UserByID(ctx context.Context, id string) (domain.User, error) {
	return oneUser(r.q.GetUserByID(ctx, id))
}

// SaveUser creates the user or, when the email exists, updates its name and
// password hash.
func (r *Repository) SaveUser(ctx context.Context, email, name, passwordHash string) (domain.User, error) {
	return oneUser(r.q.UpsertUser(ctx, sqlcgen.UpsertUserParams{Email: email, PasswordHash: passwordHash, Name: name}))
}

func oneUser(row sqlcgen.User, err error) (domain.User, error) {
	if errors.Is(err, sql.ErrNoRows) {
		return domain.User{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.User{}, err
	}
	return domain.User{ID: row.ID, Email: row.Email, Name: row.Name, PasswordHash: row.PasswordHash}, nil
}
