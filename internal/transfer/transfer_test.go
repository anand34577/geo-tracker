package transfer

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"geotracker/internal/geo"
)

var sample = []geo.Point{
	{TS: 1_700_000_000_000, Lat: 52.52, Lon: 13.405, Accuracy: geo.F(8), Altitude: geo.F(34), Battery: geo.I(90)},
	{TS: 1_700_000_060_000, Lat: 52.521, Lon: 13.406, Speed: geo.F(1.2)},
	{TS: 1_700_010_000_000, Lat: 52.53, Lon: 13.41}, // > 1 h gap → new GPX segment
}

func iter(fn func(geo.Point) error) error {
	for _, p := range sample {
		if err := fn(p); err != nil {
			return err
		}
	}
	return nil
}

// importBytes writes data to a temp file named name and imports it.
func importBytes(t *testing.T, name string, data []byte) ([]geo.Point, []Place, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	var pts []geo.Point
	var places []Place
	format, err := Import(p, name, Sink{
		Point: func(pt geo.Point) error { pts = append(pts, pt); return nil },
		Place: func(pl Place) error { places = append(places, pl); return nil },
	})
	if err != nil {
		t.Fatalf("import %s: %v", name, err)
	}
	return pts, places, format
}

func TestRoundTrips(t *testing.T) {
	cases := []struct {
		name  string
		write func(io.Writer) error
	}{
		{"x.gpx", func(w io.Writer) error { return WriteGPX(w, "t", iter) }},
		{"x.geojson", func(w io.Writer) error { return WriteGeoJSON(w, iter) }},
		{"x.csv", func(w io.Writer) error { return WriteCSV(w, iter) }},
		{"x.zip", func(w io.Writer) error {
			return WriteNative(w, "test", "a@b.c", iter, []Place{{Name: "Home", Lat: 1, Lon: 2, Radius: 50}}, nil, nil)
		}},
	}
	for _, c := range cases {
		var buf bytes.Buffer
		if err := c.write(&buf); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		pts, places, _ := importBytes(t, c.name, buf.Bytes())
		if len(pts) != len(sample) {
			t.Fatalf("%s: %d points, want %d", c.name, len(pts), len(sample))
		}
		for i := range pts {
			if pts[i].TS != sample[i].TS || pts[i].Lat != sample[i].Lat || pts[i].Lon != sample[i].Lon {
				t.Errorf("%s[%d] = %+v, want %+v", c.name, i, pts[i], sample[i])
			}
		}
		if c.name == "x.zip" && (len(places) != 1 || *pts[0].Battery != 90) {
			t.Errorf("native: places=%v first=%+v", places, pts[0])
		}
	}
}

func TestGoogleFormats(t *testing.T) {
	cases := map[string]struct {
		json   string
		points int
		format string
	}{
		"records": {`{"locations":[
			{"latitudeE7":525200000,"longitudeE7":134050000,"accuracy":10,"timestamp":"2023-11-14T22:13:20Z"},
			{"latitudeE7":525210000,"longitudeE7":134060000,"timestampMs":"1700000060000"}]}`, 2, "google-records"},
		"semantic": {`{"timelineObjects":[
			{"placeVisit":{"location":{"latitudeE7":525200000,"longitudeE7":134050000,"name":"Home"},
			  "duration":{"startTimestamp":"2023-11-14T20:00:00Z","endTimestamp":"2023-11-14T22:00:00Z"}}},
			{"activitySegment":{"startLocation":{"latitudeE7":525200000,"longitudeE7":134050000},
			  "endLocation":{"latitudeE7":525300000,"longitudeE7":134100000},
			  "duration":{"startTimestamp":"2023-11-14T22:00:00Z","endTimestamp":"2023-11-14T22:30:00Z"},
			  "simplifiedRawPath":{"points":[{"latE7":525250000,"lngE7":134070000,"timestamp":"2023-11-14T22:15:00Z"}]}}}]}`, 5, "google-semantic"},
		"android": {`{"semanticSegments":[
			{"startTime":"2024-01-01T10:00:00.000+01:00","endTime":"2024-01-01T11:00:00.000+01:00",
			 "visit":{"topCandidate":{"placeLocation":{"latLng":"52.5200000°, 13.4050000°"}}}},
			{"startTime":"2024-01-01T11:00:00.000+01:00","endTime":"2024-01-01T12:00:00.000+01:00",
			 "timelinePath":[{"point":"52.5210000°, 13.4060000°","time":"2024-01-01T11:10:00.000+01:00"}]}],
			"rawSignals":[{"position":{"LatLng":"52.5220000°, 13.4070000°","accuracyMeters":5,"timestamp":"2024-01-01T11:20:00.000+01:00"}},
			              {"wifiScan":{}}],
			"userLocationProfile":{"frequentPlaces":[]}}`, 4, "google-timeline"},
		"ios": {`[{"startTime":"2024-01-01T10:00:00.000+01:00","endTime":"2024-01-01T11:00:00.000+01:00",
			"visit":{"topCandidate":{"placeLocation":"geo:52.520000,13.405000"}}},
			{"startTime":"2024-01-01T11:00:00.000+01:00","endTime":"2024-01-01T12:00:00.000+01:00",
			 "timelinePath":[{"point":"geo:52.521000,13.406000","durationMinutesOffsetFromStartTime":"10"}]}]`, 3, "google-timeline"},
	}
	for name, c := range cases {
		pts, _, format := importBytes(t, name+".json", []byte(c.json))
		if len(pts) != c.points || format != c.format {
			t.Errorf("%s: %d points (%s), want %d (%s): %+v", name, len(pts), format, c.points, c.format, pts)
		}
		for _, p := range pts {
			if !p.Valid() {
				t.Errorf("%s: invalid point %+v", name, p)
			}
		}
	}
}

func TestUnknownJSONRejected(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.json")
	os.WriteFile(p, []byte(`{"hello":"world"}`), 0o600)
	if _, err := Import(p, "x.json", Sink{Point: func(geo.Point) error { return nil }}); err == nil {
		t.Fatal("want error")
	}
}
