// Package server embeds the exported web UI. The directory web/ is filled by scripts/start.sh and
// holds only a .gitkeep placeholder until the UI is built.
package server

import (
	"embed"
	"io/fs"
)

//go:embed all:web
var embedded embed.FS

// WebFS returns the exported UI rooted at its index.
func WebFS() fs.FS {
	sub, err := fs.Sub(embedded, "web")
	if err != nil {
		panic(err)
	}
	return sub
}
