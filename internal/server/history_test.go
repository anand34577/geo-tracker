package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"geotracker/internal/store"
	"geotracker/internal/timeline"
)

func TestVisitEditsSearchAndRecap(t *testing.T) {
	_, ts, db := newTestServer(t)
	ctx := context.Background()
	u, _ := db.CreateFirstAdmin(ctx, "ana@example.com", "Ana", HashPassword("correct horse"))
	_, b := do(t, "POST", ts.URL+"/api/v1/auth/token", "", `{"email":"ana@example.com","password":"correct horse"}`)
	tok := b["token"].(string)

	// Last month: home only. This month: home, a café, a bakery, back home.
	const h = 3_600_000
	t0 := time.Now().Add(-48 * time.Hour).UnixMilli()
	db.ReplaceTimeline(ctx, u.ID, 0, []timeline.Visit{
		{Start: t0 - 40*24*h, End: t0 - 40*24*h + h, Lat: 52.52, Lon: 13.40},
		{Start: t0, End: t0 + h, Lat: 52.52, Lon: 13.40},
		{Start: t0 + 2*h, End: t0 + 3*h, Lat: 52.50, Lon: 13.30},
		{Start: t0 + 3*h + 600_000, End: t0 + 4*h, Lat: 52.50, Lon: 13.301},
		{Start: t0 + 5*h, End: t0 + 6*h, Lat: 52.52, Lon: 13.40},
	}, []timeline.Trip{
		{Start: t0 + h, End: t0 + 2*h, Distance: 7000, Mode: "drive"},
		{Start: t0 + 3*h, End: t0 + 3*h + 600_000, Distance: 50, Mode: "walk"},
		{Start: t0 + 4*h, End: t0 + 5*h, Distance: 7000, Mode: "drive"},
	})
	do(t, "POST", ts.URL+"/api/v1/places", tok, `{"name":"Home","lat":52.52,"lon":13.40}`)
	_, gym := do(t, "POST", ts.URL+"/api/v1/places", tok, `{"name":"Gym","lat":40,"lon":10}`)
	gymID := int64(gym["id"].(float64))

	patch := func(body string) {
		t.Helper()
		if resp, out := do(t, "PATCH", ts.URL+"/api/v1/visits", tok, body); resp.StatusCode != 200 {
			t.Fatalf("patch %s: %d %v", body, resp.StatusCode, out)
		}
	}
	// Rename the café, merge it with the bakery 10 min later (the walk between disappears),
	// and say the first visit was actually at the gym. Starts are off by a minute: edits match
	// recomputed visits by time.
	patch(fmt.Sprintf(`{"start":%d,"name":"Café Kranzler"}`, t0+2*h+60_000))
	patch(fmt.Sprintf(`{"start":%d,"merge_to":%d}`, t0+2*h, t0+4*h))
	patch(fmt.Sprintf(`{"start":%d,"place_id":%d}`, t0, gymID))
	visits, trips, _ := db.Timeline(ctx, u.ID, t0-1, t0+7*h)
	if len(visits) != 3 || len(trips) != 2 || visits[1].CustomName != "Café Kranzler" || visits[1].End != t0+4*h || visits[0].PinnedPlace != gymID {
		t.Fatalf("after edits: %d visits %+v, %d trips", len(visits), visits, len(trips))
	}
	// Deleting hides it; undoing brings it back. Unknown places are refused.
	patch(fmt.Sprintf(`{"start":%d,"hidden":true}`, t0+5*h))
	if vs, _, _ := db.Timeline(ctx, u.ID, t0-1, t0+7*h); len(vs) != 2 {
		t.Fatalf("hidden visit still shown: %d", len(vs))
	}
	patch(fmt.Sprintf(`{"start":%d,"hidden":false}`, t0+5*h))
	if resp, _ := do(t, "PATCH", ts.URL+"/api/v1/visits", tok, fmt.Sprintf(`{"start":%d,"place_id":999}`, t0)); resp.StatusCode != 400 {
		t.Fatalf("foreign place accepted: %d", resp.StatusCode)
	}

	// The API labels the visit with the chosen place.
	var tl struct{ Visits []visitOut }
	r2, _ := httpGet(t, fmt.Sprintf("%s/api/v1/timeline?from=%d&to=%d", ts.URL, t0-1, t0+7*h), tok, &tl)
	if r2 != 200 || tl.Visits[0].PlaceName != "Gym" || tl.Visits[2].PlaceName != "Home" {
		t.Fatalf("timeline places: %+v", tl.Visits)
	}

	// "When was I last at the café?" finds the renamed visit.
	var hits []placeHit
	httpGet(t, ts.URL+"/api/v1/visits/search?q=When+was+I+last+at+the+caf%C3%A9%3F", tok, &hits)
	if len(hits) != 1 || hits[0].Name != "Café Kranzler" || hits[0].LastStart != t0+2*h {
		t.Fatalf("search = %+v", hits)
	}

	// Recap: the café and the gym (the visit re-pinned there) are new this month, home is not.
	var ins insightsOut
	httpGet(t, fmt.Sprintf("%s/api/v1/insights?from=%d&to=%d&new=1", ts.URL, t0-24*h, t0+7*h), tok, &ins)
	if ins.New == nil || len(ins.New.Places) != 2 || ins.New.Places[0].Name+ins.New.Places[1].Name != "Café KranzlerGym" {
		t.Fatalf("new places = %+v", ins.New)
	}
	if title, body := recapText(time.Now(), &ins, "https://t.example"); title == "" || len(body) < 20 {
		t.Fatalf("recap text: %q %q", title, body)
	}
}

func TestQuietSpells(t *testing.T) {
	b := func(n int) *int { return &n }
	const h = 3_600_000
	fixes := []store.DeviceFix{
		{TS: 0, Lat: 52.5, Lon: 13.4, Battery: b(80)},
		{TS: 3 * h, Lat: 52.5, Lon: 13.4001, Battery: b(78)}, // lay still on the desk
		{TS: 4 * h, Lat: 52.5, Lon: 13.4, Battery: b(3)},
		{TS: 9 * h, Lat: 52.6, Lon: 13.4, Battery: b(60)}, // died, recharged elsewhere
		{TS: 10 * h, Lat: 52.6, Lon: 13.4, Battery: b(59)},
		{TS: 13 * h, Lat: 52.7, Lon: 13.4, Battery: b(55)}, // moved without reporting
	}
	spells, battery := healthOf(nil, fixes, 20*h)
	want := []string{"silent", "offline", "battery", "stationary"} // newest first; still quiet since 13 h
	if len(spells) != len(want) {
		t.Fatalf("spells = %+v", spells)
	}
	for i, w := range want {
		if spells[i].Reason != w {
			t.Fatalf("spell %d = %s, want %s (%+v)", i, spells[i].Reason, w, spells)
		}
	}
	if spells[0].To != 0 || len(battery) != 6 {
		t.Fatalf("ongoing spell %+v, battery samples %d", spells[0], len(battery))
	}
}

// httpGet decodes a bearer-authenticated GET into v.
func httpGet(t *testing.T, u, token string, v any) (int, error) {
	t.Helper()
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp.StatusCode, json.NewDecoder(resp.Body).Decode(v)
}
