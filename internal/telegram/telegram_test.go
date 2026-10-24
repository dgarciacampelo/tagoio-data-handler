package telegram

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNotifyAndSendBackup(t *testing.T) {
	var gotText, gotDocument, gotCaption string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/botSECRET/sendMessage"):
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			gotText, _ = body["text"].(string)
		case strings.HasSuffix(r.URL.Path, "/botSECRET/sendDocument"):
			file, _, err := r.FormFile("document")
			if err != nil {
				t.Errorf("FormFile: %v", err)
			} else {
				raw, _ := io.ReadAll(file)
				gotDocument = string(raw)
			}
			gotCaption = r.FormValue("caption")
		default:
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	c := New("SECRET", 1, 2, "TagoIO data handler", false, quietLog())
	c.baseURL = server.URL

	if err := c.Notify(context.Background(), "hello"); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if gotText != "TagoIO data handler: hello" {
		t.Errorf("text = %q", gotText)
	}

	path := filepath.Join(t.TempDir(), "backup.zip")
	_ = os.WriteFile(path, []byte("zip-bytes"), 0o600)
	if err := c.SendBackup(context.Background(), path, "uploading backup.zip"); err != nil {
		t.Fatalf("SendBackup: %v", err)
	}
	if gotDocument != "zip-bytes" || gotCaption != "TagoIO data handler: uploading backup.zip" {
		t.Errorf("document %q, caption %q", gotDocument, gotCaption)
	}
}

func TestErrorsNeverContainTheToken(t *testing.T) {
	c := New("SECRET", 1, 2, "svc", false, quietLog())
	c.baseURL = "http://127.0.0.1:1" // Nothing listens: the request fails with the URL in the error
	err := c.Notify(context.Background(), "x")
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("error = %v; want a failure without the token", err)
	}
}

func TestDryRunSendsNothing(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	c := New("SECRET", 1, 2, "svc", true, quietLog())
	c.baseURL = server.URL
	path := filepath.Join(t.TempDir(), "backup.zip")
	_ = os.WriteFile(path, []byte("zip"), 0o600)

	if err := c.Notify(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
	if err := c.SendBackup(context.Background(), path, "y"); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("dry run sent %d requests to Telegram", calls)
	}
}

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }
