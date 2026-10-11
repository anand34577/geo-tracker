package server

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"geotracker/internal/geo"
	"geotracker/internal/store"
)

// ── Timeline corrections: rename, re-place, delete, merge ────

// patchVisit changes the user's correction of one visit. Fields left out keep their value;
// place_id 0 = automatic, -1 = "not one of my places".
func (s *Server) patchVisit(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		Start   int64   `json:"start"`
		Name    *string `json:"name"`
		PlaceID *int64  `json:"place_id"`
		Hidden  *bool   `json:"hidden"`
		MergeTo *int64  `json:"merge_to"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Start <= 0 {
		fail(w, http.StatusBadRequest, "missing visit start")
		return
	}
	e, err := s.db.VisitEditAt(r.Context(), u.ID, req.Start)
	if err != nil {
		internal(w, r, err)
		return
	}
	if req.Name != nil {
		if e.Name = strings.TrimSpace(*req.Name); len(e.Name) > 80 {
			fail(w, http.StatusBadRequest, "names can be up to 80 characters")
			return
		}
	}
	if req.PlaceID != nil {
		e.PlaceID, e.NoPlace = max(*req.PlaceID, 0), *req.PlaceID < 0
		if e.PlaceID > 0 {
			if _, err := s.db.PlaceByID(r.Context(), u.ID, e.PlaceID); err != nil {
				fail(w, http.StatusBadRequest, "unknown place")
				return
			}
		}
	}
	if req.Hidden != nil {
		e.Hidden = *req.Hidden
	}
	if req.MergeTo != nil {
		if *req.MergeTo != 0 && *req.MergeTo <= req.Start {
			fail(w, http.StatusBadRequest, "a merge must end after the visit starts")
			return
		}
		e.MergeTo = *req.MergeTo
	}
	if err := s.db.SaveVisitEdit(r.Context(), u.ID, e); err != nil {
		internal(w, r, err)
		return
	}
	// Places' visit counts and insights read the corrected timeline too.
	s.hub.Publish(u.ID, "timeline", map[string]any{"from": req.Start})
	writeJSON(w, http.StatusOK, e)
}

// ── "When was I last at …?" ──────────────────────────────────

type placeHit struct {
	Name      string  `json:"name"`
	Detail    string  `json:"detail,omitempty"` // city, or address
	PlaceID   int64   `json:"place_id,omitempty"`
	Icon      string  `json:"icon,omitempty"`
	Lat       float64 `json:"lat"`
	Lon       float64 `json:"lon"`
	Visits    int     `json:"visits"`
	TotalMs   int64   `json:"total_ms"`
	First     int64   `json:"first"`
	LastStart int64   `json:"last_start"`
	LastEnd   int64   `json:"last_end"`
}

// questionWords strip a natural question down to the place: "when was I last at the office?" → "office".
var questionWords = []string{"when was i last at", "when was i last in", "when did i last visit", "when did i last go to",
	"when was i at", "when did i visit", "last time at", "last time in", "last visit to", "how often at", "visits to"}

func searchNeedle(q string) string {
	q = strings.Trim(strings.ToLower(strings.TrimSpace(q)), "?!. ")
	for _, w := range questionWords {
		if strings.HasPrefix(q, w) {
			q = strings.TrimSpace(q[len(w):])
			break
		}
	}
	return strings.TrimPrefix(q, "the ")
}

// searchVisits finds places in the user's history by name, address or city, with when they were
// last (and first) there. ponytail: scans the whole history per query; ~4k visits a year keeps it instant.
func (s *Server) searchVisits(w http.ResponseWriter, r *http.Request, u *store.User) {
	needle := searchNeedle(r.URL.Query().Get("q"))
	if len([]rune(needle)) < 2 {
		writeJSON(w, http.StatusOK, []placeHit{})
		return
	}
	visits, _, err := s.db.Timeline(r.Context(), u.ID, 0, math.MaxInt64)
	if err != nil {
		internal(w, r, err)
		return
	}
	places, err := s.db.Places(r.Context(), u.ID)
	if err != nil {
		internal(w, r, err)
		return
	}
	hits := map[string]*placeHit{}
	for _, v := range visits {
		key, ps := placeKey(places, v)
		hay := strings.ToLower(ps.Name + " " + v.Address + " " + v.City)
		if !strings.Contains(hay, needle) {
			continue
		}
		h := hits[key]
		if h == nil {
			h = &placeHit{Name: ps.Name, Detail: v.City, PlaceID: ps.PlaceID, Icon: ps.Icon, Lat: ps.Lat, Lon: ps.Lon, First: v.Start}
			if h.Name == "" {
				h.Name = v.Address
			}
			hits[key] = h
		}
		h.Visits++
		h.TotalMs += v.End - v.Start
		if v.Start > h.LastStart {
			h.LastStart, h.LastEnd = v.Start, v.End
		}
	}
	out := make([]*placeHit, 0, len(hits))
	for _, h := range hits {
		if h.Name != "" {
			out = append(out, h)
		}
	}
	// Saved places first, then the most recent.
	sort.Slice(out, func(i, j int) bool {
		if (out[i].PlaceID != 0) != (out[j].PlaceID != 0) {
			return out[i].PlaceID != 0
		}
		return out[i].LastStart > out[j].LastStart
	})
	writeJSON(w, http.StatusOK, out[:min(len(out), 8)])
}

// ── Phone health: battery history and the times a phone went quiet ──

type quietSpell struct {
	From       int64  `json:"from"`
	To         int64  `json:"to"` // 0 = still quiet
	Reason     string `json:"reason"`
	BattBefore *int   `json:"batt_before"`
	BattAfter  *int   `json:"batt_after"`
	MovedM     int    `json:"moved_m"` // distance between the fixes either side
}

const quietAfter = 2 * time.Hour

// quietReason explains a silence from the fixes on either side of it:
// a flat battery, a phone lying still (most apps only report movement), or the phone moved
// without reporting (app stopped, no signal, location off).
func quietReason(before *store.DeviceFix, after *store.DeviceFix) (string, int) {
	if before.Battery != nil && *before.Battery <= 5 {
		return "battery", 0
	}
	if after == nil {
		return "silent", 0
	}
	moved := int(geo.Distance(before.Lat, before.Lon, after.Lat, after.Lon))
	if after.Battery != nil && before.Battery != nil && *before.Battery <= 15 && *after.Battery >= *before.Battery+20 {
		return "battery", moved // ran low, came back charged: it most likely died
	}
	if moved < 300 {
		return "stationary", moved
	}
	return "offline", moved
}

// deviceHealth: battery over the last ?days= (default 14, max 90) and every spell of 2 h+
// without a fix, with the likely reason.
func (s *Server) deviceHealth(w http.ResponseWriter, r *http.Request, u *store.User) {
	id := pathID(r)
	devices, err := s.db.Devices(r.Context(), u.ID)
	if err != nil {
		internal(w, r, err)
		return
	}
	var dev *store.Device
	for i := range devices {
		if devices[i].ID == id {
			dev = &devices[i]
		}
	}
	if dev == nil {
		fail(w, http.StatusNotFound, "device not found")
		return
	}
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days <= 0 || days > 90 {
		days = 14
	}
	now := time.Now().UnixMilli()
	from := now - int64(days)*86_400_000
	fixes, err := s.db.DeviceFixes(r.Context(), u.ID, id, from, now)
	if err != nil {
		internal(w, r, err)
		return
	}
	prev, err := s.db.LastFixBefore(r.Context(), u.ID, id, from)
	if err != nil {
		internal(w, r, err)
		return
	}
	spells, battery := healthOf(prev, fixes, now)
	var quietMs int64
	for _, q := range spells {
		if q.Reason != "stationary" {
			quietMs += cmpOr(q.To, now) - max(q.From, from)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"from":     from,
		"fixes":    len(fixes),
		"spells":   spells,
		"battery":  battery,
		"coverage": 1 - float64(quietMs)/float64(now-from), // share of the time the phone was reachable
	})
}

func cmpOr(v, def int64) int64 {
	if v == 0 {
		return def
	}
	return v
}

// healthOf finds the quiet spells in a device's fixes (newest first) and samples its battery
// every 30 minutes for the chart.
func healthOf(prev *store.DeviceFix, fixes []store.DeviceFix, now int64) ([]quietSpell, [][2]int64) {
	spells := []quietSpell{}
	battery := [][2]int64{}
	last := prev
	var bucket int64 = -1
	for i := range fixes {
		f := &fixes[i]
		if last != nil && f.TS-last.TS >= quietAfter.Milliseconds() {
			reason, moved := quietReason(last, f)
			spells = append(spells, quietSpell{From: last.TS, To: f.TS, Reason: reason, BattBefore: last.Battery, BattAfter: f.Battery, MovedM: moved})
		}
		if b := f.TS / 1_800_000; f.Battery != nil && b != bucket {
			bucket = b
			battery = append(battery, [2]int64{f.TS, int64(*f.Battery)})
		}
		last = f
	}
	if last != nil && now-last.TS >= quietAfter.Milliseconds() {
		reason, _ := quietReason(last, nil)
		spells = append(spells, quietSpell{From: last.TS, Reason: reason, BattBefore: last.Battery})
	}
	for i, j := 0, len(spells)-1; i < j; i, j = i+1, j-1 {
		spells[i], spells[j] = spells[j], spells[i]
	}
	return spells, battery
}

// ── Monthly recap notification ───────────────────────────────

// sendMonthlyRecaps sends last month's recap once, early on the 1st (server time), to everyone
// who has the "monthly_recap" event on and a channel set up.
func (s *Server) sendMonthlyRecaps(ctx context.Context) {
	t := time.Now()
	if t.Day() != 1 || t.Hour() < 8 {
		return
	}
	month := t.AddDate(0, -1, 0).Format("2006-01")
	st, err := s.db.Settings(ctx)
	if err != nil || st["recap_sent"] == month {
		return
	}
	if err := s.db.SetSettings(ctx, map[string]string{"recap_sent": month}); err != nil {
		return
	}
	users, err := s.db.NotifyUsers(ctx)
	if err != nil {
		return
	}
	start := time.Date(t.Year(), t.Month()-1, 1, 0, 0, 0, 0, t.Location())
	end := start.AddDate(0, 1, 0)
	for uid, raw := range users {
		if !parsePrefs(raw).Events["monthly_recap"] {
			continue
		}
		ins, err := s.computeInsights(ctx, uid, start.UnixMilli(), end.UnixMilli()-1, t.Location(), true)
		if err != nil {
			slog.Warn("monthly recap", "user", uid, "err", err)
			continue
		}
		if ins.DaysTracked == 0 {
			continue
		}
		title, body := recapText(start, ins, s.cfg.BaseURL)
		s.notifyUser(uid, "monthly_recap", title, body)
	}
}

func recapText(month time.Time, ins *insightsOut, baseURL string) (string, string) {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %.0f km travelled, %d places, %d days tracked.\n", month.Format("January 2006"), ins.Distance/1000, ins.Places, ins.DaysTracked)
	if len(ins.TopPlaces) > 0 {
		names := []string{}
		for _, p := range ins.TopPlaces[:min(3, len(ins.TopPlaces))] {
			if p.Name != "" {
				names = append(names, p.Name)
			}
		}
		if len(names) > 0 {
			fmt.Fprintf(&b, "Most time at: %s.\n", strings.Join(names, ", "))
		}
	}
	if ins.New != nil && len(ins.New.Places)+len(ins.New.Cities) > 0 {
		fmt.Fprintf(&b, "New this month: %d places, %d cities.\n", len(ins.New.Places), len(ins.New.Cities))
	}
	if ins.LongestTrip != nil {
		fmt.Fprintf(&b, "Longest trip: %.1f km by %s.\n", ins.LongestTrip.Distance/1000, ins.LongestTrip.Mode)
	}
	if baseURL != "" {
		fmt.Fprintf(&b, "\nSee your recap: %s/insights/recap/%s", strings.TrimRight(baseURL, "/"), month.Format("2006-01"))
	}
	return "Your " + month.Format("January") + " in places", b.String()
}
