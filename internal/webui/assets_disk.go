//go:build !embedui

package webui

import "io/fs"

// embeddedAssets returns nil without the "embedui" build tag: the Vite build output is then read from
// Config.DistDir at runtime (or not needed at all in development mode). This keeps plain `go build`, `go vet` and
// `go test` working without a frontend build.
func embeddedAssets() fs.FS {
	return nil
}
