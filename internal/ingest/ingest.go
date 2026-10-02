// Package ingest parses location payloads from phone tracking apps into points.
// Each parser is a pure function so it can be tested against captured payloads.
package ingest

import (
	"encoding/json"
	"errors"
	"math"
	"net/url"
	"strconv"

	"geotracker/internal/geo"
)

// OwnTracks parses an OwnTracks HTTP-mode message. Non-location messages
// (transitions, waypoints, status) yield no points but are not errors.
func OwnTracks(body []byte) ([]geo.Point, error) {
	var m struct {
		Type string   `json:"_type"`
		Lat  float64  `json:"lat"`
		Lon  float64  `json:"lon"`
		Tst  int64    `json:"tst"` // unix seconds
		Acc  *float64 `json:"acc"`
		Alt  *float64 `json:"alt"`
		Vel  *float64 `json:"vel"` // km/h
		Cog  *float64 `json:"cog"`
		Batt *int     `json:"batt"`
	}
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	if m.Type != "location" {
		return nil, nil
	}
	p := geo.Point{TS: m.Tst * 1000, Lat: m.Lat, Lon: m.Lon, Accuracy: m.Acc, Altitude: m.Alt, Bearing: m.Cog, Battery: m.Batt}
	if m.Vel != nil && *m.Vel >= 0 {
		p.Speed = geo.F(*m.Vel / 3.6)
	}
	return []geo.Point{p}, nil
}

type colotaFix struct {
	Lat  float64  `json:"lat"`
	Lon  float64  `json:"lon"`
	Tst  int64    `json:"tst"` // unix seconds
	Acc  *float64 `json:"acc"`
	Alt  *float64 `json:"alt"`
	Vel  *float64 `json:"vel"` // m/s
	Bear *float64 `json:"bear"`
	Batt *int     `json:"batt"`
}

// Colota parses Colota's default ("custom" template) payload: one fix per request,
// or an array of fixes, which we also accept in case batching is enabled.
func Colota(body []byte) ([]geo.Point, error) {
	var fixes []colotaFix
	if err := json.Unmarshal(body, &fixes); err != nil {
		var one colotaFix
		if err := json.Unmarshal(body, &one); err != nil {
			return nil, err
		}
		fixes = []colotaFix{one}
	}
	pts := make([]geo.Point, 0, len(fixes))
	for _, f := range fixes {
		pts = append(pts, geo.Point{TS: geo.UnixAuto(float64(f.Tst)), Lat: f.Lat, Lon: f.Lon, Accuracy: f.Acc,
			Altitude: f.Alt, Speed: nonNeg(f.Vel), Bearing: nonNeg(f.Bear), Battery: f.Batt})
	}
	return pts, nil
}

// Overland parses a batch from the Overland app (GeoJSON features).
func Overland(body []byte) ([]geo.Point, error) {
	var m struct {
		Locations []struct {
			Geometry struct {
				Coordinates []float64 `json:"coordinates"`
			} `json:"geometry"`
			Properties struct {
				Timestamp string   `json:"timestamp"`
				Altitude  *float64 `json:"altitude"`
				Speed     *float64 `json:"speed"`
				HAcc      *float64 `json:"horizontal_accuracy"`
				Course    *float64 `json:"course"`
				Battery   *float64 `json:"battery_level"` // 0..1
			} `json:"properties"`
		} `json:"locations"`
	}
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	pts := make([]geo.Point, 0, len(m.Locations))
	for _, l := range m.Locations {
		ts, ok := geo.ParseTime(l.Properties.Timestamp)
		if !ok || len(l.Geometry.Coordinates) < 2 {
			continue
		}
		p := geo.Point{TS: ts, Lon: l.Geometry.Coordinates[0], Lat: l.Geometry.Coordinates[1],
			Accuracy: l.Properties.HAcc, Altitude: l.Properties.Altitude, Speed: nonNeg(l.Properties.Speed), Bearing: nonNeg(l.Properties.Course)}
		if b := l.Properties.Battery; b != nil && *b >= 0 {
			p.Battery = geo.I(int(math.Round(*b * 100)))
		}
		pts = append(pts, p)
	}
	return pts, nil
}

// iOS reports -1 for "unknown" speed and course.
func nonNeg(v *float64) *float64 {
	if v == nil || *v < 0 {
		return nil
	}
	return v
}

// Params parses query/form parameters as sent by GPSLogger (custom URL) and the
// OsmAnd protocol used by Traccar Client. speedInKnots is true for OsmAnd.
func Params(v url.Values, speedInKnots bool) ([]geo.Point, error) {
	lat, err1 := strconv.ParseFloat(first(v, "lat", "latitude"), 64)
	lon, err2 := strconv.ParseFloat(first(v, "lon", "lng", "longitude"), 64)
	if err1 != nil || err2 != nil {
		return nil, errors.New("missing lat/lon")
	}
	ts, ok := geo.ParseTime(first(v, "timestamp", "time", "tst"))
	if !ok {
		return nil, errors.New("missing or invalid timestamp")
	}
	p := geo.Point{TS: ts, Lat: lat, Lon: lon,
		Accuracy: num(v, "acc", "accuracy", "hdop"),
		Altitude: num(v, "alt", "altitude"),
		Speed:    num(v, "spd", "speed", "vel"),
		Bearing:  num(v, "dir", "bearing", "heading", "bear", "cog"),
	}
	if p.Speed != nil && speedInKnots {
		p.Speed = geo.F(*p.Speed * 0.514444)
	}
	if b := num(v, "batt", "battery"); b != nil {
		p.Battery = geo.I(int(math.Round(*b)))
	}
	return []geo.Point{p}, nil
}

func first(v url.Values, keys ...string) string {
	for _, k := range keys {
		if s := v.Get(k); s != "" {
			return s
		}
	}
	return ""
}

func num(v url.Values, keys ...string) *float64 {
	f, err := strconv.ParseFloat(first(v, keys...), 64)
	if err != nil || math.IsNaN(f) {
		return nil
	}
	return &f
}

// Traccar parses the JSON body sent by newer Traccar Client versions and returns the
// device identifier, which we use as the device token.
func Traccar(body []byte) ([]geo.Point, string, error) {
	var m struct {
		DeviceID string `json:"device_id"`
		Location struct {
			Timestamp string `json:"timestamp"`
			Coords    struct {
				Latitude  float64  `json:"latitude"`
				Longitude float64  `json:"longitude"`
				Accuracy  *float64 `json:"accuracy"`
				Speed     *float64 `json:"speed"` // m/s
				Heading   *float64 `json:"heading"`
				Altitude  *float64 `json:"altitude"`
			} `json:"coords"`
			Battery struct {
				Level *float64 `json:"level"` // 0..1
			} `json:"battery"`
		} `json:"location"`
	}
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, "", err
	}
	ts, ok := geo.ParseTime(m.Location.Timestamp)
	if !ok {
		return nil, m.DeviceID, errors.New("missing or invalid timestamp")
	}
	c := m.Location.Coords
	p := geo.Point{TS: ts, Lat: c.Latitude, Lon: c.Longitude, Accuracy: c.Accuracy, Speed: nonNeg(c.Speed), Bearing: nonNeg(c.Heading), Altitude: c.Altitude}
	if b := m.Location.Battery.Level; b != nil && *b >= 0 {
		p.Battery = geo.I(int(math.Round(*b * 100)))
	}
	return []geo.Point{p}, m.DeviceID, nil
}

// JSON parses our own generic format: an array of points or {"points": [...]}.
func JSON(body []byte) ([]geo.Point, error) {
	var pts []geo.Point
	if err := json.Unmarshal(body, &pts); err == nil {
		return pts, nil
	}
	var wrapped struct {
		Points []geo.Point `json:"points"`
	}
	err := json.Unmarshal(body, &wrapped)
	return wrapped.Points, err
}
