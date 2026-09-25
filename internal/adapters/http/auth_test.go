package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/m0ntbl4ck/voltia/internal/domain"
)

const validToken = "valid-token"

var demoUser = domain.User{ID: validID, Email: "demo@voltia.local", Name: "Demo", PasswordHash: "$2a$10$secret-hash"}

// fakeAuth accepts one email and password, and one token.
type fakeAuth struct {
	loginErr  error
	tokenErr  error
	logins    int
	lastEmail string
}

func newFakeAuth() *fakeAuth { return &fakeAuth{} }

func (f *fakeAuth) Login(_ context.Context, email, password string) (domain.User, string, error) {
	f.logins++
	f.lastEmail = email
	if f.loginErr != nil {
		return domain.User{}, "", f.loginErr
	}
	if email != demoUser.Email || password != "right-password" {
		return domain.User{}, "", domain.ErrInvalidCredentials
	}
	return demoUser, validToken, nil
}

func (f *fakeAuth) UserFromToken(_ context.Context, token string) (domain.User, error) {
	if f.tokenErr != nil {
		return domain.User{}, f.tokenErr
	}
	if token != validToken {
		return domain.User{}, domain.ErrUnauthorized
	}
	return demoUser, nil
}

func (f *fakeAuth) TTL() time.Duration { return time.Hour }

func postJSON(h http.Handler, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func sessionSetCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie {
			return c
		}
	}
	return nil
}

func TestLoginSetsAHardenedSessionCookie(t *testing.T) {
	h := newServer(&fakeStarter{}, &fakeRuns{})
	rec := postJSON(h, "/api/v1/auth/login", `{"email":"demo@voltia.local","password":"right-password"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	c := sessionSetCookie(t, rec)
	if c == nil || c.Value != validToken {
		t.Fatalf("session cookie = %+v", c)
	}
	if !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.MaxAge != 3600 {
		t.Errorf("cookie attributes = HttpOnly %v, SameSite %v, Path %q, MaxAge %d", c.HttpOnly, c.SameSite, c.Path, c.MaxAge)
	}
	if c.Secure {
		t.Error("the cookie is Secure over plain HTTP, so a local browser would drop it")
	}
}

func TestLoginCookieIsSecureBehindHTTPS(t *testing.T) {
	h := newServer(&fakeStarter{}, &fakeRuns{})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"email":"demo@voltia.local","password":"right-password"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if c := sessionSetCookie(t, rec); c == nil || !c.Secure {
		t.Errorf("cookie behind HTTPS = %+v, want Secure", c)
	}
}

func TestLoginResponseNeverCarriesThePasswordHash(t *testing.T) {
	h := newServer(&fakeStarter{}, &fakeRuns{})
	rec := postJSON(h, "/api/v1/auth/login", `{"email":"demo@voltia.local","password":"right-password"}`)
	if strings.Contains(rec.Body.String(), "secret-hash") || strings.Contains(strings.ToLower(rec.Body.String()), "password") {
		t.Errorf("the body leaks credentials: %s", rec.Body)
	}
	var u map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &u); err != nil || u["id"] != validID || u["email"] != demoUser.Email || u["name"] != "Demo" || len(u) != 3 {
		t.Errorf("user = %v (%v)", u, err)
	}
}

func TestLoginRejections(t *testing.T) {
	tests := []struct {
		name, contentType, body string
		want                    int
	}{
		{"wrong password", "application/json", `{"email":"demo@voltia.local","password":"nope"}`, http.StatusUnauthorized},
		{"unknown email", "application/json", `{"email":"who@x.com","password":"right-password"}`, http.StatusUnauthorized},
		{"malformed JSON", "application/json", `{"email":`, http.StatusBadRequest},
		{"not JSON at all", "application/json", `email=a&password=b`, http.StatusBadRequest},
		{"missing password", "application/json", `{"email":"demo@voltia.local"}`, http.StatusBadRequest},
		{"missing email", "application/json", `{"password":"right-password"}`, http.StatusBadRequest},
		{"oversized body", "application/json", `{"email":"` + strings.Repeat("a", 5000) + `","password":"x"}`, http.StatusBadRequest},
		{"form encoding", "application/x-www-form-urlencoded", `email=demo@voltia.local&password=right-password`, http.StatusUnsupportedMediaType},
		{"no content type", "", `{"email":"demo@voltia.local","password":"right-password"}`, http.StatusUnsupportedMediaType},
	}
	for _, tt := range tests {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(tt.body))
		if tt.contentType != "" {
			req.Header.Set("Content-Type", tt.contentType)
		}
		rec := httptest.NewRecorder()
		newServer(&fakeStarter{}, &fakeRuns{}).ServeHTTP(rec, req)
		if rec.Code != tt.want {
			t.Errorf("%s: status = %d, want %d", tt.name, rec.Code, tt.want)
		}
		if rec.Header().Get("Content-Type") != "application/problem+json" {
			t.Errorf("%s: content type = %q", tt.name, rec.Header().Get("Content-Type"))
		}
		if sessionSetCookie(t, rec) != nil {
			t.Errorf("%s: a failed login set a session cookie", tt.name)
		}
	}
}

func TestLoginAcceptsAJSONContentTypeWithACharset(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"email":"demo@voltia.local","password":"right-password"}`))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	rec := httptest.NewRecorder()
	newServer(&fakeStarter{}, &fakeRuns{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestBadCredentialsDoNotSayWhichPartWasWrong(t *testing.T) {
	h := newServer(&fakeStarter{}, &fakeRuns{})
	a := postJSON(h, "/api/v1/auth/login", `{"email":"demo@voltia.local","password":"nope"}`)
	b := postJSON(h, "/api/v1/auth/login", `{"email":"who@x.com","password":"right-password"}`)
	if a.Body.String() != b.Body.String() {
		t.Errorf("the two failures differ:\n%s\n%s", a.Body, b.Body)
	}
}

func TestLoginStoreFailureIsA500WithoutTheCause(t *testing.T) {
	auth := &fakeAuth{loginErr: errors.New("connection to 10.0.0.5 refused")}
	rec := postJSON(newServerWithAuth(&fakeStarter{}, &fakeRuns{}, auth), "/api/v1/auth/login", `{"email":"a@b.c","password":"x"}`)
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "10.0.0.5") {
		t.Errorf("status %d, body %s", rec.Code, rec.Body)
	}
}

func TestLogoutClearsTheCookieWithoutNeedingASession(t *testing.T) {
	rec := doAnon(newServer(&fakeStarter{}, &fakeRuns{}), http.MethodPost, "/api/v1/auth/logout")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rec.Code)
	}
	c := sessionSetCookie(t, rec)
	if c == nil || c.Value != "" || c.MaxAge >= 0 || !c.HttpOnly {
		t.Errorf("logout cookie = %+v, want an empty expired HttpOnly cookie", c)
	}
}

func TestMeReturnsTheSessionUser(t *testing.T) {
	rec := do(newServer(&fakeStarter{}, &fakeRuns{}), http.MethodGet, "/api/v1/auth/me")
	var u map[string]string
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &u) != nil || u["email"] != demoUser.Email {
		t.Errorf("status %d, body %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "hash") {
		t.Errorf("me leaks the hash: %s", rec.Body)
	}
}

func TestEveryRouteButLoginLogoutAndHealthNeedsASession(t *testing.T) {
	starter, runs := &fakeStarter{run: domain.AnalysisRun{ID: validID}}, &fakeRuns{run: domain.AnalysisRun{ID: validID}}
	h := newServer(starter, runs)
	protected := []struct{ method, path string }{
		{http.MethodGet, "/api/v1/auth/me"},
		{http.MethodPost, "/api/v1/ai/analyze"},
		{http.MethodGet, "/api/v1/ai/analysis/latest"},
		{http.MethodGet, "/api/v1/ai/analysis/" + validID},
	}
	for _, p := range protected {
		rec := doAnon(h, p.method, p.path)
		if rec.Code != http.StatusUnauthorized || rec.Header().Get("Content-Type") != "application/problem+json" {
			t.Errorf("%s %s without a session: %d %q", p.method, p.path, rec.Code, rec.Header().Get("Content-Type"))
		}
	}
	if starter.calls != 0 || runs.runCalls+runs.lastCalls != 0 {
		t.Errorf("handlers ran without a session: %d starts, %d lookups", starter.calls, runs.runCalls+runs.lastCalls)
	}
	if rec := doAnon(h, http.MethodGet, "/healthz"); rec.Code != http.StatusOK {
		t.Errorf("healthz needs no session but gave %d", rec.Code)
	}
}

func TestInvalidOrExpiredSessionIsRejected(t *testing.T) {
	h := newServer(&fakeStarter{}, &fakeRuns{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: "forged"})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestSessionCheckFailureIsA500NotA401(t *testing.T) {
	auth := &fakeAuth{tokenErr: errors.New("database is down")}
	rec := do(newServerWithAuth(&fakeStarter{}, &fakeRuns{}, auth), http.MethodGet, "/api/v1/auth/me")
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "database") {
		t.Errorf("status %d, body %s", rec.Code, rec.Body)
	}
}
