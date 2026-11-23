// This file holds the history of finished sessions of the management panel: read from the pool's bucket (where it
// goes back further than this service's database), with the dates parsed so it sorts by date, filtered, paged,
// added up and exported as CSV.

package handlers

import (
	"cmp"
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dgarciacampelo/tagoio-handler/internal/chargepoints"
	"github.com/dgarciacampelo/tagoio-handler/internal/dto"
	"github.com/dgarciacampelo/tagoio-handler/internal/tagoio"
)

const (
	historyReadQty     = 10000           // Records read at once: the busiest pool had about 4 100 in November 2026
	historyCacheTTL    = 2 * time.Minute // A pool's history is read from TagoIO at most this often
	historyPageSize    = 25
	historyMaxPageSize = 200

	// HistoryWindowDays is how far back a CPO sees its sessions in the panel (today included): about three months.
	// An older history is exported by Velo Energy on request; the account itself (admin) sees it all.
	HistoryWindowDays = 90
)

// historyEntry is a finished session with its start parsed.
type historyEntry struct {
	item  dto.PanelHistoryItem
	start time.Time
}

// historyCache keeps the parsed history of each pool for a short while.
type historyCache struct {
	mu      sync.Mutex
	entries map[int]cachedHistory
}

type cachedHistory struct {
	entries []historyEntry
	readAt  time.Time
}

// panelHistory returns the pool's finished sessions, newest first, from the cache or the bucket.
func (a *App) panelHistory(ctx context.Context, poolCode int) ([]historyEntry, error) {
	a.history.mu.Lock()
	cached, ok := a.history.entries[poolCode]
	a.history.mu.Unlock()
	if ok && time.Since(cached.readAt) < historyCacheTTL {
		return cached.entries, nil
	}

	records, err := a.PanelData.List(ctx, poolCode, chargepoints.HistoryVariable, historyReadQty)
	if err != nil {
		return nil, err
	}
	entries := parseHistory(records, a.location())
	a.history.mu.Lock()
	a.history.entries[poolCode] = cachedHistory{entries: entries, readAt: time.Now()}
	a.history.mu.Unlock()
	return entries, nil
}

// forgetHistory drops a pool's cached history.
func (a *App) forgetHistory(poolCode int) {
	a.history.mu.Lock()
	delete(a.history.entries, poolCode)
	a.history.mu.Unlock()
}

// parseHistory turns the bucket records into sessions, newest first. Over the years the records were written with
// different keys (alias or card_alias, display_id or serial_id, date or start_date); all are read.
func parseHistory(records []tagoio.DataRecord, loc *time.Location) []historyEntry {
	seen := map[string]bool{}
	entries := make([]historyEntry, 0, len(records))
	for _, r := range records {
		key := cmp.Or(r.Group, r.ID)
		if seen[key] { // The newest record of a session wins (records come newest first)
			continue
		}
		seen[key] = true

		meta := r.Metadata
		text := func(keys ...string) string {
			for _, k := range keys {
				if v := strings.TrimSpace(tagoio.ValueString(meta[k])); v != "" {
					return v
				}
			}
			return ""
		}
		number := func(k string) float64 {
			f, err := tagoio.ValueFloat(meta[k])
			if err != nil {
				return 0
			}
			return f
		}

		band := text("time_band")
		start, minutes := sessionStart(text("date", "start_date"), band, loc)
		if start.IsZero() {
			if t, err := time.Parse(time.RFC3339Nano, r.Time); err == nil {
				start = t.In(loc)
			}
		}
		transaction, _ := tagoio.ValueInt(r.Value)
		if transaction == 0 {
			transaction, _ = strconv.Atoi(r.Group)
		}
		item := dto.PanelHistoryItem{
			TransactionID: int64(transaction),
			TimeBand:      band,
			Station:       text("display_id", "serial_id"),
			Card:          text("alias", "card_alias"),
			Minutes:       minutes,
			EnergyKWh:     number("energy"),
			Cost:          number("cost"),
			StopMotive:    text("stop_motive"),
		}
		if !start.IsZero() {
			item.StartedAt = start.Format("2006-01-02T15:04")
		}
		entries = append(entries, historyEntry{item: item, start: start})
	}
	slices.SortStableFunc(entries, func(x, y historyEntry) int { return y.start.Compare(x.start) })
	return entries
}

// sessionStart parses the start of a session from its date (DD/MM/YYYY) and time band ("HH:MM - HH:MM", local
// time), and its length in minutes (a band past midnight ends the next day).
func sessionStart(date, band string, loc *time.Location) (time.Time, int) {
	day, err := time.ParseInLocation("02/01/2006", strings.TrimSpace(date), loc)
	if err != nil {
		return time.Time{}, 0
	}
	from, to, ok := strings.Cut(band, "-")
	if !ok {
		return day, 0
	}
	begin, err1 := time.Parse("15:04", strings.TrimSpace(from))
	end, err2 := time.Parse("15:04", strings.TrimSpace(to))
	if err1 != nil {
		return day, 0
	}
	start := time.Date(day.Year(), day.Month(), day.Day(), begin.Hour(), begin.Minute(), 0, 0, loc)
	if err2 != nil {
		return start, 0
	}
	minutes := (end.Hour()*60 + end.Minute()) - (begin.Hour()*60 + begin.Minute())
	if minutes < 0 {
		minutes += 24 * 60
	}
	return start, minutes
}

// historyQuery is the filter, order and page of a history request.
type historyQuery struct {
	from, to   time.Time // [from, to); zero: open
	text       string    // Lower case; matches the station or the card
	sort       dto.PanelHistorySort
	descending bool
	page, size int
}

// parseHistoryQuery reads ?from=YYYY-MM-DD&to=YYYY-MM-DD&q=&sort=&dir=asc|desc&page=&page_size=.
func parseHistoryQuery(r *http.Request, loc *time.Location) (historyQuery, error) {
	values := r.URL.Query()
	q := historyQuery{sort: dto.PanelHistorySortDate, descending: true, page: 1, size: historyPageSize,
		text: strings.ToLower(strings.TrimSpace(values.Get("q")))}
	if v := values.Get("from"); v != "" {
		t, err := time.ParseInLocation(time.DateOnly, v, loc)
		if err != nil {
			return q, fmt.Errorf("from must be a date (YYYY-MM-DD)")
		}
		q.from = t
	}
	if v := values.Get("to"); v != "" {
		t, err := time.ParseInLocation(time.DateOnly, v, loc)
		if err != nil {
			return q, fmt.Errorf("to must be a date (YYYY-MM-DD)")
		}
		q.to = t.AddDate(0, 0, 1) // The whole last day
	}
	if v := values.Get("sort"); v != "" {
		sort := dto.PanelHistorySort(v)
		switch sort {
		case dto.PanelHistorySortDate, dto.PanelHistorySortStation, dto.PanelHistorySortCard,
			dto.PanelHistorySortMinutes, dto.PanelHistorySortEnergy, dto.PanelHistorySortCost:
			q.sort = sort
		default:
			return q, fmt.Errorf("unknown sort %q", v)
		}
	}
	switch values.Get("dir") {
	case "", "desc":
	case "asc":
		q.descending = false
	default:
		return q, fmt.Errorf("dir must be asc or desc")
	}
	if v := values.Get("page"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return q, fmt.Errorf("page must be a positive integer")
		}
		q.page = n
	}
	if v := values.Get("page_size"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > historyMaxPageSize {
			return q, fmt.Errorf("page_size must be between 1 and %d", historyMaxPageSize)
		}
		q.size = n
	}
	return q, nil
}

// limitTo keeps the query within [start, …): an earlier or open start becomes start.
func (q *historyQuery) limitTo(start time.Time) {
	if q.from.IsZero() || q.from.Before(start) {
		q.from = start
	}
}

// historyWindowStart is the first day a CPO sees: the local midnight HistoryWindowDays-1 days before now.
func historyWindowStart(now time.Time, loc *time.Location) time.Time {
	day := now.In(loc).AddDate(0, 0, -(HistoryWindowDays - 1))
	return time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, loc)
}

// panelHistoryQuery reads the history query of a request, limited to the window of the caller.
func (a *App) panelHistoryQuery(r *http.Request, caller panelCaller) (historyQuery, error) {
	query, err := parseHistoryQuery(r, a.location())
	if err == nil && !caller.claims.Admin {
		query.limitTo(historyWindowStart(time.Now(), a.location()))
	}
	return query, err
}

// apply returns the entries the query's filter selects, in its order.
func (q historyQuery) apply(entries []historyEntry) []historyEntry {
	selected := make([]historyEntry, 0, len(entries))
	for _, e := range entries {
		if !q.from.IsZero() && e.start.Before(q.from) || !q.to.IsZero() && !e.start.Before(q.to) {
			continue
		}
		if q.text != "" && !strings.Contains(strings.ToLower(e.item.Station), q.text) &&
			!strings.Contains(strings.ToLower(e.item.Card), q.text) {
			continue
		}
		selected = append(selected, e)
	}
	compare := func(x, y historyEntry) int {
		switch q.sort {
		case dto.PanelHistorySortStation:
			return strings.Compare(strings.ToLower(x.item.Station), strings.ToLower(y.item.Station))
		case dto.PanelHistorySortCard:
			return strings.Compare(strings.ToLower(x.item.Card), strings.ToLower(y.item.Card))
		case dto.PanelHistorySortMinutes:
			return cmp.Compare(x.item.Minutes, y.item.Minutes)
		case dto.PanelHistorySortEnergy:
			return cmp.Compare(x.item.EnergyKWh, y.item.EnergyKWh)
		case dto.PanelHistorySortCost:
			return cmp.Compare(x.item.Cost, y.item.Cost)
		default:
			return x.start.Compare(y.start)
		}
	}
	slices.SortStableFunc(selected, func(x, y historyEntry) int {
		c := compare(x, y)
		if c == 0 && q.sort != dto.PanelHistorySortDate {
			c = x.start.Compare(y.start) // Ties keep the date order
		}
		if q.descending {
			return -c
		}
		return c
	})
	return selected
}

// PanelHistoryAPI returns a page of the finished sessions (GET /api/panel/{device_id}/history), within the last
// HistoryWindowDays days for a CPO.
func (a *App) PanelHistoryAPI(w http.ResponseWriter, r *http.Request) {
	caller, ok := a.panelAuth(w, r)
	if !ok {
		return
	}
	query, err := a.panelHistoryQuery(r, caller)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, dto.ErrorCodeInvalidRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), panelTimeout)
	defer cancel()
	entries, err := a.panelHistory(ctx, caller.poolCode)
	if err != nil {
		a.Log.Error("Panel: reading the history failed", "pool_code", caller.poolCode, "error", err)
		writeAPIError(w, http.StatusServiceUnavailable, dto.ErrorCodeServiceUnavailable, "History not available")
		return
	}

	selected := query.apply(entries)
	page := dto.PanelHistoryPage{Items: []dto.PanelHistoryItem{}, Total: len(selected), Page: query.page, PageSize: query.size}
	for _, e := range selected {
		page.Totals.Sessions++
		page.Totals.Minutes += e.item.Minutes
		page.Totals.EnergyKWh += e.item.EnergyKWh
		page.Totals.Cost += e.item.Cost
	}
	page.Totals.EnergyKWh = round(page.Totals.EnergyKWh, 3)
	page.Totals.Cost = round(page.Totals.Cost, 2)
	start := (query.page - 1) * query.size
	for i := start; i < len(selected) && i < start+query.size; i++ {
		page.Items = append(page.Items, selected[i].item)
	}
	writeJSON(w, http.StatusOK, page)
}

// PanelHistoryCSVAPI exports the finished sessions the filter selects (GET /api/panel/{device_id}/history.csv), for
// a spreadsheet in Spanish: semicolons and decimal commas.
func (a *App) PanelHistoryCSVAPI(w http.ResponseWriter, r *http.Request) {
	caller, ok := a.panelAuth(w, r)
	if !ok {
		return
	}
	query, err := a.panelHistoryQuery(r, caller)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, dto.ErrorCodeInvalidRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), panelTimeout)
	defer cancel()
	entries, err := a.panelHistory(ctx, caller.poolCode)
	if err != nil {
		a.Log.Error("Panel: reading the history failed", "pool_code", caller.poolCode, "error", err)
		writeAPIError(w, http.StatusServiceUnavailable, dto.ErrorCodeServiceUnavailable, "History not available")
		return
	}

	name := fmt.Sprintf("recargas_%d_%s.csv", caller.poolCode, time.Now().In(a.location()).Format("2006-01-02"))
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte{0xEF, 0xBB, 0xBF}) // So Excel reads the file as UTF-8
	out := csv.NewWriter(w)
	out.Comma = ';'
	_ = out.Write([]string{"Fecha", "Franja", "Cargador", "Tarjeta", "Minutos", "Energía (kWh)", "Coste (€)", "Transacción", "Motivo de fin"})
	decimal := func(f float64, digits int) string {
		return strings.Replace(strconv.FormatFloat(f, 'f', digits, 64), ".", ",", 1)
	}
	for _, e := range query.apply(entries) {
		date := ""
		if !e.start.IsZero() {
			date = e.start.Format("02/01/2006")
		}
		_ = out.Write([]string{date, e.item.TimeBand, csvSafe(e.item.Station), csvSafe(e.item.Card),
			strconv.Itoa(e.item.Minutes), decimal(e.item.EnergyKWh, 3), decimal(e.item.Cost, 2),
			strconv.FormatInt(e.item.TransactionID, 10), csvSafe(e.item.StopMotive)})
	}
	out.Flush()
}

// csvSafe keeps a spreadsheet from reading a text cell as a formula.
func csvSafe(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

func round(f float64, digits int) float64 {
	v, _ := strconv.ParseFloat(strconv.FormatFloat(f, 'f', digits, 64), 64)
	return v
}
