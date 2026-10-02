package timeline

import (
	"math"
	"testing"

	"geotracker/internal/geo"
)

const minute = int64(60_000)

var t0 = int64(1_700_000_000_000)

// dwell produces points every step around (lat, lon) with ~10 m jitter.
func dwell(start, dur, step int64, lat, lon float64) []geo.Point {
	var pts []geo.Point
	for ts, i := start, 0; ts <= start+dur; ts, i = ts+step, i+1 {
		j := 0.0001 * math.Sin(float64(i)) // ~11 m
		pts = append(pts, geo.Point{TS: ts, Lat: lat + j, Lon: lon - j, Accuracy: geo.F(10)})
	}
	return pts
}

// move produces a straight line from a to b.
func move(start, dur, step int64, lat1, lon1, lat2, lon2 float64) []geo.Point {
	var pts []geo.Point
	for ts := start + step; ts < start+dur; ts += step {
		f := float64(ts-start) / float64(dur)
		pts = append(pts, geo.Point{TS: ts, Lat: lat1 + (lat2-lat1)*f, Lon: lon1 + (lon2-lon1)*f})
	}
	return pts
}

func run(pts []geo.Point) *Builder {
	b := New(Default)
	for _, p := range pts {
		b.Add(p)
	}
	b.Finish()
	return b
}

func TestHomeDriveWork(t *testing.T) {
	home := [2]float64{52.5200, 13.4050}
	work := [2]float64{52.5200, 13.4800} // ~5 km east
	var pts []geo.Point
	pts = append(pts, dwell(t0, 30*minute, minute, home[0], home[1])...)
	pts = append(pts, move(t0+30*minute, 10*minute, 30_000, home[0], home[1], work[0], work[1])...)
	pts = append(pts, dwell(t0+40*minute, 60*minute, minute, work[0], work[1])...)

	b := run(pts)
	if len(b.Visits) != 2 {
		t.Fatalf("visits = %d, want 2: %+v", len(b.Visits), b.Visits)
	}
	if len(b.Trips) != 1 {
		t.Fatalf("trips = %d, want 1: %+v", len(b.Trips), b.Trips)
	}
	tr := b.Trips[0]
	if tr.Mode != "drive" {
		t.Errorf("mode = %s, want drive", tr.Mode)
	}
	if tr.Distance < 4500 || tr.Distance > 5800 {
		t.Errorf("distance = %.0f, want ~5 km", tr.Distance)
	}
	if d := geo.Distance(b.Visits[1].Lat, b.Visits[1].Lon, work[0], work[1]); d > 30 {
		t.Errorf("work center off by %.0f m", d)
	}
}

func TestSilentNightAtHome(t *testing.T) {
	// One fix at 22:00, phone sleeps, next fix at 07:30 already 300 m away walking.
	var pts []geo.Point
	pts = append(pts, geo.Point{TS: t0, Lat: 52.52, Lon: 13.405})
	pts = append(pts, move(t0+570*minute, 10*minute, 30_000, 52.5227, 13.405, 52.53, 13.405)...)

	b := run(pts)
	if len(b.Visits) != 1 {
		t.Fatalf("visits = %d, want 1: %+v", len(b.Visits), b.Visits)
	}
	if h := float64(b.Visits[0].End-b.Visits[0].Start) / float64(60*minute); h < 9 {
		t.Errorf("night stay = %.1f h, want > 9 h", h)
	}
}

func TestBriefDriftMerges(t *testing.T) {
	// 40 min home, 3 bad fixes ~150 m away, 40 min home again → one visit, no trip.
	var pts []geo.Point
	pts = append(pts, dwell(t0, 40*minute, minute, 52.52, 13.405)...)
	for i := int64(1); i <= 3; i++ {
		pts = append(pts, geo.Point{TS: t0 + 40*minute + i*20_000, Lat: 52.5213, Lon: 13.405})
	}
	pts = append(pts, dwell(t0+42*minute, 40*minute, minute, 52.52, 13.405)...)

	b := run(pts)
	if len(b.Visits) != 1 || len(b.Trips) != 0 {
		t.Fatalf("visits=%d trips=%d, want 1/0", len(b.Visits), len(b.Trips))
	}
}

func TestSpikeIgnored(t *testing.T) {
	pts := dwell(t0, 30*minute, minute, 52.52, 13.405)
	pts[10].Lat = 48.0 // 500 km jump for one fix
	b := run(pts)
	if len(b.Visits) != 1 || len(b.Trips) != 0 {
		t.Fatalf("visits=%d trips=%d, want 1/0", len(b.Visits), len(b.Trips))
	}
}

func TestFlightAcrossGap(t *testing.T) {
	var pts []geo.Point
	pts = append(pts, dwell(t0, 60*minute, 5*minute, 52.36, 13.50)...)                     // BER
	pts = append(pts, geo.Point{TS: t0 + 61*minute, Lat: 52.37, Lon: 13.52})               // taxi out
	pts = append(pts, geo.Point{TS: t0 + 62*minute, Lat: 52.38, Lon: 13.54})               // take-off
	pts = append(pts, dwell(t0+62*minute+120*minute, 60*minute, 5*minute, 41.30, 2.08)...) // BCN, 1500 km later
	b := run(pts)
	if len(b.Trips) != 1 || b.Trips[0].Mode != "flight" {
		t.Fatalf("trips = %+v, want one flight", b.Trips)
	}
}
