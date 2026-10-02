package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"geotracker/internal/store"
)

type client struct {
	t    *testing.T
	base string
	http *http.Client
}

func (c *client) do(method, path string, body any, out any) int {
	c.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.base+path, r)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode
}

func TestEndToEnd(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := New(Config{DataDir: db.Dir, Version: "test", Web: fstest.MapFS{"index.html": {Data: []byte("<html>app</html>")}}}, db)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	jar, _ := cookiejar.New(nil)
	c := &client{t, ts.URL, &http.Client{Jar: jar}}

	// Setup wizard, then it must not be replayable.
	if code := c.do("GET", "/api/v1/me", nil, nil); code != 401 {
		t.Fatalf("me before setup: %d", code)
	}
	if code := c.do("POST", "/api/v1/setup", map[string]string{"email": "Ana@Example.com", "name": "Ana", "password": "correct horse"}, nil); code != 201 {
		t.Fatalf("setup: %d", code)
	}
	if code := c.do("POST", "/api/v1/setup", map[string]string{"email": "x@example.com", "name": "X", "password": "correct horse"}, nil); code != 409 {
		t.Fatalf("second setup: %d", code)
	}
	var me store.User
	c.do("GET", "/api/v1/me", nil, &me)
	if me.Email != "ana@example.com" || me.Role != "admin" {
		t.Fatalf("me = %+v", me)
	}

	// Device + OwnTracks ingest: 30 min at home, drive 5 km, 30 min at work.
	var dev struct{ Token string }
	c.do("POST", "/api/v1/devices", map[string]string{"name": "Phone", "client": "owntracks"}, &dev)
	send := func(lat, lon float64, tst int64) int {
		req, _ := http.NewRequest("POST", ts.URL+"/ingest/owntracks",
			strings.NewReader(fmt.Sprintf(`{"_type":"location","lat":%f,"lon":%f,"tst":%d,"acc":10,"batt":55}`, lat, lon, tst)))
		req.SetBasicAuth("ana", dev.Token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	t0 := int64(1_700_000_000)
	for i := int64(0); i <= 30; i++ {
		send(52.52, 13.405, t0+i*60)
	}
	for i := int64(1); i < 20; i++ {
		send(52.52, 13.405+0.075*float64(i)/20, t0+1800+i*30)
	}
	for i := int64(0); i <= 30; i++ {
		send(52.52, 13.48, t0+2400+i*60)
	}
	if code := send(1, 1, t0); code != 200 {
		t.Fatalf("duplicate timestamp must be accepted silently, got %d", code)
	}
	req, _ := http.NewRequest("POST", ts.URL+"/ingest/owntracks", strings.NewReader(`{"_type":"location","lat":1,"lon":1,"tst":1700000000}`))
	req.SetBasicAuth("x", "wrong")
	if resp, _ := http.DefaultClient.Do(req); resp.StatusCode != 401 {
		t.Fatalf("bad token: %d", resp.StatusCode)
	}
	// A wrong token is refused before the body is parsed.
	req, _ = http.NewRequest("POST", ts.URL+"/ingest/owntracks", strings.NewReader(`not json`))
	req.SetBasicAuth("x", "wrong")
	if resp, _ := http.DefaultClient.Do(req); resp.StatusCode != 401 {
		t.Fatalf("bad token with junk body: %d", resp.StatusCode)
	}

	s.processTimelines(ctx)
	var tl struct {
		Visits []visitOut
		Trips  []store.TripRow
	}
	c.do("GET", "/api/v1/timeline", nil, &tl)
	if len(tl.Visits) != 2 || len(tl.Trips) != 1 || tl.Trips[0].Mode != "drive" {
		t.Fatalf("timeline = %+v", tl)
	}

	// Saved place is matched at read time.
	c.do("POST", "/api/v1/places", map[string]any{"name": "Home", "lat": 52.52, "lon": 13.405, "radius": 100}, nil)
	c.do("GET", "/api/v1/timeline", nil, &tl)
	if tl.Visits[0].PlaceName != "Home" {
		t.Fatalf("visit not matched to place: %+v", tl.Visits[0])
	}

	// Export native → delete everything → re-import → same point count → undo import.
	resp, err := c.http.Get(ts.URL + "/api/v1/export?format=native")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("export: %v %v", err, resp.Status)
	}
	archive, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	var del map[string]int
	c.do("DELETE", "/api/v1/points?from=0&to=9999999999999", nil, &del)
	if del["deleted"] != 81 {
		t.Fatalf("deleted = %v", del)
	}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "export.zip")
	fw.Write(archive)
	mw.Close()
	req, _ = http.NewRequest("POST", ts.URL+"/api/v1/imports", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err = c.http.Do(req)
	if err != nil || resp.StatusCode != 202 {
		t.Fatalf("upload: %v %v", err, resp.Status)
	}
	imp, _ := db.NextImport(ctx)
	s.runImport(ctx, imp)
	var imports []store.Import
	c.do("GET", "/api/v1/imports", nil, &imports)
	if imports[0].Status != "done" || imports[0].Added != 81 || imports[0].Format != "geotracker" {
		t.Fatalf("import = %+v", imports[0])
	}
	if code := c.do("DELETE", fmt.Sprintf("/api/v1/imports/%d", imp.ID), nil, nil); code != 204 {
		t.Fatalf("undo import: %d", code)
	}
	var st store.PointStats
	c.do("GET", "/api/v1/stats", nil, &st)
	if st.Count != 0 {
		t.Fatalf("after undo: %d points", st.Count)
	}

	// Last admin cannot be demoted; SPA fallback serves index.html for app routes.
	if code := c.do("PATCH", fmt.Sprintf("/api/v1/admin/users/%d", me.ID), map[string]string{"role": "user"}, nil); code != 409 {
		t.Fatalf("demote last admin: %d", code)
	}
	resp, _ = http.Get(ts.URL + "/timeline/2024-01-01")
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "app") {
		t.Fatalf("spa fallback: %s", body)
	}

	// Cross-origin browser writes are blocked (CSRF).
	req, _ = http.NewRequest("POST", ts.URL+"/api/v1/places", strings.NewReader(`{}`))
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	if resp, _ := c.http.Do(req); resp.StatusCode != 403 {
		t.Fatalf("cross-site POST: %d", resp.StatusCode)
	}
}
