package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/m0ntbl4ck/voltia/internal/domain"
)

const testSecret = "0123456789abcdef0123456789abcdef"

type memoryUsers struct {
	mu     sync.Mutex
	users  map[string]domain.User
	nextID int
	err    error
}

func newMemoryUsers() *memoryUsers { return &memoryUsers{users: map[string]domain.User{}} }

func (m *memoryUsers) UserByEmail(_ context.Context, email string) (domain.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return domain.User{}, m.err
	}
	for _, u := range m.users {
		if u.Email == email {
			return u, nil
		}
	}
	return domain.User{}, domain.ErrNotFound
}

func (m *memoryUsers) UserByID(_ context.Context, id string) (domain.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id == "" {
		return domain.User{}, errors.New("invalid input syntax for type uuid") // as Postgres does
	}
	if u, ok := m.users[id]; ok {
		return u, nil
	}
	return domain.User{}, domain.ErrNotFound
}

func (m *memoryUsers) SaveUser(_ context.Context, email, name, hash string) (domain.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, u := range m.users {
		if u.Email == email {
			u.Name, u.PasswordHash = name, hash
			m.users[id] = u
			return u, nil
		}
	}
	m.nextID++
	u := domain.User{ID: "user-" + string(rune('0'+m.nextID)), Email: email, Name: name, PasswordHash: hash}
	m.users[u.ID] = u
	return u, nil
}

func (m *memoryUsers) remove(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.users, id)
}

// clock is a controllable time source.
type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

func newAuth(t *testing.T, users *memoryUsers, c *clock) *Auth {
	t.Helper()
	a, err := NewAuth(users, AuthOptions{Secret: testSecret, TTL: time.Hour, Now: c.Now})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func signedUp(t *testing.T) (*Auth, *memoryUsers, *clock) {
	t.Helper()
	users := newMemoryUsers()
	if err := EnsureUser(context.Background(), users, "Ana@Example.com ", "Ana", "s3cret-pass"); err != nil {
		t.Fatal(err)
	}
	c := &clock{now: time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)}
	return newAuth(t, users, c), users, c
}

func TestNewAuthRejectsWeakSetups(t *testing.T) {
	if _, err := NewAuth(newMemoryUsers(), AuthOptions{Secret: "too-short", TTL: time.Hour}); err == nil {
		t.Error("a short secret was accepted")
	}
	if _, err := NewAuth(newMemoryUsers(), AuthOptions{Secret: testSecret, TTL: 0}); err == nil {
		t.Error("a zero lifetime was accepted")
	}
}

func TestLoginAndSessionRoundTrip(t *testing.T) {
	a, _, _ := signedUp(t)
	ctx := context.Background()

	user, token, err := a.Login(ctx, "ana@example.com", "s3cret-pass")
	if err != nil {
		t.Fatal(err)
	}
	if user.Email != "ana@example.com" || user.Name != "Ana" || token == "" {
		t.Errorf("user %+v, token %q", user, token)
	}
	back, err := a.UserFromToken(ctx, token)
	if err != nil || back.ID != user.ID {
		t.Errorf("UserFromToken = %+v, %v; want %s", back, err, user.ID)
	}
}

func TestLoginNormalizesTheEmail(t *testing.T) {
	a, _, _ := signedUp(t)
	if _, _, err := a.Login(context.Background(), "  ANA@example.COM  ", "s3cret-pass"); err != nil {
		t.Errorf("email case and spaces should not matter: %v", err)
	}
}

func TestWrongEmailAndWrongPasswordGiveTheSameError(t *testing.T) {
	a, _, _ := signedUp(t)
	ctx := context.Background()
	_, _, wrongPass := a.Login(ctx, "ana@example.com", "nope")
	_, _, wrongEmail := a.Login(ctx, "who@example.com", "s3cret-pass")
	if !errors.Is(wrongPass, domain.ErrInvalidCredentials) || wrongPass != wrongEmail {
		t.Errorf("wrong password: %v; wrong email: %v", wrongPass, wrongEmail)
	}
}

func TestAnUnknownEmailStillCostsAHash(t *testing.T) {
	a, _, _ := signedUp(t)
	start := time.Now()
	if _, _, err := a.Login(context.Background(), "who@example.com", "whatever"); err == nil {
		t.Fatal("expected an error")
	}
	if elapsed := time.Since(start); elapsed < 10*time.Millisecond {
		t.Errorf("an unknown email was rejected in %v, fast enough to tell it apart from a wrong password", elapsed)
	}
}

func TestStoreFailureIsNotDisguisedAsBadCredentials(t *testing.T) {
	a, users, _ := signedUp(t)
	users.err = errors.New("database is down")
	_, _, err := a.Login(context.Background(), "ana@example.com", "s3cret-pass")
	if err == nil || errors.Is(err, domain.ErrInvalidCredentials) {
		t.Errorf("error = %v, want the store failure", err)
	}
}

func TestSessionExpires(t *testing.T) {
	a, _, c := signedUp(t)
	ctx := context.Background()
	_, token, err := a.Login(ctx, "ana@example.com", "s3cret-pass")
	if err != nil {
		t.Fatal(err)
	}
	c.now = c.now.Add(59 * time.Minute)
	if _, err := a.UserFromToken(ctx, token); err != nil {
		t.Errorf("a session inside its hour was rejected: %v", err)
	}
	c.now = c.now.Add(2 * time.Minute)
	if _, err := a.UserFromToken(ctx, token); !errors.Is(err, domain.ErrUnauthorized) {
		t.Errorf("an expired session gave %v, want ErrUnauthorized", err)
	}
}

func TestForgedOrMalformedTokensAreRejected(t *testing.T) {
	a, users, c := signedUp(t)
	ctx := context.Background()
	user, valid, err := a.Login(ctx, "ana@example.com", "s3cret-pass")
	if err != nil {
		t.Fatal(err)
	}
	claims := func(exp time.Time) jwt.RegisteredClaims {
		return jwt.RegisteredClaims{Subject: user.ID, ExpiresAt: jwt.NewNumericDate(exp)}
	}
	sign := func(m jwt.SigningMethod, secret string, cl jwt.RegisteredClaims) string {
		s, err := jwt.NewWithClaims(m, cl).SignedString([]byte(secret))
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	none, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims(c.now.Add(time.Hour))).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	tests := map[string]string{
		"empty":                  "",
		"garbage":                "not.a.token",
		"another secret":         sign(jwt.SigningMethodHS256, "another-secret-another-secret-1234", claims(c.now.Add(time.Hour))),
		"another algorithm":      sign(jwt.SigningMethodHS512, testSecret, claims(c.now.Add(time.Hour))),
		"no signature":           none,
		"no expiry":              sign(jwt.SigningMethodHS256, testSecret, jwt.RegisteredClaims{Subject: user.ID}),
		"no subject":             sign(jwt.SigningMethodHS256, testSecret, jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(c.now.Add(time.Hour))}),
		"tampered payload":       valid[:strings.LastIndex(valid, ".")-2] + "xx" + valid[strings.LastIndex(valid, "."):],
		"already expired":        sign(jwt.SigningMethodHS256, testSecret, claims(c.now.Add(-time.Minute))),
		"valid but user removed": "",
	}
	users2 := newMemoryUsers()
	gone := newAuth(t, users2, c)
	tests["valid but user removed"] = valid
	for name, token := range tests {
		target := a
		if name == "valid but user removed" {
			target = gone
		}
		if _, err := target.UserFromToken(ctx, token); !errors.Is(err, domain.ErrUnauthorized) {
			t.Errorf("%s: error = %v, want ErrUnauthorized", name, err)
		}
	}
	users.remove(user.ID)
	if _, err := a.UserFromToken(ctx, valid); !errors.Is(err, domain.ErrUnauthorized) {
		t.Errorf("a token of a deleted user gave %v", err)
	}
}

func TestEnsureUserStoresOnlyAHash(t *testing.T) {
	users := newMemoryUsers()
	ctx := context.Background()
	if err := EnsureUser(ctx, users, "  Demo@Voltia.local ", "Demo", "plain-password"); err != nil {
		t.Fatal(err)
	}
	u, err := users.UserByEmail(ctx, "demo@voltia.local")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(u.PasswordHash, "plain-password") || !strings.HasPrefix(u.PasswordHash, "$2") {
		t.Errorf("stored hash = %q, want a bcrypt hash", u.PasswordHash)
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte("plain-password")) != nil {
		t.Error("the hash does not match the password")
	}
}

func TestEnsureUserAppliesAChangedPassword(t *testing.T) {
	users := newMemoryUsers()
	ctx := context.Background()
	if err := EnsureUser(ctx, users, "demo@voltia.local", "Demo", "first-password"); err != nil {
		t.Fatal(err)
	}
	before, _ := users.UserByEmail(ctx, "demo@voltia.local")
	if err := EnsureUser(ctx, users, "demo@voltia.local", "Demo", "second-password"); err != nil {
		t.Fatal(err)
	}
	after, _ := users.UserByEmail(ctx, "demo@voltia.local")
	if after.ID != before.ID {
		t.Error("the user was recreated instead of updated")
	}
	if bcrypt.CompareHashAndPassword([]byte(after.PasswordHash), []byte("second-password")) != nil {
		t.Error("the new password was not applied")
	}
	if bcrypt.CompareHashAndPassword([]byte(after.PasswordHash), []byte("first-password")) == nil {
		t.Error("the old password still works")
	}
}

func TestEnsureUserNeedsEmailAndPassword(t *testing.T) {
	for _, tt := range []struct{ email, password string }{{"", "x"}, {"a@b.c", ""}, {"   ", "x"}} {
		if err := EnsureUser(context.Background(), newMemoryUsers(), tt.email, "n", tt.password); err == nil {
			t.Errorf("EnsureUser(%q, %q) succeeded", tt.email, tt.password)
		}
	}
}
