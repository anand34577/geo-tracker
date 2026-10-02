package ingest

import (
	"math"
	"net/url"
	"testing"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestOwnTracks(t *testing.T) {
	pts, err := OwnTracks([]byte(`{"_type":"location","lat":52.52,"lon":13.405,"tst":1700000000,"acc":12,"vel":36,"batt":80,"tid":"ph"}`))
	if err != nil || len(pts) != 1 {
		t.Fatalf("pts=%v err=%v", pts, err)
	}
	p := pts[0]
	if p.TS != 1700000000000 || !near(*p.Speed, 10) || *p.Battery != 80 || *p.Accuracy != 12 {
		t.Errorf("got %+v", p)
	}
	if pts, _ := OwnTracks([]byte(`{"_type":"transition","event":"enter"}`)); len(pts) != 0 {
		t.Error("transition must not produce points")
	}
}

func TestColota(t *testing.T) {
	pts, err := Colota([]byte(`{"lat":48.135124,"lon":11.581981,"acc":12,"alt":519,"vel":2.5,"batt":85,"bs":2,"tst":1704067200,"bear":180.5}`))
	if err != nil || len(pts) != 1 {
		t.Fatalf("pts=%v err=%v", pts, err)
	}
	if p := pts[0]; p.TS != 1704067200000 || *p.Speed != 2.5 || *p.Battery != 85 || *p.Bearing != 180.5 {
		t.Errorf("got %+v", p)
	}
	if pts, _ := Colota([]byte(`[{"lat":1,"lon":2,"tst":1704067200},{"lat":1,"lon":2,"tst":1704067260}]`)); len(pts) != 2 {
		t.Errorf("batch: %d points", len(pts))
	}
}

func TestOverland(t *testing.T) {
	pts, err := Overland([]byte(`{"locations":[{"type":"Feature","geometry":{"type":"Point","coordinates":[13.405,52.52]},
		"properties":{"timestamp":"2023-11-14T22:13:20Z","speed":-1,"horizontal_accuracy":5,"battery_level":0.5}}]}`))
	if err != nil || len(pts) != 1 {
		t.Fatalf("pts=%v err=%v", pts, err)
	}
	p := pts[0]
	if p.TS != 1700000000000 || p.Lat != 52.52 || p.Speed != nil || *p.Battery != 50 {
		t.Errorf("got %+v", p)
	}
}

func TestParamsOsmAnd(t *testing.T) {
	v, _ := url.ParseQuery("id=tok&lat=52.52&lon=13.405&timestamp=1700000000&speed=10&batt=77.0")
	pts, err := Params(v, true)
	if err != nil || len(pts) != 1 {
		t.Fatalf("pts=%v err=%v", pts, err)
	}
	if !near(*pts[0].Speed, 5.14444) || *pts[0].Battery != 77 || pts[0].TS != 1700000000000 {
		t.Errorf("got %+v", pts[0])
	}
	if _, err := Params(url.Values{"lat": {"1"}}, false); err == nil {
		t.Error("want error for missing lon")
	}
}

func TestTraccarJSON(t *testing.T) {
	pts, dev, err := Traccar([]byte(`{"device_id":"tok","location":{"timestamp":"2023-11-14T22:13:20.000Z",
		"coords":{"latitude":52.52,"longitude":13.405,"accuracy":4,"speed":3,"heading":-1},"battery":{"level":0.9}}}`))
	if err != nil || dev != "tok" || len(pts) != 1 {
		t.Fatalf("pts=%v dev=%q err=%v", pts, dev, err)
	}
	if pts[0].Bearing != nil || *pts[0].Battery != 90 {
		t.Errorf("got %+v", pts[0])
	}
}
