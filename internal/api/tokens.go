// Package api — tokens.go implements the personal API token CRUD surface.
//
// Endpoints (all session-only — RequireSession blocks bearer-token callers
// so a leaked token cannot mint or revoke siblings):
//
//	GET    /api/tokens          list my tokens (metadata only)
//	POST   /api/tokens          mint a new token; raw value returned ONCE
//	DELETE /api/tokens/{id}     soft-revoke (sets revoked_at)
//
// Tokens are formatted "ht_" + 32 hex chars from crypto/rand. The DB stores
// the SHA-256 hash plus the first 12 chars (the "ht_" prefix + 8 random
// chars) for visual identification in the UI. The raw token is shown to the
// user exactly once at mint time and is then unrecoverable.
package api

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/austin/hours-mcp/internal/auth"
)

// rawTokenPrefix is the literal "ht_" prefix on every minted token. Stored
// alongside the random suffix in token_prefix so the UI can render
// "ht_abcd1234..." as a stable identifier.
const rawTokenPrefix = "ht_"

// tokenPrefixLen is the visible-prefix length stored in the DB (and shown in
// the UI) — `ht_` plus 8 hex chars from the random suffix = 11 chars total.
const tokenPrefixLen = len(rawTokenPrefix) + 8

// apiTokenDTO is the metadata shape returned by GET /api/tokens. The raw
// token is intentionally absent — that's only ever returned by the mint
// endpoint, once.
type apiTokenDTO struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	Scopes      []string   `json:"scopes"`
	TokenPrefix string     `json:"token_prefix"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// apiTokenWithSecretDTO extends apiTokenDTO with the freshly-minted raw
// token. Returned exactly once from POST /api/tokens.
type apiTokenWithSecretDTO struct {
	apiTokenDTO
	Token string `json:"token"`
}

type createTokenReq struct {
	Name      string   `json:"name"`
	Scopes    []string `json:"scopes"`
	ExpiresAt *string  `json:"expires_at,omitempty"`
}

// listAPITokens returns every non-revoked token belonging to the caller.
// Filtered by user_id for multi-tenant isolation.
func (h *handlers) listAPITokens(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	userID, err := currentUserID(r)
	if err != nil {
		return nil, err
	}
	rows, err := h.db.Query(`
		SELECT id, name, scopes, token_prefix, expires_at, last_used_at, created_at
		FROM api_tokens
		WHERE user_id = ? AND revoked_at IS NULL
		ORDER BY created_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []apiTokenDTO{}
	for rows.Next() {
		var (
			dto         apiTokenDTO
			scopesCSV   string
			expiresStr  sql.NullString
			lastUsedStr sql.NullString
			createdStr  string
		)
		if err := rows.Scan(&dto.ID, &dto.Name, &scopesCSV, &dto.TokenPrefix,
			&expiresStr, &lastUsedStr, &createdStr); err != nil {
			return nil, err
		}
		dto.Scopes = splitScopes(scopesCSV)
		if expiresStr.Valid && expiresStr.String != "" {
			t := parseTime(expiresStr.String)
			if !t.IsZero() {
				dto.ExpiresAt = &t
			}
		}
		if lastUsedStr.Valid && lastUsedStr.String != "" {
			t := parseTime(lastUsedStr.String)
			if !t.IsZero() {
				dto.LastUsedAt = &t
			}
		}
		dto.CreatedAt = parseTime(createdStr)
		out = append(out, dto)
	}
	return out, nil
}

// createAPIToken validates the request, generates a fresh token, persists the
// SHA-256 hash, and returns the raw token to the caller. The raw value is
// never written to disk — only the hash and a short prefix.
func (h *handlers) createAPIToken(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	userID, err := currentUserID(r)
	if err != nil {
		return nil, err
	}
	u, _ := auth.UserFromContext(r.Context())
	isAdmin := u != nil && u.Role == "admin"

	var req createTokenReq
	if err := decodeBody(r, &req); err != nil {
		return nil, newAPIError(http.StatusBadRequest, "invalid body: %v", err)
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, newAPIError(http.StatusBadRequest, "name is required")
	}
	if len(name) > 100 {
		return nil, newAPIError(http.StatusBadRequest, "name must be 100 chars or fewer")
	}

	if len(req.Scopes) == 0 {
		return nil, newAPIError(http.StatusBadRequest, "at least one scope is required")
	}

	// Validate, dedupe, and reject privilege escalation. Non-admin users may
	// not mint "*" or "data:import" tokens — defense-in-depth on top of the
	// per-route role check on /api/import.
	seen := map[string]struct{}{}
	cleaned := make([]string, 0, len(req.Scopes))
	for _, raw := range req.Scopes {
		s := auth.Scope(strings.TrimSpace(raw))
		if s == "" {
			continue
		}
		if !auth.IsKnownScope(s) {
			return nil, newAPIError(http.StatusBadRequest, "unknown scope: %s", s)
		}
		if !isAdmin && (s == auth.ScopeAll || s == auth.ScopeDataImport) {
			return nil, newAPIError(
				http.StatusForbidden,
				"scope %s is admin-only", s,
			)
		}
		if _, dup := seen[string(s)]; dup {
			continue
		}
		seen[string(s)] = struct{}{}
		cleaned = append(cleaned, string(s))
	}
	if len(cleaned) == 0 {
		return nil, newAPIError(http.StatusBadRequest, "at least one scope is required")
	}

	var expiresAt sql.NullTime
	if req.ExpiresAt != nil && strings.TrimSpace(*req.ExpiresAt) != "" {
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(*req.ExpiresAt))
		if err != nil {
			return nil, newAPIError(http.StatusBadRequest,
				"invalid expires_at — expected RFC3339: %v", err)
		}
		if !t.After(time.Now()) {
			return nil, newAPIError(http.StatusBadRequest,
				"expires_at must be in the future")
		}
		expiresAt = sql.NullTime{Time: t.UTC(), Valid: true}
	}

	rawToken, err := mintToken()
	if err != nil {
		return nil, err
	}
	hash := sha256Hex(rawToken)
	prefix := rawToken[:tokenPrefixLen]
	scopesCSV := strings.Join(cleaned, ",")

	var (
		id         int64
		createdStr string
	)
	row := h.db.QueryRow(`
		INSERT INTO api_tokens (user_id, name, token_hash, token_prefix, scopes, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)
		RETURNING id, created_at
	`, userID, name, hash, prefix, scopesCSV, nullableTime(expiresAt))
	if err := row.Scan(&id, &createdStr); err != nil {
		return nil, err
	}

	resp := apiTokenWithSecretDTO{
		apiTokenDTO: apiTokenDTO{
			ID:          id,
			Name:        name,
			Scopes:      cleaned,
			TokenPrefix: prefix,
			CreatedAt:   parseTime(createdStr),
		},
		Token: rawToken,
	}
	if expiresAt.Valid {
		t := expiresAt.Time
		resp.ExpiresAt = &t
	}
	return resp, nil
}

// revokeAPIToken soft-deletes by setting revoked_at. Filtered by user_id so
// users cannot revoke each other's tokens. 404 when the id doesn't exist for
// the caller (or is already revoked) to avoid leaking the existence of other
// users' tokens.
func (h *handlers) revokeAPIToken(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	userID, err := currentUserID(r)
	if err != nil {
		return nil, err
	}
	id, err := pathInt(r, "id")
	if err != nil {
		return nil, newAPIError(http.StatusBadRequest, "%s", err.Error())
	}
	res, err := h.db.Exec(`
		UPDATE api_tokens
		SET revoked_at = CURRENT_TIMESTAMP
		WHERE id = ? AND user_id = ? AND revoked_at IS NULL
	`, id, userID)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, newAPIError(http.StatusNotFound, "token not found")
	}
	return map[string]int64{"deleted": n}, nil
}

// ---------- usage ----------

// tokenUsageSummaryDTO is the shape returned by GET /api/tokens/{id}/usage.
// All counters are computed against api_token_usage filtered by token_id,
// scoped to the calling user so tenants cannot probe each other's tokens.
type tokenUsageSummaryDTO struct {
	TotalCalls  int64                  `json:"total_calls"`
	Calls24h    int64                  `json:"calls_24h"`
	Calls7d     int64                  `json:"calls_7d"`
	Calls30d    int64                  `json:"calls_30d"`
	Errors24h   int64                  `json:"errors_24h"`
	LastCallAt  *time.Time             `json:"last_call_at"`
	LastPath    *string                `json:"last_path"`
	LastMethod  *string                `json:"last_method"`
	LastStatus  *int                   `json:"last_status"`
	ByPath      []tokenUsageByPathDTO  `json:"by_path"`
}

type tokenUsageByPathDTO struct {
	Path   string    `json:"path"`
	Count  int64     `json:"count"`
	Errors int64     `json:"errors"`
	LastAt time.Time `json:"last_at"`
}

type tokenUsageEventDTO struct {
	Method     string    `json:"method"`
	Path       string    `json:"path"`
	Status     int       `json:"status"`
	DurationMs int64     `json:"duration_ms"`
	Error      *string   `json:"error"`
	CreatedAt  time.Time `json:"created_at"`
}

// getAPITokenUsage returns aggregate counters for a single token. Filtered by
// (token_id, user_id) so a user cannot read another user's stats by guessing
// token ids. 404 when the token doesn't exist (or belongs to someone else)
// to avoid leaking existence.
func (h *handlers) getAPITokenUsage(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	userID, err := currentUserID(r)
	if err != nil {
		return nil, err
	}
	id, err := pathInt(r, "id")
	if err != nil {
		return nil, newAPIError(http.StatusBadRequest, "%s", err.Error())
	}

	// Verify the token is owned by the caller. We don't check revoked_at —
	// usage history for a revoked token is still useful to inspect.
	var ownerID int64
	err = h.db.QueryRow(
		`SELECT user_id FROM api_tokens WHERE id = ?`, id,
	).Scan(&ownerID)
	if err == sql.ErrNoRows || (err == nil && ownerID != userID) {
		return nil, newAPIError(http.StatusNotFound, "token not found")
	}
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	var out tokenUsageSummaryDTO

	// Single-shot aggregate query for the four counters + error count. We
	// compute the windows in Go so SQLite's date math doesn't surprise us
	// across timezones; api_token_usage.created_at is always written as UTC
	// CURRENT_TIMESTAMP.
	now := time.Now().UTC()
	cutoff24 := now.Add(-24 * time.Hour).Format("2006-01-02 15:04:05")
	cutoff7d := now.Add(-7 * 24 * time.Hour).Format("2006-01-02 15:04:05")
	cutoff30d := now.Add(-30 * 24 * time.Hour).Format("2006-01-02 15:04:05")

	// COALESCE the SUMs because SQLite returns NULL for SUM over zero rows,
	// and Scan into int64 fails on NULL. COUNT(*) is always 0+ so no COALESCE
	// needed there, but symmetry is nicer.
	err = h.db.QueryRow(`
		SELECT
			COALESCE(COUNT(*), 0),
			COALESCE(SUM(CASE WHEN created_at >= ? THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN created_at >= ? THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN created_at >= ? THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN created_at >= ? AND status >= 400 THEN 1 ELSE 0 END), 0)
		FROM api_token_usage
		WHERE token_id = ? AND user_id = ?
	`, cutoff24, cutoff7d, cutoff30d, cutoff24, id, userID).Scan(
		&out.TotalCalls, &out.Calls24h, &out.Calls7d, &out.Calls30d, &out.Errors24h,
	)
	if err != nil {
		return nil, err
	}

	// Most recent call — separate query so the LIMIT 1 can use the index.
	var (
		lastMethod    sql.NullString
		lastPath      sql.NullString
		lastStatus    sql.NullInt64
		lastCreatedAt sql.NullString
	)
	err = h.db.QueryRow(`
		SELECT method, path, status, created_at
		FROM api_token_usage
		WHERE token_id = ? AND user_id = ?
		ORDER BY id DESC LIMIT 1
	`, id, userID).Scan(&lastMethod, &lastPath, &lastStatus, &lastCreatedAt)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if lastCreatedAt.Valid && lastCreatedAt.String != "" {
		t := parseTime(lastCreatedAt.String)
		if !t.IsZero() {
			out.LastCallAt = &t
		}
	}
	if lastMethod.Valid {
		s := lastMethod.String
		out.LastMethod = &s
	}
	if lastPath.Valid {
		s := lastPath.String
		out.LastPath = &s
	}
	if lastStatus.Valid {
		v := int(lastStatus.Int64)
		out.LastStatus = &v
	}

	// Top paths — bounded so a token with thousands of distinct paths
	// can't blow up the response payload.
	rows, err := h.db.Query(`
		SELECT path, COUNT(*) AS cnt,
		       SUM(CASE WHEN status >= 400 THEN 1 ELSE 0 END) AS errs,
		       MAX(created_at) AS last_at
		FROM api_token_usage
		WHERE token_id = ? AND user_id = ?
		GROUP BY path
		ORDER BY cnt DESC, last_at DESC
		LIMIT 10
	`, id, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out.ByPath = []tokenUsageByPathDTO{}
	for rows.Next() {
		var (
			row     tokenUsageByPathDTO
			lastStr string
		)
		if err := rows.Scan(&row.Path, &row.Count, &row.Errors, &lastStr); err != nil {
			return nil, err
		}
		row.LastAt = parseTime(lastStr)
		out.ByPath = append(out.ByPath, row)
	}
	return out, nil
}

// getAPITokenUsageRecent returns the 50 most recent usage rows for a token,
// newest first. Same ownership filter as getAPITokenUsage.
func (h *handlers) getAPITokenUsageRecent(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	userID, err := currentUserID(r)
	if err != nil {
		return nil, err
	}
	id, err := pathInt(r, "id")
	if err != nil {
		return nil, newAPIError(http.StatusBadRequest, "%s", err.Error())
	}

	var ownerID int64
	err = h.db.QueryRow(
		`SELECT user_id FROM api_tokens WHERE id = ?`, id,
	).Scan(&ownerID)
	if err == sql.ErrNoRows || (err == nil && ownerID != userID) {
		return nil, newAPIError(http.StatusNotFound, "token not found")
	}
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	rows, err := h.db.Query(`
		SELECT method, path, status, duration_ms, error, created_at
		FROM api_token_usage
		WHERE token_id = ? AND user_id = ?
		ORDER BY id DESC
		LIMIT 50
	`, id, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []tokenUsageEventDTO{}
	for rows.Next() {
		var (
			ev         tokenUsageEventDTO
			errStr     sql.NullString
			createdStr string
		)
		if err := rows.Scan(&ev.Method, &ev.Path, &ev.Status, &ev.DurationMs, &errStr, &createdStr); err != nil {
			return nil, err
		}
		if errStr.Valid && errStr.String != "" {
			s := errStr.String
			ev.Error = &s
		}
		ev.CreatedAt = parseTime(createdStr)
		out = append(out, ev)
	}
	return out, nil
}

// ---------- helpers ----------

// mintToken generates 32 cryptographically-random bytes, hex-encodes them,
// and prefixes "ht_". Result is 3 + 64 = 67 chars.
func mintToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return rawTokenPrefix + hex.EncodeToString(buf), nil
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func splitScopes(csv string) []string {
	if csv == "" {
		return []string{}
	}
	parts := strings.Split(csv, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

// parseTime is a thin wrapper around the auth package's SQLite-time parser.
// We re-implement here to avoid widening the auth API surface for an internal
// helper. Failures return the zero time, which the marshaller emits as null.
func parseTime(s string) time.Time {
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

// nullableTime returns the time value for a sql.NullTime, or nil when not
// valid, in a form Exec can accept. SQLite driver handles nil as NULL.
func nullableTime(t sql.NullTime) interface{} {
	if !t.Valid {
		return nil
	}
	return t.Time
}
