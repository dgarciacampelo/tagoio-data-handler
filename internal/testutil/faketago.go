package testutil

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/dgarciacampelo/tagoio-handler/internal/tagoio"
)

// FakeTagoIO is an in-memory stand-in for the TagoIO REST API, covering the calls of internal/tagoio: device
// listing, device tokens and creation, run users, and the data of each device bucket. It answers with TagoIO's
// envelopes, so the real tagoio.Client is used against it.
type FakeTagoIO struct {
	*httptest.Server

	mu      sync.Mutex
	devices []tagoio.Device
	tokens  map[string]string              // Device ID -> token
	data    map[string][]tagoio.DataRecord // Device token -> records, oldest first
	calls   map[string]int                 // "METHOD /path" -> count (the path without its last segment for tokens)
	failing map[string]bool                // "METHOD /path" -> answer with an error
	nextID  int

	// BeforeDeviceList, when set, runs before each device listing is answered (outside the lock), e.g. to hold
	// concurrent callers at a barrier.
	BeforeDeviceList func()
}

// NewFakeTagoIO starts a fake TagoIO API that is closed with the test.
func NewFakeTagoIO(t testing.TB) *FakeTagoIO {
	t.Helper()
	f := &FakeTagoIO{
		tokens: map[string]string{}, data: map[string][]tagoio.DataRecord{},
		calls: map[string]int{}, failing: map[string]bool{},
	}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.Close)
	return f
}

// Client returns a tagoio.Client of the fake API (not in dry-run mode).
func (f *FakeTagoIO) Client() *tagoio.Client {
	return tagoio.NewClient(f.URL, "account-token", false, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// AddDevice adds a device to the account, with its token.
func (f *FakeTagoIO) AddDevice(id, name, token string, tags ...tagoio.Tag) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.devices = append(f.devices, tagoio.Device{ID: id, Name: name, Tags: tags})
	f.tokens[id] = token
}

// AddRecord stores a record in the bucket of the device with token.
func (f *FakeTagoIO) AddRecord(token string, r tagoio.DataRecord) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.data[token] = append(f.data[token], r)
}

// Records returns the records of a variable in a bucket, oldest first.
func (f *FakeTagoIO) Records(token, variable string) []tagoio.DataRecord {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []tagoio.DataRecord
	for _, r := range f.data[token] {
		if r.Variable == variable {
			out = append(out, r)
		}
	}
	return out
}

// DeviceCount returns the number of devices in the account.
func (f *FakeTagoIO) DeviceCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.devices)
}

// Calls returns how many times an endpoint was called, e.g. Calls("GET /device") or Calls("POST /data").
func (f *FakeTagoIO) Calls(endpoint string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[endpoint]
}

// Fail makes an endpoint answer with an error (or stop failing).
func (f *FakeTagoIO) Fail(endpoint string, failing bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failing[endpoint] = failing
}

func (f *FakeTagoIO) serve(w http.ResponseWriter, r *http.Request) {
	endpoint := r.Method + " " + r.URL.Path
	if strings.HasPrefix(r.URL.Path, "/device/token/") {
		endpoint = r.Method + " /device/token"
	}
	if r.Method == http.MethodGet && r.URL.Path == "/device" && f.BeforeDeviceList != nil {
		f.BeforeDeviceList()
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[endpoint]++
	if f.failing[endpoint] {
		writeEnvelope(w, http.StatusInternalServerError, false, nil, "fake TagoIO failure")
		return
	}

	switch {
	case endpoint == "GET /device":
		filter := r.URL.Query().Get("filter[name]")
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		matched := []tagoio.Device{}
		if page <= 1 {
			for _, d := range f.devices {
				if filter == "" || strings.Contains(d.Name, filter) {
					matched = append(matched, d)
				}
			}
		}
		writeEnvelope(w, http.StatusOK, true, matched, "")

	case endpoint == "GET /device/token":
		id := strings.TrimPrefix(r.URL.Path, "/device/token/")
		tokens := []map[string]string{}
		if token, ok := f.tokens[id]; ok {
			tokens = append(tokens, map[string]string{"token": token})
		}
		writeEnvelope(w, http.StatusOK, true, tokens, "")

	case endpoint == "POST /device":
		var d tagoio.NewDevice
		if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
			writeEnvelope(w, http.StatusBadRequest, false, nil, err.Error())
			return
		}
		f.nextID++
		id, token := fmt.Sprintf("created-%d", f.nextID), fmt.Sprintf("created-token-%d", f.nextID)
		f.devices = append(f.devices, tagoio.Device{ID: id, Name: d.Name, Tags: d.Tags})
		f.tokens[id] = token
		writeEnvelope(w, http.StatusOK, true, map[string]string{"device_id": id, "token": token}, "")

	case endpoint == "POST /run/users":
		writeEnvelope(w, http.StatusOK, true, "User created", "")

	case r.URL.Path == "/data":
		f.serveData(w, r)

	default:
		writeEnvelope(w, http.StatusNotFound, false, nil, "fake TagoIO: no route "+endpoint)
	}
}

// serveData handles the bucket calls; the caller holds the lock.
func (f *FakeTagoIO) serveData(w http.ResponseWriter, r *http.Request) {
	token := r.Header.Get("Device-Token")
	if _, ok := f.data[token]; !ok && !slices.Contains(mapValues(f.tokens), token) {
		writeEnvelope(w, http.StatusUnauthorized, false, nil, "Authorization denied")
		return
	}
	query := r.URL.Query()

	switch r.Method {
	case http.MethodPost:
		raw, _ := io.ReadAll(r.Body)
		var inputs []tagoio.DataInput
		if err := json.Unmarshal(raw, &inputs); err != nil {
			var one tagoio.DataInput
			if err := json.Unmarshal(raw, &one); err != nil {
				writeEnvelope(w, http.StatusBadRequest, false, nil, err.Error())
				return
			}
			inputs = []tagoio.DataInput{one}
		}
		for _, in := range inputs {
			f.data[token] = append(f.data[token], tagoio.DataRecord{
				ID: strconv.Itoa(len(f.data[token]) + 1), Variable: in.Variable, Value: in.Value, Group: in.Group, Metadata: in.Metadata,
			})
		}
		writeEnvelope(w, http.StatusOK, true, fmt.Sprintf("%d Data Added", len(inputs)), "")

	case http.MethodGet:
		qty, _ := strconv.Atoi(query.Get("qty"))
		skip, _ := strconv.Atoi(query.Get("skip"))
		variable := query.Get("variable")
		var newestFirst []tagoio.DataRecord
		records := f.data[token]
		for i := len(records) - 1; i >= 0; i-- {
			if variable == "" || records[i].Variable == variable {
				newestFirst = append(newestFirst, records[i])
			}
		}
		newestFirst = newestFirst[min(skip, len(newestFirst)):]
		if qty > 0 && len(newestFirst) > qty {
			newestFirst = newestFirst[:qty]
		}
		if newestFirst == nil {
			newestFirst = []tagoio.DataRecord{}
		}
		writeEnvelope(w, http.StatusOK, true, newestFirst, "")

	case http.MethodDelete:
		variable, group := query.Get("variable"), query.Get("group")
		kept := f.data[token][:0:0]
		removed := 0
		for _, rec := range f.data[token] {
			if rec.Variable == variable && (group == "" || rec.Group == group) {
				removed++
				continue
			}
			kept = append(kept, rec)
		}
		f.data[token] = kept
		writeEnvelope(w, http.StatusOK, true, fmt.Sprintf("%d Data Removed", removed), "")
	}
}

func mapValues(m map[string]string) []string {
	values := make([]string, 0, len(m))
	for _, v := range m {
		values = append(values, v)
	}
	return values
}

func writeEnvelope(w http.ResponseWriter, status int, ok bool, result any, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	body := map[string]any{"status": ok}
	if ok {
		body["result"] = result
	} else {
		body["message"] = message
	}
	_ = json.NewEncoder(w).Encode(body)
}
