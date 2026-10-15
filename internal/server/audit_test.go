package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"strconv"
	"time"

	"geotracker/internal/geo"
	"geotracker/internal/store"
	"geotracker/internal/timeline"
)

func appToken(t *testing.T, ts *httptest.Server, email string) string {
	t.Helper()
	_, b := do(t, "POST", ts.URL+"/api/v1/auth/token", "", `{"email":"`+email+`","password":"correct horse"}`)
	tok, _ := b["token"].(string)
	if tok == "" {
		t.Fatalf("no token for %s: %v", email, b)
	}
	return tok
}

// A visit inside a privacy zone must not be named to family just because the person left it minutes ago.
func TestFamilyPlaceHidesPrivateZoneVisit(t *testing.T) {
	_, ts, db := newTestServer(t)
	ctx := context.Background()
	db.CreateFirstAdmin(ctx, "ana@example.com", "Ana", HashPassword("correct horse"))
	sam, _ := db.CreateUser(ctx, "sam@example.com", "Sam", HashPassword("correct horse"), "user")
	anaT, samT := appToken(t, ts, "ana@example.com"), appToken(t, ts, "sam@example.com")
	_, g := do(t, "POST", ts.URL+"/api/v1/groups", samT, `{"name":"Family"}`)
	gid := fmt.Sprint(int64(g["id"].(float64)))
	do(t, "POST", ts.URL+"/api/v1/groups/"+gid+"/members", samT, `{"email":"ana@example.com"}`)
	do(t, "PUT", ts.URL+"/api/v1/groups/"+gid+"/me", anaT, `{"share_live":true,"share_history_days":-1,"precision":"exact"}`)
	do(t, "PUT", ts.URL+"/api/v1/groups/"+gid+"/me", samT, `{"share_live":true,"share_history_days":-1,"precision":"exact"}`)
	do(t, "POST", ts.URL+"/api/v1/places", samT, `{"name":"Home","lat":52.53,"lon":13.45,"radius":100,"private":true}`)

	now := time.Now().UnixMilli()
	db.InsertPoints(ctx, sam.ID, 0, 0, []geo.Point{{TS: now - 60_000, Lat: 52.50, Lon: 13.40}})
	if err := db.ReplaceTimeline(ctx, sam.ID, 0, []timeline.Visit{{Start: now - 3_600_000, End: now - 300_000, Lat: 52.53, Lon: 13.45, Radius: 30, Points: 10}}, nil); err != nil {
		t.Fatal(err)
	}
	vs, _, _ := db.Timeline(ctx, sam.ID, 0, now)
	gc, _ := db.InsertGeocode(ctx, store.Geocode{Lat: 52.53, Lon: 13.45, Provider: "x", Name: "12 Secret Street", City: "Berlin"})
	db.SetVisitGeocode(ctx, vs[0].ID, gc)

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/family", nil)
	req.Header.Set("Authorization", "Bearer "+anaT)
	resp, _ := http.DefaultClient.Do(req)
	var fam []familyPerson
	json.NewDecoder(resp.Body).Decode(&fam)
	resp.Body.Close()
	if len(fam) != 1 || fam[0].Point == nil {
		t.Fatalf("setup wrong: %+v", fam)
	}
	if fam[0].Place != "" {
		t.Errorf("family sees the private-zone visit: place=%q", fam[0].Place)
	}
}

// The native archive must keep the privacy flag, and importing it twice must not duplicate places.
func TestNativeRoundTripKeepsPrivateFlag(t *testing.T) {
	s, _, db := newTestServer(t)
	ctx := context.Background()
	u, _ := db.CreateFirstAdmin(ctx, "ana@example.com", "Ana", HashPassword("correct horse"))
	db.CreatePlace(ctx, u.ID, &store.Place{Name: "Home", Icon: "home", Lat: 52.53, Lon: 13.45, Radius: 100, Private: true})
	db.InsertPoints(ctx, u.ID, 0, 0, []geo.Point{{TS: time.Now().UnixMilli() - 1000, Lat: 52.5, Lon: 13.4}})

	var buf bytes.Buffer
	if err := s.writeExport(ctx, &buf, u, "native", 0, 1<<62); err != nil {
		t.Fatal(err)
	}
	pl, _ := db.Places(ctx, u.ID)
	db.DeletePlace(ctx, u.ID, pl[0].ID)
	for i := 0; i < 2; i++ {
		id, _ := db.CreateImport(ctx, u.ID, "x.zip")
		os.MkdirAll(s.cfg.DataDir+"/imports", 0o750)
		os.WriteFile(s.importPath(id), buf.Bytes(), 0o600)
		imp, _ := db.NextImport(ctx)
		s.runImport(ctx, imp)
	}
	places, _ := db.Places(ctx, u.ID)
	if len(places) != 1 {
		t.Fatalf("want 1 place after importing twice, got %d", len(places))
	}
	if !places[0].Private {
		t.Errorf("restored place lost its privacy flag: %+v", places[0])
	}
}

// Two phones must not make arrive/leave flap.
func TestTwoDevicesDoNotFlapAutomations(t *testing.T) {
	_, ts, db := newTestServer(t)
	ctx := context.Background()
	u, _ := db.CreateFirstAdmin(ctx, "ana@example.com", "Ana", HashPassword("correct horse"))
	app := appToken(t, ts, "ana@example.com")

	var hits atomic.Int32
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer hook.Close()
	pl := &store.Place{Name: "Home", Icon: "home", Lat: 52.53, Lon: 13.45, Radius: 100}
	db.CreatePlace(ctx, u.ID, pl)
	body := fmt.Sprintf(`{"name":"hook","place_id":%d,"on_arrive":true,"on_leave":true,"enabled":true,"cooldown_min":1,"actions":[{"type":"webhook","url":%q}]}`, pl.ID, hook.URL)
	if resp, out := do(t, "POST", ts.URL+"/api/v1/automations", app, body); resp.StatusCode != 201 {
		t.Fatalf("automation: %d %v", resp.StatusCode, out)
	}
	mk := func(name string) string {
		_, o := do(t, "POST", ts.URL+"/api/v1/devices", app, fmt.Sprintf(`{"name":%q,"client":"other"}`, name))
		return o["token"].(string)
	}
	phone, tablet := mk("phone"), mk("tablet")
	now := time.Now().UnixMilli()
	send := func(tok string, lat, lon float64, i int) {
		j, _ := json.Marshal([]geo.Point{{TS: now - 600_000 + int64(i)*1000, Lat: lat, Lon: lon, Accuracy: geo.F(10)}})
		req, _ := http.NewRequest("POST", ts.URL+"/ingest/json", bytes.NewReader(j))
		req.Header.Set("Authorization", "Bearer "+tok)
		resp, _ := http.DefaultClient.Do(req)
		resp.Body.Close()
	}
	for i := 0; i < 8; i += 2 {
		send(phone, 52.53, 13.45, i)
		send(tablet, 52.58, 13.45, i+1)
	}
	time.Sleep(1500 * time.Millisecond)
	if n := hits.Load(); n > 0 {
		t.Errorf("%d webhooks fired for a user who never left home", n)
	}
}

// A manual rebuild must keep history older than the oldest retained point.
func TestRebuildKeepsHistoryBeyondRetention(t *testing.T) {
	s, ts, db := newTestServer(t)
	ctx := context.Background()
	u, _ := db.CreateFirstAdmin(ctx, "ana@example.com", "Ana", HashPassword("correct horse"))
	app := appToken(t, ts, "ana@example.com")
	day := int64(86_400_000)
	now := time.Now().UnixMilli()
	var pts []geo.Point
	for i := int64(0); i < 12; i++ {
		pts = append(pts, geo.Point{TS: now - 90*day + i*300_000, Lat: 52.5, Lon: 13.4}, geo.Point{TS: now - day + i*300_000, Lat: 52.6, Lon: 13.5})
	}
	db.InsertPoints(ctx, u.ID, 0, 0, pts)
	s.processTimelines(ctx)
	db.SetRetention(ctx, u.ID, 30)
	db.ApplyRetention(ctx)
	v1, _, _ := db.Timeline(ctx, u.ID, 0, now)
	do(t, "POST", ts.URL+"/api/v1/timeline/rebuild", app, "")
	s.processTimelines(ctx)
	v2, _, _ := db.Timeline(ctx, u.ID, 0, now)
	if len(v2) < len(v1) || len(v1) != 2 {
		t.Errorf("visits before rebuild=%d after=%d", len(v1), len(v2))
	}
}

func TestIngestWithoutTokenIsRefusedBeforeReading(t *testing.T) {
	_, ts, _ := newTestServer(t)
	for _, path := range []string{"/ingest/owntracks", "/ingest/overland", "/ingest/json", "/ingest/gpslogger"} {
		req, _ := http.NewRequest("POST", ts.URL+path, strings.NewReader(`{"_type":"location"}`))
		resp, _ := http.DefaultClient.Do(req)
		resp.Body.Close()
		if resp.StatusCode != 401 {
			t.Errorf("%s without token: %d", path, resp.StatusCode)
		}
	}
}

func TestEmailTargetsLimitedToOwnAddress(t *testing.T) {
	_, ts, db := newTestServer(t)
	ctx := context.Background()
	db.CreateFirstAdmin(ctx, "ana@example.com", "Ana", HashPassword("correct horse"))
	db.CreateUser(ctx, "sam@example.com", "Sam", HashPassword("correct horse"), "user")
	samT := appToken(t, ts, "sam@example.com")
	resp, _ := do(t, "PUT", ts.URL+"/api/v1/me/notifications", samT, `{"email":{"enabled":true,"to":"victim@example.org"}}`)
	if resp.StatusCode != 400 {
		t.Errorf("user set a foreign notification address: %d", resp.StatusCode)
	}
	resp, _ = do(t, "PUT", ts.URL+"/api/v1/me/notifications", samT, `{"email":{"enabled":true,"to":"sam@example.com"}}`)
	if resp.StatusCode != 200 {
		t.Errorf("own address refused: %d", resp.StatusCode)
	}
}

func TestStrangerCannotLockOutOwner(t *testing.T) {
	_, _, db := newTestServer(t)
	s := New(Config{}, db)
	r1, _ := http.NewRequest("POST", "/", nil)
	r1.RemoteAddr = "10.0.0.1:1"
	r2, _ := http.NewRequest("POST", "/", nil)
	r2.RemoteAddr = "10.0.0.2:1"
	for i := 0; i < 12; i++ {
		s.loginAllowed(r1, "ana@example.com")
	}
	if !s.loginAllowed(r2, "ana@example.com") {
		t.Error("an attacker's failed attempts locked the owner out from another address")
	}
}

func TestForwardedProtoNeedsTrustedProxy(t *testing.T) {
	_, _, db := newTestServer(t)
	s := New(Config{}, db)
	r, _ := http.NewRequest("GET", "/", nil)
	r.RemoteAddr = "203.0.113.5:1"
	r.Header.Set("X-Forwarded-Proto", "https")
	if s.isHTTPS(r) {
		t.Error("X-Forwarded-Proto from an untrusted client was believed")
	}
}

func TestSafeNextStaysOnSite(t *testing.T) {
	for _, bad := range []string{"//evil.com", `/\evil.com`, "/\t/evil.com", "/\n/evil.com", "https://evil.com", "evil.com", ""} {
		if got := safeNext(bad); got != "/" {
			t.Errorf("safeNext(%q) = %q, want /", bad, got)
		}
	}
	for _, ok := range []string{"/", "/map", "/timeline?day=2026-01-02"} {
		if got := safeNext(ok); got != ok {
			t.Errorf("safeNext(%q) = %q, want unchanged", ok, got)
		}
	}
}

func TestLimiterEvictionKeepsHotKeys(t *testing.T) {
	l := newLimiter()
	for range 5 {
		l.allow("victim", 5, time.Hour)
	}
	for i := range maxLimiterKeys + 500 {
		l.allow("junk"+strconv.Itoa(i), 5, time.Hour)
	}
	if l.allow("victim", 5, time.Hour) {
		t.Fatal("flooding junk keys reset a limited key")
	}
	if l.allow("brand-new", 5, time.Hour) {
		t.Fatal("a full limiter must refuse unseen keys")
	}
}
