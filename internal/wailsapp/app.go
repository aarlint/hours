package wailsapp

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/austin/hours-mcp/internal/api"
	"github.com/austin/hours-mcp/internal/web"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is the struct exposed to the Wails JS frontend via auto-generated
// bindings. All method signatures here become callable from TypeScript as
// `window.go.main.App.MethodName(...)`.
//
// We keep the binding surface intentionally tiny: a single Request() method
// that dispatches through the existing HTTP mux. This lets us reuse every
// handler unchanged while still giving the frontend a direct Go call (no
// network hop, no port). The frontend keeps its familiar fetch-shaped api.ts
// layer — only the underlying transport flips from fetch to Wails.
type App struct {
	ctx     context.Context
	db      *sql.DB
	handler http.Handler
}

// NewApp wires the Wails app to the same DB + HTTP mux the standalone --serve
// mode uses, so every route is available to the frontend via Request().
func NewApp(db *sql.DB) *App {
	srv := api.NewServer(db, web.Assets())
	return &App{
		db:      db,
		handler: srv.Handler(),
	}
}

// Startup is invoked by Wails once the frontend is ready. We capture the
// context for later runtime calls (EventsEmit) and register ourselves as the
// external event listener so every DB-derived broadcast is also pushed to the
// JS side via the Wails event bus.
//
// The listener ignores the userID parameter — the Wails GUI is single-user by
// definition, so every event already belongs to the local user.
func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
	api.SetEventListener(func(_ int64, kind string, data map[string]any) {
		wailsruntime.EventsEmit(ctx, kind, data)
	})
	// Emit an initial "hello" so the frontend knows the transport is live.
	wailsruntime.EventsEmit(ctx, "hello", map[string]any{"ok": true})
}

// Shutdown clears the event listener so a subsequent relaunch (rare for a
// desktop app, but cheap to handle) doesn't leak a dead ctx.
func (a *App) Shutdown(ctx context.Context) {
	api.SetEventListener(nil)
}

// Response is the shape returned to JS for every Request() call. We surface
// the HTTP status so the frontend can preserve its existing error-handling
// pattern (throw on non-2xx, read error JSON body).
type Response struct {
	Status int    `json:"status"`
	Body   string `json:"body"`
}

// Request dispatches an HTTP-shaped call through the in-process mux. This is
// the single binding point: path handling, query params, path params, and
// JSON encoding all stay inside the existing handlers.
func (a *App) Request(method, path, body string) (Response, error) {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rr := httptest.NewRecorder()
	a.handler.ServeHTTP(rr, req)
	return Response{
		Status: rr.Code,
		Body:   rr.Body.String(),
	}, nil
}

// PickDirectory opens the native folder picker and returns the selected
// absolute path, or "" if the user cancelled.
func (a *App) PickDirectory(title string) (string, error) {
	if title == "" {
		title = "Select folder"
	}
	return wailsruntime.OpenDirectoryDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title: title,
	})
}

// SaveTextFile prompts the user with the native save dialog and writes the
// supplied content to the chosen path. Returns the absolute path on success
// or "" when the user cancels.
//
// This is the proper way to "download" data in the Wails webview — the
// browser's <a download> shim doesn't trigger a real save in the embedded
// webkit, so without this binding the file silently evaporates.
func (a *App) SaveTextFile(suggestedName, content string) (string, error) {
	if suggestedName == "" {
		suggestedName = "export.txt"
	}
	defaultDir := ""
	if home, err := os.UserHomeDir(); err == nil {
		defaultDir = filepath.Join(home, "Downloads")
	}
	path, err := wailsruntime.SaveFileDialog(a.ctx, wailsruntime.SaveDialogOptions{
		Title:                "Save export",
		DefaultFilename:      suggestedName,
		DefaultDirectory:     defaultDir,
		CanCreateDirectories: true,
	})
	if err != nil || path == "" {
		return "", err
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// savePDFThroughDialog dispatches a POST through the in-process HTTP mux,
// reads the resulting PDF bytes, then prompts the user with a native
// Save-As dialog and writes the bytes to the chosen path. Used by
// SaveInvoicePDF and SaveQuotePDF.
//
// Returns the absolute path the user picked, or "" if they cancelled.
// We surface upstream HTTP errors as Go errors so the JS layer can show them.
func (a *App) savePDFThroughDialog(apiPath, suggestedFilename string) (string, error) {
	req := httptest.NewRequest(http.MethodPost, apiPath, nil)
	rr := httptest.NewRecorder()
	a.handler.ServeHTTP(rr, req)
	if rr.Code < 200 || rr.Code >= 300 {
		// The handler emits JSON {"error": "..."} on failure. Surface that
		// verbatim so the frontend gets the same error shape as the web path.
		body := strings.TrimSpace(rr.Body.String())
		if body == "" {
			body = http.StatusText(rr.Code)
		}
		return "", fmt.Errorf("%s", body)
	}

	chosen, err := wailsruntime.SaveFileDialog(a.ctx, wailsruntime.SaveDialogOptions{
		Title:           "Save PDF",
		DefaultFilename: suggestedFilename,
	})
	if err != nil {
		return "", err
	}
	if chosen == "" {
		return "", nil // user cancelled — frontend treats this as a no-op
	}
	if err := os.WriteFile(chosen, rr.Body.Bytes(), 0o644); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}
	return chosen, nil
}

// SaveInvoicePDF generates the invoice PDF in-process, prompts the user for
// a destination path via the native Save dialog, and writes the bytes there.
// Returns the chosen path, or "" if the user cancelled.
func (a *App) SaveInvoicePDF(invoiceNumber, suggestedFilename string) (string, error) {
	if invoiceNumber == "" {
		return "", fmt.Errorf("invoiceNumber required")
	}
	if suggestedFilename == "" {
		suggestedFilename = "invoice.pdf"
	}
	apiPath := "/api/invoices/" + url.PathEscape(invoiceNumber) + "/download"
	return a.savePDFThroughDialog(apiPath, suggestedFilename)
}

// SaveQuotePDF mirrors SaveInvoicePDF for quotes.
func (a *App) SaveQuotePDF(quoteNumber, suggestedFilename string) (string, error) {
	if quoteNumber == "" {
		return "", fmt.Errorf("quoteNumber required")
	}
	if suggestedFilename == "" {
		suggestedFilename = "quote.pdf"
	}
	apiPath := "/api/quotes/" + url.PathEscape(quoteNumber) + "/download"
	return a.savePDFThroughDialog(apiPath, suggestedFilename)
}

// RevealInFinder opens the enclosing folder of the given path and selects
// the file (macOS). On Linux/Windows it falls back to opening the folder.
func (a *App) RevealInFinder(path string) error {
	if path == "" {
		return nil
	}
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", "-R", path).Start()
	case "windows":
		return exec.Command("explorer", "/select,", path).Start()
	default:
		return exec.Command("xdg-open", filepath.Dir(path)).Start()
	}
}
