// Package auth wires generic OIDC sign-in into the HTTP server.
//
// Auth is opt-in: it activates only when the required OIDC_* environment
// variables are present. When inactive, NewFromEnv returns (nil, nil) and the
// server runs unauthenticated (back-compat with the original single-user
// deployment + the Wails GUI which is local-only).
//
// Flow: GET /auth/login -> redirect to issuer -> GET /auth/callback exchanges
// the code, verifies the ID token, upserts the user, mints an opaque session
// stored in the sessions table, and sets it as a cookie. Subsequent /api/*
// requests use Middleware to look the session up and attach the User to
// the request context.
package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const (
	cookieName       = "hours_session"
	stateCookie      = "hours_oauth_state"
	sessionDuration  = 30 * 24 * time.Hour
	stateValidWindow = 10 * time.Minute
)

type ctxKey int

const userKey ctxKey = 1

// User mirrors the users table row plus an Authenticated flag set by
// Middleware so handlers can branch on auth state without nil checks.
type User struct {
	ID            int64     `json:"id"`
	OIDCSubject   string    `json:"oidc_subject"`
	Email         string    `json:"email"`
	Name          string    `json:"name"`
	Role          string    `json:"role"`
	CreatedAt     time.Time `json:"created_at"`
	LastLoginAt   time.Time `json:"last_login_at"`
	Authenticated bool      `json:"-"`
}

// Auth is the runtime auth service. A nil *Auth means auth is disabled and
// the server should serve all routes openly.
type Auth struct {
	db           *sql.DB
	provider     *oidc.Provider
	verifier     *oidc.IDTokenVerifier
	oauth        *oauth2.Config
	allowedEmail map[string]struct{} // empty == allow any verified user
	cookieSecure bool
}

// NewFromEnv returns an *Auth configured from environment variables, or
// (nil, nil) if OIDC is not configured. Required env vars:
//
//	OIDC_ISSUER, OIDC_CLIENT_ID, OIDC_CLIENT_SECRET, OIDC_REDIRECT_URL
//
// Optional:
//
//	OIDC_ALLOWED_EMAILS  comma-separated allowlist
//	OIDC_SCOPES          space-separated, defaults to "openid profile email"
//	OIDC_COOKIE_SECURE   "1"/"true" to mark the cookie Secure (set behind TLS)
func NewFromEnv(ctx context.Context, db *sql.DB) (*Auth, error) {
	issuer := strings.TrimSpace(os.Getenv("OIDC_ISSUER"))
	clientID := strings.TrimSpace(os.Getenv("OIDC_CLIENT_ID"))
	clientSecret := strings.TrimSpace(os.Getenv("OIDC_CLIENT_SECRET"))
	redirect := strings.TrimSpace(os.Getenv("OIDC_REDIRECT_URL"))
	if issuer == "" || clientID == "" || redirect == "" {
		return nil, nil
	}

	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery (%s): %w", issuer, err)
	}

	scopesEnv := strings.TrimSpace(os.Getenv("OIDC_SCOPES"))
	scopes := []string{oidc.ScopeOpenID, "profile", "email"}
	if scopesEnv != "" {
		scopes = strings.Fields(scopesEnv)
	}

	cfg := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  redirect,
		Scopes:       scopes,
	}

	allowed := map[string]struct{}{}
	for _, e := range strings.Split(os.Getenv("OIDC_ALLOWED_EMAILS"), ",") {
		e = strings.TrimSpace(strings.ToLower(e))
		if e != "" {
			allowed[e] = struct{}{}
		}
	}

	cookieSecure := false
	switch strings.ToLower(strings.TrimSpace(os.Getenv("OIDC_COOKIE_SECURE"))) {
	case "1", "true", "yes":
		cookieSecure = true
	}

	return &Auth{
		db:           db,
		provider:     provider,
		verifier:     provider.Verifier(&oidc.Config{ClientID: clientID}),
		oauth:        cfg,
		allowedEmail: allowed,
		cookieSecure: cookieSecure,
	}, nil
}

// Enabled reports whether auth checks should be applied. Safe to call on a
// nil receiver so callers can write `if a.Enabled() { ... }` unconditionally.
func (a *Auth) Enabled() bool { return a != nil }

// LoginHandler kicks off the OIDC dance. We mint a random state, stash it in
// a short-lived cookie, and redirect to the issuer. Optional ?return= query
// gets piggybacked into the state cookie so we can land the user back where
// they started.
func (a *Auth) LoginHandler(w http.ResponseWriter, r *http.Request) {
	state, err := randomToken(24)
	if err != nil {
		http.Error(w, "state error", http.StatusInternalServerError)
		return
	}
	ret := r.URL.Query().Get("return")
	if ret == "" || !strings.HasPrefix(ret, "/") {
		ret = "/"
	}
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookie,
		Value:    state + "|" + ret,
		Path:     "/",
		MaxAge:   int(stateValidWindow.Seconds()),
		HttpOnly: true,
		Secure:   a.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, a.oauth.AuthCodeURL(state), http.StatusFound)
}

// CallbackHandler completes the exchange. On success we set the session
// cookie and 302 back to the original page. On failure we render plaintext
// because by the time we hit this URL there is no SPA shell to lean on.
func (a *Auth) CallbackHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if errMsg := q.Get("error"); errMsg != "" {
		http.Error(w, "oidc error: "+errMsg, http.StatusBadRequest)
		return
	}
	stateCk, err := r.Cookie(stateCookie)
	if err != nil {
		http.Error(w, "missing state cookie", http.StatusBadRequest)
		return
	}
	parts := strings.SplitN(stateCk.Value, "|", 2)
	wantState := parts[0]
	ret := "/"
	if len(parts) == 2 && strings.HasPrefix(parts[1], "/") {
		ret = parts[1]
	}
	if q.Get("state") != wantState {
		http.Error(w, "state mismatch", http.StatusBadRequest)
		return
	}
	// Clear the state cookie regardless of outcome.
	http.SetCookie(w, &http.Cookie{
		Name: stateCookie, Value: "", Path: "/", MaxAge: -1,
	})

	tok, err := a.oauth.Exchange(r.Context(), q.Get("code"))
	if err != nil {
		http.Error(w, "token exchange failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	rawID, ok := tok.Extra("id_token").(string)
	if !ok {
		http.Error(w, "missing id_token", http.StatusBadGateway)
		return
	}
	idTok, err := a.verifier.Verify(r.Context(), rawID)
	if err != nil {
		http.Error(w, "id_token verify: "+err.Error(), http.StatusUnauthorized)
		return
	}
	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
	}
	if err := idTok.Claims(&claims); err != nil {
		http.Error(w, "claims: "+err.Error(), http.StatusBadGateway)
		return
	}
	email := strings.ToLower(strings.TrimSpace(claims.Email))
	if email == "" {
		http.Error(w, "id_token missing email claim", http.StatusUnauthorized)
		return
	}
	if len(a.allowedEmail) > 0 {
		if _, ok := a.allowedEmail[email]; !ok {
			http.Error(w, "email not authorized", http.StatusForbidden)
			return
		}
	}

	user, err := a.upsertUser(idTok.Subject, email, claims.Name)
	if err != nil {
		http.Error(w, "user upsert: "+err.Error(), http.StatusInternalServerError)
		return
	}

	sessionToken, err := a.createSession(user.ID)
	if err != nil {
		http.Error(w, "session create: "+err.Error(), http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    sessionToken,
		Path:     "/",
		MaxAge:   int(sessionDuration.Seconds()),
		HttpOnly: true,
		Secure:   a.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, ret, http.StatusFound)
}

// LogoutHandler deletes the session row and clears the cookie.
func (a *Auth) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		_, _ = a.db.Exec(`DELETE FROM sessions WHERE token = ?`, c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: "", Path: "/", MaxAge: -1,
	})
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

// MeHandler returns the current user (or 401 if unauthenticated). When auth
// is disabled the API server doesn't register this route at all.
func (a *Auth) MeHandler(w http.ResponseWriter, r *http.Request) {
	u, ok := UserFromContext(r.Context())
	w.Header().Set("Content-Type", "application/json")
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"not authenticated"}`))
		return
	}
	fmt.Fprintf(w,
		`{"id":%d,"email":%q,"name":%q,"role":%q}`,
		u.ID, u.Email, u.Name, u.Role,
	)
}

// Middleware loads the session and attaches the user to the context. When the
// session is missing/expired it short-circuits with 401 for /api/* requests
// and a redirect for everything else. Public paths (/auth/*, static assets)
// must be excluded by the caller.
func (a *Auth) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := a.lookupUser(r)
		if u == nil {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"authentication required"}`))
				return
			}
			http.Redirect(w, r, "/auth/login?return="+r.URL.Path, http.StatusFound)
			return
		}
		ctx := context.WithValue(r.Context(), userKey, u)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireRole rejects the request unless the authenticated user holds one of
// the supplied roles. Use it for destructive endpoints like /api/import.
func (a *Auth) RequireRole(roles ...string) func(http.Handler) http.Handler {
	want := make(map[string]struct{}, len(roles))
	for _, r := range roles {
		want[r] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, ok := UserFromContext(r.Context())
			if !ok {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"authentication required"}`))
				return
			}
			if _, allowed := want[u.Role]; !allowed {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error":"insufficient role"}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// UserFromContext fetches the user attached by Middleware. Returns (nil, false)
// when no session is associated with the request.
func UserFromContext(ctx context.Context) (*User, bool) {
	u, ok := ctx.Value(userKey).(*User)
	if !ok || u == nil {
		return nil, false
	}
	return u, u.Authenticated
}

func (a *Auth) lookupUser(r *http.Request) *User {
	c, err := r.Cookie(cookieName)
	if err != nil || c.Value == "" {
		return nil
	}
	row := a.db.QueryRow(`
		SELECT u.id, u.oidc_subject, u.email, COALESCE(u.name,''), u.role,
		       u.created_at, COALESCE(u.last_login_at, u.created_at), s.expires_at
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.token = ?
	`, c.Value)
	var u User
	var expires time.Time
	if err := row.Scan(&u.ID, &u.OIDCSubject, &u.Email, &u.Name, &u.Role,
		&u.CreatedAt, &u.LastLoginAt, &expires); err != nil {
		return nil
	}
	if time.Now().After(expires) {
		_, _ = a.db.Exec(`DELETE FROM sessions WHERE token = ?`, c.Value)
		return nil
	}
	u.Authenticated = true
	return &u
}

func (a *Auth) upsertUser(subject, email, name string) (*User, error) {
	tx, err := a.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// First user is bootstrapped as admin so somebody can hit /api/import.
	var userCount int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&userCount); err != nil {
		return nil, err
	}
	role := "user"
	if userCount == 0 {
		role = "admin"
	}

	var id int64
	err = tx.QueryRow(`SELECT id FROM users WHERE oidc_subject = ?`, subject).Scan(&id)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		res, ierr := tx.Exec(`
			INSERT INTO users (oidc_subject, email, name, role, last_login_at)
			VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
		`, subject, email, name, role)
		if ierr != nil {
			return nil, ierr
		}
		id, _ = res.LastInsertId()
	case err != nil:
		return nil, err
	default:
		if _, uerr := tx.Exec(`
			UPDATE users SET email = ?, name = ?, last_login_at = CURRENT_TIMESTAMP
			WHERE id = ?
		`, email, name, id); uerr != nil {
			return nil, uerr
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	u := &User{}
	row := a.db.QueryRow(`
		SELECT id, oidc_subject, email, COALESCE(name,''), role, created_at,
		       COALESCE(last_login_at, created_at)
		FROM users WHERE id = ?
	`, id)
	if err := row.Scan(&u.ID, &u.OIDCSubject, &u.Email, &u.Name, &u.Role,
		&u.CreatedAt, &u.LastLoginAt); err != nil {
		return nil, err
	}
	u.Authenticated = true
	return u, nil
}

func (a *Auth) createSession(userID int64) (string, error) {
	tok, err := randomToken(32)
	if err != nil {
		return "", err
	}
	exp := time.Now().Add(sessionDuration)
	_, err = a.db.Exec(`
		INSERT INTO sessions (token, user_id, expires_at) VALUES (?, ?, ?)
	`, tok, userID, exp)
	if err != nil {
		return "", err
	}
	// Best-effort GC of expired sessions.
	_, _ = a.db.Exec(`DELETE FROM sessions WHERE expires_at < CURRENT_TIMESTAMP`)
	return tok, nil
}

func randomToken(nBytes int) (string, error) {
	buf := make([]byte, nBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
