//go:build embedui

package webui

import (
	"embed"
	"io/fs"
)

// The Vite build output (frontend: npm run build). The "all:" prefix keeps files whose names start with "_" or
// ".", such as Rollup helper chunks and .vite/manifest.json.
//
//go:embed all:dist
var embeddedDist embed.FS

// embeddedAssets returns the Vite build output compiled into the binary.
func embeddedAssets() fs.FS {
	sub, err := fs.Sub(embeddedDist, "dist")
	if err != nil {
		panic(err) // Unreachable: the embed directive guarantees the "dist" directory exists
	}
	return sub
}
