// Package server — scope.go contains the per-tool scope-enforcement helpers
// used by every MCP tool handler. The HTTP MCP transport may carry bearer
// tokens whose scope set is narrower than ScopeAll, so each tool must
// re-check its required scope before touching the database.
//
// Stdio (Wails / local) traffic carries the synthetic local user with
// ScopeAll, so these checks are no-ops on that path — that's intentional;
// don't special-case the transport in tool handlers.
package server

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/austin/hours-mcp/internal/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// requireScopes returns a non-nil error if the calling identity lacks ANY of
// the required scopes. Use this at the top of every tool handler that touches
// data; sessions/local users implicitly satisfy every scope.
//
// On scope failure the returned error renders as a user-visible MCP error,
// not a transport-level failure — the client sees "permission denied: ..."
// in the tool result and can react.
func requireScopes(ctx context.Context, scopes ...auth.Scope) error {
	u, ok := auth.UserFromContext(ctx)
	if !ok || u == nil {
		return fmt.Errorf("permission denied: not authenticated")
	}
	var missing []string
	for _, s := range scopes {
		if !auth.HasScope(u.Scopes, s) {
			missing = append(missing, string(s))
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("permission denied: missing scope(s) %s", strings.Join(missing, ", "))
	}
	return nil
}

// scopeError wraps a scope-check failure into the MCP CallToolResult shape so
// the caller sees it as a tool-level error (returning Content with IsError
// rather than a transport error). Pair with `return scopeError(err), nil, nil`.
func scopeError(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
	}
}

// wrapList packages a slice as the structuredContent value of an MCP tool
// result. The MCP spec requires structuredContent to be an object — clients
// like Claude.ai validate this at the Pydantic layer and reject bare arrays
// with "Input should be a valid dictionary". Always shape list-style
// responses through this helper so the response is `{count, <key>: [...]}`.
//
// Example: `return result, wrapList("clients", clients), nil`.
//
// Extra fields (e.g. an aggregate total) can be merged via wrapListWith.
func wrapList(key string, items any) map[string]any {
	return map[string]any{
		"count": sliceLen(items),
		key:     items,
	}
}

// wrapListWith is wrapList plus extra top-level fields. Use it when the tool
// has a meaningful summary value alongside the list — e.g. total_hours on
// list_hours.
func wrapListWith(key string, items any, extra map[string]any) map[string]any {
	out := wrapList(key, items)
	for k, v := range extra {
		out[k] = v
	}
	return out
}

// sliceLen returns the length of a slice/array/map/string passed via `any`.
// Returns 0 for nil or non-lengthable values — defensive; callers always
// pass a slice.
func sliceLen(v any) int {
	if v == nil {
		return 0
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map, reflect.Chan, reflect.String:
		return rv.Len()
	}
	return 0
}
