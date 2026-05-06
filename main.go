package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"net/http"
	"os"

	"github.com/austin/hours-mcp/internal/api"
	"github.com/austin/hours-mcp/internal/auth"
	"github.com/austin/hours-mcp/internal/database"
	"github.com/austin/hours-mcp/internal/server"
	"github.com/austin/hours-mcp/internal/web"
	"github.com/austin/hours-mcp/internal/wailsapp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

// version is set by build-time ldflags
var version = "dev"

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Printf("hours-mcp version %s\n", version)
		os.Exit(0)
	}

	fs := flag.NewFlagSet("hours-mcp", flag.ExitOnError)
	serve := fs.Bool("serve", false, "Run HTTP+frontend server instead of the native app")
	mcpMode := fs.Bool("mcp", false, "Run MCP stdio server (for Claude Desktop)")
	addr := fs.String("addr", ":7878", "HTTP listen address (when --serve)")
	_ = fs.Parse(os.Args[1:])

	db, err := database.Initialize()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	switch {
	case *mcpMode:
		runMCP(db)
	case *serve:
		runHTTP(db, *addr)
	default:
		runGUI(db)
	}
}

func runMCP(db *sql.DB) {
	mcpServer := mcp.NewServer(&mcp.Implementation{
		Name:    "hours-mcp",
		Version: version,
	}, nil)
	server.RegisterTools(mcpServer, db, server.TransportStdio)
	// Stdio MCP is the local desktop path — no OIDC, single user.
	ctx := auth.WithLocalUser(context.Background(), database.DefaultUserID)
	if err := mcpServer.Run(ctx, &mcp.StdioTransport{}); err != nil {
		fmt.Fprintf(os.Stderr, "MCP server error: %v\n", err)
		os.Exit(1)
	}
}

func runHTTP(db *sql.DB, addr string) {
	a, err := auth.NewFromEnv(context.Background(), db)
	if err != nil {
		fmt.Fprintf(os.Stderr, "OIDC init failed: %v\n", err)
		os.Exit(1)
	}
	if !a.Enabled() {
		fmt.Fprintln(os.Stderr,
			"OIDC env vars required for --serve mode. Set OIDC_ISSUER, OIDC_CLIENT_ID, "+
				"OIDC_CLIENT_SECRET, OIDC_REDIRECT_URL.")
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, "OIDC auth enabled")
	srv := api.NewServerWithAuth(db, web.Assets(), a)

	// Mount the remote MCP endpoint at /api/mcp. Tool handlers are
	// tenant-scoped via userIDFromCtx and gated per-tool via the scope
	// helper in internal/server/scope.go, so token callers only see the
	// scopes they were minted with. TransportHTTP suppresses the whole-DB
	// backup/restore tools — those are stdio-only.
	mcpServer := mcp.NewServer(&mcp.Implementation{
		Name:    "hours-mcp",
		Version: version,
	}, nil)
	server.RegisterTools(mcpServer, db, server.TransportHTTP)
	// Stateless: no session map. Every request gets a fresh transient
	// transport using the user/scopes from the bearer token in the request
	// context. This sidesteps the SDK's in-memory session tracking, which
	// dies on every container restart and on any network blip, forcing
	// Claude Desktop users to restart the app. We don't issue
	// server->client notifications or progress events, so the only
	// behaviour we lose by going stateless is one we never used.
	mcpHTTP := mcp.NewStreamableHTTPHandler(func(req *http.Request) *mcp.Server {
		return mcpServer
	}, &mcp.StreamableHTTPOptions{Stateless: true})
	srv.AttachMCPHandler(mcpHTTP)

	if err := srv.ListenAndServe(addr); err != nil {
		fmt.Fprintf(os.Stderr, "HTTP server error: %v\n", err)
		os.Exit(1)
	}
}

func runGUI(db *sql.DB) {
	app := wailsapp.NewApp(db)
	assets := web.AssetsEmbed()

	err := wails.Run(&options.App{
		Title:     "Hours",
		Width:     1280,
		Height:    820,
		MinWidth:  960,
		MinHeight: 620,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 244, G: 242, B: 238, A: 1},
		OnStartup:        app.Startup,
		OnShutdown:       app.Shutdown,
		Bind:             []interface{}{app},
		Mac: &mac.Options{
			TitleBar:             mac.TitleBarHiddenInset(),
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			About: &mac.AboutInfo{
				Title:   "Hours",
				Message: "Quiet, premium time tracking and invoicing.",
			},
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "GUI error: %v\n", err)
		os.Exit(1)
	}
}
