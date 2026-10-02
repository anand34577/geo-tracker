// Package geo holds the shared location point type and small geodesy helpers.
package geo

import (
	"math"
	"strconv"
	"strings"
	"time"
)

// Point is one raw location fix. Optional fields are nil when the source did not report them.
type Point struct {
	TS       int64    `json:"ts"` // UTC unix milliseconds
	Lat      float64  `json:"lat"`
	Lon      float64  `json:"lon"`
	Accuracy *float64 `json:"acc,omitempty"`     // meters
	Altitude *float64 `json:"alt,omitempty"`     // meters
	Speed    *float64 `json:"speed,omitempty"`   // m/s
	Bearing  *float64 `json:"bearing,omitempty"` // degrees
	Battery  *int     `json:"batt,omitempty"`    // percent
}

// minTS rejects fixes from devices with a broken clock (before 1990).
const minTS = 631152000000

// Valid reports whether the point is plausible enough to store.
func (p Point) Valid() bool {
	return p.Lat >= -90 && p.Lat <= 90 && p.Lon >= -180 && p.Lon <= 180 &&
		!(p.Lat == 0 && p.Lon == 0) &&
		p.TS > minTS && p.TS < time.Now().Add(24*time.Hour).UnixMilli()
}

const earthRadius = 6371008.8 // meters

// Distance returns the great-circle distance in meters (haversine).
func Distance(lat1, lon1, lat2, lon2 float64) float64 {
	p1, p2 := lat1*math.Pi/180, lat2*math.Pi/180
	dp := p2 - p1
	dl := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(dp/2)*math.Sin(dp/2) + math.Cos(p1)*math.Cos(p2)*math.Sin(dl/2)*math.Sin(dl/2)
	return 2 * earthRadius * math.Asin(math.Sqrt(math.Min(1, a)))
}

// F and I return pointers, for building points with optional fields.
func F(v float64) *float64 { return &v }
func I(v int) *int         { return &v }

var layouts = []string{
	time.RFC3339Nano,
	"2006-01-02T15:04:05Z0700",
	"2006-01-02 15:04:05Z07:00",
	"2006-01-02T15:04:05.999999999",
	"2006-01-02 15:04:05.999999999",
	"2006/01/02 15:04:05",
}

// ParseTime accepts RFC 3339 and common variants (zone-less means UTC) or unix seconds/milliseconds.
func ParseTime(s string) (int64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	if n, err := strconv.ParseFloat(s, 64); err == nil {
		return UnixAuto(n), true
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			return t.UnixMilli(), true
		}
	}
	return 0, false
}

// UnixAuto converts a unix timestamp in seconds or milliseconds (guessed by magnitude) to milliseconds.
func UnixAuto(n float64) int64 {
	if n > 1e11 {
		return int64(n)
	}
	return int64(n * 1000)
}
