// This file holds the eMSP dashboard (the stations of every pool, with their QR posters) and its APIs.

package handlers

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"maps"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"rsc.io/qr"

	"github.com/dgarciacampelo/tagoio-handler/internal/dto"
)

// EmspPage renders the eMSP dashboard (GET /emsp-dashboard).
func (a *App) EmspPage(w http.ResponseWriter, _ *http.Request) {
	statuses := a.ChargePoints.StationStatuses()
	view := dto.EmspPage{Pools: []dto.EmspPool{}}
	for _, poolCode := range slices.Sorted(maps.Keys(statuses)) {
		cpoName := a.Pools.Get(poolCode).CPOName
		pool := dto.EmspPool{
			PoolCode: poolCode, CPOName: cpoName,
			AuditsURL: fmt.Sprintf("/emsp-dashboard/audits/%d", poolCode),
			Stations:  []dto.EmspStation{},
		}
		stations := statuses[poolCode]
		for _, name := range slices.Sorted(maps.Keys(stations)) {
			connectors := stations[name]
			if len(connectors) == 0 {
				continue // Only connector 0 (the station itself) reported
			}
			station := dto.EmspStation{Name: name, Connectors: []dto.EmspConnector{}, Link: a.stationLink(poolCode, name, cpoName)}
			for _, id := range slices.Sorted(maps.Keys(connectors)) {
				station.Connectors = append(station.Connectors, dto.EmspConnector{ID: id, Status: dto.ConnectorStatus(connectors[id])})
			}
			pool.Stations = append(pool.Stations, station)
		}
		if len(pool.Stations) > 0 {
			view.Pools = append(view.Pools, pool)
		}
	}
	a.page(w, "emsp", "Velo Energy - Panel eMSP", view)
}

// stationLink returns the public page of a station and its QR resources.
func (a *App) stationLink(poolCode int, station, cpoName string) dto.StationLink {
	path := fmt.Sprintf("%d/%s", poolCode, url.PathEscape(station))
	return dto.StationLink{
		PoolCode: poolCode, StationName: station, CPOName: cpoName,
		URL:         a.Config.ShortLinkURL + "/" + path,
		QRImageURL:  "/api/admin/qr/" + path,
		PosterA5URL: "/emsp-dashboard/poster/" + path + "?size=a5",
		PosterA4URL: "/emsp-dashboard/poster/" + path + "?size=a4",
	}
}

// poolCodePattern is the format of a pool code: two digits of the year and a four-digit sequence.
var poolCodePattern = regexp.MustCompile(`^[0-9]{6}$`)

// StationLinkAPI returns the public page and QR resources of any station, also one that has not reported yet
// (GET /api/admin/station-link?pool_code=&station_name=).
func (a *App) StationLinkAPI(w http.ResponseWriter, r *http.Request) {
	poolText := strings.TrimSpace(r.URL.Query().Get("pool_code"))
	station := strings.TrimSpace(r.URL.Query().Get("station_name"))
	if !poolCodePattern.MatchString(poolText) || station == "" || strings.Contains(station, "/") {
		writeAPIError(w, http.StatusUnprocessableEntity, dto.ErrorCodeInvalidRequest, "pool_code must have 6 digits and station_name is required")
		return
	}
	poolCode, _ := strconv.Atoi(poolText)
	cpoName := ""
	if config := a.Pools.Get(poolCode); config.Loaded {
		cpoName = config.CPOName
	}
	writeJSON(w, http.StatusOK, a.stationLink(poolCode, station, cpoName))
}

// region QR codes

const (
	qrTargetSize = 840 // Pixels: printed at 280 CSS pixels, three times sharper
	qrMargin     = 2   // Quiet zone, in modules
)

// qrGreen is the brand's forest green, used for the QR modules.
var qrGreen = color.RGBA{R: 0x00, G: 0x57, B: 0x1b, A: 0xff}

// QRImage returns the QR code of a station's public page as a PNG (GET /api/admin/qr/{pool_code}/{station_name}).
func (a *App) QRImage(w http.ResponseWriter, r *http.Request) {
	poolCode, ok := pathInt(w, r, "pool_code")
	if !ok {
		return
	}
	station, ok := pathText(w, r, "station_name")
	if !ok {
		return
	}
	link := a.stationLink(poolCode, station, "")
	image, err := qrPNG(link.URL)
	if err != nil {
		a.Log.Error("QR generation failed", "url", link.URL, "error", err)
		writeAPIError(w, http.StatusInternalServerError, dto.ErrorCodeInternalError, "QR generation failed")
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	_, _ = w.Write(image)
}

// qrPNG encodes text as a QR code (error correction M) drawn in the brand green on white.
func qrPNG(text string) ([]byte, error) {
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return nil, err
	}
	modules := code.Size + 2*qrMargin
	scale := max(1, qrTargetSize/modules)
	size := modules * scale

	img := image.NewPaletted(image.Rect(0, 0, size, size), color.Palette{color.White, qrGreen})
	for y := range code.Size {
		for x := range code.Size {
			if !code.Black(x, y) {
				continue
			}
			for dy := range scale {
				row := (y+qrMargin)*scale + dy
				for dx := range scale {
					img.SetColorIndex((x+qrMargin)*scale+dx, row, 1)
				}
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// endregion

// PosterPage renders a printable QR poster of a station (GET /emsp-dashboard/poster/{pool_code}/{station_name}).
func (a *App) PosterPage(w http.ResponseWriter, r *http.Request) {
	poolCode, ok := pathInt(w, r, "pool_code")
	if !ok {
		return
	}
	station, ok := pathText(w, r, "station_name")
	if !ok {
		return
	}
	size := dto.PosterSizeA5
	if strings.EqualFold(r.URL.Query().Get("size"), string(dto.PosterSizeA4)) {
		size = dto.PosterSizeA4
	}
	cpoName := ""
	if config := a.Pools.Get(poolCode); config.Loaded {
		cpoName = config.CPOName
	}
	a.page(w, "poster", "Cartel QR - "+station, dto.PosterPage{Size: size, Link: a.stationLink(poolCode, station, cpoName)})
}

// DeleteStationAPI forgets a station and its connectors (DELETE /api/admin/stations/{pool_code}/{station_name}).
// The station comes back if it reports again.
func (a *App) DeleteStationAPI(w http.ResponseWriter, r *http.Request) {
	poolCode, ok := pathInt(w, r, "pool_code")
	if !ok {
		return
	}
	station, ok := pathText(w, r, "station_name")
	if !ok {
		return
	}
	existed, err := a.ChargePoints.DeleteStation(r.Context(), poolCode, station)
	switch {
	case err != nil:
		a.Log.Error("Station deletion failed", "pool_code", poolCode, "station", station, "error", err)
		writeAPIError(w, http.StatusInternalServerError, dto.ErrorCodeInternalError, "Station deletion failed")
	case !existed:
		writeAPIError(w, http.StatusNotFound, dto.ErrorCodeNotFound, "Unknown station")
	default:
		a.Log.Info("Station deleted", "pool_code", poolCode, "station", station)
		w.WriteHeader(http.StatusNoContent)
	}
}
