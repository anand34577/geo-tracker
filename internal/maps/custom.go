package maps

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
)

// Custom is an admin-defined basemap: an XYZ/TMS/WMS raster tile template, or a MapLibre style URL.
type Custom struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`               // "raster" (tile URL template) | "style" (MapLibre style JSON URL)
	URL         string `json:"url"`                // tile template or style URL (light theme)
	URLDark     string `json:"url_dark,omitempty"` // optional style URL for dark theme ("style" only)
	Attribution string `json:"attribution,omitempty"`
	MinZoom     int    `json:"min_zoom"`
	MaxZoom     int    `json:"max_zoom"`
	TileSize    int    `json:"tile_size"`            // 256 or 512
	Subdomains  string `json:"subdomains,omitempty"` // letters for {s}, default "abc"
	Proxy       bool   `json:"proxy"`                // fetch tiles through the server (hides API keys, allows http:// and LAN servers)
}

const MaxCustom = 30

// ParseCustom decodes the map_custom setting; an empty or broken value is no custom maps.
func ParseCustom(raw string) []Custom {
	var out []Custom
	if strings.TrimSpace(raw) == "" || json.Unmarshal([]byte(raw), &out) != nil {
		return nil
	}
	return out
}

// FindCustom returns the custom map with that id.
func FindCustom(list []Custom, id string) (Custom, bool) {
	for _, c := range list {
		if c.ID == id {
			return c, true
		}
	}
	return Custom{}, false
}

func newID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return "c" + hex.EncodeToString(b)
}

func httpURL(v string) (*url.URL, bool) {
	u, err := url.Parse(v)
	return u, err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != ""
}

// placeholders a tile template may use; anything else in braces is rejected so typos surface at save time.
var placeholders = []string{"{z}", "{x}", "{y}", "{-y}", "{s}", "{r}", "{quadkey}", "{bbox-epsg-3857}"}

func isTemplate(v string) bool {
	return strings.Contains(v, "{z}") || strings.Contains(v, "{quadkey}") || strings.Contains(v, "{bbox-epsg-3857}")
}

// IsTileTemplate reports whether v looks like an XYZ tile template rather than a style URL.
func IsTileTemplate(v string) bool { return isTemplate(v) }

func validTemplate(v string) error {
	if len(v) > 2000 {
		return errors.New("URL is too long")
	}
	rest := v
	for _, p := range placeholders {
		rest = strings.ReplaceAll(rest, p, "")
	}
	if i := strings.IndexAny(rest, "{}"); i >= 0 {
		end := strings.IndexByte(rest[i:], '}')
		bad := rest[i:]
		if end > 0 {
			bad = rest[i : i+end+1]
		}
		return fmt.Errorf("unknown placeholder %s (use {z} {x} {y}, {-y} for TMS, {s}, {r}, {quadkey} or {bbox-epsg-3857})", bad)
	}
	hasXYZ := strings.Contains(v, "{z}") && strings.Contains(v, "{x}") && (strings.Contains(v, "{y}") || strings.Contains(v, "{-y}"))
	if !hasXYZ && !strings.Contains(v, "{quadkey}") && !strings.Contains(v, "{bbox-epsg-3857}") {
		return errors.New("tile URL needs {z}, {x} and {y} (or {quadkey}, or {bbox-epsg-3857} for WMS)")
	}
	if _, ok := httpURL(strings.NewReplacer("{s}", "a", "{r}", "", "{z}", "0", "{x}", "0", "{y}", "0", "{-y}", "0", "{quadkey}", "0", "{bbox-epsg-3857}", "0").Replace(v)); !ok {
		return errors.New("tile URL must start with http:// or https://")
	}
	return nil
}

// Normalize validates c, fills defaults and keeps an existing id (or assigns a new one).
func (c *Custom) Normalize() error {
	c.Name = strings.TrimSpace(c.Name)
	c.URL = strings.TrimSpace(c.URL)
	c.URLDark = strings.TrimSpace(c.URLDark)
	c.Attribution = strings.TrimSpace(c.Attribution)
	c.Subdomains = strings.TrimSpace(c.Subdomains)
	if c.Name == "" || len(c.Name) > 60 {
		return errors.New("enter a name (up to 60 characters)")
	}
	if len(c.Attribution) > 500 {
		return errors.New("attribution is too long (500 characters max)")
	}
	if c.ID == "" {
		c.ID = newID()
	}
	if len(c.ID) > 16 || strings.Trim(c.ID, "abcdefghijklmnopqrstuvwxyz0123456789") != "" {
		return errors.New("invalid map id")
	}
	if c.Kind == "" {
		c.Kind = "raster"
		if !strings.ContainsAny(c.URL, "{}") {
			c.Kind = "style"
		}
	}
	switch c.Kind {
	case "raster":
		if err := validTemplate(c.URL); err != nil {
			return err
		}
		if strings.HasPrefix(c.URL, "http://") {
			c.Proxy = true // the browser blocks plain-http tiles on the map (CSP), the server can still fetch them
		}
		c.URLDark = ""
		if c.MaxZoom == 0 {
			c.MaxZoom = 19
		}
		if c.MinZoom < 0 || c.MaxZoom > 24 || c.MinZoom > c.MaxZoom {
			return errors.New("zoom levels must be between 0 and 24, minimum below maximum")
		}
		if c.TileSize == 0 {
			c.TileSize = 256
		}
		if c.TileSize != 256 && c.TileSize != 512 {
			return errors.New("tile size must be 256 or 512")
		}
		if c.Subdomains == "" && strings.Contains(c.URL, "{s}") {
			c.Subdomains = "abc"
		}
		if len(c.Subdomains) > 10 {
			return errors.New("too many subdomains")
		}
	case "style":
		if c.URL == "" {
			return errors.New("enter the style URL")
		}
		for _, v := range []string{c.URL, c.URLDark} {
			if v == "" {
				continue // no dark variant: the light style is used in both themes
			}
			if strings.ContainsAny(v, "{}") {
				return errors.New("that is a tile URL; choose the \"Tile URL (XYZ)\" type")
			}
			if u, ok := httpURL(v); !ok || u.Scheme != "https" {
				return errors.New("style URL must be an https:// link to a MapLibre/Mapbox style JSON")
			}
		}
		c.Proxy, c.MinZoom, c.MaxZoom, c.TileSize, c.Subdomains = false, 0, 0, 0, ""
	default:
		return errors.New("map type must be raster or style")
	}
	return nil
}

// NormalizeList validates a whole list and rejects duplicate ids and names.
func NormalizeList(list []Custom) error {
	if len(list) > MaxCustom {
		return fmt.Errorf("at most %d custom maps", MaxCustom)
	}
	ids, names := map[string]bool{}, map[string]bool{}
	for i := range list {
		if err := list[i].Normalize(); err != nil {
			return fmt.Errorf("%s: %w", orUnnamed(list[i].Name), err)
		}
		n := strings.ToLower(list[i].Name)
		if ids[list[i].ID] || names[n] {
			return fmt.Errorf("%s: two maps share that name", list[i].Name)
		}
		ids[list[i].ID], names[n] = true, true
	}
	return nil
}

func orUnnamed(s string) string {
	if s == "" {
		return "custom map"
	}
	return s
}

// BrowserTiles turns the template into MapLibre "tiles" URLs: {s} is expanded into one URL per
// subdomain, {r} dropped, and {-y} becomes {y} with scheme "tms".
func (c Custom) BrowserTiles() (tiles []string, tms bool) {
	u := strings.ReplaceAll(c.URL, "{r}", "")
	if strings.Contains(u, "{-y}") {
		u, tms = strings.ReplaceAll(u, "{-y}", "{y}"), true
	}
	if !strings.Contains(u, "{s}") {
		return []string{u}, tms
	}
	for _, s := range c.Subdomains {
		tiles = append(tiles, strings.ReplaceAll(u, "{s}", string(s)))
	}
	return tiles, tms
}

// TileURL is the upstream URL of tile z/x/y (XYZ numbering), used by the server-side proxy.
func (c Custom) TileURL(z, x, y int) string {
	sub := "a"
	if c.Subdomains != "" {
		sub = string(c.Subdomains[(x+y)%len(c.Subdomains)])
	}
	return strings.NewReplacer(
		"{z}", strconv.Itoa(z), "{x}", strconv.Itoa(x), "{y}", strconv.Itoa(y), "{-y}", strconv.Itoa((1<<z)-1-y),
		"{s}", sub, "{r}", "", "{quadkey}", quadkey(z, x, y), "{bbox-epsg-3857}", bbox3857(z, x, y),
	).Replace(c.URL)
}

func quadkey(z, x, y int) string {
	var b strings.Builder
	for i := z; i > 0; i-- {
		d := byte('0')
		m := 1 << (i - 1)
		if x&m != 0 {
			d++
		}
		if y&m != 0 {
			d += 2
		}
		b.WriteByte(d)
	}
	return b.String()
}

func bbox3857(z, x, y int) string {
	const half = 20037508.342789244
	size := 2 * half / math.Exp2(float64(z))
	minX, maxY := -half+float64(x)*size, half-float64(y)*size
	f := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
	return f(minX) + "," + f(maxY-size) + "," + f(minX+size) + "," + f(maxY)
}

// CustomRasterStyle is the MapLibre style for a raster custom map; tiles are absolute URLs.
func CustomRasterStyle(c Custom, tiles []string, tms bool) map[string]any {
	src := map[string]any{"type": "raster", "tiles": tiles, "tileSize": c.TileSize, "minzoom": c.MinZoom, "maxzoom": c.MaxZoom, "attribution": c.Attribution}
	if tms {
		src["scheme"] = "tms"
	}
	return map[string]any{
		"version": 8,
		"name":    c.Name,
		"sources": map[string]any{"raster": src},
		"layers":  []any{map[string]any{"id": "raster", "type": "raster", "source": "raster"}},
	}
}
