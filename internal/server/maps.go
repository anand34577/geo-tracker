package server

import (
	"errors"
	"net/http"
	"strconv"

	"geotracker/internal/maps"
	"geotracker/internal/store"
)

type basemap struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Light   string `json:"light"` // style URL in light theme
	Dark    string `json:"dark"`  // style URL in dark theme
	Offline bool   `json:"offline,omitempty"`
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
	if mb, err := s.mbtiles(r, st); err == nil && mb != nil {
		name := mb.Name
		if name == "" {
			name = "Offline map"
		}
		list = append(list, basemap{ID: "offline", Name: name + " (offline)", Light: base + "offline?theme=light", Dark: base + "offline?theme=dark", Offline: true})
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
func (s *Server) mbtiles(r *http.Request, st map[string]string) (*maps.MBTiles, error) {
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
		mb, err := s.mbtiles(r, st)
		if err != nil || mb == nil {
			fail(w, http.StatusNotFound, "no offline map configured")
			return
		}
		style = mb.Style(s.baseURL(r)+"/api/v1/map/tiles/{z}/{x}/{y}", st["map_glyphs"], r.URL.Query().Get("theme") == "dark")
	default:
		fail(w, http.StatusNotFound, "unknown map style")
		return
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
	mb, err := s.mbtiles(r, st)
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
