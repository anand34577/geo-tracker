// Package transfer reads and writes location history files.
// Every importer streams, so multi-gigabyte Google exports use constant memory.
package transfer

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strconv"
	"strings"

	"geotracker/internal/geo"
	"geotracker/internal/ingest"
)

type Place struct {
	Name   string  `json:"name"`
	Icon   string  `json:"icon"`
	Lat    float64 `json:"lat"`
	Lon    float64 `json:"lon"`
	Radius float64 `json:"radius"`
}

// Sink receives imported data. Place may be nil when the caller ignores places.
type Sink struct {
	Point func(geo.Point) error
	Place func(Place) error
}

var errUnknownJSON = errors.New("unrecognized JSON file (expected a Google Timeline, GeoJSON or GeoTracker export)")

// Import reads the file at p; name (the original filename) selects the parser.
// It returns a short format label for the import report.
func Import(p, name string, sink Sink) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if strings.EqualFold(path.Ext(name), ".zip") {
		st, err := f.Stat()
		if err != nil {
			return "", err
		}
		return importZip(f, st.Size(), sink)
	}
	return importReader(name, f, sink)
}

func importReader(name string, r io.Reader, sink Sink) (string, error) {
	switch ext := strings.ToLower(path.Ext(name)); ext {
	case ".json", ".geojson":
		return importJSON(r, sink)
	case ".gpx":
		return "gpx", importGPX(r, sink)
	case ".csv":
		return "csv", importCSV(r, sink)
	case ".rec":
		return "owntracks", importRec(r, sink)
	default:
		return "", fmt.Errorf("unsupported file type %q (supported: .zip .json .geojson .gpx .csv .rec)", ext)
	}
}

func importZip(r io.ReaderAt, size int64, sink Sink) (string, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return "", err
	}
	hasRecords := false
	for _, f := range zr.File {
		switch path.Base(f.Name) {
		case "manifest.json":
			return "geotracker", importNative(zr, sink)
		case "Records.json":
			hasRecords = true
		}
	}
	var formats []string
	for _, f := range zr.File {
		// Takeout ships both raw records and the semantic summary derived from them; raw wins.
		if f.FileInfo().IsDir() || (hasRecords && strings.Contains(f.Name, "Semantic Location History")) {
			continue
		}
		switch strings.ToLower(path.Ext(f.Name)) {
		case ".json", ".geojson", ".gpx", ".csv", ".rec":
		default:
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		format, err := importReader(f.Name, rc, sink)
		rc.Close()
		if errors.Is(err, errUnknownJSON) {
			continue // Takeout zips contain unrelated JSON (settings, other products)
		}
		if err != nil {
			return "", fmt.Errorf("%s: %w", f.Name, err)
		}
		if !contains(formats, format) {
			formats = append(formats, format)
		}
	}
	if len(formats) == 0 {
		return "", errors.New("no supported location files found in zip")
	}
	return strings.Join(formats, "+"), nil
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// ── JSON: Google (3 formats), GeoJSON, generic ───────────────

func importJSON(r io.Reader, sink Sink) (string, error) {
	dec := json.NewDecoder(bufio.NewReaderSize(r, 1<<20))
	tok, err := dec.Token()
	if err != nil {
		return "", err
	}
	if tok == json.Delim('[') {
		// iOS Timeline export, or a plain array of {ts, lat, lon} points.
		return "google-timeline", eachElem(dec, func(raw json.RawMessage) error { return segment(raw, sink) })
	}
	if tok != json.Delim('{') {
		return "", errUnknownJSON
	}
	format := ""
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return format, err
		}
		key, _ := kt.(string)
		var fn func(json.RawMessage, Sink) error
		switch key {
		case "locations":
			format, fn = "google-records", record
		case "timelineObjects":
			format, fn = "google-semantic", semantic
		case "semanticSegments":
			format, fn = "google-timeline", segment
		case "rawSignals":
			format, fn = "google-timeline", rawSignal
		case "features":
			format, fn = "geojson", feature
		}
		if fn == nil {
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				return format, err
			}
			continue
		}
		if t, err := dec.Token(); err != nil || t != json.Delim('[') {
			return format, fmt.Errorf("%q: expected an array", key)
		}
		if err := eachElem(dec, func(raw json.RawMessage) error { return fn(raw, sink) }); err != nil {
			return format, err
		}
	}
	if format == "" {
		return "", errUnknownJSON
	}
	return format, nil
}

// eachElem decodes array elements one at a time and consumes the closing bracket.
func eachElem(dec *json.Decoder, fn func(json.RawMessage) error) error {
	for dec.More() {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return err
		}
		if err := fn(raw); err != nil {
			return err
		}
	}
	_, err := dec.Token()
	return err
}

func e7(v *int64) float64 { return float64(*v) / 1e7 }

func gtime(iso, ms string) (int64, bool) {
	if ts, ok := geo.ParseTime(iso); ok {
		return ts, true
	}
	return geo.ParseTime(ms)
}

// record: legacy Takeout "Records.json" entry.
func record(raw json.RawMessage, sink Sink) error {
	var l struct {
		Lat         *int64   `json:"latitudeE7"`
		Lon         *int64   `json:"longitudeE7"`
		Timestamp   string   `json:"timestamp"`
		TimestampMs string   `json:"timestampMs"`
		Accuracy    *float64 `json:"accuracy"`
		Altitude    *float64 `json:"altitude"`
		Velocity    *float64 `json:"velocity"`
		Heading     *float64 `json:"heading"`
	}
	if json.Unmarshal(raw, &l) != nil || l.Lat == nil || l.Lon == nil {
		return nil
	}
	ts, ok := gtime(l.Timestamp, l.TimestampMs)
	if !ok {
		return nil
	}
	return sink.Point(geo.Point{TS: ts, Lat: e7(l.Lat), Lon: e7(l.Lon), Accuracy: l.Accuracy, Altitude: l.Altitude, Speed: l.Velocity, Bearing: l.Heading})
}

type e7loc struct {
	Lat  *int64 `json:"latitudeE7"`
	Lon  *int64 `json:"longitudeE7"`
	LatS *int64 `json:"latE7"`
	LonS *int64 `json:"lngE7"`
}

func (l e7loc) point(ts int64) (geo.Point, bool) {
	lat, lon := l.Lat, l.Lon
	if lat == nil {
		lat, lon = l.LatS, l.LonS
	}
	if lat == nil || lon == nil {
		return geo.Point{}, false
	}
	return geo.Point{TS: ts, Lat: e7(lat), Lon: e7(lon)}, true
}

type gduration struct {
	Start   string `json:"startTimestamp"`
	End     string `json:"endTimestamp"`
	StartMs string `json:"startTimestampMs"`
	EndMs   string `json:"endTimestampMs"`
}

// semantic: legacy Takeout "Semantic Location History" monthly entry.
// ponytail: imported as points and re-derived by our engine; Google's place names are dropped.
func semantic(raw json.RawMessage, sink Sink) error {
	var o struct {
		PlaceVisit *struct {
			Location e7loc     `json:"location"`
			Duration gduration `json:"duration"`
		} `json:"placeVisit"`
		Activity *struct {
			Start    e7loc     `json:"startLocation"`
			End      e7loc     `json:"endLocation"`
			Duration gduration `json:"duration"`
			Path     struct {
				Points []struct {
					e7loc
					Timestamp   string `json:"timestamp"`
					TimestampMs string `json:"timestampMs"`
				} `json:"points"`
			} `json:"simplifiedRawPath"`
		} `json:"activitySegment"`
	}
	if json.Unmarshal(raw, &o) != nil {
		return nil
	}
	emit := func(l e7loc, ts int64, ok bool) error {
		if p, valid := l.point(ts); ok && valid {
			return sink.Point(p)
		}
		return nil
	}
	if v := o.PlaceVisit; v != nil {
		start, ok1 := gtime(v.Duration.Start, v.Duration.StartMs)
		end, ok2 := gtime(v.Duration.End, v.Duration.EndMs)
		if err := emit(v.Location, start, ok1); err != nil {
			return err
		}
		return emit(v.Location, end, ok2)
	}
	if a := o.Activity; a != nil {
		start, ok := gtime(a.Duration.Start, a.Duration.StartMs)
		if err := emit(a.Start, start, ok); err != nil {
			return err
		}
		for _, p := range a.Path.Points {
			ts, ok := gtime(p.Timestamp, p.TimestampMs)
			if err := emit(p.e7loc, ts, ok); err != nil {
				return err
			}
		}
		end, ok := gtime(a.Duration.End, a.Duration.EndMs)
		return emit(a.End, end, ok)
	}
	return nil
}

// parseLatLng handles both on-device export styles: "48.1°, 11.5°" (Android) and "geo:48.1,11.5" (iOS).
func parseLatLng(s string) (lat, lon float64, ok bool) {
	s = strings.TrimPrefix(strings.ReplaceAll(s, "°", ""), "geo:")
	a, b, found := strings.Cut(s, ",")
	if !found {
		return 0, 0, false
	}
	lat, err1 := strconv.ParseFloat(strings.TrimSpace(a), 64)
	lon, err2 := strconv.ParseFloat(strings.TrimSpace(b), 64)
	return lat, lon, err1 == nil && err2 == nil
}

// latLngOf accepts either a string or an object holding "latLng"/"LatLng".
func latLngOf(raw json.RawMessage) (float64, float64, bool) {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return parseLatLng(s)
	}
	var o struct {
		A string `json:"latLng"`
		B string `json:"LatLng"`
	}
	if json.Unmarshal(raw, &o) == nil {
		return parseLatLng(o.A + o.B)
	}
	return 0, 0, false
}

// segment: on-device Timeline export (Android "semanticSegments" or the iOS top-level array).
// Also accepts plain {ts|timestamp, lat, lon} objects.
func segment(raw json.RawMessage, sink Sink) error {
	var s struct {
		StartTime    string `json:"startTime"`
		EndTime      string `json:"endTime"`
		TimelinePath []struct {
			Point  string `json:"point"`
			Time   string `json:"time"`
			Offset string `json:"durationMinutesOffsetFromStartTime"`
		} `json:"timelinePath"`
		Visit *struct {
			TopCandidate struct {
				PlaceLocation json.RawMessage `json:"placeLocation"`
			} `json:"topCandidate"`
		} `json:"visit"`
		Activity *struct {
			Start json.RawMessage `json:"start"`
			End   json.RawMessage `json:"end"`
		} `json:"activity"`
		// generic point
		Lat       *float64 `json:"lat"`
		Lon       *float64 `json:"lon"`
		TS        any      `json:"ts"`
		Timestamp any      `json:"timestamp"`
	}
	if json.Unmarshal(raw, &s) != nil {
		return nil
	}
	if s.Lat != nil && s.Lon != nil {
		ts, ok := anyTime(s.TS)
		if !ok {
			ts, ok = anyTime(s.Timestamp)
		}
		if !ok {
			return nil
		}
		var p geo.Point
		_ = json.Unmarshal(raw, &p) // a string "ts" fails only that field; the rest still decodes
		p.TS = ts
		return sink.Point(p)
	}
	start, okStart := geo.ParseTime(s.StartTime)
	end, okEnd := geo.ParseTime(s.EndTime)
	at := func(ll json.RawMessage, ts int64, ok bool) error {
		if lat, lon, valid := latLngOf(ll); ok && valid {
			return sink.Point(geo.Point{TS: ts, Lat: lat, Lon: lon})
		}
		return nil
	}
	if v := s.Visit; v != nil {
		if err := at(v.TopCandidate.PlaceLocation, start, okStart); err != nil {
			return err
		}
		if err := at(v.TopCandidate.PlaceLocation, end, okEnd); err != nil {
			return err
		}
	}
	if a := s.Activity; a != nil {
		if err := at(a.Start, start, okStart); err != nil {
			return err
		}
		if err := at(a.End, end, okEnd); err != nil {
			return err
		}
	}
	for _, tp := range s.TimelinePath {
		lat, lon, ok := parseLatLng(tp.Point)
		if !ok {
			continue
		}
		ts, okT := geo.ParseTime(tp.Time)
		if !okT && okStart {
			if mins, err := strconv.ParseFloat(tp.Offset, 64); err == nil {
				ts, okT = start+int64(mins*60_000), true
			}
		}
		if okT {
			if err := sink.Point(geo.Point{TS: ts, Lat: lat, Lon: lon}); err != nil {
				return err
			}
		}
	}
	return nil
}

// rawSignal: Android on-device export raw position fixes.
func rawSignal(raw json.RawMessage, sink Sink) error {
	var r struct {
		Position *struct {
			LatLng    string   `json:"LatLng"`
			Accuracy  *float64 `json:"accuracyMeters"`
			Altitude  *float64 `json:"altitudeMeters"`
			Speed     *float64 `json:"speedMetersPerSecond"`
			Timestamp string   `json:"timestamp"`
		} `json:"position"`
	}
	if json.Unmarshal(raw, &r) != nil || r.Position == nil {
		return nil
	}
	lat, lon, ok := parseLatLng(r.Position.LatLng)
	ts, okT := geo.ParseTime(r.Position.Timestamp)
	if !ok || !okT {
		return nil
	}
	return sink.Point(geo.Point{TS: ts, Lat: lat, Lon: lon, Accuracy: r.Position.Accuracy, Altitude: r.Position.Altitude, Speed: r.Position.Speed})
}

// feature: GeoJSON Point (time in properties) or LineString (with "coordTimes").
func feature(raw json.RawMessage, sink Sink) error {
	var f struct {
		Geometry struct {
			Type        string          `json:"type"`
			Coordinates json.RawMessage `json:"coordinates"`
		} `json:"geometry"`
		Properties map[string]any `json:"properties"`
	}
	if json.Unmarshal(raw, &f) != nil {
		return nil
	}
	props := f.Properties
	switch f.Geometry.Type {
	case "Point":
		var c []float64
		if json.Unmarshal(f.Geometry.Coordinates, &c) != nil || len(c) < 2 {
			return nil
		}
		ts, ok := propTime(props, "timestamp", "time", "ts", "date")
		if !ok {
			return nil
		}
		p := geo.Point{TS: ts, Lon: c[0], Lat: c[1],
			Accuracy: propNum(props, "acc", "accuracy", "horizontal_accuracy"),
			Altitude: propNum(props, "alt", "altitude", "ele"),
			Speed:    propNum(props, "speed"),
			Bearing:  propNum(props, "bearing", "course"),
		}
		if len(c) > 2 && p.Altitude == nil {
			p.Altitude = geo.F(c[2])
		}
		if b := propNum(props, "batt", "battery"); b != nil {
			p.Battery = geo.I(int(*b))
		}
		return sink.Point(p)
	case "LineString":
		var cs [][]float64
		if json.Unmarshal(f.Geometry.Coordinates, &cs) != nil {
			return nil
		}
		times, _ := props["coordTimes"].([]any)
		for i, c := range cs {
			if len(c) < 2 || i >= len(times) {
				continue
			}
			ts, ok := anyTime(times[i])
			if ok {
				if err := sink.Point(geo.Point{TS: ts, Lon: c[0], Lat: c[1]}); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func anyTime(v any) (int64, bool) {
	switch t := v.(type) {
	case string:
		return geo.ParseTime(t)
	case float64:
		return geo.UnixAuto(t), true
	}
	return 0, false
}

func propTime(p map[string]any, keys ...string) (int64, bool) {
	for _, k := range keys {
		if ts, ok := anyTime(p[k]); ok {
			return ts, true
		}
	}
	return 0, false
}

func propNum(p map[string]any, keys ...string) *float64 {
	for _, k := range keys {
		if f, ok := p[k].(float64); ok {
			return &f
		}
	}
	return nil
}

// ── GPX ──────────────────────────────────────────────────────

func importGPX(r io.Reader, sink Sink) error {
	dec := xml.NewDecoder(bufio.NewReaderSize(r, 1<<20))
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		se, ok := tok.(xml.StartElement)
		if !ok || (se.Name.Local != "trkpt" && se.Name.Local != "rtept" && se.Name.Local != "wpt") {
			continue
		}
		var pt struct {
			Lat   float64  `xml:"lat,attr"`
			Lon   float64  `xml:"lon,attr"`
			Ele   *float64 `xml:"ele"`
			Time  string   `xml:"time"`
			Speed *float64 `xml:"speed"`
		}
		if err := dec.DecodeElement(&pt, &se); err != nil {
			return err
		}
		if ts, ok := geo.ParseTime(pt.Time); ok { // untimed points cannot go on a timeline
			if err := sink.Point(geo.Point{TS: ts, Lat: pt.Lat, Lon: pt.Lon, Altitude: pt.Ele, Speed: pt.Speed}); err != nil {
				return err
			}
		}
	}
}

// ── CSV ──────────────────────────────────────────────────────

// importCSV detects columns by header name.
// ponytail: header auto-detection only; add a column-mapping UI if users hit odd headers.
func importCSV(r io.Reader, sink Sink) error {
	cr := csv.NewReader(bufio.NewReader(r))
	cr.FieldsPerRecord = -1
	cr.ReuseRecord = true
	header, err := cr.Read()
	if err != nil {
		return err
	}
	col := func(names ...string) int {
		for i, h := range header {
			h = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(h, "\xef\xbb\xbf")))
			for _, n := range names {
				if h == n {
					return i
				}
			}
		}
		return -1
	}
	iLat, iLon := col("lat", "latitude"), col("lon", "lng", "long", "longitude")
	iTime := col("time", "timestamp", "datetime", "date", "ts", "tst")
	if iLat < 0 || iLon < 0 || iTime < 0 {
		return errors.New("CSV needs latitude, longitude and time columns")
	}
	iAcc, iAlt, iSpd, iBrg, iBat := col("accuracy", "acc"), col("altitude", "alt", "ele", "elevation"), col("speed"), col("bearing", "course", "heading"), col("battery", "batt")
	opt := func(rec []string, i int) *float64 {
		if i < 0 || i >= len(rec) {
			return nil
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(rec[i]), 64)
		if err != nil {
			return nil
		}
		return &f
	}
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		lat, lon := opt(rec, iLat), opt(rec, iLon)
		if lat == nil || lon == nil || iTime >= len(rec) {
			continue
		}
		ts, ok := geo.ParseTime(rec[iTime])
		if !ok {
			continue
		}
		p := geo.Point{TS: ts, Lat: *lat, Lon: *lon, Accuracy: opt(rec, iAcc), Altitude: opt(rec, iAlt), Speed: opt(rec, iSpd), Bearing: opt(rec, iBrg)}
		if b := opt(rec, iBat); b != nil {
			p.Battery = geo.I(int(*b))
		}
		if err := sink.Point(p); err != nil {
			return err
		}
	}
}

// ── OwnTracks Recorder .rec ──────────────────────────────────

func importRec(r io.Reader, sink Sink) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 4<<20)
	for sc.Scan() {
		line := sc.Bytes()
		i := bytes.IndexByte(line, '{')
		if i < 0 {
			continue
		}
		pts, err := ingest.OwnTracks(line[i:])
		if err != nil {
			continue
		}
		for _, p := range pts {
			if err := sink.Point(p); err != nil {
				return err
			}
		}
	}
	return sc.Err()
}

// ── Native archive ───────────────────────────────────────────

func importNative(zr *zip.Reader, sink Sink) error {
	var m Manifest
	for _, f := range zr.File {
		if f.Name == "manifest.json" {
			if err := readJSONEntry(f, &m); err != nil {
				return err
			}
		}
	}
	if m.Format != nativeFormat || m.FormatVersion > nativeVersion {
		return fmt.Errorf("unsupported export (format %q v%d); upgrade GeoTracker to import it", m.Format, m.FormatVersion)
	}
	for _, f := range zr.File {
		switch f.Name {
		case "places.json":
			var places []Place
			if err := readJSONEntry(f, &places); err != nil {
				return err
			}
			for _, p := range places {
				if sink.Place != nil {
					if err := sink.Place(p); err != nil {
						return err
					}
				}
			}
		case "points.ndjson":
			rc, err := f.Open()
			if err != nil {
				return err
			}
			dec := json.NewDecoder(bufio.NewReaderSize(rc, 1<<20))
			for dec.More() {
				var p geo.Point
				if err := dec.Decode(&p); err != nil {
					rc.Close()
					return err
				}
				if err := sink.Point(p); err != nil {
					rc.Close()
					return err
				}
			}
			rc.Close()
		}
	}
	return nil
}

func readJSONEntry(f *zip.File, v any) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	return json.NewDecoder(rc).Decode(v)
}
