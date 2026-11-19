package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dgarciacampelo/tagoio-handler/internal/config"
	"github.com/dgarciacampelo/tagoio-handler/internal/devices"
	"github.com/dgarciacampelo/tagoio-handler/internal/events"
	"github.com/dgarciacampelo/tagoio-handler/internal/handlers"
	"github.com/dgarciacampelo/tagoio-handler/internal/inspect"
	"github.com/dgarciacampelo/tagoio-handler/internal/store"
	"github.com/dgarciacampelo/tagoio-handler/internal/tagoio"
	"github.com/dgarciacampelo/tagoio-handler/internal/testutil"
	"github.com/dgarciacampelo/tagoio-handler/internal/webui"
)

func TestTagoIOCommandPaths(t *testing.T) {
	valid := map[string]string{
		"devices":                                 "/api/tagoio/devices",
		"pool 261001":                             "/api/tagoio/pools/261001",
		"users -email ops@example.test":           "/api/tagoio/users?email=ops%40example.test",
		"users -group-id 261002":                  "/api/tagoio/users?group_id=261002",
		"users -email a@example.test -group-id 9": "/api/tagoio/users?email=a%40example.test&group_id=9",
	}
	for line, want := range valid {
		got, err := tagoioCommandPath(strings.Fields(line))
		if err != nil || got != want {
			t.Errorf("%q -> %q, %v; want %q", line, got, err, want)
		}
	}
	for _, line := range []string{"", "pool", "pool abc", "pool -1", "pool 1 2", "devices x", "users", "users x", "token 261001"} {
		if _, err := tagoioCommandPath(strings.Fields(line)); !errors.Is(err, errUsage) {
			t.Errorf("%q accepted: %v", line, err)
		}
	}
}

func TestTagoIOCommandRejectsAWrongCommandLine(t *testing.T) {
	var stderr strings.Builder
	if code := runTagoIOCommand(context.Background(), []string{"pool"}, io.Discard, &stderr); code != 2 {
		t.Errorf("exit code = %d", code)
	}
	if !strings.Contains(stderr.String(), "usage: server tagoio") {
		t.Errorf("stderr = %q", stderr.String())
	}
}

// TestTagoIOCommandAsksTheService runs the command's request against the real routes, with the inspection wired
// to a fake TagoIO.
func TestTagoIOCommandAsksTheService(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "cmd.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	tago := testutil.NewFakeTagoIO(t)
	tago.AddDevice("dev-6", "MASTER-BUSINESS-990006", "token-6", tagoio.Tag{Key: "group_id", Value: "990006"})
	registry := devices.NewRegistry(db, tago.Client(), "MASTER-", log)

	ui := webui.Disabled(errors.New("no UI in tests"))
	creds := config.Credentials{User: "svc", Secret: "svc-secret"}
	app := &application{ui: ui, handlers: handlers.New(handlers.Deps{
		Log: log, Store: db, Broker: events.NewBroker(log, false), UI: ui, Jobs: context.Background(),
		Config:  &config.Config{ServiceAPI: creds, Admin: config.Credentials{User: "admin", Secret: "admin-secret"}},
		Inspect: inspect.New(tago.Client(), registry, nil, log),
	})}
	server := httptest.NewServer(app.routes())
	defer server.Close()

	body, err := fetchTagoIO(context.Background(), server.Client(), server.URL+"/api/tagoio/pools/990006", creds)
	if err != nil {
		t.Fatal(err)
	}
	var report inspect.PoolReport
	if err := json.Unmarshal(body, &report); err != nil {
		t.Fatal(err)
	}
	if report.PoolCode != 990006 || len(report.Devices) != 1 || report.Devices[0].ID != "dev-6" {
		t.Errorf("report = %+v", report)
	}
	if strings.Contains(string(body), "token-6") || !strings.Contains(string(body), "\n  ") {
		t.Errorf("answer carries the token or is not indented:\n%s", body)
	}

	_, err = fetchTagoIO(context.Background(), server.Client(), server.URL+"/api/tagoio/users", creds)
	if err == nil || !strings.Contains(err.Error(), "HTTP 400") {
		t.Errorf("users without a filter: %v", err)
	}
	_, err = fetchTagoIO(context.Background(), server.Client(), server.URL+"/api/tagoio/devices", config.Credentials{User: "svc", Secret: "wrong"})
	if err == nil || !strings.Contains(err.Error(), "HTTP 401") {
		t.Errorf("wrong credentials: %v", err)
	}
	tago.Fail("GET /device", true)
	_, err = fetchTagoIO(context.Background(), server.Client(), server.URL+"/api/tagoio/devices", creds)
	if err == nil || !strings.Contains(err.Error(), "HTTP 502") {
		t.Errorf("TagoIO down: %v", err)
	}
}
