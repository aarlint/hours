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
//
// Admin role provisioning: every OIDC sign-in lands a user with role="user".
// Admin must be granted explicitly — either out-of-band via SQL
// (`UPDATE users SET role='admin' WHERE email = ...`) or by setting the
// OIDC_BOOTSTRAP_ADMIN_EMAILS environment variable to a comma-separated
// allowlist; matching emails are promoted (or admin-inserted) on every login.
// The previous "first user wins admin" behavior was removed because, with an
// open-signup OIDC provider, the first random visitor would otherwise inherit
// destructive privileges (e.g. /api/import) by race.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
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
//
// Scopes carries the permission set granted to this request. Session-cookie
// auth (and the synthetic Wails local user) get the wildcard "*" — full UI
// access. Bearer-token auth gets only the scopes the token was minted with,
// and routes are gated by RequireScope.
//
// AuthMethod records how the request was authenticated:
//   - "session" — cookie / OIDC flow
//   - "token"   — Authorization: Bearer ht_...
//   - "local"   — synthetic Wails / single-user context
//
// Token-management endpoints check this to reject "tokens minting tokens"
// — see RequireSession.
type User struct {
	ID            int64     `json:"id"`
	OIDCSubject   string    `json:"oidc_subject"`
	Email         string    `json:"email"`
	Name          string    `json:"name"`
	Role          string    `json:"role"`
	CreatedAt     time.Time `json:"created_at"`
	LastLoginAt   time.Time `json:"last_login_at"`
	Authenticated bool      `json:"-"`
	Scopes        []Scope   `json:"-"`
	AuthMethod    string    `json:"-"`
	// TokenID is the api_tokens.id row that authenticated this request,
	// or 0 when the request came in via session/local auth. Read by the
	// UsageRecorder middleware (HTTP path) and by recordToolCall (MCP
	// tool handlers) to attribute usage to a specific token.
	TokenID int64 `json:"-"`
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
	if !safeReturnPath(ret) {
		ret = "/"
	}
	// SameSite=None (with Secure) so the cookie survives the cross-site
	// redirect chain through the OIDC provider. Lax usually works for direct
	// IdP flows, but Cloudflare Access' SaaS OIDC bounces through extra hosts
	// and browsers drop Lax cookies on those non-top-level navigations.
	sameSite := http.SameSiteLaxMode
	if a.cookieSecure {
		sameSite = http.SameSiteNoneMode
	}
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookie,
		Value:    state + "|" + ret,
		Path:     "/",
		MaxAge:   int(stateValidWindow.Seconds()),
		HttpOnly: true,
		Secure:   a.cookieSecure,
		SameSite: sameSite,
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
	if len(parts) == 2 && safeReturnPath(parts[1]) {
		ret = parts[1]
	}
	if q.Get("state") != wantState {
		gotState := q.Get("state")
		fmt.Fprintf(os.Stderr, "auth: state mismatch want=%q got=%q cookieRaw=%q\n",
			wantState, gotState, stateCk.Value)
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

// RequireScope wraps next in a check that the authenticated identity has
// the given scope. Session-cookie users implicitly have every scope (Scopes
// contains ScopeAll). Bearer-token users must have the scope (or "*") in
// their token's scope set.
//
// On a nil receiver (auth disabled) the wrapper is a no-op — local Wails
// traffic isn't gated by scopes.
func (a *Auth) RequireScope(scope Scope) func(http.Handler) http.Handler {
	if a == nil {
		return func(next http.Handler) http.Handler { return next }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, ok := UserFromContext(r.Context())
			if !ok || u == nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"authentication required"}`))
				return
			}
			if !HasScope(u.Scopes, scope) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(fmt.Sprintf(`{"error":"missing scope: %s"}`, scope)))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireSession rejects requests authenticated via an API token. Used by the
// /api/tokens management endpoints so that a leaked token cannot be used to
// mint additional tokens or revoke siblings — token management is a session-
// (or local-Wails-) only operation.
//
// On a nil receiver (auth disabled) the wrapper is a no-op.
func (a *Auth) RequireSession() func(http.Handler) http.Handler {
	if a == nil {
		return func(next http.Handler) http.Handler { return next }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, ok := UserFromContext(r.Context())
			if !ok || u == nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"authentication required"}`))
				return
			}
			if u.AuthMethod == "token" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error":"token management requires session authentication"}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// UsageRecorder is a middleware that, after the request completes, persists a
// row to api_token_usage when the request was authenticated via a bearer
// token. Session/local traffic is not recorded.
//
// Records: method, request URL path, response status, total duration, and a
// best-effort error string (read from response body for status >= 400).
//
// The record is written asynchronously so the request's response time isn't
// affected by DB pressure. Drops rows silently on DB failure.
//
// Wire as: Middleware(UsageRecorder(apiMux)) — the outer Middleware populates
// the user-in-ctx that we read here, after `next` returns.
func (a *Auth) UsageRecorder(next http.Handler) http.Handler {
	if a == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		uw := &usageWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(uw, r)

		u, ok := UserFromContext(r.Context())
		if !ok || u == nil || u.TokenID == 0 {
			return
		}

		method := r.Method
		path := r.URL.Path
		status := uw.status
		duration := time.Since(started).Milliseconds()
		var errMsg string
		if status >= 400 {
			errMsg = string(uw.errBody())
		}
		tokenID := u.TokenID
		userID := u.ID

		go func() {
			_, _ = a.db.Exec(`
				INSERT INTO api_token_usage
					(token_id, user_id, method, path, status, duration_ms, error)
				VALUES (?, ?, ?, ?, ?, ?, ?)
			`, tokenID, userID, method, path, status, duration, nullableString(errMsg))
			_, _ = a.db.Exec(
				`UPDATE api_tokens SET last_used_at = CURRENT_TIMESTAMP WHERE id = ?`,
				tokenID,
			)
		}()
	})
}

// usageWriter wraps http.ResponseWriter to capture the status code and a
// short prefix of the response body so the UsageRecorder can extract a
// human-readable error string for failed requests. The body buffer is
// capped at usageErrCap bytes — we only keep enough for diagnostics.
type usageWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
	body        []byte
}

const usageErrCap = 256

func (uw *usageWriter) WriteHeader(code int) {
	if uw.wroteHeader {
		return
	}
	uw.status = code
	uw.wroteHeader = true
	uw.ResponseWriter.WriteHeader(code)
}

func (uw *usageWriter) Write(p []byte) (int, error) {
	if !uw.wroteHeader {
		uw.wroteHeader = true
	}
	if uw.status >= 400 && len(uw.body) < usageErrCap {
		room := usageErrCap - len(uw.body)
		if room > len(p) {
			room = len(p)
		}
		uw.body = append(uw.body, p[:room]...)
	}
	return uw.ResponseWriter.Write(p)
}

func (uw *usageWriter) Flush() {
	if f, ok := uw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (uw *usageWriter) Unwrap() http.ResponseWriter { return uw.ResponseWriter }

// errBody returns the captured body bytes (already capped at usageErrCap).
// Empty slice when the request didn't fail or wrote no body.
func (uw *usageWriter) errBody() []byte { return uw.body }

func nullableString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
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

// WithLocalUser attaches a synthetic local-only User to the context. Used by
// the embedded HTTP server inside the Wails GUI so handlers can derive a
// user_id even though no OIDC session exists. The synthetic user is marked
// Authenticated=true so handlers that gate on UserFromContext still work.
//
// Local users get the wildcard scope and AuthMethod="local" so RequireScope
// passes everywhere and RequireSession lets through the token-management
// endpoints (the Wails app needs to manage tokens for its single user).
func WithLocalUser(ctx context.Context, id int64) context.Context {
	return context.WithValue(ctx, userKey, &User{
		ID:            id,
		Email:         "local@hours.local",
		Name:          "Local User",
		Role:          "admin",
		Authenticated: true,
		Scopes:        []Scope{ScopeAll},
		AuthMethod:    "local",
	})
}

func (a *Auth) lookupUser(r *http.Request) *User {
	// Bearer token wins when present — keeps the contract clear for clients
	// that may, for some reason, also send a stale session cookie.
	if u := a.lookupTokenUser(r); u != nil {
		return u
	}
	return a.lookupSessionUser(r)
}

func (a *Auth) lookupSessionUser(r *http.Request) *User {
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
	var createdStr, lastLoginStr, expiresStr string
	if err := row.Scan(&u.ID, &u.OIDCSubject, &u.Email, &u.Name, &u.Role,
		&createdStr, &lastLoginStr, &expiresStr); err != nil {
		return nil
	}
	u.CreatedAt = parseSQLiteTime(createdStr)
	u.LastLoginAt = parseSQLiteTime(lastLoginStr)
	expires := parseSQLiteTime(expiresStr)
	if time.Now().After(expires) {
		_, _ = a.db.Exec(`DELETE FROM sessions WHERE token = ?`, c.Value)
		return nil
	}
	u.Authenticated = true
	u.Scopes = []Scope{ScopeAll} // session cookies grant full UI access
	u.AuthMethod = "session"
	return &u
}

// lookupTokenUser pulls Authorization: Bearer <token>, hashes it, and resolves
// the owning user. Returns nil for any of: no header, malformed header, hash
// not found, token revoked, token expired. The owning api_tokens row id is
// stashed on User.TokenID so downstream middleware (UsageRecorder) can
// attribute usage and update last_used_at — see UsageRecorder.
func (a *Auth) lookupTokenUser(r *http.Request) *User {
	raw := extractBearer(r.Header.Get("Authorization"))
	if raw == "" {
		return nil
	}
	hash := hashToken(raw)
	row := a.db.QueryRow(`
		SELECT t.id, t.scopes, t.expires_at,
		       u.id, u.oidc_subject, u.email, COALESCE(u.name,''), u.role,
		       u.created_at, COALESCE(u.last_login_at, u.created_at)
		FROM api_tokens t
		JOIN users u ON u.id = t.user_id
		WHERE t.token_hash = ? AND t.revoked_at IS NULL
	`, hash)
	var (
		tokenID                  int64
		scopesCSV                string
		expiresStr               sql.NullString
		u                        User
		createdStr, lastLoginStr string
	)
	if err := row.Scan(&tokenID, &scopesCSV, &expiresStr,
		&u.ID, &u.OIDCSubject, &u.Email, &u.Name, &u.Role,
		&createdStr, &lastLoginStr); err != nil {
		return nil
	}
	if expiresStr.Valid && expiresStr.String != "" {
		exp := parseSQLiteTime(expiresStr.String)
		if !exp.IsZero() && time.Now().After(exp) {
			return nil
		}
	}
	u.CreatedAt = parseSQLiteTime(createdStr)
	u.LastLoginAt = parseSQLiteTime(lastLoginStr)
	u.Authenticated = true
	u.Scopes = parseScopes(scopesCSV)
	u.AuthMethod = "token"
	u.TokenID = tokenID
	return &u
}

// extractBearer parses a "Bearer xyz" header value and returns the token, or
// "" for any malformed input. Case-insensitive on the scheme as required by
// RFC 7235.
func extractBearer(h string) string {
	if h == "" {
		return ""
	}
	const prefix = "bearer "
	if len(h) < len(prefix) {
		return ""
	}
	if !strings.EqualFold(h[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(h[len(prefix):])
}

// hashToken returns the SHA-256 hex digest of a raw token string. The DB only
// ever stores the hash — the raw token is shown to the user once at mint time
// and is never recoverable.
func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// parseScopes splits a comma-separated scopes string into typed Scope values,
// trimming whitespace and dropping empty entries. Unknown scope strings are
// preserved verbatim — RequireScope still won't match them, but we don't want
// to silently strip them either (makes debugging easier).
func parseScopes(csv string) []Scope {
	if csv == "" {
		return nil
	}
	parts := strings.Split(csv, ",")
	out := make([]Scope, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, Scope(p))
	}
	return out
}

func (a *Auth) upsertUser(subject, email, name string) (*User, error) {
	tx, err := a.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Default role for every login is plain "user". Admin is granted only via
	// out-of-band SQL or via the OIDC_BOOTSTRAP_ADMIN_EMAILS allowlist below.
	// We do NOT promote based on user count — with open OIDC signup that
	// would let the first random visitor inherit destructive privileges.
	role := "user"
	if _, ok := bootstrapAdminEmails()[strings.ToLower(strings.TrimSpace(email))]; ok {
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
		// Existing user: refresh email/name/last_login. Only force role to
		// "admin" if they're in the bootstrap allowlist — never demote
		// existing admins back to "user" via this path (admin removal must
		// be a deliberate SQL action by an operator).
		if role == "admin" {
			if _, uerr := tx.Exec(`
				UPDATE users SET email = ?, name = ?, role = 'admin',
				                 last_login_at = CURRENT_TIMESTAMP
				WHERE id = ?
			`, email, name, id); uerr != nil {
				return nil, uerr
			}
		} else {
			if _, uerr := tx.Exec(`
				UPDATE users SET email = ?, name = ?,
				                 last_login_at = CURRENT_TIMESTAMP
				WHERE id = ?
			`, email, name, id); uerr != nil {
				return nil, uerr
			}
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
	var createdStr, lastLoginStr string
	if err := row.Scan(&u.ID, &u.OIDCSubject, &u.Email, &u.Name, &u.Role,
		&createdStr, &lastLoginStr); err != nil {
		return nil, err
	}
	u.CreatedAt = parseSQLiteTime(createdStr)
	u.LastLoginAt = parseSQLiteTime(lastLoginStr)
	u.Authenticated = true
	return u, nil
}

// parseSQLiteTime decodes the formats SQLite emits for DATETIME columns.
// CURRENT_TIMESTAMP yields "YYYY-MM-DD HH:MM:SS" with no zone; explicit
// inserts may include the T separator and a zone. Falls back to zero time
// if nothing matches — caller treats that as "use the column's default".
func parseSQLiteTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	layouts := []string{
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02T15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02T15:04:05.999999999",
		"2006-01-02 15:04:05-07:00",
		"2006-01-02T15:04:05-07:00",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		time.RFC3339Nano,
		time.RFC3339,
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			return t
		}
	}
	return time.Time{}
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

// safeReturnPath validates a post-login `return=` URL. Only same-origin
// absolute paths are allowed. Anything that could be interpreted by the
// browser as a foreign authority — protocol-relative `//host/...`,
// backslash-mangled `/\host`, or paths containing a backslash that some
// browsers normalize to `/` — is rejected and the caller should fall back to
// `"/"`. We also reject the empty string and any value that doesn't start
// with `/`.
func safeReturnPath(p string) bool {
	if p == "" {
		return false
	}
	if !strings.HasPrefix(p, "/") {
		return false
	}
	// Protocol-relative: //evil.com or /\evil.com (some browsers normalize \ to /).
	if strings.HasPrefix(p, "//") || strings.HasPrefix(p, `/\`) {
		return false
	}
	// Reject any backslashes anywhere in the path — defense against future
	// browser normalizations and tools that translate `\` to `/`.
	if strings.Contains(p, `\`) {
		return false
	}
	return true
}

// bootstrapAdminEmails reads the OIDC_BOOTSTRAP_ADMIN_EMAILS env var and
// returns the lowercased, trimmed set of email addresses that should be
// promoted to role="admin" on login. Returns an empty set when the var is
// unset or empty.
func bootstrapAdminEmails() map[string]struct{} {
	out := map[string]struct{}{}
	for _, e := range strings.Split(os.Getenv("OIDC_BOOTSTRAP_ADMIN_EMAILS"), ",") {
		e = strings.TrimSpace(strings.ToLower(e))
		if e != "" {
			out[e] = struct{}{}
		}
	}
	return out
}

func randomToken(nBytes int) (string, error) {
	buf := make([]byte, nBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
