package webui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// distDir writes a minimal Vite build: the manifest with one page entry, its bundle, and a file outside assets/.
func distDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		".vite/manifest.json": `{"src/pages/station/main.ts": {"file": "assets/station-abc123.js", "src": "src/pages/station/main.ts",
			"isEntry": true, "css": ["assets/station-def456.css"]}}`,
		"assets/station-abc123.js":  "console.log('station')",
		"assets/station-def456.css": "body{}",
		"secret.txt":                "not public",
	}
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestAssetsHandlerServesOnlyTheBundles(t *testing.T) {
	if embeddedAssets() != nil {
		t.Skip("built with the embedui tag: the renderer ignores DistDir")
	}
	r, err := New(Config{DistDir: distDir(t)})
	if err != nil {
		t.Fatal(err)
	}
	h := r.AssetsHandler()

	tests := map[string]int{
		"/assets/station-abc123.js":         http.StatusOK,
		"/assets/station-def456.css":        http.StatusOK,
		"/assets/missing.js":                http.StatusNotFound,
		"/assets/":                          http.StatusNotFound, // No directory listings
		"/assets/../secret.txt":             http.StatusNotFound,
		"/assets/..%2fsecret.txt":           http.StatusNotFound,
		"/assets/../.vite/manifest.json":    http.StatusNotFound,
		"/.vite/manifest.json":              http.StatusNotFound,
		"/assets/%2e%2e/%2e%2e/secret.txt":  http.StatusNotFound,
		"/assets/station-abc123.js/../../x": http.StatusNotFound,
	}
	for path, want := range tests {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != want {
			t.Errorf("GET %s = %d; want %d", path, rec.Code, want)
		}
		immutable := rec.Header().Get("Cache-Control") == "public, max-age=31536000, immutable"
		if immutable != (want == http.StatusOK) {
			t.Errorf("GET %s: Cache-Control %q", path, rec.Header().Get("Cache-Control"))
		}
	}
}

func TestNewFailsWithoutAPageEntry(t *testing.T) {
	if embeddedAssets() != nil {
		t.Skip("built with the embedui tag: the renderer ignores DistDir")
	}
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, ".vite"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, ".vite", "manifest.json"), []byte(`{"src/lib/api.ts": {"file": "assets/api.js"}}`), 0o644)

	if _, err := New(Config{DistDir: dir}); err == nil {
		t.Fatal("a manifest without page entries was accepted")
	}
	if _, err := New(Config{DistDir: t.TempDir()}); err == nil {
		t.Fatal("a missing build was accepted")
	}
}

func TestPageEntryName(t *testing.T) {
	tests := map[string]struct {
		page string
		ok   bool
	}{
		"src/pages/station/main.ts":     {"station", true},
		"src/pages/emsp/main.ts":        {"emsp", true},
		"src/pages/station/sub/main.ts": {"", false},
		"src/lib/main.ts":               {"", false},
		"src/pages//main.ts":            {"", false},
	}
	for key, want := range tests {
		if page, ok := pageEntryName(key); page != want.page || ok != want.ok {
			t.Errorf("pageEntryName(%q) = %q, %v", key, page, ok)
		}
	}
}
