package tagoio

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRunUserCalls(t *testing.T) {
	var putBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Account-Token") != "acc" {
			t.Errorf("%s %s without the account token", r.Method, r.URL.Path)
		}
		query := r.URL.Query()
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/run/users":
			// The email is looked up in lower case, and the tags must be asked for (TagoIO lists id and name only)
			if query.Get("filter[email]") != "ana@acme.es" || query.Get("fields[4]") != "tags" || query.Get("fields[2]") != "email" {
				t.Errorf("list query = %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"status":true,"result":[
				{"id":"u1","name":"Ana","email":"ana@acme.es","active":true,"tags":[{"key":"group_id","value":"261005"}]},
				{"id":"u2","name":"Other","email":"ana@acme.es.example","active":true,"tags":[]}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/run/users/u1":
			_, _ = w.Write([]byte(`{"status":true,"result":{"id":"u1","email":"ana@acme.es","tags":[{"key":"group_id","value":"261005"}]}}`))
		case r.Method == http.MethodPut && r.URL.Path == "/run/users/u1":
			putBody = readAll(t, r)
			_, _ = w.Write([]byte(`{"status":true,"result":"User Successfully Updated"}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	c := NewClient(server.URL, "acc", false, quietLog())
	ctx := context.Background()

	users, err := c.FindUsersByEmail(ctx, " Ana@ACME.es ")
	if err != nil || len(users) != 1 || users[0].ID != "u1" || !users[0].HasTag("group_id", "261005") {
		t.Fatalf("FindUsersByEmail = %+v, %v; want only the exact email", users, err)
	}
	user, err := c.User(ctx, "u1")
	if err != nil || user.ID != "u1" || len(user.Tags) != 1 {
		t.Fatalf("User = %+v, %v", user, err)
	}
	tags := append(user.Tags, Tag{Key: "group_id", Value: "261006"})
	if err := c.SetUserTags(ctx, "u1", tags); err != nil {
		t.Fatal(err)
	}
	var body map[string][]Tag
	if err := json.Unmarshal(putBody, &body); err != nil || len(body) != 1 || len(body["tags"]) != 2 {
		t.Errorf("edit body = %s (only the full tag list is sent)", putBody)
	}
}

func readAll(t *testing.T, r *http.Request) []byte {
	t.Helper()
	var raw json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		t.Fatal(err)
	}
	return raw
}
