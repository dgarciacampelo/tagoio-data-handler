package sessionview

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/dgarciacampelo/tagoio-handler/internal/dto"
	"github.com/dgarciacampelo/tagoio-handler/internal/series"
	"github.com/dgarciacampelo/tagoio-handler/internal/store"
)

var madrid = func() *time.Location {
	loc, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		panic(err)
	}
	return loc
}()

// labSession is a session of a made-up pool: 6 kW from 19:00 to 20:00 (a meter value a minute, 1 kWh flat and the
// rest peak), then plugged in without charging until 20:40 (a meter value every 10 minutes).
func labSession() (store.SessionSummary, []store.TelemetryRecord) {
	s := store.SessionSummary{
		ID: 7, CreatedAt: "2026-11-04 19:40:05", TransactionID: 4321, PoolCode: 990001, StationName: "ST 1",
		ConnectorID: 2, StartDate: "04/11/2026", TimeBand: "19:00 - 20:40", Cost: 2.55, CardAlias: "Pago QR",
		TotalEnergyKWh: 6, RateOffPeak: 0.25, RateFlat: 0.35, RatePeak: 0.45,
		EnergyFlat: 1000, EnergyPeak: 5000, StopMotive: "CAR",
	}
	start := time.Date(2026, 11, 4, 19, 0, 0, 0, madrid)
	var telemetry []store.TelemetryRecord
	add := func(at time.Time, energy, power int64) {
		flat := min(energy, 1000)
		telemetry = append(telemetry, store.TelemetryRecord{
			TransactionID: 4321, Timestamp: at.Format("2006-01-02T15:04:05.000000-07:00"), MeterValue: 50_000 + energy,
			Power: power, CurrentTariffBand: "Peak", EnergyFlat: flat, EnergyPeak: energy - flat,
		})
	}
	for m := 1; m <= 60; m++ {
		power := int64(6000)
		if m == 30 {
			power = 7200 // A peak
		}
		add(start.Add(time.Duration(m)*time.Minute), int64(100*m), power)
	}
	for m := 70; m <= 100; m += 10 {
		add(start.Add(time.Duration(m)*time.Minute), 6000, 0)
	}
	return s, telemetry
}

func TestBuildReadsTheTimesAndPowerOfTheSession(t *testing.T) {
	s, telemetry := labSession()
	d := Build(s, telemetry, madrid, time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC))

	check := func(name string, got *string, want string) {
		t.Helper()
		if got == nil || *got != want {
			t.Errorf("%s = %v; want %s", name, deref(got), want)
		}
	}
	check("started_at", d.StartedAt, "2026-11-04T19:00:00+01:00")
	check("ended_at", d.EndedAt, "2026-11-04T20:40:00+01:00")
	check("last_delivery_at", d.LastDeliveryAt, "2026-11-04T20:00:00+01:00")
	if d.DurationSeconds == nil || *d.DurationSeconds != 100*60 {
		t.Errorf("duration = %v; want 6000 s", d.DurationSeconds)
	}
	if d.IdleSeconds == nil || *d.IdleSeconds != 40*60 {
		t.Errorf("idle = %v; want 2400 s (plugged in from 20:00 to 20:40)", d.IdleSeconds)
	}
	if d.MeanPowerW == nil || *d.MeanPowerW != 6000 || d.PeakPowerW == nil || *d.PeakPowerW != 7200 {
		t.Errorf("mean power %v, peak power %v; want 6000 W and 7200 W", d.MeanPowerW, d.PeakPowerW)
	}
	if d.EnergyWh != 6000 || d.MeterValueCount != 64 || len(d.MeterValues) != 64 || d.TelemetryPurged {
		t.Errorf("energy %d Wh, %d meter values (%d rows), purged %v", d.EnergyWh, d.MeterValueCount, len(d.MeterValues), d.TelemetryPurged)
	}
	if row := d.MeterValues[59]; row.FlatWh != 1000 || row.PeakWh != 5000 || row.EvaluatedCost != 2.6 {
		t.Errorf("meter value at 20:00 = %+v; want 1000 Wh flat, 5000 Wh peak, 2.6 € evaluated", row)
	}
	if d.ExportURL != "/api/admin/audits/7/export" || d.AuditsURL != "/emsp-dashboard/audits/990001" ||
		d.PaymentsURL != "/api/admin/sessions/990001/ST%201/2/4321/payments" {
		t.Errorf("urls: %s, %s, %s", d.ExportURL, d.AuditsURL, d.PaymentsURL)
	}
}

func TestBuildChartsTheSeriesOfTheEmail(t *testing.T) {
	s, telemetry := labSession()
	d := Build(s, telemetry, madrid, time.Now())

	// What GET /api/sessions/.../series gives the CSMS for the email, with the session's start as started_at
	started := time.Date(2026, 11, 4, 19, 0, 0, 0, madrid)
	email, err := series.Build(append(series.FromTelemetry(telemetry), series.Reading{At: started}))
	if err != nil {
		t.Fatal(err)
	}
	want, _ := json.Marshal(dto.SessionSeries{Start: email.Start.Format(time.RFC3339), Step: email.Step, Power: email.Power, Energy: email.Energy})
	got, _ := json.Marshal(d.Series)
	if string(got) != string(want) {
		t.Fatalf("page series\n%s\nwant the email's\n%s", got, want)
	}
	// The window ends at the last delivery (20:00), not at the end of the session
	if d.Series.Start != "2026-11-04T18:00:00Z" || d.Series.Step != 60 || len(d.Series.Power) != 60 {
		t.Errorf("series from %s, %d s steps, %d steps; want 60 one-minute steps from 18:00 UTC", d.Series.Start, d.Series.Step, len(d.Series.Power))
	}
}

func TestBuildChecksTheCostAgainstTheBands(t *testing.T) {
	cases := []struct {
		name       string
		cost       float64
		stopMotive string
		want       dto.CostCheck
	}{
		{"charged as the bands add up", 2.6, "CAR", dto.CostCheckOK},
		{"a cent apart: the CSMS rounds", 2.59, "CAR", dto.CostCheckOK},
		{"stopped at the authorized amount", 2.5, "COST", dto.CostCheckCapped},
		{"below the bands for no reason", 2.5, "CAR", dto.CostCheckMismatch},
		{"above the bands, even when capped", 2.7, "COST", dto.CostCheckMismatch},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, telemetry := labSession()
			s.Cost, s.StopMotive = c.cost, c.stopMotive
			d := Build(s, telemetry, madrid, time.Now())
			if d.CostCheck != c.want || d.EvaluatedCost != 2.6 {
				t.Errorf("check %s, evaluated %v; want %s, 2.6", d.CostCheck, d.EvaluatedCost, c.want)
			}
		})
	}

	s, telemetry := labSession()
	d := Build(s, telemetry, madrid, time.Now())
	want := []dto.SessionBand{
		{Band: dto.TariffBandOffPeak, EnergyWh: 0, Rate: 0.25, Cost: 0},
		{Band: dto.TariffBandFlat, EnergyWh: 1000, Rate: 0.35, Cost: 0.35},
		{Band: dto.TariffBandPeak, EnergyWh: 5000, Rate: 0.45, Cost: 2.25},
	}
	for i, band := range d.Bands {
		if band != want[i] {
			t.Errorf("band %d = %+v; want %+v", i, band, want[i])
		}
	}
}

func TestBuildWithoutMeterValues(t *testing.T) {
	s, _ := labSession()
	stored := time.Date(2026, 11, 4, 19, 40, 5, 0, time.UTC)

	recent := Build(s, nil, madrid, stored.Add(30*24*time.Hour))
	if recent.TelemetryPurged || recent.Series != nil || recent.IdleSeconds != nil || recent.PeakPowerW != nil {
		t.Errorf("recent session without meter values: %+v", recent)
	}
	// The end is when the history stored it, so the duration still shows
	if recent.EndedAt == nil || *recent.EndedAt != "2026-11-04T19:40:05Z" || recent.DurationSeconds == nil || *recent.DurationSeconds != 100*60+5 {
		t.Errorf("ended %v after %v s; want 19:40:05 UTC, 6005 s", deref(recent.EndedAt), recent.DurationSeconds)
	}

	old := Build(s, nil, madrid, stored.Add((store.TelemetryRetentionDays+1)*24*time.Hour))
	if !old.TelemetryPurged {
		t.Error("a session older than the retention: its meter values were purged")
	}
}

func TestBuildWithAnUnreadableTimeBand(t *testing.T) {
	s, telemetry := labSession()
	s.TimeBand = "t"
	d := Build(s, telemetry, madrid, time.Now())
	if d.StartedAt != nil || d.DurationSeconds != nil || d.MeanPowerW != nil {
		t.Errorf("start %v, duration %v, mean power %v; want none", deref(d.StartedAt), d.DurationSeconds, d.MeanPowerW)
	}
	if d.Series == nil || d.IdleSeconds == nil || *d.IdleSeconds != 40*60 {
		t.Errorf("the meter values still give the chart and the idle time: series %v, idle %v", d.Series != nil, d.IdleSeconds)
	}
}

func TestURLEscapesTheStationName(t *testing.T) {
	if got := URL(990001, "ST 1/A", 2, 4321); got != "/emsp-dashboard/sessions/990001/ST%201%2FA/2/4321" {
		t.Errorf("URL = %s", got)
	}
}

func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}
