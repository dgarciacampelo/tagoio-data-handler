// Package export builds the audit workbook of a charging session: its summary (station, period, frozen rates and
// totals) and every meter value, with the cost recomputed from the per-band energy to check the CSMS's own.
package export

import (
	"bytes"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/dgarciacampelo/tagoio-handler/internal/store"
)

const (
	brandFill = "00571B" // Forest green of the brand, with white text
	brandText = "FFFFFF"
	headerRow = 6
)

// auditHeaders are the columns of the meter values table.
var auditHeaders = []string{
	"Timestamp (UTC)", "Meter Value (Wh)", "Power (W)", "Reported Cost (€)", "Evaluated Cost (€)",
	"Active Band", "Off-Peak Cum. (Wh)", "Flat Cum. (Wh)", "Peak Cum. (Wh)",
}

// bandNames translates the tariff bands of the CSMS to the Spanish names.
var bandNames = map[string]string{"Off-Peak": "Valle", "Flat": "Llano", "Peak": "Punta"}

// SessionAudit returns the audit workbook (XLSX) of a session.
func SessionAudit(session store.SessionSummary, telemetry []store.TelemetryRecord) ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()

	sheet := fmt.Sprintf("Audit_%d", session.TransactionID)
	if err := f.SetSheetName("Sheet1", sheet); err != nil {
		return nil, err
	}

	titleStyle, err := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 14, Color: brandText},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{brandFill}},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})
	if err != nil {
		return nil, err
	}
	headerStyle, err := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: brandText},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{brandFill}},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})
	if err != nil {
		return nil, err
	}
	labelStyle, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	if err != nil {
		return nil, err
	}
	dateFormat := "yyyy-mm-dd hh:mm:ss"
	dateStyle, err := f.NewStyle(&excelize.Style{CustomNumFmt: &dateFormat})
	if err != nil {
		return nil, err
	}

	// Summary (rows 1-4).
	cells := map[string]any{
		"A1": fmt.Sprintf("Velo Energy: Charging Session Audit Report - Transaction %d", session.TransactionID),
		"A2": "Station:",
		"B2": fmt.Sprintf("%d / %s [Conn: %d]", session.PoolCode, session.StationName, session.ConnectorID),
		"C2": "Start Date:", "D2": session.StartDate,
		"E2": "Charging Interval:", "F2": session.TimeBand,
		"A3": "Rates (€/kWh):",
		"B3": fmt.Sprintf("Valle: %.4f €", session.RateOffPeak),
		"C3": fmt.Sprintf("Llano: %.4f €", session.RateFlat),
		"D3": fmt.Sprintf("Punta: %.4f €", session.RatePeak),
		"A4": "Final Energy:", "B4": fmt.Sprintf("%.3f kWh", session.TotalEnergyKWh),
		"C4": "Final Cost:", "D4": fmt.Sprintf("%.2f €", session.Cost),
	}
	for cell, value := range cells {
		if err := f.SetCellValue(sheet, cell, value); err != nil {
			return nil, err
		}
	}
	if err := f.MergeCell(sheet, "A1", "I1"); err != nil {
		return nil, err
	}
	if err := f.SetCellStyle(sheet, "A1", "I1", titleStyle); err != nil {
		return nil, err
	}
	for _, cell := range []string{"A2", "A3", "A4", "C2", "C4", "E2"} {
		if err := f.SetCellStyle(sheet, cell, cell, labelStyle); err != nil {
			return nil, err
		}
	}

	// Meter values (header on row 6).
	for i, header := range auditHeaders {
		cell, _ := excelize.CoordinatesToCellName(i+1, headerRow)
		if err := f.SetCellValue(sheet, cell, header); err != nil {
			return nil, err
		}
	}
	if err := f.SetCellStyle(sheet, "A6", "I6", headerStyle); err != nil {
		return nil, err
	}
	if err := f.SetColWidth(sheet, "A", "I", 20); err != nil {
		return nil, err
	}

	for i, t := range telemetry {
		row := headerRow + 1 + i
		evaluated := float64(t.EnergyOffPeak)/1000*session.RateOffPeak +
			float64(t.EnergyFlat)/1000*session.RateFlat +
			float64(t.EnergyPeak)/1000*session.RatePeak
		band := t.CurrentTariffBand
		if name, ok := bandNames[band]; ok {
			band += "/" + name
		} else {
			band += "/" + band
		}
		values := []any{
			parseTimestamp(t.Timestamp), t.MeterValue, t.Power, t.Cost, roundTo4(evaluated),
			band, t.EnergyOffPeak, t.EnergyFlat, t.EnergyPeak,
		}
		cell, _ := excelize.CoordinatesToCellName(1, row)
		if err := f.SetSheetRow(sheet, cell, &values); err != nil {
			return nil, err
		}
		if err := f.SetCellStyle(sheet, cell, cell, dateStyle); err != nil {
			return nil, err
		}
	}

	lastRow := max(headerRow+len(telemetry), headerRow)
	if err := f.AutoFilter(sheet, fmt.Sprintf("A%d:I%d", headerRow, lastRow), nil); err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// parseTimestamp converts an ISO 8601 timestamp to a time without zone (Excel has none), keeping the text when
// it does not parse.
func parseTimestamp(s string) any {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, strings.TrimSpace(s)); err == nil {
			return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.UTC)
		}
	}
	return s
}

func roundTo4(f float64) float64 {
	return math.Round(f*10000) / 10000
}
