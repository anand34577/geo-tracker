package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"geotracker/internal/maps"
	"geotracker/internal/store"
)

type basemap struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Light   string `json:"light"` // style URL in light theme
	Dark    string `json:"dark"`  // style URL in dark theme
	Offline bool   `json:"offline,omitempty"`
	Custom  bool   `json:"custom,omitempty"`
}

// basemaps lists the map styles users can pick from: free/open-source online maps,
// plus the admin's own offline MBTiles file when configured.
func (s *Server) basemaps(r *http.Request, st map[string]string) []basemap {
	base := s.baseURL(r) + "/api/v1/map/style/"
	ofm := "https://tiles.openfreemap.org/styles/"
	list := []basemap{
		{ID: "auto", Name: "Standard (follows theme)", Light: st["map_style_light"], Dark: st["map_style_dark"]},
		{ID: "liberty", Name: "OpenFreeMap Liberty", Light: ofm + "liberty", Dark: ofm + "liberty"},
		{ID: "bright", Name: "OpenFreeMap Bright", Light: ofm + "bright", Dark: ofm + "bright"},
		{ID: "osm", Name: "OpenStreetMap", Light: base + "osm", Dark: base + "osm"},
		{ID: "topo", Name: "OpenTopoMap (terrain)", Light: base + "topo", Dark: base + "topo"},
	}
	if mb, err := s.mbtiles(st); err == nil && mb != nil {
		name := mb.Name
		if name == "" {
			name = "Offline map"
		}
		list = append(list, basemap{ID: "offline", Name: name + " (offline)", Light: base + "offline?theme=light", Dark: base + "offline?theme=dark", Offline: true})
	}
	for _, c := range maps.ParseCustom(st["map_custom"]) {
		// The version changes with the definition, so an edited map is not served from the browser's style cache.
		raw, _ := json.Marshal(c)
		h := fnv.New32a()
		h.Write(raw)
		u := fmt.Sprintf("%s%s?v=%x", base, c.ID, h.Sum32())
		b := basemap{ID: c.ID, Name: c.Name, Light: u, Dark: u, Custom: true}
		if c.Kind == "style" {
			b.Light, b.Dark = c.URL, c.URL
			if c.URLDark != "" {
				b.Dark = c.URLDark
			}
		}
		list = append(list, b)
	}
	return list
}

// Close releases the offline map file.
func (s *Server) Close() {
	s.mapMu.Lock()
	defer s.mapMu.Unlock()
	if s.mb != nil {
		s.mb.Close()
		s.mb = nil
	}
}

// mbtiles returns the configured offline map, reopening it when the setting changes.
func (s *Server) mbtiles(st map[string]string) (*maps.MBTiles, error) {
	path := st["map_mbtiles"]
	s.mapMu.Lock()
	defer s.mapMu.Unlock()
	if s.mb != nil && s.mb.Path == path {
		return s.mb, nil
	}
	if s.mb != nil {
		s.mb.Close()
		s.mb = nil
	}
	if path == "" {
		return nil, nil
	}
	mb, err := maps.Open(path)
	if err != nil {
		return nil, err
	}
	s.mb = mb
	return mb, nil
}

func (s *Server) mapStyle(w http.ResponseWriter, r *http.Request, _ *store.User) {
	st, err := s.db.Settings(r.Context())
	if err != nil {
		internal(w, r, err)
		return
	}
	var style map[string]any
	switch r.PathValue("id") {
	case "osm":
		style = maps.RasterStyle("OpenStreetMap", []string{"https://tile.openstreetmap.org/{z}/{x}/{y}.png"},
			`© <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors`, 19)
	case "topo":
		style = maps.RasterStyle("OpenTopoMap", []string{"https://a.tile.opentopomap.org/{z}/{x}/{y}.png", "https://b.tile.opentopomap.org/{z}/{x}/{y}.png", "https://c.tile.opentopomap.org/{z}/{x}/{y}.png"},
			`© OpenStreetMap contributors, SRTM · style © <a href="https://opentopomap.org">OpenTopoMap</a> (CC-BY-SA)`, 17)
	case "offline":
		mb, err := s.mbtiles(st)
		if err != nil || mb == nil {
			fail(w, http.StatusNotFound, "no offline map configured")
			return
		}
		style = mb.Style(s.baseURL(r)+"/api/v1/map/tiles/{z}/{x}/{y}", st["map_glyphs"], r.URL.Query().Get("theme") == "dark")
	default:
		c, ok := maps.FindCustom(maps.ParseCustom(st["map_custom"]), r.PathValue("id"))
		if !ok || c.Kind != "raster" {
			fail(w, http.StatusNotFound, "unknown map style")
			return
		}
		tiles, tms := c.BrowserTiles()
		if c.Proxy {
			tiles, tms = []string{s.baseURL(r) + "/api/v1/map/proxy/" + c.ID + "/{z}/{x}/{y}"}, false
		}
		style = maps.CustomRasterStyle(c, tiles, tms)
	}
	w.Header().Set("Cache-Control", "private, max-age=300")
	writeJSON(w, http.StatusOK, style)
}

func (s *Server) mapTile(w http.ResponseWriter, r *http.Request, _ *store.User) {
	st, err := s.db.Settings(r.Context())
	if err != nil {
		internal(w, r, err)
		return
	}
	mb, err := s.mbtiles(st)
	if err != nil || mb == nil {
		fail(w, http.StatusNotFound, "no offline map configured")
		return
	}
	z, e1 := strconv.Atoi(r.PathValue("z"))
	x, e2 := strconv.Atoi(r.PathValue("x"))
	y, e3 := strconv.Atoi(r.PathValue("y"))
	if e1 != nil || e2 != nil || e3 != nil || z < 0 || z > 24 {
		fail(w, http.StatusBadRequest, "bad tile coordinates")
		return
	}
	data, err := mb.Tile(r.Context(), z, x, y)
	if errors.Is(err, maps.ErrNoTile) {
		w.WriteHeader(http.StatusNoContent) // outside the file's area: an empty tile, not an error
		return
	}
	if err != nil {
		internal(w, r, err)
		return
	}
	w.Header().Set("Content-Type", mb.ContentType())
	if len(data) > 2 && data[0] == 0x1f && data[1] == 0x8b {
		w.Header().Set("Content-Encoding", "gzip") // vector tiles are usually stored gzipped
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Write(data)
}

// ── Custom map tile proxy ────────────────────────────────────

const maxTileBytes = 8 << 20

var tileClient = &http.Client{Timeout: 20 * time.Second}

// tileSlots caps concurrent upstream fetches so a fast-panning map cannot pile up hundreds of requests.
var tileSlots = make(chan struct{}, 24)

func tileType(ct string) (string, bool) {
	mt, _, err := mime.ParseMediaType(ct)
	if err != nil {
		return "", false
	}
	ok := strings.HasPrefix(mt, "image/") || mt == "application/x-protobuf" || mt == "application/vnd.mapbox-vector-tile" || mt == "application/octet-stream"
	return mt, ok && mt != "image/svg+xml" // SVG can carry script; never serve it from our origin
}

// fetchTile gets one upstream tile; the error message is safe to show to an admin.
func (s *Server) fetchTile(ctx context.Context, c maps.Custom, z, x, y int) (data []byte, contentType string, status int, err error) {
	select {
	case tileSlots <- struct{}{}:
		defer func() { <-tileSlots }()
	case <-ctx.Done():
		return nil, "", 0, ctx.Err()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.TileURL(z, x, y), nil)
	if err != nil {
		return nil, "", 0, err
	}
	// Public tile servers (OpenStreetMap among them) require an identifying User-Agent.
	req.Header.Set("User-Agent", "GeoTracker/"+s.cfg.Version+" (self-hosted tile proxy)")
	res, err := tileClient.Do(req)
	if err != nil {
		return nil, "", 0, fmt.Errorf("tile server unreachable: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, "", res.StatusCode, fmt.Errorf("tile server answered %s", res.Status)
	}
	ct, ok := tileType(res.Header.Get("Content-Type"))
	if !ok {
		return nil, "", res.StatusCode, fmt.Errorf("tile server sent %q, not an image", res.Header.Get("Content-Type"))
	}
	data, err = io.ReadAll(io.LimitReader(res.Body, maxTileBytes+1))
	if err == nil && len(data) > maxTileBytes {
		err = errors.New("tile is too large")
	}
	return data, ct, res.StatusCode, err
}

func (s *Server) mapProxy(w http.ResponseWriter, r *http.Request, _ *store.User) {
	st, err := s.db.Settings(r.Context())
	if err != nil {
		internal(w, r, err)
		return
	}
	c, ok := maps.FindCustom(maps.ParseCustom(st["map_custom"]), r.PathValue("id"))
	if !ok || c.Kind != "raster" || !c.Proxy {
		fail(w, http.StatusNotFound, "unknown map")
		return
	}
	z, e1 := strconv.Atoi(r.PathValue("z"))
	x, e2 := strconv.Atoi(r.PathValue("x"))
	y, e3 := strconv.Atoi(r.PathValue("y"))
	if e1 != nil || e2 != nil || e3 != nil || z < 0 || z > 24 || x < 0 || y < 0 || x >= 1<<z || y >= 1<<z {
		fail(w, http.StatusBadRequest, "bad tile coordinates")
		return
	}
	data, ct, status, err := s.fetchTile(r.Context(), c, z, x, y)
	if status == http.StatusNotFound || status == http.StatusNoContent {
		w.WriteHeader(http.StatusNoContent) // outside the server's coverage: an empty tile
		return
	}
	if err != nil {
		if r.Context().Err() == nil {
			fail(w, http.StatusBadGateway, err.Error())
		}
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Write(data)
}

// testCustomMap validates a custom map the admin is editing and fetches one tile to prove it works.
func (s *Server) testCustomMap(w http.ResponseWriter, r *http.Request, _ *store.User) {
	var c maps.Custom
	if !decode(w, r, &c) {
		return
	}
	if err := c.Normalize(); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if c.Kind == "style" {
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.URL, nil)
		res, err := tileClient.Do(req)
		if err != nil {
			fail(w, http.StatusBadGateway, "style unreachable: "+err.Error())
			return
		}
		defer res.Body.Close()
		var style struct {
			Version int            `json:"version"`
			Sources map[string]any `json:"sources"`
		}
		if res.StatusCode != http.StatusOK || decodeLimited(res.Body, &style) != nil || style.Version != 8 {
			fail(w, http.StatusBadGateway, "that URL did not return a MapLibre style (version 8 JSON)")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	// A low-zoom tile in the middle of the map's zoom range is the one most servers have.
	z := max(c.MinZoom, min(c.MaxZoom, 2))
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	data, ct, _, err := s.fetchTile(ctx, c, z, 1<<z/2, 1<<z/2-1)
	if err != nil {
		fail(w, http.StatusBadGateway, err.Error())
		return
	}
	out := map[string]any{"ok": true, "content_type": ct, "bytes": len(data)}
	if strings.HasPrefix(ct, "image/") && len(data) < 1<<20 {
		out["preview"] = "data:" + ct + ";base64," + base64.StdEncoding.EncodeToString(data)
	}
	writeJSON(w, http.StatusOK, out)
}

func decodeLimited(r io.Reader, v any) error {
	return json.NewDecoder(io.LimitReader(r, 4<<20)).Decode(v)
}
