package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/m0ntbl4ck/voltia/internal/domain"
	"github.com/m0ntbl4ck/voltia/internal/ports"
)

// minSecretLength is the shortest signing secret accepted: 32 bytes is what
// HS256 needs to be as strong as the hash it is built on.
const minSecretLength = 32

// AuthOptions configures sessions.
type AuthOptions struct {
	// Secret signs the session tokens.
	Secret string
	// TTL is how long a session lasts.
	TTL time.Duration
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

// Auth signs users in and checks their sessions.
type Auth struct {
	users ports.Users
	opts  AuthOptions
	// decoy is compared against when the email is unknown, so a wrong email
	// takes as long to reject as a wrong password.
	decoy []byte
}

func NewAuth(users ports.Users, opts AuthOptions) (*Auth, error) {
	if len(opts.Secret) < minSecretLength {
		return nil, fmt.Errorf("the signing secret needs at least %d characters", minSecretLength)
	}
	if opts.TTL <= 0 {
		return nil, errors.New("the session lifetime must be positive")
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	decoy, err := bcrypt.GenerateFromPassword([]byte("decoy"), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	return &Auth{users: users, opts: opts, decoy: decoy}, nil
}

// Login checks the credentials and returns the user with a signed session
// token. A wrong email and a wrong password give the same error.
func (a *Auth) Login(ctx context.Context, email, password string) (domain.User, string, error) {
	user, err := a.users.UserByEmail(ctx, normalizeEmail(email))
	if errors.Is(err, domain.ErrNotFound) {
		_ = bcrypt.CompareHashAndPassword(a.decoy, []byte(password))
		return domain.User{}, "", domain.ErrInvalidCredentials
	}
	if err != nil {
		return domain.User{}, "", err
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return domain.User{}, "", domain.ErrInvalidCredentials
	}
	token, err := a.sign(user.ID)
	if err != nil {
		return domain.User{}, "", err
	}
	return user, token, nil
}

// UserFromToken returns the user a valid session token belongs to, or
// domain.ErrUnauthorized when the token is invalid, expired, or its user is gone.
func (a *Auth) UserFromToken(ctx context.Context, token string) (domain.User, error) {
	claims := jwt.RegisteredClaims{}
	_, err := jwt.ParseWithClaims(token, &claims, func(*jwt.Token) (any, error) {
		return []byte(a.opts.Secret), nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithTimeFunc(a.opts.Now), jwt.WithExpirationRequired())
	if err != nil || claims.Subject == "" {
		return domain.User{}, domain.ErrUnauthorized
	}
	user, err := a.users.UserByID(ctx, claims.Subject)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.User{}, domain.ErrUnauthorized
	}
	return user, err
}

// TTL is how long a session lasts, so the cookie can expire with its token.
func (a *Auth) TTL() time.Duration { return a.opts.TTL }

func (a *Auth) sign(userID string) (string, error) {
	now := a.opts.Now()
	return jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Subject:   userID,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(a.opts.TTL)),
	}).SignedString([]byte(a.opts.Secret))
}

// EnsureUser creates the user, or updates its name and password, so the
// account matches what the environment says on every start. Only the bcrypt
// hash is stored.
func EnsureUser(ctx context.Context, users ports.Users, email, name, password string) error {
	email = normalizeEmail(email)
	if email == "" || password == "" {
		return errors.New("a user needs an email and a password")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = users.SaveUser(ctx, email, name, string(hash))
	return err
}

func normalizeEmail(email string) string { return strings.ToLower(strings.TrimSpace(email)) }
