// Package geocode turns coordinates into place names via Nominatim or Photon (ADR-012).
package geocode

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type Result struct {
	Name, Address, City, CountryCode string
}

var defaultURLs = map[string]string{
	"nominatim": "https://nominatim.openstreetmap.org",
	"photon":    "https://photon.komoot.io",
}

type Client struct {
	HTTP      *http.Client
	UserAgent string // Nominatim's usage policy requires an identifying User-Agent
}

func (c *Client) Reverse(ctx context.Context, provider, base string, lat, lon float64) (*Result, error) {
	if base == "" {
		base = defaultURLs[provider]
	}
	base = strings.TrimRight(base, "/")
	q := url.Values{"lat": {fmt.Sprint(lat)}, "lon": {fmt.Sprint(lon)}}
	switch provider {
	case "nominatim":
		q.Set("format", "jsonv2")
		q.Set("zoom", "18")
		q.Set("addressdetails", "1")
		var r struct {
			Name    string            `json:"name"`
			Error   string            `json:"error"`
			Address map[string]string `json:"address"`
		}
		if err := c.get(ctx, base+"/reverse?"+q.Encode(), &r); err != nil {
			return nil, err
		}
		if r.Error != "" {
			return &Result{}, nil // e.g. middle of the ocean: cache the empty answer
		}
		a := r.Address
		street := join(" ", a["road"], a["house_number"])
		city := firstOf(a["city"], a["town"], a["village"], a["hamlet"], a["municipality"])
		return &Result{
			Name:        firstOf(r.Name, street, a["suburb"], city),
			Address:     join(", ", street, city),
			City:        city,
			CountryCode: strings.ToUpper(a["country_code"]),
		}, nil
	case "photon":
		var r struct {
			Features []struct {
				Properties map[string]string `json:"properties"`
			} `json:"features"`
		}
		if err := c.get(ctx, base+"/reverse?"+q.Encode(), &r); err != nil {
			return nil, err
		}
		if len(r.Features) == 0 {
			return &Result{}, nil
		}
		p := r.Features[0].Properties
		street := join(" ", p["street"], p["housenumber"])
		city := firstOf(p["city"], p["town"], p["village"], p["district"])
		return &Result{
			Name:        firstOf(p["name"], street, city),
			Address:     join(", ", street, city),
			City:        city,
			CountryCode: strings.ToUpper(p["countrycode"]),
		}, nil
	}
	return nil, fmt.Errorf("unknown geocoder %q", provider)
}

type Hit struct {
	Name    string  `json:"name"`
	Address string  `json:"address"`
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
}

// Search finds places by name or address (forward geocoding), at most 6 results.
func (c *Client) Search(ctx context.Context, provider, base, q, lang string) ([]Hit, error) {
	if base == "" {
		base = defaultURLs[provider]
	}
	base = strings.TrimRight(base, "/")
	var hits []Hit
	switch provider {
	case "nominatim":
		u := base + "/search?" + url.Values{"q": {q}, "format": {"jsonv2"}, "limit": {"6"}, "addressdetails": {"1"}, "accept-language": {lang}}.Encode()
		var rs []struct {
			Name        string `json:"name"`
			DisplayName string `json:"display_name"`
			Lat         string `json:"lat"`
			Lon         string `json:"lon"`
		}
		if err := c.get(ctx, u, &rs); err != nil {
			return nil, err
		}
		for _, r := range rs {
			var h Hit
			fmt.Sscan(r.Lat, &h.Lat)
			fmt.Sscan(r.Lon, &h.Lon)
			h.Name, h.Address = firstOf(r.Name, r.DisplayName), r.DisplayName
			hits = append(hits, h)
		}
	case "photon":
		u := base + "/api/?" + url.Values{"q": {q}, "limit": {"6"}}.Encode()
		var r struct {
			Features []struct {
				Geometry struct {
					Coordinates []float64 `json:"coordinates"`
				} `json:"geometry"`
				Properties map[string]any `json:"properties"`
			} `json:"features"`
		}
		if err := c.get(ctx, u, &r); err != nil {
			return nil, err
		}
		for _, f := range r.Features {
			if len(f.Geometry.Coordinates) < 2 {
				continue
			}
			p := func(k string) string { s, _ := f.Properties[k].(string); return s }
			street := join(" ", p("street"), p("housenumber"))
			hits = append(hits, Hit{Name: firstOf(p("name"), street), Address: join(", ", street, p("city"), p("country")),
				Lon: f.Geometry.Coordinates[0], Lat: f.Geometry.Coordinates[1]})
		}
	default:
		return nil, fmt.Errorf("address search is off (geocoder: %q)", provider)
	}
	return hits, nil
}

func (c *Client) get(ctx context.Context, u string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("geocoder: HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

func firstOf(xs ...string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}

func join(sep string, xs ...string) string {
	var parts []string
	for _, x := range xs {
		if x != "" {
			parts = append(parts, x)
		}
	}
	return strings.Join(parts, sep)
}
