package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"

	"github.com/austin/hours-mcp/internal/api"
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
	server.RegisterTools(mcpServer, db)
	if err := mcpServer.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		fmt.Fprintf(os.Stderr, "MCP server error: %v\n", err)
		os.Exit(1)
	}
}

func runHTTP(db *sql.DB, addr string) {
	srv := api.NewServer(db, web.Assets())
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
