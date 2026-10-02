package server

import (
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
}

type areaStat struct {
	Name    string `json:"name"`
	Country string `json:"country,omitempty"`
	Visits  int    `json:"visits"`
	Ms      int64  `json:"ms"`
	First   int64  `json:"first"`
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
// top places and the countries and cities visited.
func (s *Server) insights(w http.ResponseWriter, r *http.Request, u *store.User) {
	from, to, ok := timeRange(r)
	if !ok {
		fail(w, http.StatusBadRequest, "invalid from/to")
		return
	}
	loc := tzParam(r)
	visits, trips, err := s.db.Timeline(r.Context(), u.ID, from, to)
	if err != nil {
		internal(w, r, err)
		return
	}
	places, err := s.db.Places(r.Context(), u.ID)
	if err != nil {
		internal(w, r, err)
		return
	}
	pdays, err := s.pointDays(r, u.ID, from, to, loc)
	if err != nil {
		internal(w, r, err)
		return
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
	for _, v := range visits {
		start, end := max(v.Start, from), min(v.End, to)
		ms := max(end-start, 0)
		day(dayOf(v.Start, loc)).Visits++
		var key string
		ps := placeStat{Name: v.Name, City: v.City, Country: v.Country, Lat: v.Lat, Lon: v.Lon}
		if p := matchPlace(places, v.Lat, v.Lon); p != nil {
			key, ps.Name, ps.PlaceID, ps.Icon, ps.Lat, ps.Lon = fmt.Sprint("p", p.ID), p.Name, p.ID, p.Icon, p.Lat, p.Lon
		} else if v.Name != "" {
			key = "g" + v.Name + "|" + v.City
		} else {
			key = fmt.Sprintf("c%.3f,%.3f", v.Lat, v.Lon)
		}
		if top[key] == nil {
			top[key] = &ps
		}
		top[key].Visits++
		top[key].Ms += ms
		if v.Country != "" {
			a := countries[v.Country]
			if a == nil {
				a = &areaStat{Name: v.Country, First: v.Start}
				countries[v.Country] = a
			}
			a.Visits++
			a.Ms += ms
		}
		if v.City != "" {
			k := v.City + "|" + v.Country
			a := cities[k]
			if a == nil {
				a = &areaStat{Name: v.City, Country: v.Country, First: v.Start}
				cities[k] = a
			}
			a.Visits++
			a.Ms += ms
		}
	}

	series := topN(daily, 0, func(a, b *dayStat) bool { return a.Day < b.Day })
	byTime := func(a, b *areaStat) bool { return a.Ms > b.Ms }
	writeJSON(w, http.StatusOK, map[string]any{
		"distance":     distance,
		"moving_ms":    moving,
		"visits":       len(visits),
		"trips":        len(trips),
		"places":       len(top),
		"days_tracked": len(pdays),
		"daily":        series,
		"modes":        topN(modes, 0, func(a, b *modeStat) bool { return a.Distance > b.Distance }),
		"top_places":   topN(top, 12, func(a, b *placeStat) bool { return a.Ms > b.Ms }),
		"countries":    topN(countries, 0, byTime),
		"cities":       topN(cities, 30, byTime),
		"longest_trip": longest,
	})
}
