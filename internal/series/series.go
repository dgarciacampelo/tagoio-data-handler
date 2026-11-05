// Package series turns a charging session's meter values into the power and energy series that the email of a
// finished session charts: neos-ocpp fetches it when the session stops and passes it on to the mailer.
package series

import (
	"errors"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/dgarciacampelo/tagoio-handler/internal/store"
)

const (
	// MaxPoints bounds the steps of a series: enough for a 600 px wide chart, and about 1 KB of JSON.
	MaxPoints = 96
	// Window is how much of the session the series covers, ending when the session last delivered energy.
	Window = 24 * time.Hour

	minStep   = time.Minute      // Stations send a meter value every 30 to 120 s: finer steps would add no detail
	minGap    = 10 * time.Minute // Readings further apart than this, and than gapFactor usual intervals, leave a gap
	gapFactor = 5
)

// ErrNoSeries reports a session with nothing to chart: fewer than two readings, or no energy delivered.
var ErrNoSeries = errors.New("series: the session has no energy delivery to chart")

// Reading is the energy delivered since the session started (Wh) at a moment.
type Reading struct {
	At     time.Time
	Energy int64
}

// Series is a session's mean power and delivered energy over consecutive steps of equal length.
//
// ! Wire contract: neos-ocpp passes it, with the pool's time zone added, to the mailer (neos-mailer-mqtt) in the
// "series" field of email/paycomet/afterqrstop.
type Series struct {
	Start  time.Time `json:"start"`  // Start of the first step (UTC, whole seconds)
	Step   int       `json:"step"`   // Seconds per step: a whole number of minutes
	Power  []*int64  `json:"power"`  // Mean power of each step (W), from the energy delivered in it; null in a gap
	Energy []*int64  `json:"energy"` // Energy delivered since the session started, at the end of each step (Wh); null in a gap
}

// FromTelemetry turns a session's stored meter values into readings. The energy delivered since the session started
// is the sum of its per-band energies, as in the email's band table. Rows without a valid timestamp are skipped.
func FromTelemetry(records []store.TelemetryRecord) []Reading {
	readings := make([]Reading, 0, len(records))
	for _, r := range records {
		if at, ok := parseTimestamp(r.Timestamp); ok {
			readings = append(readings, Reading{At: at, Energy: r.EnergyOffPeak + r.EnergyFlat + r.EnergyPeak})
		}
	}
	return readings
}

// parseTimestamp reads the ISO 8601 timestamps of the CSMS: with the pool's UTC offset, or without one (UTC), as the
// audit export does.
func parseTimestamp(s string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, strings.TrimSpace(s)); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// Build downsamples the readings of a session (in any order) into at most MaxPoints steps. The series ends at the
// last reading that delivered energy, so a car left plugged in after charging still shows its charge, and covers
// the Window before it (or the whole session when shorter). Each step's energy is interpolated from the readings, and
// its power is the energy delivered in it divided by its length. Steps inside a gap between readings have neither.
func Build(readings []Reading) (Series, error) {
	points := normalize(readings)
	if len(points) < 2 {
		return Series{}, ErrNoSeries
	}
	last := len(points) - 1
	for last > 0 && points[last].Energy == points[last-1].Energy {
		last--
	}
	if last == 0 {
		return Series{}, ErrNoSeries
	}
	points = points[:last+1]

	// The window ends at the whole second after the last delivery; the steps are laid out backwards from it, so the
	// last step ends exactly there and the first one may begin a little before the window
	end := points[last].At.Add(time.Second - 1).Truncate(time.Second)
	window := end.Sub(points[0].At)
	if window > Window {
		window = Window
	}
	step := max(minStep, time.Duration(math.Ceil(float64(window)/float64(MaxPoints)/float64(time.Minute)))*time.Minute)
	n := max(1, int(math.Ceil(float64(window)/float64(step))))
	start := end.Add(-time.Duration(n) * step)

	gap := gapThreshold(points)
	s := Series{Start: start.UTC(), Step: int(step / time.Second), Power: make([]*int64, n), Energy: make([]*int64, n)}
	for i := range n {
		from, to := start.Add(time.Duration(i)*step), start.Add(time.Duration(i+1)*step)
		if insideGap(points, from, to, gap) {
			continue
		}
		before, after := energyAt(points, from), energyAt(points, to)
		energy, power := int64(math.Round(after)), int64(math.Round((after-before)/step.Hours()))
		s.Energy[i], s.Power[i] = &energy, &power
	}
	return s, nil
}

// normalize sorts the readings by time (the timestamps carry the pool's UTC offset, so their text order breaks at a
// daylight saving change), keeps the highest energy of readings at the same moment, and never lets the energy go
// down: the CSMS ignores a meter that drops.
func normalize(readings []Reading) []Reading {
	points := slices.Clone(readings)
	slices.SortStableFunc(points, func(a, b Reading) int { return a.At.Compare(b.At) })
	merged := points[:0]
	for _, p := range points {
		if n := len(merged); n > 0 && merged[n-1].At.Equal(p.At) {
			merged[n-1].Energy = max(merged[n-1].Energy, p.Energy)
			continue
		}
		if n := len(merged); n > 0 {
			p.Energy = max(p.Energy, merged[n-1].Energy)
		}
		merged = append(merged, p)
	}
	return merged
}

// gapThreshold is the time between readings that counts as a gap: gapFactor times the usual (median) interval, and
// at least minGap.
func gapThreshold(points []Reading) time.Duration {
	intervals := make([]time.Duration, 0, len(points)-1)
	for i := 1; i < len(points); i++ {
		intervals = append(intervals, points[i].At.Sub(points[i-1].At))
	}
	slices.Sort(intervals)
	return max(minGap, gapFactor*intervals[len(intervals)/2])
}

// insideGap reports whether [from, to] lies wholly between two readings that are more than threshold apart.
func insideGap(points []Reading, from, to time.Time, threshold time.Duration) bool {
	i := afterIndex(points, from) // First reading after from
	if i == 0 || i == len(points) {
		return false // Before the first reading or after the last one: energyAt holds the value
	}
	previous, next := points[i-1], points[i]
	return !next.At.Before(to) && next.At.Sub(previous.At) > threshold
}

// energyAt interpolates the delivered energy at t between the readings around it; outside them it holds the nearest.
func energyAt(points []Reading, t time.Time) float64 {
	i := afterIndex(points, t)
	switch {
	case i == 0:
		return float64(points[0].Energy)
	case i == len(points):
		return float64(points[len(points)-1].Energy)
	}
	a, b := points[i-1], points[i]
	share := float64(t.Sub(a.At)) / float64(b.At.Sub(a.At))
	return float64(a.Energy) + share*float64(b.Energy-a.Energy)
}

// afterIndex returns the index of the first reading after t (len(points) when there is none).
func afterIndex(points []Reading, t time.Time) int {
	i, _ := slices.BinarySearchFunc(points, t, func(p Reading, t time.Time) int {
		if p.At.After(t) {
			return 1
		}
		return -1
	})
	return i
}
