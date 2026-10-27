package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The admin UI saves by sending back everything GET returned. Internal and display-only keys
// must not break that (regression: "invalid value for recap_sent").
func TestSettingsRoundTrip(t *testing.T) {
	_, ts, db := newTestServer(t)
	ctx := context.Background()
	db.CreateFirstAdmin(ctx, "ana@example.com", "Ana", HashPassword("correct horse"))
	_, body := do(t, "POST", ts.URL+"/api/v1/auth/token", "", `{"email":"ana@example.com","password":"correct horse"}`)
	tok := body["token"].(string)
	db.SetSettings(ctx, map[string]string{"recap_sent": "2026-09"})

	_, got := do(t, "GET", ts.URL+"/api/v1/admin/settings", tok, "")
	if _, leaked := got["recap_sent"]; leaked {
		t.Fatal("internal setting exposed")
	}
	got["recap_sent"] = "2020-01" // a stale client may still send it
	got["smtp_host"] = "mail.example.com"
	b, _ := json.Marshal(got)
	if resp, out := do(t, "PUT", ts.URL+"/api/v1/admin/settings", tok, string(b)); resp.StatusCode != 200 {
		t.Fatalf("round trip: %d %v", resp.StatusCode, out)
	}
	all, _ := db.Settings(ctx)
	if all["recap_sent"] != "2026-09" || all["smtp_host"] != "mail.example.com" {
		t.Fatalf("settings = %v", all)
	}
	if resp, _ := do(t, "PUT", ts.URL+"/api/v1/admin/settings", tok, `{"nope":"1"}`); resp.StatusCode != 400 {
		t.Fatalf("unknown key accepted: %d", resp.StatusCode)
	}
	if resp, out := do(t, "PUT", ts.URL+"/api/v1/admin/settings", tok, `{"map_style_light":"https://tile.example.com/{z}/{x}/{y}.png"}`); resp.StatusCode != 400 || !strings.Contains(out["title"].(string), "Custom maps") {
		t.Fatalf("tile URL accepted as a style: %d %v", resp.StatusCode, out)
	}
	if resp, _ := do(t, "PUT", ts.URL+"/api/v1/admin/settings", tok, `{"oidc_enabled":"true"}`); resp.StatusCode != 400 {
		t.Fatalf("SSO enabled without issuer: %d", resp.StatusCode)
	}
}

func TestCustomXYZMap(t *testing.T) {
	_, ts, db := newTestServer(t)
	ctx := context.Background()
	db.CreateFirstAdmin(ctx, "ana@example.com", "Ana", HashPassword("correct horse"))
	_, body := do(t, "POST", ts.URL+"/api/v1/auth/token", "", `{"email":"ana@example.com","password":"correct horse"}`)
	tok := body["token"].(string)

	var asked []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.Path)
		if r.URL.Path == "/tiles/9/0/0.png" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("\x89PNG"))
	}))
	defer up.Close()

	// Invalid templates are rejected with a reason.
	for _, bad := range []string{`[{"name":"X","kind":"raster","url":"https://t.example.com/{z}/{x}.png"}]`, `[{"name":"X","url":"https://t.example.com/{zoom}/{x}/{y}.png"}]`, `[{"name":"","url":"https://t.example.com/{z}/{x}/{y}.png"}]`} {
		b, _ := json.Marshal(map[string]string{"map_custom": bad})
		if resp, _ := do(t, "PUT", ts.URL+"/api/v1/admin/settings", tok, string(b)); resp.StatusCode != 400 {
			t.Fatalf("accepted %s", bad)
		}
	}

	// An http:// tile server is proxied automatically (the browser's CSP would block it).
	list := `[{"id":"ctopo","name":"Local tiles","url":"` + up.URL + `/tiles/{z}/{x}/{y}.png","attribution":"© me","max_zoom":18}]`
	b, _ := json.Marshal(map[string]string{"map_custom": list, "map_default": "ctopo"})
	if resp, out := do(t, "PUT", ts.URL+"/api/v1/admin/settings", tok, string(b)); resp.StatusCode != 200 {
		t.Fatalf("save custom map: %d %v", resp.StatusCode, out)
	}
	_, cfg := do(t, "GET", ts.URL+"/api/v1/config", tok, "")
	var styleURL string
	for _, m := range cfg["basemaps"].([]any) {
		if mm := m.(map[string]any); mm["id"] == "ctopo" {
			styleURL = mm["light"].(string)
		}
	}
	if styleURL == "" || cfg["basemap_default"] != "ctopo" {
		t.Fatalf("config = %v", cfg)
	}
	_, style := do(t, "GET", styleURL, tok, "")
	src := style["sources"].(map[string]any)["raster"].(map[string]any)
	tiles := src["tiles"].([]any)
	if len(tiles) != 1 || !strings.HasSuffix(tiles[0].(string), "/api/v1/map/proxy/ctopo/{z}/{x}/{y}") || src["maxzoom"] != float64(18) {
		t.Fatalf("style = %v", style)
	}

	get := func(path string) *http.Response {
		req, _ := http.NewRequest("GET", ts.URL+path, nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	resp := get("/api/v1/map/proxy/ctopo/3/2/5")
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/png" || string(data) != "\x89PNG" || asked[len(asked)-1] != "/tiles/3/2/5.png" {
		t.Fatalf("proxy: %d %q %v", resp.StatusCode, data, asked)
	}
	if resp := get("/api/v1/map/proxy/ctopo/9/0/0"); resp.StatusCode != 204 {
		t.Fatalf("missing upstream tile: %d", resp.StatusCode)
	}
	if resp := get("/api/v1/map/proxy/ctopo/1/5/0"); resp.StatusCode != 400 {
		t.Fatalf("out of range tile: %d", resp.StatusCode)
	}

	// The admin's "test" button fetches a tile and returns a preview.
	if resp, out := do(t, "POST", ts.URL+"/api/v1/admin/maps/test", tok, `{"name":"T","url":"`+up.URL+`/tiles/{z}/{x}/{y}.png"}`); resp.StatusCode != 200 || !strings.HasPrefix(out["preview"].(string), "data:image/png;base64,") {
		t.Fatalf("test: %d %v", resp.StatusCode, out)
	}

	// Deleting the default map falls back to the standard one instead of failing.
	if resp, out := do(t, "PUT", ts.URL+"/api/v1/admin/settings", tok, `{"map_custom":"[]"}`); resp.StatusCode != 200 || out["map_default"] != "auto" {
		t.Fatalf("remove default map: %d %v", resp.StatusCode, out)
	}
}
