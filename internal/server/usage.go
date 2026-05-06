// Package server — usage.go contains the per-tool usage recorder used by
// MCP tool handlers. The HTTP transport's UsageRecorder middleware records
// the underlying POST /api/mcp call as a single row; recordToolCall adds
// per-tool granularity ("list_hours called 12 times in the last week")
// that the HTTP layer cannot see.
package server

import (
	"context"
	"database/sql"
	"time"

	"github.com/austin/hours-mcp/internal/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// recordToolCall is called at the END of every MCP tool handler to log a
// per-tool usage row. It is a no-op for session/local users (which carry
// TokenID == 0). The HTTP-side UsageRecorder already records the underlying
// POST /api/mcp call; this adds tool-name granularity that the HTTP layer
// cannot see.
//
// Use as a defer at the top of each tool handler, with named returns so the
// closure observes the final values:
//
//	defer recordToolCall(ctx, db, "list_hours", &result, &err, time.Now())
//
// `result` and `err` are the closure's named return values, captured by
// pointer so we observe the final value at defer time.
//
// Status semantics: 200 if result.IsError == false and err == nil, 500 if
// err != nil, 400 if result.IsError == true (typical scope-failure shape).
//
// Path stored as "tool:<name>" (e.g. "tool:list_hours"); method stored as
// "MCP". The tool name is recorded even for scope failures — they return
// early via scopeError(...) but Go runs deferred funcs on every return.
func recordToolCall(
	ctx context.Context,
	db *sql.DB,
	tool string,
	result **mcp.CallToolResult,
	err *error,
	startedAt time.Time,
) {
	u, ok := auth.UserFromContext(ctx)
	if !ok || u == nil || u.TokenID == 0 {
		return
	}

	// Best-effort error text; either the Go error chain (transport-level
	// failure) or the IsError content text (tool-level user error).
	var errText string
	status := 200
	if err != nil && *err != nil {
		status = 500
		errText = (*err).Error()
		if len(errText) > 256 {
			errText = errText[:256]
		}
	} else if result != nil && *result != nil && (*result).IsError {
		status = 400
		// Pull the first text block, if present, for diagnostic context.
		for _, c := range (*result).Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				errText = tc.Text
				break
			}
		}
		if len(errText) > 256 {
			errText = errText[:256]
		}
	}

	tokenID := u.TokenID
	userID := u.ID
	duration := time.Since(startedAt).Milliseconds()
	path := "tool:" + tool

	go func() {
		_, _ = db.Exec(`
			INSERT INTO api_token_usage
				(token_id, user_id, method, path, status, duration_ms, error)
			VALUES (?, ?, 'MCP', ?, ?, ?, ?)
		`, tokenID, userID, path, status, duration, nullableUsageString(errText))
		_, _ = db.Exec(
			`UPDATE api_tokens SET last_used_at = CURRENT_TIMESTAMP WHERE id = ?`,
			tokenID,
		)
	}()
}

func nullableUsageString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
