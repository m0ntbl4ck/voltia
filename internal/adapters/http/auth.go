package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/m0ntbl4ck/voltia/internal/domain"
)

// sessionCookie holds the signed session token.
const sessionCookie = "voltia_session"

// maxBody bounds the JSON bodies of the API: they are a few short fields.
const maxBody = 4 << 10

// Authenticator signs users in and turns a session token back into its user.
type Authenticator interface {
	Login(ctx context.Context, email, password string) (domain.User, string, error)
	UserFromToken(ctx context.Context, token string) (domain.User, error)
	// TTL is how long a session lasts; the cookie expires with it.
	TTL() time.Duration
}

type authResource struct {
	auth Authenticator
	log  *slog.Logger
}

// public registers the routes that need no session.
func (a authResource) public(r chi.Router) {
	r.Post("/auth/login", a.login)
	r.Post("/auth/logout", a.logout)
}

// protected registers the routes that need one.
func (a authResource) protected(r chi.Router) {
	r.Get("/auth/me", a.me)
}

type ctxKey struct{}

// requireSession lets a request through only when it carries a valid session
// cookie, and puts the user in the request context.
func (a authResource) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookie)
		if err != nil {
			writeProblem(w, http.StatusUnauthorized, "sign in to use the API")
			return
		}
		user, err := a.auth.UserFromToken(r.Context(), cookie.Value)
		if errors.Is(err, domain.ErrUnauthorized) {
			writeProblem(w, http.StatusUnauthorized, "the session is invalid or has expired")
			return
		}
		if err != nil {
			serverError(w, a.log, "check session", err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, user)))
	})
}

// currentUser is the user requireSession found.
func currentUser(r *http.Request) domain.User {
	u, _ := r.Context().Value(ctxKey{}).(domain.User)
	return u
}

type credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// userResponse is what the API says about a user: never the password hash.
type userResponse struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

func toUserResponse(u domain.User) userResponse {
	return userResponse{ID: u.ID, Email: u.Email, Name: u.Name}
}

func (a authResource) login(w http.ResponseWriter, r *http.Request) {
	if mediaType, _, _ := strings.Cut(r.Header.Get("Content-Type"), ";"); strings.TrimSpace(mediaType) != "application/json" {
		writeProblem(w, http.StatusUnsupportedMediaType, "send the credentials as application/json")
		return
	}
	var c credentials
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(&c); err != nil {
		writeProblem(w, http.StatusBadRequest, "the body must be JSON with email and password")
		return
	}
	if c.Email == "" || c.Password == "" {
		writeProblem(w, http.StatusBadRequest, "email and password are required")
		return
	}
	user, token, err := a.auth.Login(r.Context(), c.Email, c.Password)
	if errors.Is(err, domain.ErrInvalidCredentials) {
		writeProblem(w, http.StatusUnauthorized, "invalid email or password")
		return
	}
	if err != nil {
		serverError(w, a.log, "login", err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(a.auth.TTL().Seconds()),
		HttpOnly: true,
		Secure:   isSecure(r),
		SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, http.StatusOK, toUserResponse(user))
}

func (a authResource) logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   isSecure(r),
		SameSite: http.SameSiteLaxMode,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (a authResource) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, toUserResponse(currentUser(r)))
}

// isSecure reports whether the request came over HTTPS, directly or through a proxy.
func isSecure(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}
