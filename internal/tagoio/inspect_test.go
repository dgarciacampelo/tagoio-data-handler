package tagoio

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sync"
	"testing"
)

func TestListRunUsersSendsTheFiltersAndStopsOnAPageWithoutNewUsers(t *testing.T) {
	var mu sync.Mutex
	var queries []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		queries = append(queries, r.URL.Query())
		mu.Unlock()
		if r.Header.Get("Account-Token") != "account" || r.URL.Path != "/run/users" || r.Method != http.MethodGet {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		// A TagoIO that ignores the paging: the same page every time.
		_, _ = w.Write([]byte(`{"status":true,"result":[
			{"id":"u1","name":"Manager","email":"ops@example.test","tags":[{"key":"group_id","value":"990005"},{"key":"group_id","value":"990006"}],
			"active":true,"created_at":"2026-07-27T09:40:00.000Z","last_login":null}]}`))
	}))
	defer server.Close()
	client := NewClient(server.URL, "account", false, quietLog())

	users, err := client.ListRunUsers(context.Background(), UserFilter{
		Email: "ops@example.test", Tags: []Tag{{Key: "group_id", Value: "990006"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || users[0].ID != "u1" || users[0].LastLogin != "" || users[0].CreatedAt != "2026-07-27T09:40:00.000Z" {
		t.Fatalf("users = %+v", users)
	}
	if got := users[0].TagValues("group_id"); !slices.Equal(got, []string{"990005", "990006"}) {
		t.Errorf("group_id values = %v", got)
	}
	if len(queries) != 2 {
		t.Fatalf("%d requests; want 2 (the second page brings no new user)", len(queries))
	}
	q := queries[0]
	for key, want := range map[string]string{
		"filter[email]": "ops@example.test", "filter[tags][0][key]": "group_id", "filter[tags][0][value]": "990006", "page": "1",
	} {
		if got := q.Get(key); got != want {
			t.Errorf("%s = %q; want %q", key, got, want)
		}
	}
	if fields := q["fields"]; slices.Contains(fields, "password") || !slices.Contains(fields, "tags") {
		t.Errorf("fields = %v", fields)
	}
}

func TestListDeviceDetailsAsksForTheDescriptiveFields(t *testing.T) {
	var query url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		_, _ = w.Write([]byte(`{"status":true,"result":[{"id":"d1","name":"MASTER-BUSINESS-990005","active":true,
			"tags":[{"key":"group_id","value":"990005"}],"created_at":"2026-07-27T09:40:00.000Z","last_input":null,"type":"mutable"}]}`))
	}))
	defer server.Close()
	client := NewClient(server.URL, "account", false, quietLog())

	devices, err := client.ListDeviceDetails(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 || !devices[0].Active || devices[0].TagValue("group_id") != "990005" || devices[0].LastInput != "" {
		t.Fatalf("devices = %+v", devices)
	}
	if fields := query["fields"]; !slices.Contains(fields, "last_input") || slices.Contains(fields, "token") {
		t.Errorf("fields = %v", fields)
	}
}

func TestLooseStringTakesTextNumbersAndNull(t *testing.T) {
	for raw, want := range map[string]string{`"2026-07-27"`: "2026-07-27", `1753609200000`: "1753609200000", `null`: "", `10.50`: "10.50"} {
		var s LooseString
		if err := json.Unmarshal([]byte(raw), &s); err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		if string(s) != want {
			t.Errorf("%s -> %q; want %q", raw, s, want)
		}
	}
	var s LooseString
	if err := json.Unmarshal([]byte(`{`), &s); err == nil {
		t.Error("invalid JSON accepted")
	}
}
