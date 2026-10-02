// Package maps serves offline basemaps from MBTiles files and builds MapLibre styles.
//
// MBTiles is itself a SQLite database, so it is read with the driver we already ship:
// offline maps add no dependency.
package maps

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type MBTiles struct {
	Path         string
	Format       string // pbf | png | jpg | webp
	Name         string
	Attribution  string
	MinZoom      int
	MaxZoom      int
	Bounds       []float64 // west, south, east, north
	VectorLayers []string
	db           *sql.DB
}

var ErrNoTile = errors.New("tile not found")

func Open(path string) (*MBTiles, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("map file: %w", err)
	}
	db, err := sql.Open("sqlite", path+"?_pragma=query_only(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	m := &MBTiles{Path: path, db: db, MinZoom: 0, MaxZoom: 14}
	rows, err := db.Query(`SELECT name, value FROM metadata`)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("not an MBTiles file (no metadata table): %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if rows.Scan(&k, &v) != nil {
			continue
		}
		switch k {
		case "format":
			m.Format = strings.ToLower(v)
		case "name":
			m.Name = v
		case "attribution":
			m.Attribution = v
		case "minzoom":
			m.MinZoom, _ = strconv.Atoi(v)
		case "maxzoom":
			m.MaxZoom, _ = strconv.Atoi(v)
		case "bounds":
			for _, s := range strings.Split(v, ",") {
				if f, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
					m.Bounds = append(m.Bounds, f)
				}
			}
		case "json":
			var j struct {
				VectorLayers []struct {
					ID string `json:"id"`
				} `json:"vector_layers"`
			}
			if json.Unmarshal([]byte(v), &j) == nil {
				for _, l := range j.VectorLayers {
					m.VectorLayers = append(m.VectorLayers, l.ID)
				}
			}
		}
	}
	if m.Format == "" {
		m.Format = "pbf"
	}
	if len(m.Bounds) != 4 {
		m.Bounds = nil
	}
	return m, nil
}

func (m *MBTiles) Close() error { return m.db.Close() }

// Tile returns tile z/x/y in XYZ (web) numbering; MBTiles stores rows flipped (TMS).
func (m *MBTiles) Tile(ctx context.Context, z, x, y int) ([]byte, error) {
	var data []byte
	err := m.db.QueryRowContext(ctx, `SELECT tile_data FROM tiles WHERE zoom_level = ? AND tile_column = ? AND tile_row = ?`,
		z, x, (1<<z)-1-y).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoTile
	}
	return data, err
}

// ContentType is the MIME type of the (decompressed) tiles.
func (m *MBTiles) ContentType() string {
	switch m.Format {
	case "png":
		return "image/png"
	case "jpg", "jpeg":
		return "image/jpeg"
	case "webp":
		return "image/webp"
	}
	return "application/x-protobuf"
}

func (m *MBTiles) IsVector() bool { return m.Format == "pbf" || m.Format == "mvt" }

// ── Styles ───────────────────────────────────────────────────

type palette struct {
	bg, water, park, wood, landuse, building, road, roadCase, major, minor, rail, boundary, label, halo string
}

var light = palette{"#f5f3ef", "#aad3df", "#cdebb0", "#add19e", "#ebe7e0", "#ddd6cc", "#ffffff", "#cfc8bd", "#fcd6a4", "#ffffff", "#b5b0a8", "#9e7fa9", "#3b3632", "#ffffff"}
var dark = palette{"#16181b", "#1c2c3a", "#1d2b20", "#1a2a1d", "#1d1f22", "#2a2c30", "#34373c", "#101214", "#5a4a31", "#34373c", "#3f4247", "#6c5775", "#c9c5c0", "#16181b"}

const labelFont = "Noto Sans Regular"

// Style builds a MapLibre style for this file. tileURL is an absolute URL template
// with {z}/{x}/{y}. glyphs may be empty for a fully offline map without labels.
func (m *MBTiles) Style(tileURL, glyphs string, isDark bool) map[string]any {
	src := map[string]any{"tiles": []string{tileURL}, "minzoom": m.MinZoom, "maxzoom": m.MaxZoom, "attribution": m.Attribution}
	if m.Bounds != nil {
		src["bounds"] = m.Bounds
	}
	style := map[string]any{"version": 8, "name": m.Name, "sources": map[string]any{"offline": src}}
	if !m.IsVector() {
		src["type"], src["tileSize"] = "raster", 256
		style["layers"] = []any{map[string]any{"id": "offline", "type": "raster", "source": "offline"}}
		return style
	}
	src["type"] = "vector"
	p := light
	if isDark {
		p = dark
	}
	if glyphs != "" {
		style["glyphs"] = glyphs
	}
	has := map[string]bool{}
	for _, l := range m.VectorLayers {
		has[l] = true
	}
	if has["transportation"] && has["water"] { // OpenMapTiles schema (also used by OpenFreeMap, MapTiler)
		style["layers"] = openMapTilesLayers(p, glyphs != "")
	} else {
		style["layers"] = genericLayers(p, m.VectorLayers)
	}
	return style
}

func layer(id, typ, srcLayer string, filter any, paint map[string]any, extra ...map[string]any) map[string]any {
	l := map[string]any{"id": id, "type": typ, "source": "offline", "source-layer": srcLayer, "paint": paint}
	if filter != nil {
		l["filter"] = filter
	}
	for _, e := range extra {
		for k, v := range e {
			l[k] = v
		}
	}
	return l
}

func in(field string, values ...string) []any {
	f := []any{"in", field}
	for _, v := range values {
		f = append(f, v)
	}
	return f
}

// width scales a line width with zoom.
func width(z1, w1, z2, w2 float64) []any {
	return []any{"interpolate", []any{"exponential", 1.5}, []any{"zoom"}, z1, w1, z2, w2}
}

// openMapTilesLayers is a compact, readable style for the OpenMapTiles vector schema.
func openMapTilesLayers(p palette, labels bool) []any {
	ls := []any{
		map[string]any{"id": "background", "type": "background", "paint": map[string]any{"background-color": p.bg}},
		layer("landuse", "fill", "landuse", in("class", "residential", "commercial", "industrial", "retail"), map[string]any{"fill-color": p.landuse}),
		layer("park", "fill", "park", nil, map[string]any{"fill-color": p.park, "fill-opacity": 0.7}),
		layer("wood", "fill", "landcover", in("class", "wood", "grass", "farmland"), map[string]any{"fill-color": p.wood, "fill-opacity": 0.5}),
		layer("water", "fill", "water", nil, map[string]any{"fill-color": p.water}),
		layer("waterway", "line", "waterway", nil, map[string]any{"line-color": p.water, "line-width": width(8, 0.5, 18, 6)}),
		layer("building", "fill", "building", nil, map[string]any{"fill-color": p.building, "fill-opacity": 0.8}, map[string]any{"minzoom": 13}),
		layer("boundary", "line", "boundary", []any{"<=", "admin_level", 4}, map[string]any{"line-color": p.boundary, "line-width": 1, "line-dasharray": []any{3, 2}}),
		layer("road-minor", "line", "transportation", in("class", "minor", "service", "track", "path"),
			map[string]any{"line-color": p.minor, "line-width": width(12, 0.5, 18, 10)}, map[string]any{"minzoom": 12, "layout": map[string]any{"line-cap": "round", "line-join": "round"}}),
		layer("road-case", "line", "transportation", in("class", "motorway", "trunk", "primary", "secondary", "tertiary"),
			map[string]any{"line-color": p.roadCase, "line-width": width(6, 1, 18, 22)}, map[string]any{"layout": map[string]any{"line-cap": "round", "line-join": "round"}}),
		layer("road", "line", "transportation", in("class", "secondary", "tertiary"),
			map[string]any{"line-color": p.road, "line-width": width(6, 0.5, 18, 18)}, map[string]any{"layout": map[string]any{"line-cap": "round", "line-join": "round"}}),
		layer("road-major", "line", "transportation", in("class", "motorway", "trunk", "primary"),
			map[string]any{"line-color": p.major, "line-width": width(5, 0.8, 18, 20)}, map[string]any{"layout": map[string]any{"line-cap": "round", "line-join": "round"}}),
		layer("rail", "line", "transportation", in("class", "rail", "transit"), map[string]any{"line-color": p.rail, "line-width": 1, "line-dasharray": []any{3, 3}}),
	}
	if !labels {
		return ls
	}
	text := func(id, srcLayer string, filter any, size []any, extra map[string]any) map[string]any {
		layout := map[string]any{"text-field": []any{"coalesce", []any{"get", "name:latin"}, []any{"get", "name"}}, "text-font": []any{labelFont}, "text-size": size, "text-max-width": 8}
		for k, v := range extra {
			layout[k] = v
		}
		return layer(id, "symbol", srcLayer, filter, map[string]any{"text-color": p.label, "text-halo-color": p.halo, "text-halo-width": 1.5}, map[string]any{"layout": layout})
	}
	return append(ls,
		text("road-label", "transportation_name", nil, []any{"interpolate", []any{"linear"}, []any{"zoom"}, 13, 10, 18, 14}, map[string]any{"symbol-placement": "line"}),
		text("water-label", "water_name", nil, []any{"literal", 12}, nil),
		text("place-minor", "place", in("class", "suburb", "neighbourhood", "village", "hamlet"), []any{"interpolate", []any{"linear"}, []any{"zoom"}, 10, 11, 16, 15}, nil),
		text("place-city", "place", in("class", "city", "town"), []any{"interpolate", []any{"linear"}, []any{"zoom"}, 4, 11, 12, 20}, nil),
		text("place-country", "place", in("class", "country", "state"), []any{"literal", 13}, map[string]any{"text-transform": "uppercase", "text-letter-spacing": 0.1}),
	)
}

// genericLayers renders any vector schema: polygons filled, lines and outlines stroked.
func genericLayers(p palette, vectorLayers []string) []any {
	ls := []any{map[string]any{"id": "background", "type": "background", "paint": map[string]any{"background-color": p.bg}}}
	for _, l := range vectorLayers {
		ls = append(ls,
			layer(l+"-fill", "fill", l, []any{"==", "$type", "Polygon"}, map[string]any{"fill-color": p.landuse, "fill-opacity": 0.6}),
			layer(l+"-line", "line", l, nil, map[string]any{"line-color": p.roadCase, "line-width": 0.8}),
		)
	}
	return ls
}

// RasterStyle is a style for a public XYZ raster tile server.
func RasterStyle(name string, tiles []string, attribution string, maxZoom int) map[string]any {
	return map[string]any{
		"version": 8,
		"name":    name,
		"sources": map[string]any{"raster": map[string]any{"type": "raster", "tiles": tiles, "tileSize": 256, "maxzoom": maxZoom, "attribution": attribution}},
		"layers":  []any{map[string]any{"id": "raster", "type": "raster", "source": "raster"}},
	}
}
