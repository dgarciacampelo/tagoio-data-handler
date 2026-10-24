package export

import (
	"bytes"
	"testing"

	"github.com/xuri/excelize/v2"

	"github.com/dgarciacampelo/tagoio-handler/internal/store"
)

func TestSessionAuditLayout(t *testing.T) {
	session := store.SessionSummary{
		TransactionID: 42, PoolCode: 261001, StationName: "ST1", ConnectorID: 2, StartDate: "22/10/2026",
		TimeBand: "09:00 - 09:30", Cost: 1.234, TotalEnergyKWh: 5.5, RateOffPeak: 0.1, RateFlat: 0.2, RatePeak: 0.3,
	}
	telemetry := []store.TelemetryRecord{
		{Timestamp: "2026-10-22T07:10:00Z", MeterValue: 2000, Power: 7000, Cost: 0.2, CurrentTariffBand: "Flat", EnergyFlat: 1000},
		{Timestamp: "2026-10-22T07:20:00.5+00:00", MeterValue: 6500, Power: 7100, Cost: 1.2, CurrentTariffBand: "Peak", EnergyFlat: 1000, EnergyPeak: 3500},
	}

	raw, err := SessionAudit(session, telemetry)
	if err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	sheet := "Audit_42"
	cells := map[string]string{
		"A1": "Velo Energy: Charging Session Audit Report - Transaction 42",
		"B2": "261001 / ST1 [Conn: 2]",
		"B3": "Valle: 0.1000 €",
		"B4": "5.500 kWh",
		"D4": "1.23 €",
		"A6": "Timestamp (UTC)",
		"F7": "Flat/Llano",
		"E8": "1.25", // 1 kWh flat × 0.2 + 3.5 kWh peak × 0.3 = 1.25
		"F8": "Peak/Punta",
	}
	for cell, want := range cells {
		got, err := f.GetCellValue(sheet, cell)
		if err != nil || got != want {
			t.Errorf("%s = %q (%v); want %q", cell, got, err, want)
		}
	}
}
