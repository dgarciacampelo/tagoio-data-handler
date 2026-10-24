// This file holds the access to the records of an analysis trigger (TagoIO calls them the scope).
//
// The Python service read every field by its position in the scope, which depends on the order of the fields in
// the TagoIO dashboard form. Fields are now looked up by variable name; the position is only a fallback for the
// fields whose variable name is not confirmed yet (their names are logged with every trigger, see Service.Handler,
// so they can be added from real payloads).

package analysis

import (
	"strings"

	"github.com/dgarciacampelo/tagoio-handler/internal/tagoio"
)

// scope gives access to the records of a trigger.
type scope struct {
	records    []tagoio.DataRecord
	byVariable map[string]*tagoio.DataRecord
}

func newScope(records []tagoio.DataRecord) scope {
	s := scope{records: records, byVariable: make(map[string]*tagoio.DataRecord, len(records))}
	for i := range records {
		name := strings.ToLower(records[i].Variable)
		if _, seen := s.byVariable[name]; !seen && name != "" {
			s.byVariable[name] = &records[i]
		}
	}
	return s
}

// field is a scope field: its variable names (any of them) and its position in the legacy scope order.
type field struct {
	names    []string
	position int
}

// record returns the record of a field: the first record with one of its variable names or, when none has, the
// record at its legacy position. It returns nil when the scope has neither.
func (s scope) record(f field) *tagoio.DataRecord {
	for _, name := range f.names {
		if r, ok := s.byVariable[name]; ok {
			return r
		}
	}
	if f.position < len(s.records) {
		return &s.records[f.position]
	}
	return nil
}

// value returns the value of a field (nil when missing).
func (s scope) value(f field) any {
	if r := s.record(f); r != nil {
		return r.Value
	}
	return nil
}

// str returns the value of a field as a string ("" when missing or null).
func (s scope) str(f field) string {
	return tagoio.ValueString(s.value(f))
}

// device returns the device the trigger came from (the pool's MASTER device).
func (s scope) device() string {
	for _, r := range s.records {
		if r.Device != "" {
			return r.Device
		}
	}
	return ""
}

// variables returns the variable names of the scope, in order (logged to document the payloads).
func (s scope) variables() []string {
	names := make([]string, 0, len(s.records))
	for _, r := range s.records {
		names = append(names, r.Variable)
	}
	return names
}
