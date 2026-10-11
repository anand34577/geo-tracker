package server

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"time"

	"geotracker/internal/store"
)

// tzParam reads ?tz= (IANA name from the browser); days are split in that timezone.
func tzParam(r *http.Request) *time.Location {
	if loc, err := time.LoadLocation(r.URL.Query().Get("tz")); err == nil && r.URL.Query().Get("tz") != "" {
		return loc
	}
	return time.UTC
}

func dayOf(ts int64, loc *time.Location) string {
	return time.UnixMilli(ts).In(loc).Format(time.DateOnly)
}

// pointDays counts points per local day via 15-minute buckets: a year is ~35k rows, not millions.
func (s *Server) pointDays(r *http.Request, userID, from, to int64, loc *time.Location) (map[string]int, error) {
	buckets, err := s.db.QuarterHourCounts(r.Context(), userID, from, to)
	if err != nil {
		return nil, err
	}
	days := map[string]int{}
	for b, n := range buckets {
		days[dayOf(b*900_000, loc)] += n
	}
	return days, nil
}

// days lists the days that have data, for calendar dots and the activity heatmap.
func (s *Server) days(w http.ResponseWriter, r *http.Request, u *store.User) {
	from, to, ok := timeRange(r)
	if !ok {
		fail(w, http.StatusBadRequest, "invalid from/to")
		return
	}
	days, err := s.pointDays(r, u.ID, from, to, tzParam(r))
	if err != nil {
		internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, days)
}

type dayStat struct {
	Day      string  `json:"day"`
	Distance float64 `json:"distance"`
	MovingMs int64   `json:"moving_ms"`
	Visits   int     `json:"visits"`
	Points   int     `json:"points"`
}

type modeStat struct {
	Mode     string  `json:"mode"`
	Distance float64 `json:"distance"`
	Ms       int64   `json:"ms"`
	Trips    int     `json:"trips"`
}

type placeStat struct {
	Name    string  `json:"name"`
	City    string  `json:"city,omitempty"`
	Country string  `json:"country,omitempty"`
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
	PlaceID int64   `json:"place_id,omitempty"`
	Icon    string  `json:"icon,omitempty"`
	Visits  int     `json:"visits"`
	Ms      int64   `json:"ms"`
	Last    int64   `json:"last"` // start of the latest visit, to open it on the timeline
}

type areaStat struct {
	Name    string `json:"name"`
	Country string `json:"country,omitempty"`
	Visits  int    `json:"visits"`
	Ms      int64  `json:"ms"`
	First   int64  `json:"first"`
	Last    int64  `json:"last"`
}

func topN[T any](m map[string]*T, n int, less func(a, b *T) bool) []*T {
	out := make([]*T, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return less(out[i], out[j]) })
	if n > 0 && len(out) > n {
		out = out[:n]
	}
	return out
}

// insights summarizes a time range: totals, a per-day series, transport modes,
// top places and the countries and cities visited. ?new=1 also lists what was new
// in the range (places, cities, countries never visited before it), for the monthly recap.
func (s *Server) insights(w http.ResponseWriter, r *http.Request, u *store.User) {
	from, to, ok := timeRange(r)
	if !ok {
		fail(w, http.StatusBadRequest, "invalid from/to")
		return
	}
	ins, err := s.computeInsights(r.Context(), u.ID, from, to, tzParam(r), r.URL.Query().Get("new") == "1")
	if err != nil {
		internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ins)
}

type insightsOut struct {
	Distance    float64        `json:"distance"`
	MovingMs    int64          `json:"moving_ms"`
	Visits      int            `json:"visits"`
	Trips       int            `json:"trips"`
	Places      int            `json:"places"`
	DaysTracked int            `json:"days_tracked"`
	Daily       []*dayStat     `json:"daily"`
	Modes       []*modeStat    `json:"modes"`
	TopPlaces   []*placeStat   `json:"top_places"`
	Countries   []*areaStat    `json:"countries"`
	Cities      []*areaStat    `json:"cities"`
	LongestTrip *store.TripRow `json:"longest_trip"`
	New         *newThings     `json:"new,omitempty"`
}

// newThings: first-ever visits within the range.
type newThings struct {
	Places    []*placeStat `json:"places"`
	Cities    []*areaStat  `json:"cities"`
	Countries []*areaStat  `json:"countries"`
}

// placeKey groups visits into "places": a saved place, else a named spot, else a ~100 m cell.
func placeKey(places []store.Place, v store.VisitRow) (string, placeStat) {
	ps := placeStat{Name: v.Name, City: v.City, Country: v.Country, Lat: v.Lat, Lon: v.Lon}
	if v.CustomName != "" {
		ps.Name = v.CustomName
	}
	switch p := visitPlace(places, v); {
	case p != nil:
		ps.Name, ps.PlaceID, ps.Icon, ps.Lat, ps.Lon = p.Name, p.ID, p.Icon, p.Lat, p.Lon
		return fmt.Sprint("p", p.ID), ps
	case ps.Name != "":
		return "g" + ps.Name + "|" + v.City, ps
	default:
		return fmt.Sprintf("c%.3f,%.3f", v.Lat, v.Lon), ps
	}
}

func (s *Server) computeInsights(ctx context.Context, userID, from, to int64, loc *time.Location, withNew bool) (*insightsOut, error) {
	visits, trips, err := s.db.Timeline(ctx, userID, from, to)
	if err != nil {
		return nil, err
	}
	places, err := s.db.Places(ctx, userID)
	if err != nil {
		return nil, err
	}
	buckets, err := s.db.QuarterHourCounts(ctx, userID, from, to)
	if err != nil {
		return nil, err
	}
	pdays := map[string]int{}
	for b, n := range buckets {
		pdays[dayOf(b*900_000, loc)] += n
	}

	daily := map[string]*dayStat{}
	day := func(d string) *dayStat {
		if daily[d] == nil {
			daily[d] = &dayStat{Day: d}
		}
		return daily[d]
	}
	for d, n := range pdays {
		day(d).Points = n
	}
	modes := map[string]*modeStat{}
	var distance float64
	var moving int64
	var longest *store.TripRow
	for i, t := range trips {
		d := day(dayOf(t.Start, loc))
		d.Distance += t.Distance
		d.MovingMs += t.End - t.Start
		distance += t.Distance
		moving += t.End - t.Start
		m := modes[t.Mode]
		if m == nil {
			m = &modeStat{Mode: t.Mode}
			modes[t.Mode] = m
		}
		m.Distance += t.Distance
		m.Ms += t.End - t.Start
		m.Trips++
		if longest == nil || t.Distance > longest.Distance {
			longest = &trips[i]
		}
	}

	top := map[string]*placeStat{}
	countries := map[string]*areaStat{}
	cities := map[string]*areaStat{}
	area := func(m map[string]*areaStat, k, name, country string, v store.VisitRow, ms int64) {
		a := m[k]
		if a == nil {
			a = &areaStat{Name: name, Country: country, First: v.Start}
			m[k] = a
		}
		a.Visits++
		a.Ms += ms
		a.Last = max(a.Last, v.Start)
	}
	for _, v := range visits {
		start, end := max(v.Start, from), min(v.End, to)
		ms := max(end-start, 0)
		day(dayOf(v.Start, loc)).Visits++
		key, ps := placeKey(places, v)
		if top[key] == nil {
			top[key] = &ps
		}
		top[key].Visits++
		top[key].Ms += ms
		top[key].Last = max(top[key].Last, v.Start)
		if v.Country != "" {
			area(countries, v.Country, v.Country, "", v, ms)
		}
		if v.City != "" {
			area(cities, v.City+"|"+v.Country, v.City, v.Country, v, ms)
		}
	}

	byTime := func(a, b *areaStat) bool { return a.Ms > b.Ms }
	out := &insightsOut{
		Distance: distance, MovingMs: moving, Visits: len(visits), Trips: len(trips), Places: len(top), DaysTracked: len(pdays),
		Daily:       topN(daily, 0, func(a, b *dayStat) bool { return a.Day < b.Day }),
		Modes:       topN(modes, 0, func(a, b *modeStat) bool { return a.Distance > b.Distance }),
		TopPlaces:   topN(top, 12, func(a, b *placeStat) bool { return a.Ms > b.Ms }),
		Countries:   topN(countries, 0, byTime),
		Cities:      topN(cities, 30, byTime),
		LongestTrip: longest,
	}
	if withNew {
		// ponytail: reads all earlier visits; fine for a monthly recap (~10 visits a day).
		before, _, err := s.db.Timeline(ctx, userID, 0, from-1)
		if err != nil {
			return nil, err
		}
		seen := map[string]bool{}
		for _, v := range before {
			if v.End >= from { // still going when the range starts: not new
				continue
			}
			k, _ := placeKey(places, v)
			seen[k], seen["city:"+v.City+"|"+v.Country], seen["country:"+v.Country] = true, true, true
		}
		n := &newThings{Places: []*placeStat{}, Cities: []*areaStat{}, Countries: []*areaStat{}}
		for k, p := range top {
			if !seen[k] && p.Name != "" && p.Ms >= 15*60_000 { // skip unnamed spots and drive-bys
				n.Places = append(n.Places, p)
			}
		}
		sort.Slice(n.Places, func(i, j int) bool { return n.Places[i].Ms > n.Places[j].Ms })
		n.Places = n.Places[:min(len(n.Places), 12)]
		for k, c := range cities {
			if !seen["city:"+k] {
				n.Cities = append(n.Cities, c)
			}
		}
		for k, c := range countries {
			if !seen["country:"+k] {
				n.Countries = append(n.Countries, c)
			}
		}
		sort.Slice(n.Cities, func(i, j int) bool { return n.Cities[i].First < n.Cities[j].First })
		sort.Slice(n.Countries, func(i, j int) bool { return n.Countries[i].First < n.Countries[j].First })
		out.New = n
	}
	return out, nil
}
