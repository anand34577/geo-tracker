package transfer

import (
	"archive/zip"
	"bufio"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"geotracker/internal/geo"
)

const (
	nativeFormat  = "geotracker-export"
	nativeVersion = 1
)

// Manifest describes a native export (ADR-013). Newer app versions always import older formats.
type Manifest struct {
	Format        string         `json:"format"`
	FormatVersion int            `json:"format_version"`
	AppVersion    string         `json:"app_version"`
	ExportedAt    string         `json:"exported_at"`
	User          string         `json:"user,omitempty"`
	Counts        map[string]int `json:"counts"`
}

// PointIter streams points in time order.
type PointIter func(fn func(geo.Point) error) error

// Formats lists supported export formats with their file extension and MIME type.
var Formats = map[string][2]string{
	"native":  {"zip", "application/zip"},
	"gpx":     {"gpx", "application/gpx+xml"},
	"geojson": {"geojson", "application/geo+json"},
	"csv":     {"csv", "text/csv"},
}

// WriteNative writes the lossless archive. Visits and trips are included for other tools;
// on import they are recomputed from points (ADR-009).
func WriteNative(w io.Writer, appVersion, user string, points PointIter, places []Place, visits, trips []any) error {
	zw := zip.NewWriter(w)
	m := Manifest{Format: nativeFormat, FormatVersion: nativeVersion, AppVersion: appVersion,
		ExportedAt: time.Now().UTC().Format(time.RFC3339), User: user, Counts: map[string]int{}}

	f, err := zw.Create("points.ndjson")
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	if err := points(func(p geo.Point) error { m.Counts["points"]++; return enc.Encode(p) }); err != nil {
		return err
	}
	for name, rows := range map[string][]any{"visits.ndjson": visits, "trips.ndjson": trips} {
		f, err := zw.Create(name)
		if err != nil {
			return err
		}
		enc := json.NewEncoder(f)
		for _, r := range rows {
			if err := enc.Encode(r); err != nil {
				return err
			}
		}
		m.Counts[strings.TrimSuffix(name, ".ndjson")] = len(rows)
	}
	m.Counts["places"] = len(places)
	for name, v := range map[string]any{"places.json": places, "manifest.json": m} {
		f, err := zw.Create(name)
		if err != nil {
			return err
		}
		e := json.NewEncoder(f)
		e.SetIndent("", "  ")
		if err := e.Encode(v); err != nil {
			return err
		}
	}
	return zw.Close()
}

func isoMs(ts int64) string { return time.UnixMilli(ts).UTC().Format("2006-01-02T15:04:05.000Z") }

// WriteGPX writes one track, starting a new segment after gaps longer than an hour.
func WriteGPX(w io.Writer, name string, points PointIter) error {
	bw := bufio.NewWriter(w)
	bw.WriteString(xml.Header + `<gpx version="1.1" creator="GeoTracker" xmlns="http://www.topografix.com/GPX/1/1">` + "\n<trk><name>")
	xml.EscapeText(bw, []byte(name))
	bw.WriteString("</name>\n<trkseg>\n")
	var last int64
	err := points(func(p geo.Point) error {
		if last != 0 && p.TS-last > 3_600_000 {
			bw.WriteString("</trkseg>\n<trkseg>\n")
		}
		last = p.TS
		fmt.Fprintf(bw, `<trkpt lat="%.7f" lon="%.7f">`, p.Lat, p.Lon)
		if p.Altitude != nil {
			fmt.Fprintf(bw, "<ele>%.1f</ele>", *p.Altitude)
		}
		_, err := fmt.Fprintf(bw, "<time>%s</time></trkpt>\n", isoMs(p.TS))
		return err
	})
	if err != nil {
		return err
	}
	bw.WriteString("</trkseg>\n</trk>\n</gpx>\n")
	return bw.Flush()
}

// WriteGeoJSON writes a FeatureCollection of Point features.
func WriteGeoJSON(w io.Writer, points PointIter) error {
	bw := bufio.NewWriter(w)
	bw.WriteString(`{"type":"FeatureCollection","features":[` + "\n")
	n := 0
	err := points(func(p geo.Point) error {
		props := map[string]any{"time": isoMs(p.TS)}
		for k, v := range map[string]*float64{"accuracy": p.Accuracy, "altitude": p.Altitude, "speed": p.Speed, "bearing": p.Bearing} {
			if v != nil {
				props[k] = *v
			}
		}
		if p.Battery != nil {
			props["battery"] = *p.Battery
		}
		b, err := json.Marshal(map[string]any{"type": "Feature", "geometry": map[string]any{"type": "Point", "coordinates": []float64{p.Lon, p.Lat}}, "properties": props})
		if err != nil {
			return err
		}
		if n > 0 {
			bw.WriteString(",\n")
		}
		n++
		_, err = bw.Write(b)
		return err
	})
	if err != nil {
		return err
	}
	bw.WriteString("\n]}\n")
	return bw.Flush()
}

func WriteCSV(w io.Writer, points PointIter) error {
	cw := csv.NewWriter(w)
	cw.Write([]string{"time", "latitude", "longitude", "accuracy", "altitude", "speed", "bearing", "battery"})
	opt := func(v *float64) string {
		if v == nil {
			return ""
		}
		return strconv.FormatFloat(*v, 'f', -1, 64)
	}
	err := points(func(p geo.Point) error {
		batt := ""
		if p.Battery != nil {
			batt = strconv.Itoa(*p.Battery)
		}
		return cw.Write([]string{isoMs(p.TS), strconv.FormatFloat(p.Lat, 'f', 7, 64), strconv.FormatFloat(p.Lon, 'f', 7, 64),
			opt(p.Accuracy), opt(p.Altitude), opt(p.Speed), opt(p.Bearing), batt})
	})
	if err != nil {
		return err
	}
	cw.Flush()
	return cw.Error()
}
