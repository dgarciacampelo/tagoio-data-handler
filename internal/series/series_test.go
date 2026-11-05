package series

import (
	"encoding/json"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/dgarciacampelo/tagoio-handler/internal/store"
)

var sessionStart = time.Date(2026, 11, 4, 18, 30, 0, 0, time.UTC)

// perMinute returns a reading at the session start (nothing delivered) and one per minute after it, where wh(m) is the
// energy delivered during minute m (1-based).
func perMinute(minutes int, wh func(int) float64) []Reading {
	readings := []Reading{{At: sessionStart}}
	total := 0.0
	for m := 1; m <= minutes; m++ {
		total += wh(m)
		readings = append(readings, Reading{At: sessionStart.Add(time.Duration(m) * time.Minute), Energy: int64(math.Round(total))})
	}
	return readings
}

func constant(watts float64) func(int) float64 { return func(int) float64 { return watts / 60 } }

// resolution is how far a step's power can be off when the readings are whole Wh: 1 Wh over the step.
func resolution(s Series) float64 { return 3600 / float64(s.Step) }

func value(p *int64) int64 {
	if p == nil {
		return -1
	}
	return *p
}

// checkShape checks what every series keeps: at most MaxPoints steps of whole minutes, ending at the last delivery.
func checkShape(t *testing.T, s Series, lastDelivery time.Time) {
	t.Helper()
	if n := len(s.Power); n == 0 || n > MaxPoints || n != len(s.Energy) {
		t.Fatalf("%d power and %d energy points; want the same number, 1 to %d", len(s.Power), len(s.Energy), MaxPoints)
	}
	if s.Step < 60 || s.Step%60 != 0 {
		t.Errorf("step = %d s; want whole minutes", s.Step)
	}
	end := s.Start.Add(time.Duration(len(s.Power)*s.Step) * time.Second)
	if !end.Equal(lastDelivery.Truncate(time.Second)) && !end.Equal(lastDelivery.Truncate(time.Second).Add(time.Second)) {
		t.Errorf("series ends at %v; want the last delivery, %v", end, lastDelivery)
	}
	if s.Start.Location() != time.UTC || s.Start.Nanosecond() != 0 {
		t.Errorf("start = %v; want UTC whole seconds", s.Start)
	}
}

func TestALabSessionKeepsItsShape(t *testing.T) {
	// 6 h: 7 kW for 5 h, then tapering to 1 kW over the last hour
	readings := perMinute(360, func(m int) float64 {
		if m <= 300 {
			return 7000.0 / 60
		}
		return (7000 - 6000*float64(m-300)/60) / 60
	})
	s, err := Build(readings)
	if err != nil {
		t.Fatal(err)
	}
	checkShape(t, s, readings[len(readings)-1].At)
	if s.Step != 240 || len(s.Power) != 90 {
		t.Fatalf("%d points of %d s; want 90 of 240 s (6 h in at most 96 whole minutes)", len(s.Power), s.Step)
	}
	if got, want := value(s.Energy[89]), readings[len(readings)-1].Energy; got != want {
		t.Errorf("last energy = %d Wh; want the session's %d Wh", got, want)
	}
	for i, p := range s.Power[:75] {
		if math.Abs(float64(value(p))-7000) > resolution(s) {
			t.Fatalf("power[%d] = %d W; want 7000 W while it charges at 7 kW", i, value(p))
		}
	}
	if last := value(s.Power[89]); last < 1000 || last > 1500 {
		t.Errorf("last power = %d W; want the taper's end, about 1.2 kW", last)
	}
	// The powers add up to the energy: nothing is lost or counted twice between steps
	sum := 0.0
	for _, p := range s.Power {
		sum += float64(value(p)) * float64(s.Step) / 3600
	}
	if math.Abs(sum-float64(value(s.Energy[89]))) > 10 {
		t.Errorf("power × time = %.0f Wh; want the delivered %d Wh", sum, value(s.Energy[89]))
	}
}

func TestACarLeftPluggedInShowsItsCharge(t *testing.T) {
	// 11 kW for 4 h, then full and plugged in (nothing delivered) for 26 h more: a literal "last 24 h" would be flat
	readings := perMinute(30*60, func(m int) float64 {
		if m <= 240 {
			return 11000.0 / 60
		}
		return 0
	})
	s, err := Build(readings)
	if err != nil {
		t.Fatal(err)
	}
	lastDelivery := sessionStart.Add(240 * time.Minute)
	checkShape(t, s, lastDelivery)
	if !s.Start.Equal(sessionStart) || s.Step != 180 || len(s.Power) != 80 {
		t.Fatalf("start %v, %d points of %d s; want the 4 h charge from its start in 80 points of 3 min", s.Start, len(s.Power), s.Step)
	}
	if p := value(s.Power[40]); p != 11000 {
		t.Errorf("power = %d W; want 11000 W", p)
	}
}

func TestAChargeLongerThanADayShowsItsLastDay(t *testing.T) {
	readings := perMinute(30*60, constant(1500)) // 1.5 kW for 30 h
	s, err := Build(readings)
	if err != nil {
		t.Fatal(err)
	}
	last := readings[len(readings)-1]
	checkShape(t, s, last.At)
	if s.Step != 900 || len(s.Power) != 96 || !s.Start.Equal(last.At.Add(-Window)) {
		t.Fatalf("start %v, %d points of %d s; want the last 24 h in 96 points of 15 min", s.Start, len(s.Power), s.Step)
	}
	// The energy counts from the session's start, not the window's
	if first := value(s.Energy[0]); first < 6*1500+300 {
		t.Errorf("first energy = %d Wh; want what the session had delivered by then (over 9.3 kWh)", first)
	}
	if value(s.Energy[95]) != last.Energy {
		t.Errorf("last energy = %d Wh; want %d Wh", value(s.Energy[95]), last.Energy)
	}
}

func TestAGapBetweenReadingsHasNoValues(t *testing.T) {
	// 7 kW for 3 h, without readings from 01:00 to 01:45 (the CSMS or this service down): the energy of the gap
	// arrives with the next reading
	all := perMinute(180, constant(7000))
	var readings []Reading
	for _, r := range all {
		if offset := r.At.Sub(sessionStart); offset <= time.Hour || offset >= 105*time.Minute {
			readings = append(readings, r)
		}
	}
	s, err := Build(readings)
	if err != nil {
		t.Fatal(err)
	}
	checkShape(t, s, all[len(all)-1].At)
	if s.Step != 120 {
		t.Fatalf("step = %d s; want 120", s.Step)
	}
	gaps := 0
	for i := range s.Power {
		from := s.Start.Add(time.Duration(i*s.Step) * time.Second)
		inGap := !from.Before(sessionStart.Add(time.Hour)) && !from.Add(2*time.Minute).After(sessionStart.Add(105*time.Minute))
		if (s.Power[i] == nil) != inGap || (s.Energy[i] == nil) != inGap {
			t.Errorf("step %d (%s): power %d, energy %d; want values only outside the gap", i, from.Format("15:04"), value(s.Power[i]), value(s.Energy[i]))
		}
		if s.Power[i] == nil {
			gaps++
		} else if p := value(s.Power[i]); math.Abs(float64(p)-7000) > resolution(s) {
			t.Errorf("step %d: power %d W; want 7000 W", i, p)
		}
	}
	if gaps != 22 {
		t.Errorf("%d steps in the gap; want 22 (01:00 to 01:44)", gaps)
	}
}

func TestReadingsAreOrderedByTimeAcrossADaylightSavingChange(t *testing.T) {
	// The night Spain goes back from +02:00 to +01:00: 02:59+02:00 is before 02:00+01:00, though not as text
	records := []store.TelemetryRecord{
		{Timestamp: "2026-10-25T02:00:00.000123+01:00", EnergyOffPeak: 3000},
		{Timestamp: "2026-10-25T02:30:00+02:00", EnergyOffPeak: 1000},
		{Timestamp: "2026-10-25T02:59:00+02:00", EnergyOffPeak: 2000},
		{Timestamp: "not a timestamp", EnergyOffPeak: 9999},
		{Timestamp: "2026-10-25T02:30:00+01:00", EnergyOffPeak: 3500, EnergyFlat: 500},
	}
	readings := FromTelemetry(records)
	if len(readings) != 4 {
		t.Fatalf("%d readings; want the 4 with a timestamp", len(readings))
	}
	s, err := Build(readings)
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC); !s.Start.Equal(want) {
		t.Errorf("start = %v; want %v", s.Start, want)
	}
	if last := value(s.Energy[len(s.Energy)-1]); last != 4000 {
		t.Errorf("last energy = %d Wh; want 4000 (the latest reading, all its bands)", last)
	}
	for i := 1; i < len(s.Energy); i++ {
		if s.Energy[i] != nil && s.Energy[i-1] != nil && *s.Energy[i] < *s.Energy[i-1] {
			t.Fatalf("energy goes down at step %d: %d -> %d", i, *s.Energy[i-1], *s.Energy[i])
		}
	}
}

func TestTheFinalReadingIsMergedWithAStoredOne(t *testing.T) {
	readings := perMinute(10, constant(6000))
	last := readings[len(readings)-1]
	again := append(readings, Reading{At: last.At, Energy: last.Energy}, Reading{At: last.At.Add(30 * time.Second), Energy: last.Energy - 5})
	s, err := Build(again)
	if err != nil {
		t.Fatal(err)
	}
	checkShape(t, s, last.At)
	if len(s.Power) != 10 || value(s.Energy[9]) != 1000 || value(s.Power[9]) != 6000 {
		t.Fatalf("series %+v; want 10 points of 6 kW ending at 1000 Wh", s)
	}
}

func TestNothingToChart(t *testing.T) {
	for name, readings := range map[string][]Reading{
		"no readings":      nil,
		"a single reading": {{At: sessionStart, Energy: 100}},
		"no energy":        perMinute(30, constant(0)),
	} {
		if _, err := Build(readings); !errors.Is(err, ErrNoSeries) {
			t.Errorf("%s: error %v; want ErrNoSeries", name, err)
		}
	}
}

func TestTheWireFormat(t *testing.T) {
	s, err := Build(perMinute(3, constant(6000)))
	if err != nil {
		t.Fatal(err)
	}
	s.Power[1] = nil
	s.Energy[1] = nil
	b, _ := json.Marshal(s)
	const want = `{"start":"2026-11-04T18:30:00Z","step":60,"power":[6000,null,6000],"energy":[100,null,300]}`
	if string(b) != want {
		t.Errorf("JSON = %s\nwant   %s", b, want)
	}
}
