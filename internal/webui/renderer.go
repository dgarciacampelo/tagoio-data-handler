// Package webui renders the HTML shell of the Svelte (Vite) pages.
//
// The Go server keeps ownership of routing, authentication and the HTML document, while each page's UI is a Svelte
// application mounted into <div id="app">:
//   - Development mode: scripts are loaded from the Vite dev server (Hot Module Replacement).
//   - Production mode: the hashed bundles listed in Vite's manifest are served under /assets/, read from the build
//     output embedded in the binary (build tag "embedui") or, without that tag, from Config.DistDir on disk.
//
// A page entry "<page>" is the module frontend/src/pages/<page>/main.ts.
package webui

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path"
	"strings"
)

// manifestPath is the location of Vite's build manifest inside the build output.
const manifestPath = ".vite/manifest.json"

// Config defines where the page assets come from.
type Config struct {
	// DevServerURL is the Vite dev server origin (e.g. "http://localhost:5173"). When set, the renderer runs in
	// development mode and ignores the build output.
	DevServerURL string

	// DistDir is the Vite build output directory, used in production mode when the binary was compiled without
	// the "embedui" build tag.
	DistDir string
}

// Page describes a single Svelte page render.
type Page struct {
	Entry     string // Page entry name: frontend/src/pages/<Entry>/main.ts
	Title     string // Document title
	Bootstrap any    // Serialized as JSON into <script id="bootstrap"> (frontend/src/lib/bootstrap.ts)
}

// pageAssets lists the resolved tags for one page entry.
type pageAssets struct {
	Script      string
	Stylesheets []string
	Preloads    []string
}

// Renderer renders page shells and serves the built assets.
type Renderer struct {
	devServerURL string
	assets       fs.FS                 // Build output root (nil in development mode)
	pages        map[string]pageAssets // Resolved production assets per page entry
	disabled     error                 // Set when the UI cannot be served (missing build)
}

// New creates a Renderer. In production mode it fails when the Vite build output (and its manifest) is missing.
func New(cfg Config) (*Renderer, error) {
	if cfg.DevServerURL != "" {
		return &Renderer{devServerURL: strings.TrimSuffix(cfg.DevServerURL, "/")}, nil
	}

	assets := embeddedAssets()
	if assets == nil {
		if cfg.DistDir == "" {
			return nil, errors.New("webui: no embedded assets and no DistDir configured")
		}
		assets = os.DirFS(cfg.DistDir)
	}

	pages, err := loadManifest(assets)
	if err != nil {
		return nil, err
	}
	return &Renderer{assets: assets, pages: pages}, nil
}

// Disabled returns a Renderer that answers every page with 503, so the machine endpoints keep working when the
// frontend build is missing.
func Disabled(reason error) *Renderer {
	return &Renderer{disabled: reason}
}

// IsDevelopment reports whether the renderer loads scripts from the Vite dev server.
func (r *Renderer) IsDevelopment() bool {
	return r.devServerURL != ""
}

// Render writes the HTML shell of a page.
func (r *Renderer) Render(w http.ResponseWriter, p Page) {
	if r.disabled != nil {
		http.Error(w, "The user interface is temporarily unavailable.", http.StatusServiceUnavailable)
		return
	}

	data := shellData{Title: p.Title, Bootstrap: p.Bootstrap}
	if r.IsDevelopment() {
		data.DevClient = r.devServerURL + "/@vite/client"
		data.Script = r.devServerURL + "/" + entrySource(p.Entry)
	} else {
		assets, ok := r.pages[p.Entry]
		if !ok {
			slog.Error("webui: page entry not found in Vite manifest", "entry", p.Entry)
			http.Error(w, "Page not available.", http.StatusInternalServerError)
			return
		}
		data.Script = assets.Script
		data.Stylesheets = assets.Stylesheets
		data.Preloads = assets.Preloads
	}

	// Render into a buffer first, so template errors never produce half-written pages.
	var buf bytes.Buffer
	if err := shellTemplate.Execute(&buf, data); err != nil {
		slog.Error("webui: failed to render page shell", "entry", p.Entry, "error", err)
		http.Error(w, "Failed to render page.", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache") // Always revalidate: the shell references hashed bundles
	_, _ = w.Write(buf.Bytes())
}

// AssetsHandler serves the hashed build output under /assets/ with immutable caching. In development mode (or
// when disabled) the assets come from the Vite dev server, so it answers 404.
func (r *Renderer) AssetsHandler() http.Handler {
	if r.assets == nil {
		return http.NotFoundHandler()
	}

	fileServer := http.FileServerFS(r.assets)
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// Only the bundles under assets/ are public (never .vite/manifest.json), without directory listings, and
		// 404s must not get the long-lived cache header.
		name := strings.TrimPrefix(path.Clean(req.URL.Path), "/")
		if !strings.HasPrefix(name, "assets/") {
			http.NotFound(w, req)
			return
		}
		if info, err := fs.Stat(r.assets, name); err != nil || info.IsDir() {
			http.NotFound(w, req)
			return
		}
		// Vite names bundles with a content hash, so they never change once published.
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		fileServer.ServeHTTP(w, req)
	})
}

// region Vite manifest

// manifestChunk is a single entry of Vite's .vite/manifest.json.
type manifestChunk struct {
	File    string   `json:"file"`
	Src     string   `json:"src"`
	IsEntry bool     `json:"isEntry"`
	Imports []string `json:"imports"`
	CSS     []string `json:"css"`
}

// loadManifest reads Vite's manifest and resolves the tags of every page entry.
func loadManifest(assets fs.FS) (map[string]pageAssets, error) {
	raw, err := fs.ReadFile(assets, manifestPath)
	if err != nil {
		return nil, fmt.Errorf("webui: reading Vite manifest (run 'npm run build' in frontend/): %w", err)
	}

	var manifest map[string]manifestChunk
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, fmt.Errorf("webui: parsing Vite manifest: %w", err)
	}

	pages := make(map[string]pageAssets)
	for key, chunk := range manifest {
		entry, ok := pageEntryName(key)
		if !ok || !chunk.IsEntry {
			continue
		}
		pages[entry] = resolvePageAssets(manifest, key)
	}
	if len(pages) == 0 {
		return nil, errors.New("webui: Vite manifest contains no page entries (src/pages/<page>/main.ts)")
	}
	return pages, nil
}

// Source layout of the page entries, relative to frontend/ (keep in sync with vite.config.ts).
const (
	pagesDir  = "src/pages/"
	entryFile = "/main.ts"
)

// entrySource returns the source path of a page entry, relative to frontend/.
func entrySource(entry string) string {
	return pagesDir + entry + entryFile
}

// pageEntryName is the inverse of entrySource: it extracts the entry name from a manifest key.
func pageEntryName(key string) (string, bool) {
	rest, ok := strings.CutSuffix(key, entryFile)
	if !ok {
		return "", false
	}
	page, ok := strings.CutPrefix(rest, pagesDir)
	if !ok || page == "" || strings.Contains(page, "/") {
		return "", false
	}
	return page, true
}

// resolvePageAssets collects the entry script, its stylesheets and the stylesheets and module preloads of every
// statically imported chunk (depth-first, without duplicates).
func resolvePageAssets(manifest map[string]manifestChunk, entryKey string) pageAssets {
	entry := manifest[entryKey]
	result := pageAssets{Script: "/" + entry.File}

	seenCSS := make(map[string]bool)
	addCSS := func(files []string) {
		for _, file := range files {
			if !seenCSS[file] {
				seenCSS[file] = true
				result.Stylesheets = append(result.Stylesheets, "/"+file)
			}
		}
	}
	addCSS(entry.CSS)

	seenChunks := map[string]bool{entryKey: true}
	var walk func(imports []string)
	walk = func(imports []string) {
		for _, key := range imports {
			if seenChunks[key] {
				continue
			}
			seenChunks[key] = true
			chunk := manifest[key]
			result.Preloads = append(result.Preloads, "/"+chunk.File)
			addCSS(chunk.CSS)
			walk(chunk.Imports)
		}
	}
	walk(entry.Imports)
	return result
}

// endregion

// region HTML shell

type shellData struct {
	Title       string
	Bootstrap   any
	DevClient   string
	Script      string
	Stylesheets []string
	Preloads    []string
}

// favicon is the bolt of the brand, in its forest green.
const favicon = `data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='%2300571b'><path d='M11 21h-1l1-7H7.5c-.58 0-.57-.32-.38-.66.19-.34.05-.08.16-.28L11.5 5h1l-1 7h3.5c.49 0 .56.33.47.51l-.07.15C12.96 17.55 11 21 11 21z'/></svg>`

// shellTemplate is the HTML document shared by all Svelte pages. The bootstrap data is rendered in a JSON script
// block (html/template escapes it for that context).
var shellTemplate = template.Must(template.New("shell").Funcs(template.FuncMap{
	"favicon": func() template.URL { return template.URL(favicon) },
}).Parse(`<!doctype html>
<html lang="es">
<head>
	<meta charset="utf-8">
	<meta name="viewport" content="width=device-width, initial-scale=1">
	<title>{{.Title}}</title>
	<link rel="icon" type="image/svg+xml" href="{{favicon}}">
{{- range .Stylesheets}}
	<link rel="stylesheet" href="{{.}}">
{{- end}}
{{- range .Preloads}}
	<link rel="modulepreload" href="{{.}}">
{{- end}}
	<script id="bootstrap" type="application/json">{{.Bootstrap}}</script>
{{- if .DevClient}}
	<script type="module" src="{{.DevClient}}"></script>
{{- end}}
	<script type="module" src="{{.Script}}"></script>
</head>
<body>
	<div id="app"></div>
</body>
</html>
`))

// endregion
