package tagoio

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestEnvelopes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/data":
			if r.Header.Get("Device-Token") != "dev-token" || r.URL.Query().Get("qty") != "1" {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`{"status":true,"result":[{"variable":"rate_costs","value":"0","metadata":{"IVA":10.0}}]}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/data":
			if r.URL.Query().Get("variable") != "state_st1_1" || r.URL.Query().Get("qty") != "1000" || r.URL.Query().Get("end_date") == "" {
				t.Errorf("delete query = %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"status":true,"result":"12 Data Removed"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/data":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"status":false,"message":"` + DeviceFullMessage + `"}`))
		default:
			_, _ = w.Write([]byte(`{"status":false,"message":"Authorization denied"}`)) // 200 with a failure
		}
	}))
	defer server.Close()
	c := NewClient(server.URL, "acc", false, quietLog())
	ctx := context.Background()

	record, err := c.LastValue(ctx, "dev-token", "rate_costs")
	if err != nil || record == nil || record.Metadata["IVA"] != json.Number("10.0") {
		t.Fatalf("LastValue = %+v, %v; want the record with IVA as written (10.0)", record, err)
	}

	removed, err := c.DeleteData(ctx, "dev-token", DeleteFilter{Variable: "state_ST1_1", Before: time.Now()})
	if err != nil || removed != 12 {
		t.Fatalf("DeleteData = %d, %v; want 12 (variable lower-cased)", removed, err)
	}

	err = c.InsertData(ctx, "dev-token", DataInput{Variable: "x", Value: 1})
	if !HasMessage(err, DeviceFullMessage) || !IsStatus(err, http.StatusBadRequest) {
		t.Fatalf("InsertData error = %v; want the device-full APIError", err)
	}

	if _, err := c.DataAmount(ctx, "dev"); !IsStatus(err, http.StatusOK) {
		t.Fatalf("a 200 with status false must be an APIError, got %v", err)
	}
}

func TestDryRunSendsNoWrites(t *testing.T) {
	var writes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writes.Add(1)
		}
		_, _ = w.Write([]byte(`{"status":true,"result":[]}`))
	}))
	defer server.Close()
	c := NewClient(server.URL, "acc", true, quietLog())
	ctx := context.Background()

	_ = c.InsertData(ctx, "t", DataInput{Variable: "x"})
	_, _ = c.DeleteData(ctx, "t", DeleteFilter{Variable: "x"})
	_ = c.CreateUser(ctx, NewUser{Email: "a@b.es", Password: "secret"})
	if _, err := c.GetData(ctx, "t", "x", 1, 0); err != nil {
		t.Fatal(err)
	}
	if writes.Load() != 0 {
		t.Fatalf("dry run sent %d writes", writes.Load())
	}
}

func TestValueHelpers(t *testing.T) {
	if f, err := ValueFloat("0,45"); err != nil || f != 0.45 {
		t.Errorf("ValueFloat(decimal comma) = %v, %v", f, err)
	}
	if _, err := ValueInt(json.Number("1.5")); err == nil {
		t.Error("ValueInt(1.5) must fail")
	}
	for in, want := range map[any]string{nil: "", "a": "a", json.Number("10.0"): "10.0", true: "True"} {
		if got := ValueString(in); got != want {
			t.Errorf("ValueString(%v) = %q; want %q", in, got, want)
		}
	}
}
