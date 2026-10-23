// This file holds the data records of a device bucket and the calls that read, insert and delete them, all
// authenticated with the device token of the bucket.

package tagoio

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DataRecord is a data record as TagoIO returns it, from the API or inside an analysis trigger. Numbers are kept as
// json.Number. The MQTT fields (Params, Topic, Payload) only appear in the records of the MQTT meter analysis.
type DataRecord struct {
	ID       string         `json:"id"`
	Device   string         `json:"device"`
	Variable string         `json:"variable"`
	Value    any            `json:"value"`
	Group    string         `json:"group"`
	Time     string         `json:"time"`
	Metadata map[string]any `json:"metadata"`

	Params  map[string]any  `json:"params"`
	Topic   string          `json:"topic"`
	Payload json.RawMessage `json:"payload"`
}

// DataInput is a record to insert in a device bucket.
type DataInput struct {
	Variable string         `json:"variable"`
	Value    any            `json:"value"`
	Group    string         `json:"group,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// DeviceFullMessage is TagoIO's answer when a bucket reached its record limit.
const DeviceFullMessage = "The device has reached the limit of 50000 data registers"

// InsertData adds records to a device bucket.
func (c *Client) InsertData(ctx context.Context, deviceToken string, records ...DataInput) error {
	var body any = records
	if len(records) == 1 {
		body = records[0]
	}
	return c.do(ctx, http.MethodPost, "/data", nil, device(deviceToken), body, nil)
}

// GetData returns the latest records of a variable (newest first), up to qty, skipping the first skip.
func (c *Client) GetData(ctx context.Context, deviceToken, variable string, qty, skip int) ([]DataRecord, error) {
	query := url.Values{"qty": {strconv.Itoa(qty)}}
	if variable != "" {
		query.Set("variable", variable)
	}
	if skip > 0 {
		query.Set("skip", strconv.Itoa(skip))
	}
	var records []DataRecord
	if err := c.do(ctx, http.MethodGet, "/data", query, device(deviceToken), nil, &records); err != nil {
		return nil, err
	}
	return records, nil
}

// LastValue returns the latest record of a variable, or nil when the variable has none.
func (c *Client) LastValue(ctx context.Context, deviceToken, variable string) (*DataRecord, error) {
	records, err := c.GetData(ctx, deviceToken, variable, 1, 0)
	if err != nil || len(records) == 0 {
		return nil, err
	}
	return &records[0], nil
}

// VariableNames returns the variable of each record in a page of the bucket (the other fields are left out, to
// keep the page small), to count the records per variable.
func (c *Client) VariableNames(ctx context.Context, deviceToken string, qty, skip int) ([]string, error) {
	query := url.Values{"qty": {strconv.Itoa(qty)}, "skip": {strconv.Itoa(skip)}, "fields": {"variable"}}
	var records []struct {
		Variable string `json:"variable"`
	}
	if err := c.do(ctx, http.MethodGet, "/data", query, device(deviceToken), nil, &records); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(records))
	for _, r := range records {
		names = append(names, r.Variable)
	}
	return names, nil
}

// DeleteFilter selects the records of a variable to delete.
type DeleteFilter struct {
	Variable string
	Group    string    // Optional: only the records of this group
	Before   time.Time // Optional: only the records older than this date (whole days); zero deletes them all
}

// Delete limits per call: TagoIO deletes at most this many records at once.
const (
	deleteAllQty    = 5000
	deleteBeforeQty = 1000
)

// DeleteData deletes records of a variable and returns how many TagoIO removed.
func (c *Client) DeleteData(ctx context.Context, deviceToken string, f DeleteFilter) (int, error) {
	// TagoIO stores variable names in lower case.
	query := url.Values{"variable": {strings.ToLower(f.Variable)}, "qty": {strconv.Itoa(deleteAllQty)}}
	if f.Group != "" {
		query.Set("group", f.Group)
	}
	if !f.Before.IsZero() {
		query.Set("start_date", "2020-01-01")
		query.Set("end_date", f.Before.Format(time.DateOnly))
		query.Set("qty", strconv.Itoa(deleteBeforeQty))
	}

	var result any // "N Data Removed"
	if err := c.do(ctx, http.MethodDelete, "/data", query, device(deviceToken), nil, &result); err != nil {
		return 0, err
	}
	message, _ := result.(string)
	count, _ := strconv.Atoi(strings.Fields(message + " 0")[0])
	return count, nil
}

// region Values

// ValueString formats a record value as Python's str() did for the JSON types: strings as they are, numbers as
// written, booleans as "True"/"False" and null as "" (Python wrote "None", which ended up in the dashboards).
func ValueString(v any) string {
	switch v := v.(type) {
	case nil:
		return ""
	case string:
		return v
	case json.Number:
		return v.String()
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		if v {
			return "True"
		}
		return "False"
	default:
		return fmt.Sprint(v)
	}
}

// ValueFloat parses a record value as a number. Strings may use a decimal comma ("0,45"), as typed in Spain.
func ValueFloat(v any) (float64, error) {
	switch v := v.(type) {
	case json.Number:
		return v.Float64()
	case float64:
		return v, nil
	case string:
		s := strings.ReplaceAll(strings.TrimSpace(v), ",", ".")
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0, fmt.Errorf("%q is not a number", v)
		}
		return f, nil
	default:
		return 0, fmt.Errorf("%v (%T) is not a number", v, v)
	}
}

// ValueInt parses a record value as an integer; a number with decimals is rejected.
func ValueInt(v any) (int, error) {
	f, err := ValueFloat(v)
	if err != nil {
		return 0, err
	}
	if f != math.Trunc(f) || math.Abs(f) > math.MaxInt32 {
		return 0, fmt.Errorf("%v is not an integer", v)
	}
	return int(f), nil
}

// endregion
