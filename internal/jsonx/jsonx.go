// Package jsonx holds lenient JSON number types for the payloads of other services. The Python service validated
// them with Pydantic, which accepts an integer written as 5.0 or "5" and a float written as "0.4"; these types
// accept the same, so a sender that worked before keeps working.
package jsonx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Int is an int64 that also accepts integral floats (5.0) and numeric strings ("5").
type Int int64

func (i *Int) UnmarshalJSON(data []byte) error {
	text, err := numberText(data)
	if err != nil {
		return err
	}
	if n, err := strconv.ParseInt(text, 10, 64); err == nil {
		*i = Int(n)
		return nil
	}
	f, err := strconv.ParseFloat(text, 64)
	if err != nil || f != math.Trunc(f) || math.Abs(f) > 1<<53 {
		return fmt.Errorf("%s is not an integer", data)
	}
	*i = Int(f)
	return nil
}

// Float is a float64 that also accepts numeric strings ("0.4").
type Float float64

func (f *Float) UnmarshalJSON(data []byte) error {
	text, err := numberText(data)
	if err != nil {
		return err
	}
	v, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return fmt.Errorf("%s is not a number", data)
	}
	*f = Float(v)
	return nil
}

// numberText returns the text of a JSON number, or of a JSON string holding one.
func numberText(data []byte) (string, error) {
	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("null")) {
		return "", fmt.Errorf("a number is required")
	}
	if len(data) > 0 && data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return "", err
		}
		return strings.TrimSpace(s), nil
	}
	return string(data), nil
}
