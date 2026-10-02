package server

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/go-jose/go-jose/v4"

	"geotracker/internal/geo"
	"geotracker/internal/store"
	"geotracker/internal/timeline"
)

func newTestServer(t *testing.T) (*Server, *httptest.Server, *store.Store) {
	t.Helper()
	db, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s := New(Config{DataDir: db.Dir, Version: "test", Web: fstest.MapFS{"index.html": {Data: []byte("app")}}}, db)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(func() { ts.Close(); s.Close() })
	return s, ts, db
}

func do(t *testing.T, method, u, token, body string) (*http.Response, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, u, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return resp, out
}

func TestAppTokenAndColota(t *testing.T) {
	_, ts, db := newTestServer(t)
	ctx := context.Background()
	u, _ := db.CreateFirstAdmin(ctx, "ana@example.com", "Ana", HashPassword("correct horse"))

	// A native app signs in and gets a read/write/ingest token.
	resp, body := do(t, "POST", ts.URL+"/api/v1/auth/token", "", `{"email":"ana@example.com","password":"correct horse","device_name":"Pixel app"}`)
	if resp.StatusCode != 201 {
		t.Fatalf("app login: %d %v", resp.StatusCode, body)
	}
	appTok := body["token"].(string)
	if resp, _ := do(t, "GET", ts.URL+"/api/v1/me", appTok, ""); resp.StatusCode != 200 {
		t.Fatalf("bearer read: %d", resp.StatusCode)
	}
	if resp, _ := do(t, "POST", ts.URL+"/api/v1/places", appTok, `{"name":"Home","lat":52.52,"lon":13.4}`); resp.StatusCode != 200 {
		t.Fatalf("bearer write: %d", resp.StatusCode)
	}

	// A Colota device token can upload but never read.
	d, _ := db.CreateDevice(ctx, u.ID, "Phone", "colota", hashToken("devtoken"))
	_ = d
	if resp, _ := do(t, "GET", ts.URL+"/api/v1/points", "devtoken", ""); resp.StatusCode != 403 {
		t.Fatalf("device token read must be forbidden, got %d", resp.StatusCode)
	}
	now := time.Now().Unix()
	if resp, _ := do(t, "POST", ts.URL+"/ingest/colota", "devtoken", `{"lat":52.52,"lon":13.4,"acc":8,"vel":1.2,"batt":80,"bs":1,"tst":`+jsonNum(now)+`}`); resp.StatusCode != 200 {
		t.Fatalf("colota POST: %d", resp.StatusCode)
	}
	if resp, _ := do(t, "GET", ts.URL+"/ingest/colota?lat=52.53&lon=13.41&tst="+jsonNum(now+60)+"&token=devtoken", "", ""); resp.StatusCode != 200 {
		t.Fatalf("colota GET: %d", resp.StatusCode)
	}
	st, _ := db.PointStats(ctx, u.ID)
	if st.Count != 2 {
		t.Fatalf("points = %d, want 2", st.Count)
	}
}

func jsonNum(n int64) string { b, _ := json.Marshal(n); return string(b) }

func TestNotifyPrefsAndSettingsSecrets(t *testing.T) {
	_, ts, db := newTestServer(t)
	ctx := context.Background()
	db.CreateFirstAdmin(ctx, "ana@example.com", "Ana", HashPassword("correct horse"))
	resp, body := do(t, "POST", ts.URL+"/api/v1/auth/token", "", `{"email":"ana@example.com","password":"correct horse"}`)
	if resp.StatusCode != 201 {
		t.Fatal(body)
	}
	tok := body["token"].(string)

	resp, body = do(t, "PUT", ts.URL+"/api/v1/me/notifications", tok, `{"gotify":{"enabled":true,"url":"https://gotify.example.com","token":"abc","priority":99},"events":{"place_leave":true,"bogus":true}}`)
	if resp.StatusCode != 200 {
		t.Fatalf("put notify: %d %v", resp.StatusCode, body)
	}
	p := body["prefs"].(map[string]any)
	ev := p["events"].(map[string]any)
	if p["gotify"].(map[string]any)["priority"] != float64(10) || ev["place_leave"] != true || ev["place_arrive"] != true || ev["bogus"] != nil {
		t.Fatalf("prefs = %v", p)
	}
	if resp, _ := do(t, "PUT", ts.URL+"/api/v1/me/notifications", tok, `{"gotify":{"enabled":true,"url":"ftp://x","token":"a"}}`); resp.StatusCode != 400 {
		t.Fatalf("bad gotify url accepted: %d", resp.StatusCode)
	}

	// Secrets are masked on read and kept when the mask is sent back.
	do(t, "PUT", ts.URL+"/api/v1/admin/settings", tok, `{"smtp_host":"mail.example.com","smtp_password":"s3cret","smtp_from":"GeoTracker <gt@example.com>"}`)
	_, got := do(t, "GET", ts.URL+"/api/v1/admin/settings", tok, "")
	if got["smtp_password"] != secretMask {
		t.Fatalf("password leaked: %v", got["smtp_password"])
	}
	do(t, "PUT", ts.URL+"/api/v1/admin/settings", tok, `{"smtp_password":"********","smtp_port":"465"}`)
	all, _ := db.Settings(ctx)
	if all["smtp_password"] != "s3cret" || all["smtp_port"] != "465" {
		t.Fatalf("settings = %v", all)
	}
	if resp, _ := do(t, "PUT", ts.URL+"/api/v1/admin/settings", tok, `{"map_mbtiles":"C:/does/not/exist.mbtiles"}`); resp.StatusCode != 400 {
		t.Fatalf("missing mbtiles accepted: %d", resp.StatusCode)
	}
}

func TestOfflineMapServing(t *testing.T) {
	_, ts, db := newTestServer(t)
	ctx := context.Background()
	db.CreateFirstAdmin(ctx, "ana@example.com", "Ana", HashPassword("correct horse"))
	_, body := do(t, "POST", ts.URL+"/api/v1/auth/token", "", `{"email":"ana@example.com","password":"correct horse"}`)
	tok := body["token"].(string)

	p := filepath.Join(t.TempDir(), "india.mbtiles")
	mdb, _ := sql.Open("sqlite", p)
	mdb.Exec(`CREATE TABLE metadata (name TEXT, value TEXT); CREATE TABLE tiles (zoom_level INTEGER, tile_column INTEGER, tile_row INTEGER, tile_data BLOB);
		INSERT INTO metadata VALUES ('format','png'),('name','India');
		INSERT INTO tiles VALUES (1, 0, 0, x'89504e47');`)
	mdb.Close()
	if resp, b := do(t, "PUT", ts.URL+"/api/v1/admin/settings", tok, `{"map_mbtiles":`+string(must(json.Marshal(p)))+`,"map_default":"offline"}`); resp.StatusCode != 200 {
		t.Fatalf("set mbtiles: %d %v", resp.StatusCode, b)
	}
	_, cfg := do(t, "GET", ts.URL+"/api/v1/config", tok, "")
	maps := cfg["basemaps"].([]any)
	if last := maps[len(maps)-1].(map[string]any); last["id"] != "offline" || cfg["basemap_default"] != "offline" {
		t.Fatalf("config = %v", cfg)
	}
	_, style := do(t, "GET", ts.URL+"/api/v1/map/style/offline", tok, "")
	if style["sources"].(map[string]any)["offline"].(map[string]any)["type"] != "raster" {
		t.Fatalf("style = %v", style)
	}
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/map/tiles/1/0/1", nil) // XYZ y=1 at z1 == TMS row 0
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("tile: %v %v", err, resp)
	}
}

func must(b []byte, _ error) []byte { return b }

// fakeIdP is a minimal OpenID provider that signs real RS256 ID tokens.
// verified is the email_verified claim; nil leaves it out.
func fakeIdP(t *testing.T, email string, verified any) *httptest.Server {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	signer, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithHeader("kid", "k1"))
	var nonce string
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			json.NewEncoder(w).Encode(map[string]any{"issuer": srv.URL, "authorization_endpoint": srv.URL + "/authorize",
				"token_endpoint": srv.URL + "/token", "jwks_uri": srv.URL + "/jwks", "id_token_signing_alg_values_supported": []string{"RS256"}})
		case "/jwks":
			json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"}}})
		case "/authorize":
			nonce = r.URL.Query().Get("nonce")
			if r.URL.Query().Get("code_challenge_method") != "S256" {
				t.Error("PKCE not used")
			}
			http.Redirect(w, r, r.URL.Query().Get("redirect_uri")+"?code=c0de&state="+url.QueryEscape(r.URL.Query().Get("state")), http.StatusFound)
		case "/token":
			if r.FormValue("code_verifier") == "" {
				t.Error("no PKCE verifier")
			}
			c := map[string]any{"iss": srv.URL, "sub": "idp-user-1", "aud": "gt", "exp": time.Now().Add(time.Hour).Unix(),
				"iat": time.Now().Unix(), "nonce": nonce, "email": email, "name": "Sam"}
			if verified != nil {
				c["email_verified"] = verified
			}
			claims, _ := json.Marshal(c)
			sig, _ := signer.Sign(claims)
			w.Header().Set("Content-Type", "application/json")
			idToken, _ := sig.CompactSerialize()
			json.NewEncoder(w).Encode(map[string]any{"access_token": "at", "token_type": "Bearer", "expires_in": 3600, "id_token": idToken})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestOIDCLogin(t *testing.T) {
	_, ts, db := newTestServer(t)
	ctx := context.Background()
	db.CreateFirstAdmin(ctx, "ana@example.com", "Ana", HashPassword("correct horse"))
	idp := fakeIdP(t, "sam@example.com", true)
	db.SetSettings(ctx, map[string]string{"oidc_enabled": "true", "oidc_issuer": idp.URL, "oidc_client_id": "gt", "oidc_client_secret": "x"})

	login := func() (*http.Client, *http.Response) {
		jar, _ := cookiejar.New(nil)
		c := &http.Client{Jar: jar, CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if strings.HasPrefix(req.URL.Path, "/login") || req.URL.Path == "/timeline" {
				return http.ErrUseLastResponse // stop at our own final redirect
			}
			return nil
		}}
		resp, err := c.Get(ts.URL + "/api/v1/auth/oidc/login?next=/timeline")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return c, resp
	}

	// Unknown email and auto-registration off → friendly error, no account.
	_, resp := login()
	if loc := resp.Header.Get("Location"); !strings.Contains(loc, "/login?error=") {
		t.Fatalf("expected login error redirect, got %q", loc)
	}

	// With auto-registration the account is created and signed in.
	db.SetSettings(ctx, map[string]string{"oidc_auto_register": "true"})
	c, resp := login()
	if loc := resp.Header.Get("Location"); loc != "/timeline" {
		t.Fatalf("redirect = %q", loc)
	}
	r2, err := c.Get(ts.URL + "/api/v1/me")
	if err != nil || r2.StatusCode != 200 {
		t.Fatalf("session after SSO: %v %v", err, r2.Status)
	}
	var me store.User
	json.NewDecoder(r2.Body).Decode(&me)
	if me.Email != "sam@example.com" || me.Role != "user" || me.Name != "Sam" {
		t.Fatalf("me = %+v", me)
	}
	// SSO-only accounts have no usable password.
	if resp, _ := do(t, "POST", ts.URL+"/api/v1/auth/login", "", `{"email":"sam@example.com","password":"!"}`); resp.StatusCode != 401 {
		t.Fatalf("password login for SSO user: %d", resp.StatusCode)
	}

	// An existing account is only linked when the provider says the email is verified.
	db.LinkOIDC(ctx, me.ID, "someone-else")
	db.SetSettings(ctx, map[string]string{"oidc_issuer": fakeIdP(t, "ana@example.com", nil).URL})
	if _, resp := login(); !strings.Contains(resp.Header.Get("Location"), "error=") {
		t.Fatal("linked an existing account without email_verified")
	}
	if u, _ := db.UserByOIDC(ctx, "idp-user-1"); u != nil {
		t.Fatalf("account linked: %+v", u)
	}

	// Forged state is rejected.
	resp3, _ := c.Get(ts.URL + "/api/v1/auth/oidc/callback?code=x&state=forged")
	if !strings.Contains(resp3.Header.Get("Location"), "error=") {
		t.Fatal("forged state accepted")
	}
}

func TestInsightsAndShares(t *testing.T) {
	s, ts, db := newTestServer(t)
	ctx := context.Background()
	u, _ := db.CreateFirstAdmin(ctx, "ana@example.com", "Ana Silva", HashPassword("correct horse"))
	_, body := do(t, "POST", ts.URL+"/api/v1/auth/token", "", `{"email":"ana@example.com","password":"correct horse"}`)
	tok := body["token"].(string)

	// 30 min home, 10 min drive east, 30 min work.
	t0 := time.Now().Add(-3 * time.Hour).UnixMilli()
	var pts []geo.Point
	for i := int64(0); i <= 30; i++ {
		pts = append(pts, geo.Point{TS: t0 + i*60_000, Lat: 52.52, Lon: 13.405})
	}
	for i := int64(1); i < 20; i++ {
		pts = append(pts, geo.Point{TS: t0 + 1_800_000 + i*30_000, Lat: 52.52, Lon: 13.405 + 0.075*float64(i)/20})
	}
	for i := int64(0); i <= 30; i++ {
		pts = append(pts, geo.Point{TS: t0 + 2_400_000 + i*60_000, Lat: 52.52, Lon: 13.48})
	}
	db.InsertPoints(ctx, u.ID, 0, 0, pts)
	s.processTimelines(ctx)

	_, ins := do(t, "GET", ts.URL+"/api/v1/insights?tz=Europe/Berlin", tok, "")
	if ins["visits"] != float64(2) || ins["trips"] != float64(1) || ins["distance"].(float64) < 4500 || ins["days_tracked"].(float64) < 1 {
		t.Fatalf("insights = %v", ins)
	}
	if modes := ins["modes"].([]any); modes[0].(map[string]any)["mode"] != "drive" {
		t.Fatalf("modes = %v", modes)
	}

	// Range link with approximate precision.
	from, to := t0-1, t0+5_000_000
	resp, created := do(t, "POST", ts.URL+"/api/v1/shares", tok,
		fmt.Sprintf(`{"name":"Trip","kind":"range","from":%d,"to":%d,"precision":"approx","expires_in_hours":24}`, from, to))
	if resp.StatusCode != 201 {
		t.Fatalf("create share: %d %v", resp.StatusCode, created)
	}
	link := created["url"].(string)
	token := link[strings.LastIndex(link, "/")+1:]
	resp, pub := do(t, "GET", ts.URL+"/api/v1/public/shares/"+token, "", "")
	if resp.StatusCode != 200 || pub["owner"] != "Ana" || len(pub["points"].([]any)) != 81 {
		t.Fatalf("public share: %d %v", resp.StatusCode, pub["owner"])
	}
	first := pub["points"].([]any)[0].([]any)
	if first[1] != 52.52 || first[2] != 13.41 || first[3] != nil { // rounded, accuracy hidden
		t.Fatalf("approx point = %v", first)
	}
	// Revoked links stop working.
	id := int64(created["share"].(map[string]any)["id"].(float64))
	do(t, "DELETE", fmt.Sprintf("%s/api/v1/shares/%d", ts.URL, id), tok, "")
	if resp, _ := do(t, "GET", ts.URL+"/api/v1/public/shares/"+token, "", ""); resp.StatusCode != 404 {
		t.Fatalf("revoked link still works: %d", resp.StatusCode)
	}
}
func TestDaysInHalfHourTimezone(t *testing.T) {
	_, ts, db := newTestServer(t)
	ctx := context.Background()
	u, _ := db.CreateFirstAdmin(ctx, "ana@example.com", "Ana", HashPassword("correct horse"))
	_, body := do(t, "POST", ts.URL+"/api/v1/auth/token", "", `{"email":"ana@example.com","password":"correct horse"}`)
	// 18:40 UTC on 29 Sep is 00:10 on 30 Sep in India (UTC+5:30).
	at := time.Date(2026, 9, 29, 18, 40, 0, 0, time.UTC).UnixMilli()
	db.InsertPoints(ctx, u.ID, 0, 0, []geo.Point{{TS: at, Lat: 28.6, Lon: 77.2}})
	_, days := do(t, "GET", ts.URL+"/api/v1/days?tz=Asia/Kolkata", body["token"].(string), "")
	if days["2026-09-30"] != float64(1) || days["2026-09-29"] != nil {
		t.Fatalf("days = %v, want the point on 2026-09-30", days)
	}
}

func TestFamilySharingRules(t *testing.T) {
	_, ts, db := newTestServer(t)
	ctx := context.Background()
	ana, _ := db.CreateFirstAdmin(ctx, "ana@example.com", "Ana", HashPassword("correct horse"))
	sam, _ := db.CreateUser(ctx, "sam@example.com", "Sam", HashPassword("correct horse"), "user")
	db.CreateUser(ctx, "eve@example.com", "Eve", HashPassword("correct horse"), "user")
	tok := func(email string) string {
		_, b := do(t, "POST", ts.URL+"/api/v1/auth/token", "", `{"email":"`+email+`","password":"correct horse"}`)
		return b["token"].(string)
	}
	anaT, samT, eveT := tok("ana@example.com"), tok("sam@example.com"), tok("eve@example.com")

	// Sam has points from 3 days ago and from now.
	now := time.Now().UnixMilli()
	db.InsertPoints(ctx, sam.ID, 0, 0, []geo.Point{
		{TS: now - 3*86_400_000, Lat: 52.51234, Lon: 13.41234},
		{TS: now - 60_000, Lat: 52.52345, Lon: 13.40567},
	})

	_, g := do(t, "POST", ts.URL+"/api/v1/groups", anaT, `{"name":"Family"}`)
	gid := fmt.Sprint(int64(g["id"].(float64)))
	if resp, _ := do(t, "POST", ts.URL+"/api/v1/groups/"+gid+"/members", anaT, `{"email":"sam@example.com"}`); resp.StatusCode != 204 {
		t.Fatalf("invite: %d", resp.StatusCode)
	}
	// Invited but not accepted: nothing is shared.
	if resp, _ := do(t, "GET", ts.URL+fmt.Sprintf("/api/v1/points?user=%d", sam.ID), anaT, ""); resp.StatusCode != 403 {
		t.Fatalf("history visible before consent: %d", resp.StatusCode)
	}
	// Sam accepts: live + 1 day of history, approximate.
	if resp, _ := do(t, "PUT", ts.URL+"/api/v1/groups/"+gid+"/me", samT, `{"share_live":true,"share_history_days":1,"precision":"approx"}`); resp.StatusCode != 204 {
		t.Fatalf("accept: %d", resp.StatusCode)
	}
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/family", nil)
	req.Header.Set("Authorization", "Bearer "+anaT)
	resp, _ := http.DefaultClient.Do(req)
	var fam []familyPerson
	json.NewDecoder(resp.Body).Decode(&fam)
	resp.Body.Close()
	if len(fam) != 1 || !fam[0].Live || fam[0].Point == nil || fam[0].Point.Lat != 52.52 || fam[0].Point.Lon != 13.41 {
		t.Fatalf("family = %+v", fam)
	}
	_, pts := do(t, "GET", ts.URL+fmt.Sprintf("/api/v1/points?user=%d", sam.ID), anaT, "")
	rows := pts["points"].([]any)
	if len(rows) != 1 || rows[0].([]any)[1] != 52.52 { // only the last day, rounded
		t.Fatalf("points = %v", pts)
	}
	// Strangers see nothing; approximate sharers can't be geofenced.
	if resp, _ := do(t, "GET", ts.URL+fmt.Sprintf("/api/v1/points?user=%d", sam.ID), eveT, ""); resp.StatusCode != 403 {
		t.Fatalf("stranger access: %d", resp.StatusCode)
	}
	place := &store.Place{Name: "School", Lat: 52.5234, Lon: 13.4056, Radius: 100}
	db.CreatePlace(ctx, ana.ID, place)
	alert := fmt.Sprintf(`{"subject_id":%d,"place_id":%d,"on_arrive":true}`, sam.ID, place.ID)
	if resp, _ := do(t, "POST", ts.URL+"/api/v1/alerts", anaT, alert); resp.StatusCode != 403 {
		t.Fatalf("alert on approximate sharer: %d", resp.StatusCode)
	}
	do(t, "PUT", ts.URL+"/api/v1/groups/"+gid+"/me", samT, `{"share_live":true,"share_history_days":1,"precision":"exact"}`)
	if resp, _ := do(t, "POST", ts.URL+"/api/v1/alerts", anaT, alert); resp.StatusCode != 201 {
		t.Fatalf("alert: %d", resp.StatusCode)
	}
	// Ghost mode hides everything.
	until := time.Now().Add(time.Hour).UnixMilli()
	do(t, "PUT", ts.URL+"/api/v1/groups/"+gid+"/me", samT, fmt.Sprintf(`{"share_live":true,"share_history_days":1,"precision":"exact","paused_until":%d}`, until))
	if resp, _ := do(t, "GET", ts.URL+fmt.Sprintf("/api/v1/points?user=%d", sam.ID), anaT, ""); resp.StatusCode != 403 {
		t.Fatalf("paused member still visible: %d", resp.StatusCode)
	}
	// Leaving removes access and the alert.
	do(t, "DELETE", ts.URL+fmt.Sprintf("/api/v1/groups/%s/members/%d", gid, sam.ID), samT, "")
	if as, _ := db.AlertsByWatcher(ctx, ana.ID); len(as) != 0 {
		t.Fatalf("alerts survived leaving: %v", as)
	}
}

func TestSessionsAuditRetentionTripMode(t *testing.T) {
	s, ts, db := newTestServer(t)
	ctx := context.Background()
	u, _ := db.CreateFirstAdmin(ctx, "ana@example.com", "Ana", HashPassword("correct horse"))
	do(t, "POST", ts.URL+"/api/v1/auth/login", "", `{"email":"ana@example.com","password":"wrong password"}`)
	_, b := do(t, "POST", ts.URL+"/api/v1/auth/token", "", `{"email":"ana@example.com","password":"correct horse"}`)
	tok := b["token"].(string)
	entries, _ := db.AuditLog(ctx, 0, 10)
	if len(entries) < 2 || entries[0].Action != "login" || entries[1].Action != "login.failed" {
		t.Fatalf("audit = %+v", entries)
	}

	// Retention keeps visits before the cutoff.
	old := time.Now().AddDate(0, 0, -40).UnixMilli()
	var pts []geo.Point
	for i := int64(0); i <= 30; i++ {
		pts = append(pts, geo.Point{TS: old + i*60_000, Lat: 52.52, Lon: 13.405})
	}
	pts = append(pts, geo.Point{TS: time.Now().Add(-time.Hour).UnixMilli(), Lat: 52.53, Lon: 13.41})
	db.InsertPoints(ctx, u.ID, 0, 0, pts)
	s.processTimelines(ctx)
	if resp, _ := do(t, "PUT", ts.URL+"/api/v1/me/retention", tok, `{"days":30}`); resp.StatusCode != 200 {
		t.Fatalf("retention: %d", resp.StatusCode)
	}
	if n, err := db.ApplyRetention(ctx); err != nil || n != 31 {
		t.Fatalf("retention removed %d (%v), want 31", n, err)
	}
	s.processTimelines(ctx)
	if vs, _, _ := db.Timeline(ctx, u.ID, 0, time.Now().UnixMilli()); len(vs) != 1 {
		t.Fatalf("old visit lost after retention: %d visits", len(vs))
	}

	// Trip mode correction survives recomputation (matched by start time).
	db.ReplaceTimeline(ctx, u.ID, 0, nil, []timeline.Trip{{Start: 1_000_000, End: 2_000_000, Distance: 900, Mode: "cycle"}})
	do(t, "PUT", ts.URL+"/api/v1/trips/mode", tok, `{"start":1030000,"mode":"walk"}`)
	if _, trips, _ := db.Timeline(ctx, u.ID, 0, 3_000_000); len(trips) != 1 || trips[0].Mode != "walk" || !trips[0].Corrected {
		t.Fatalf("trips = %+v", trips)
	}
}

func TestPrivacyZonesAndOwnerDeletion(t *testing.T) {
	_, ts, db := newTestServer(t)
	ctx := context.Background()
	ana, _ := db.CreateFirstAdmin(ctx, "ana@example.com", "Ana", HashPassword("correct horse"))
	sam, _ := db.CreateUser(ctx, "sam@example.com", "Sam", HashPassword("correct horse"), "user")
	tok := func(email string) string {
		_, b := do(t, "POST", ts.URL+"/api/v1/auth/token", "", `{"email":"`+email+`","password":"correct horse"}`)
		return b["token"].(string)
	}
	anaT, samT := tok("ana@example.com"), tok("sam@example.com")
	_, g := do(t, "POST", ts.URL+"/api/v1/groups", samT, `{"name":"Family"}`)
	gid := fmt.Sprint(int64(g["id"].(float64)))
	do(t, "POST", ts.URL+"/api/v1/groups/"+gid+"/members", samT, `{"email":"ana@example.com"}`)
	do(t, "PUT", ts.URL+"/api/v1/groups/"+gid+"/me", anaT, `{"share_live":true,"share_history_days":-1,"precision":"exact"}`)
	do(t, "PUT", ts.URL+"/api/v1/groups/"+gid+"/me", samT, `{"share_live":true,"share_history_days":-1,"precision":"exact"}`)

	// Sam is at home (a private place) now, and was in town an hour ago.
	now := time.Now().UnixMilli()
	db.InsertPoints(ctx, sam.ID, 0, 0, []geo.Point{{TS: now - 3_600_000, Lat: 52.50, Lon: 13.40}, {TS: now - 60_000, Lat: 52.53, Lon: 13.45}})
	if resp, _ := do(t, "POST", ts.URL+"/api/v1/places", samT, `{"name":"Home","lat":52.53,"lon":13.45,"radius":100,"private":true}`); resp.StatusCode != 200 {
		t.Fatalf("private place: %d", resp.StatusCode)
	}
	_, pts := do(t, "GET", ts.URL+fmt.Sprintf("/api/v1/points?user=%d", sam.ID), anaT, "")
	if rows := pts["points"].([]any); len(rows) != 1 || rows[0].([]any)[1] != 52.50 {
		t.Fatalf("points inside a privacy zone leaked: %v", rows)
	}
	_, own := do(t, "GET", ts.URL+"/api/v1/points", samT, "")
	if len(own["points"].([]any)) != 2 {
		t.Fatalf("owner must still see everything: %v", own)
	}
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/family", nil)
	req.Header.Set("Authorization", "Bearer "+anaT)
	resp, _ := http.DefaultClient.Do(req)
	var fam []familyPerson
	json.NewDecoder(resp.Body).Decode(&fam)
	resp.Body.Close()
	if len(fam) != 1 || fam[0].Point != nil {
		t.Fatalf("live position inside a privacy zone leaked: %+v", fam)
	}
	_, created := do(t, "POST", ts.URL+"/api/v1/shares", samT, `{"name":"Now","kind":"live","expires_in_hours":1}`)
	link := created["url"].(string)
	token := link[strings.LastIndex(link, "/")+1:]
	_, pub := do(t, "GET", ts.URL+"/api/v1/public/shares/"+token, "", "")
	if pub["latest"] != nil || len(pub["points"].([]any)) != 1 {
		t.Fatalf("share link leaked a privacy zone: %v", pub)
	}
	do(t, "GET", ts.URL+"/api/v1/public/shares/"+token+"?poll=1", "", "")
	if shares, _ := db.Shares(ctx, sam.ID); shares[0].Views != 1 {
		t.Fatalf("live refreshes counted as views: %d", shares[0].Views)
	}

	// Deleting the group's owner hands the group to the remaining member.
	if resp, _ := do(t, "DELETE", ts.URL+fmt.Sprintf("/api/v1/admin/users/%d", sam.ID), anaT, ""); resp.StatusCode != 204 {
		t.Fatalf("delete user: %d", resp.StatusCode)
	}
	if m, err := db.Membership(ctx, int64(g["id"].(float64)), ana.ID); err != nil || m.Role != "owner" {
		t.Fatalf("group left without an owner: %+v %v", m, err)
	}
}

func TestBackgroundExportAndAccountRules(t *testing.T) {
	s, ts, db := newTestServer(t)
	ctx := context.Background()
	u, _ := db.CreateFirstAdmin(ctx, "ana@example.com", "Ana", HashPassword("correct horse"))
	_, b := do(t, "POST", ts.URL+"/api/v1/auth/token", "", `{"email":"ana@example.com","password":"correct horse"}`)
	tok := b["token"].(string)
	db.InsertPoints(ctx, u.ID, 0, 0, []geo.Point{{TS: time.Now().Add(-time.Hour).UnixMilli(), Lat: 52.5, Lon: 13.4}})

	if resp, _ := do(t, "POST", ts.URL+"/api/v1/exports", tok, `{"format":"nope"}`); resp.StatusCode != 400 {
		t.Fatalf("bad format: %d", resp.StatusCode)
	}
	resp, created := do(t, "POST", ts.URL+"/api/v1/exports", tok, `{"format":"gpx"}`)
	if resp.StatusCode != 202 {
		t.Fatalf("queue export: %d", resp.StatusCode)
	}
	id := int64(created["id"].(float64))
	dl := fmt.Sprintf("%s/api/v1/exports/%d/download", ts.URL, id)
	if resp, _ := do(t, "GET", dl, tok, ""); resp.StatusCode != 404 {
		t.Fatalf("download before it is built: %d", resp.StatusCode)
	}
	e, _ := db.NextExport(ctx)
	if e == nil || e.ID != id {
		t.Fatalf("next export = %+v", e)
	}
	s.runExport(ctx, e)
	req, _ := http.NewRequest("GET", dl, nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	r, err := http.DefaultClient.Do(req)
	if err != nil || r.StatusCode != 200 {
		t.Fatalf("download: %v %v", err, r)
	}
	var body strings.Builder
	io.Copy(&body, r.Body)
	r.Body.Close()
	if !strings.Contains(body.String(), "<gpx") || !strings.Contains(body.String(), "52.5") {
		t.Fatalf("export content: %.200s", body.String())
	}
	if resp, _ := do(t, "DELETE", fmt.Sprintf("%s/api/v1/exports/%d", ts.URL, id), tok, ""); resp.StatusCode != 204 {
		t.Fatalf("delete export: %d", resp.StatusCode)
	}
	s.cleanExports(ctx)
	if _, err := os.Stat(s.exportPath(id, "gpx")); err == nil {
		t.Fatal("export file survived deletion")
	}

	// Changing the email needs the current password; other edits don't.
	if resp, _ := do(t, "PATCH", ts.URL+"/api/v1/me", tok, `{"email":"new@example.com"}`); resp.StatusCode != 403 {
		t.Fatalf("email change without password: %d", resp.StatusCode)
	}
	if resp, _ := do(t, "PATCH", ts.URL+"/api/v1/me", tok, `{"email":"new@example.com","password":"correct horse"}`); resp.StatusCode != 200 {
		t.Fatalf("email change with password: %d", resp.StatusCode)
	}
	if resp, _ := do(t, "PATCH", ts.URL+"/api/v1/me", tok, `{"name":"Ana S","email":"new@example.com"}`); resp.StatusCode != 200 {
		t.Fatalf("name change: %d", resp.StatusCode)
	}

	// Notification tokens never come back in clear text, and the mask keeps the stored one.
	do(t, "PUT", ts.URL+"/api/v1/me/notifications", tok, `{"telegram":{"enabled":true,"token":"123:SECRET","chat_id":"9"}}`)
	_, n := do(t, "GET", ts.URL+"/api/v1/me/notifications", tok, "")
	if n["prefs"].(map[string]any)["telegram"].(map[string]any)["token"] != secretMask {
		t.Fatalf("telegram token leaked: %v", n["prefs"])
	}
	do(t, "PUT", ts.URL+"/api/v1/me/notifications", tok, `{"telegram":{"enabled":true,"token":"`+secretMask+`","chat_id":"9"}}`)
	if raw, _ := db.UserNotify(ctx, u.ID); !strings.Contains(string(raw), "123:SECRET") {
		t.Fatalf("stored token lost: %s", raw)
	}
}
