package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// Assets returns the embedded frontend file system rooted at dist/.
// Returns nil if the dist directory is empty (frontend not built).
func Assets() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return nil
	}
	// Quick sanity check: is there an index.html?
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil
	}
	return sub
}

// AssetsEmbed returns the raw embed.FS so Wails' assetserver can consume it
// directly. Wails handles its own path resolution; we expose the bytes.
func AssetsEmbed() embed.FS {
	return distFS
}
