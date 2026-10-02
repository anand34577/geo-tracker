// Package timeline turns a time-ordered stream of raw points into visits (stays) and trips.
//
// It is a streaming state machine: memory use is bounded by the size of one stay or trip,
// never by the size of the history, so it can rebuild years of data on a Raspberry Pi.
package timeline

import (
	"math"
	"sort"

	"geotracker/internal/geo"
)

type Params struct {
	StayRadius  float64 // meters a stay may spread over
	StayMinMs   int64   // minimum duration of a stay
	MaxAccuracy float64 // fixes less accurate than this (meters) are ignored
}

var Default = Params{StayRadius: 100, StayMinMs: 5 * 60 * 1000, MaxAccuracy: 100}

type Visit struct {
	Start  int64   `json:"start"`
	End    int64   `json:"end"`
	Lat    float64 `json:"lat"`
	Lon    float64 `json:"lon"`
	Radius float64 `json:"radius"`
	Points int     `json:"points"`
}

type Trip struct {
	Start      int64   `json:"start"`
	End        int64   `json:"end"`
	Distance   float64 `json:"distance"` // meters
	Mode       string  `json:"mode"`     // walk|cycle|drive|train|flight|unknown
	Confidence float64 `json:"confidence"`
}

const (
	maxSpeed   = 350.0 // m/s; faster than an airliner means a GPS glitch
	stillSpeed = 1.5   // m/s; slower than this across a gap means we probably stayed put
	walkSpeed  = 1.4   // m/s; used to estimate when we left after a silent gap
	minTripM   = 30.0  // shorter "trips" are GPS jitter between two stays
	mergeGapMs = 30 * 60 * 1000
)

type Builder struct {
	P      Params
	Visits []Visit
	Trips  []Trip

	cluster        []geo.Point // candidate stay
	sumLat, sumLon float64
	trip           []geo.Point // points since the last stay
	last           geo.Point
	hasLast        bool
	rejected       int
}

func New(p Params) *Builder { return &Builder{P: p} }

// Add feeds the next point. Points must arrive in ascending time order.
func (b *Builder) Add(p geo.Point) {
	if p.Accuracy != nil && *p.Accuracy > b.P.MaxAccuracy {
		return
	}
	if b.hasLast {
		dt := float64(p.TS-b.last.TS) / 1000
		if dt <= 0 {
			return
		}
		// Reject spikes, but after 3 in a row assume the previous point was the bad one.
		if geo.Distance(b.last.Lat, b.last.Lon, p.Lat, p.Lon)/dt > maxSpeed && b.rejected < 3 {
			b.rejected++
			return
		}
	}
	b.rejected, b.last, b.hasLast = 0, p, true

	if len(b.cluster) == 0 {
		b.push(p)
		return
	}
	cLat, cLon := b.sumLat/float64(len(b.cluster)), b.sumLon/float64(len(b.cluster))
	d := geo.Distance(cLat, cLon, p.Lat, p.Lon)
	if d <= b.P.StayRadius {
		b.push(p)
		return
	}

	// p left the cluster: decide whether the cluster was a stay.
	last := b.cluster[len(b.cluster)-1]
	end := last.TS
	if gap := p.TS - last.TS; gap >= b.P.StayMinMs && d/(float64(gap)/1000) < stillSpeed {
		// Phones go quiet when stationary: assume we stayed until we had to leave to reach p.
		end = max(last.TS, p.TS-int64(d/walkSpeed*1000))
	}
	if end-b.cluster[0].TS >= b.P.StayMinMs {
		b.emitStay(end)
	} else {
		b.trip = append(b.trip, b.cluster...)
	}
	b.cluster, b.sumLat, b.sumLon = b.cluster[:0], 0, 0
	b.push(p)
}

// Finish flushes the open stay or trip at the end of the stream.
func (b *Builder) Finish() {
	if len(b.cluster) == 0 {
		return
	}
	if last := b.cluster[len(b.cluster)-1]; last.TS-b.cluster[0].TS >= b.P.StayMinMs {
		b.emitStay(last.TS)
		return
	}
	b.trip = append(b.trip, b.cluster...)
	b.closeTrip()
}

func (b *Builder) push(p geo.Point) {
	b.cluster = append(b.cluster, p)
	b.sumLat += p.Lat
	b.sumLon += p.Lon
}

func (b *Builder) emitStay(end int64) {
	lats := make([]float64, len(b.cluster))
	lons := make([]float64, len(b.cluster))
	for i, p := range b.cluster {
		lats[i], lons[i] = p.Lat, p.Lon
	}
	v := Visit{Start: b.cluster[0].TS, End: end, Lat: median(lats), Lon: median(lons), Points: len(b.cluster)}
	dists := make([]float64, len(b.cluster))
	for i, p := range b.cluster {
		dists[i] = geo.Distance(v.Lat, v.Lon, p.Lat, p.Lon)
	}
	v.Radius = math.Max(20, percentile(dists, 0.8))

	// Drifting briefly out of range and back is still the same visit.
	if n := len(b.Visits); n > 0 {
		prev := &b.Visits[n-1]
		if v.Start-prev.End <= mergeGapMs && geo.Distance(prev.Lat, prev.Lon, v.Lat, v.Lon) <= b.P.StayRadius && b.tripStaysNear(prev) {
			prev.End, prev.Points, prev.Radius = v.End, prev.Points+v.Points, math.Max(prev.Radius, v.Radius)
			b.anchor(*prev)
			return
		}
	}
	b.trip = append(b.trip, b.cluster[0])
	b.closeTrip()
	b.Visits = append(b.Visits, v)
	b.anchor(v)
}

func (b *Builder) tripStaysNear(v *Visit) bool {
	for _, p := range b.trip {
		if geo.Distance(v.Lat, v.Lon, p.Lat, p.Lon) > 2*b.P.StayRadius {
			return false
		}
	}
	return true
}

// anchor starts the next trip at the visit's center, when the visit ended.
func (b *Builder) anchor(v Visit) {
	b.trip = append(b.trip[:0], geo.Point{TS: v.End, Lat: v.Lat, Lon: v.Lon})
}

func (b *Builder) closeTrip() {
	pts := b.trip
	b.trip = nil
	if len(pts) < 2 || pts[len(pts)-1].TS <= pts[0].TS {
		return
	}
	var dist float64
	var speeds []float64
	for i := 1; i < len(pts); i++ {
		d := geo.Distance(pts[i-1].Lat, pts[i-1].Lon, pts[i].Lat, pts[i].Lon)
		dist += d
		if dt := float64(pts[i].TS-pts[i-1].TS) / 1000; dt >= 5 {
			speeds = append(speeds, d/dt)
		}
	}
	if dist < minTripM {
		return
	}
	t := Trip{Start: pts[0].TS, End: pts[len(pts)-1].TS, Distance: dist}
	t.Mode, t.Confidence = classify(speeds, dist, t.End-t.Start)
	b.Trips = append(b.Trips, t)
}

// classify guesses the transport mode from speed statistics.
// ponytail: transparent heuristics, no ML; users can correct the mode later.
func classify(speeds []float64, dist float64, durMs int64) (string, float64) {
	if durMs <= 0 {
		return "unknown", 0
	}
	avg := dist / (float64(durMs) / 1000)
	if len(speeds) == 0 {
		speeds = []float64{avg}
	}
	med, p95 := percentile(speeds, 0.5), percentile(speeds, 0.95)
	conf := 0.5 + 0.4*math.Min(1, float64(len(speeds))/20)
	switch {
	case avg > 70 || p95 > 90: // > 250 km/h average or sustained > 320 km/h
		return "flight", conf
	case p95 < 2.5: // < 9 km/h
		return "walk", conf
	case med < 7 && p95 < 11: // < 25 km/h typical, < 40 km/h peak
		return "cycle", conf
	case med > 38 && dist > 50_000: // > 137 km/h sustained over 50 km
		return "train", conf * 0.8
	default:
		return "drive", conf
	}
}

func median(xs []float64) float64 { return percentile(xs, 0.5) }

// percentile sorts xs in place.
func percentile(xs []float64, q float64) float64 {
	sort.Float64s(xs)
	return xs[int(float64(len(xs)-1)*q)]
}
