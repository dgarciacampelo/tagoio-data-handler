// Package sessionview builds the page of a finished charging session from its history row and its meter values: its
// times, power and idle time, the cost of each tariff band, the power and energy series of its email, and the rows of
// its XLSX audit.
package sessionview

import (
	"fmt"
	"math"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/dgarciacampelo/tagoio-handler/internal/dto"
	"github.com/dgarciacampelo/tagoio-handler/internal/series"
	"github.com/dgarciacampelo/tagoio-handler/internal/store"
)

// costTolerance is how far apart the CSMS's cost and the bands' may be: the CSMS rounds the cost to cents.
const costTolerance = 0.01

// stopMotiveCost is the stop motive of a session that reached its authorized amount.
const stopMotiveCost = "COST"

// URL returns the page of a session.
func URL(poolCode int, stationName string, connectorID int, transactionID int64) string {
	return fmt.Sprintf("/emsp-dashboard/sessions/%d/%s/%d/%d", poolCode, url.PathEscape(stationName), connectorID, transactionID)
}

// Build returns the detail of a session. loc is the time zone of the pool, which the start date and the time band
// are written in; now tells whether its meter values were already purged.
func Build(s store.SessionSummary, telemetry []store.TelemetryRecord, loc *time.Location, now time.Time) dto.SessionDetail {
	d := dto.SessionDetail{
		ID: s.ID, TransactionID: s.TransactionID, PoolCode: s.PoolCode, StationName: s.StationName,
		ConnectorID: s.ConnectorID, CardAlias: s.CardAlias, StartDate: s.StartDate, TimeBand: s.TimeBand,
		StopMotive: s.StopMotive, EnergyWh: int64(math.Round(s.TotalEnergyKWh * 1000)), Cost: s.Cost,
		MeterValueCount: len(telemetry), MeterValues: make([]dto.SessionMeterValue, 0, len(telemetry)),
		ExportURL:   fmt.Sprintf("/api/admin/audits/%d/export", s.ID),
		PaymentsURL: "/api/admin" + strings.TrimPrefix(URL(s.PoolCode, s.StationName, s.ConnectorID, s.TransactionID), "/emsp-dashboard") + "/payments",
		AuditsURL:   fmt.Sprintf("/emsp-dashboard/audits/%d", s.PoolCode),
	}
	d.Bands, d.EvaluatedCost, d.CostCheck = bands(s)

	started, startKnown := StartedAt(s, loc)
	if startKnown {
		d.StartedAt = isoTime(started)
	}
	readings := readingsOf(telemetry)
	ended, endKnown := endedAt(s, readings)
	if endKnown {
		d.EndedAt = isoTime(ended)
	}
	if startKnown && endKnown && !ended.Before(started) {
		d.DurationSeconds = ptr(int64(ended.Sub(started) / time.Second))
	}

	for _, t := range telemetry {
		d.MeterValues = append(d.MeterValues, meterValue(s, t))
		if d.PeakPowerW == nil || t.Power > *d.PeakPowerW {
			d.PeakPowerW = ptr(t.Power)
		}
	}
	if lastDelivery, ok := lastDeliveryAt(readings); ok {
		d.LastDeliveryAt = isoTime(lastDelivery)
		if endKnown && !ended.Before(lastDelivery) {
			d.IdleSeconds = ptr(int64(ended.Sub(lastDelivery) / time.Second))
		}
		if startKnown && lastDelivery.After(started) {
			d.MeanPowerW = ptr(int64(math.Round(float64(d.EnergyWh) / lastDelivery.Sub(started).Hours())))
		}
	}
	if len(telemetry) == 0 {
		stored, ok := createdAt(s)
		d.TelemetryPurged = ok && now.Sub(stored) > store.TelemetryRetentionDays*24*time.Hour
	}

	d.Series = Series(telemetry, started, startKnown)
	return d
}

// Series is the power and energy series of the session's email, built the same way: from its stored meter values,
// plus its start with nothing delivered yet. It is nil when the session has nothing to chart.
func Series(telemetry []store.TelemetryRecord, started time.Time, startKnown bool) *dto.SessionSeries {
	readings := series.FromTelemetry(telemetry)
	if len(readings) == 0 {
		return nil
	}
	if startKnown {
		readings = append(readings, series.Reading{At: started})
	}
	s, err := series.Build(readings)
	if err != nil {
		return nil // series.ErrNoSeries: no energy delivered
	}
	return &dto.SessionSeries{Start: s.Start.Format(time.RFC3339), Step: s.Step, Power: s.Power, Energy: s.Energy}
}

// StartedAt is the start of the session: its start date and the start of its time band ("HH:MM - HH:MM"), in the
// pool's time zone. It is only precise to the minute.
func StartedAt(s store.SessionSummary, loc *time.Location) (time.Time, bool) {
	start, _, ok := strings.Cut(s.TimeBand, "-")
	if !ok {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation("02/01/2006 15:04", strings.TrimSpace(s.StartDate)+" "+strings.TrimSpace(start), loc)
	return t, err == nil
}

// endedAt is the end of the session: its last meter value (the CSMS posts the final one when the session stops), or
// else when the history stored the end.
func endedAt(s store.SessionSummary, readings []series.Reading) (time.Time, bool) {
	if len(readings) > 0 {
		return readings[len(readings)-1].At, true
	}
	return createdAt(s)
}

// createdAt is when the history row was stored (SQLite's CURRENT_TIMESTAMP, in UTC).
func createdAt(s store.SessionSummary) (time.Time, bool) {
	t, err := time.Parse(time.DateTime, s.CreatedAt)
	return t, err == nil
}

// readingsOf returns the readings of the meter values in time order (their text order breaks at a daylight saving
// change, as the timestamps carry the pool's UTC offset).
func readingsOf(telemetry []store.TelemetryRecord) []series.Reading {
	readings := series.FromTelemetry(telemetry)
	slices.SortStableFunc(readings, func(a, b series.Reading) int { return a.At.Compare(b.At) })
	return readings
}

// lastDeliveryAt returns the time of the last reading that added energy.
func lastDeliveryAt(readings []series.Reading) (time.Time, bool) {
	var last time.Time
	found := false
	for i := 1; i < len(readings); i++ {
		if readings[i].Energy > readings[i-1].Energy {
			last, found = readings[i].At, true
		}
	}
	return last, found
}

// bands returns the energy and cost of each tariff band, their total, and how it compares with the CSMS's cost.
func bands(s store.SessionSummary) ([]dto.SessionBand, float64, dto.CostCheck) {
	list := []dto.SessionBand{
		{Band: dto.TariffBandOffPeak, EnergyWh: s.EnergyOffPeak, Rate: s.RateOffPeak},
		{Band: dto.TariffBandFlat, EnergyWh: s.EnergyFlat, Rate: s.RateFlat},
		{Band: dto.TariffBandPeak, EnergyWh: s.EnergyPeak, Rate: s.RatePeak},
	}
	total := 0.0
	for i := range list {
		list[i].Cost = round(float64(list[i].EnergyWh)/1000*list[i].Rate, 4)
		total += float64(list[i].EnergyWh) / 1000 * list[i].Rate
	}
	total = round(total, 4)

	check := dto.CostCheckOK
	switch {
	case math.Abs(total-s.Cost) <= costTolerance+1e-9:
	case strings.EqualFold(s.StopMotive, stopMotiveCost) && s.Cost < total:
		check = dto.CostCheckCapped
	default:
		check = dto.CostCheckMismatch
	}
	return list, total, check
}

// meterValue is a row of the XLSX audit, with the cost recomputed from the per-band energy.
func meterValue(s store.SessionSummary, t store.TelemetryRecord) dto.SessionMeterValue {
	evaluated := float64(t.EnergyOffPeak)/1000*s.RateOffPeak + float64(t.EnergyFlat)/1000*s.RateFlat +
		float64(t.EnergyPeak)/1000*s.RatePeak
	return dto.SessionMeterValue{
		Timestamp: t.Timestamp, MeterValue: t.MeterValue, Power: t.Power, Cost: t.Cost, EvaluatedCost: round(evaluated, 4),
		Band: t.CurrentTariffBand, OffPeakWh: t.EnergyOffPeak, FlatWh: t.EnergyFlat, PeakWh: t.EnergyPeak,
	}
}

func isoTime(t time.Time) *string {
	text := t.Format(time.RFC3339)
	return &text
}

func round(f float64, decimals int) float64 {
	scale := math.Pow10(decimals)
	return math.Round(f*scale) / scale
}

func ptr[T any](v T) *T { return &v }
