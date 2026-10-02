package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestGeofenceAutomations(t *testing.T) {
	_, ts, db := newTestServer(t)
	ctx := context.Background()
	ana, _ := db.CreateFirstAdmin(ctx, "ana@example.com", "Ana", HashPassword("correct horse"))
	sam, _ := db.CreateUser(ctx, "sam@example.com", "Sam", HashPassword("correct horse"), "user")
	eve, _ := db.CreateUser(ctx, "eve@example.com", "Eve", HashPassword("correct horse"), "user")
	tok := func(email string) string {
		_, b := do(t, "POST", ts.URL+"/api/v1/auth/token", "", `{"email":"`+email+`","password":"correct horse"}`)
		return b["token"].(string)
	}
	anaT, samT := tok("ana@example.com"), tok("sam@example.com")

	// Sam is in Ana's family group; Eve is not.
	_, g := do(t, "POST", ts.URL+"/api/v1/groups", anaT, `{"name":"Family"}`)
	gid := fmt.Sprint(int64(g["id"].(float64)))
	do(t, "POST", ts.URL+"/api/v1/groups/"+gid+"/members", anaT, `{"email":"sam@example.com"}`)
	do(t, "PUT", ts.URL+"/api/v1/groups/"+gid+"/me", samT, `{"share_live":true,"share_history_days":1,"precision":"exact"}`)

	// A webhook receiver that remembers what it was sent.
	hits := make(chan string, 8)
	recv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		hits <- r.URL.Path + " " + r.Header.Get("X-Key") + " " + string(b)
	}))
	defer recv.Close()

	_, pl := do(t, "POST", ts.URL+"/api/v1/places", anaT, `{"name":"Home","lat":52.52,"lon":13.4,"radius":100}`)
	place := fmt.Sprint(int64(pl["id"].(float64)))
	base := fmt.Sprintf(`"name":"Home hook","place_id":%s,"on_arrive":true,"on_leave":true,"enabled":true,"cooldown_min":1`, place)

	// Validation.
	for name, body := range map[string]string{
		"no actions":            `{` + base + `,"actions":[]}`,
		"unknown type":          `{` + base + `,"actions":[{"type":"carrier-pigeon"}]}`,
		"family stranger":       fmt.Sprintf(`{`+base+`,"actions":[{"type":"notify_family","members":[%d]}]}`, eve.ID),
		"bad webhook url":       `{` + base + `,"actions":[{"type":"webhook","url":"ftp://x"}]}`,
		"header injection":      `{` + base + `,"actions":[{"type":"webhook","url":"` + recv.URL + `","headers":{"X-Key":"a\r\nEvil: 1"}}]}`,
		"ntfy topic with slash": `{` + base + `,"actions":[{"type":"ntfy","topic":"a/b"}]}`,
	} {
		if resp, _ := do(t, "POST", ts.URL+"/api/v1/automations", anaT, body); resp.StatusCode != 400 {
			t.Fatalf("%s: status %d, want 400", name, resp.StatusCode)
		}
	}

	// Create: a webhook with a secret in its URL and header, plus a family notification.
	create := fmt.Sprintf(`{`+base+`,"actions":[
		{"type":"webhook","url":"%s/hook/s3cretid","headers":{"X-Key":"k-123"},"body":"{\"msg\":\"{{user}} {{verb}} {{place}}\"}"},
		{"type":"notify_family","members":[%d,%d]}]}`, recv.URL, sam.ID, eve.ID)
	resp, a := do(t, "POST", ts.URL+"/api/v1/automations", anaT, create)
	if resp.StatusCode != 201 {
		t.Fatalf("create: %d %v", resp.StatusCode, a)
	}
	id := fmt.Sprint(int64(a["id"].(float64)))
	actions := a["actions"].([]any)
	fam := actions[1].(map[string]any)["members"].([]any)
	if len(fam) != 1 || int64(fam[0].(float64)) != sam.ID {
		t.Fatalf("a non-member must be dropped from notify_family: %v", fam)
	}

	// The API never returns the secrets, and saving the masked form keeps them.
	list, _ := io.ReadAll(mustGet(t, ts.URL+"/api/v1/automations", anaT).Body)
	if strings.Contains(string(list), "s3cretid") || strings.Contains(string(list), "k-123") {
		t.Fatalf("secrets leaked: %s", list)
	}
	resp, upd := do(t, "PUT", ts.URL+"/api/v1/automations/"+id, anaT, strings.Replace(create, "Home hook", "Home hook 2", 1))
	if resp.StatusCode != 200 || upd["name"] != "Home hook 2" {
		t.Fatalf("update: %d %v", resp.StatusCode, upd)
	}
	masked := mustGet(t, ts.URL+"/api/v1/automations", anaT)
	var maskedList []map[string]any
	decodeJSON(t, masked, &maskedList)
	mb, _ := json.Marshal(maskedList[0])
	resp, _ = do(t, "PUT", ts.URL+"/api/v1/automations/"+id, anaT, string(mb))
	if resp.StatusCode != 200 {
		t.Fatalf("saving the masked form: %d", resp.StatusCode)
	}
	if got, _ := db.AutomationByID(ctx, ana.ID, autoID(id)); !strings.Contains(string(got.Actions), "s3cretid") || !strings.Contains(string(got.Actions), "k-123") {
		t.Fatalf("saving the masked form lost the secrets: %s", got.Actions)
	}

	// Another user cannot see, edit or test it.
	if resp, _ := do(t, "POST", ts.URL+"/api/v1/automations/"+id+"/test", samT, ""); resp.StatusCode != 404 {
		t.Fatalf("other user's test: %d", resp.StatusCode)
	}

	// Real flow: a fix elsewhere, then arriving home, then leaving.
	d, _ := db.CreateDevice(ctx, ana.ID, "Phone", "overland", hashToken("anatoken"))
	_ = d
	send := func(lat, lon float64, ago time.Duration) {
		body := fmt.Sprintf(`[{"ts":%d,"lat":%f,"lon":%f}]`, time.Now().Add(-ago).UnixMilli(), lat, lon)
		if resp, _ := do(t, "POST", ts.URL+"/ingest/json", "anatoken", body); resp.StatusCode != 200 {
			t.Fatalf("ingest: %d", resp.StatusCode)
		}
	}
	expect := func(want string) {
		t.Helper()
		select {
		case got := <-hits:
			if !strings.Contains(got, want) {
				t.Fatalf("webhook got %q, want it to contain %q", got, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("no webhook for %q", want)
		}
	}
	send(52.60, 13.50, 3*time.Minute) // far away: learns the starting state
	send(52.5201, 13.4001, 2*time.Minute)
	expect(`/hook/s3cretid k-123 {"msg":"Ana arrived at Home"}`)
	send(52.60, 13.50, time.Minute)
	expect(`"msg":"Ana left Home"`)

	// The test button runs the actions once and reports each result.
	resp, tr := do(t, "POST", ts.URL+"/api/v1/automations/"+id+"/test", anaT, "")
	if resp.StatusCode != 200 {
		t.Fatalf("test: %d %v", resp.StatusCode, tr)
	}
	expect(`"msg":"Ana arrived at Home"`)
	for _, r := range tr["results"].([]any) {
		if r.(map[string]any)["ok"] != true {
			t.Fatalf("test result: %v", tr)
		}
	}
}

func mustGet(t *testing.T, u, token string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func decodeJSON(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatal(err)
	}
}

func autoID(s string) int64 { n, _ := strconv.ParseInt(s, 10, 64); return n }

func TestTemplateEscaping(t *testing.T) {
	vars := map[string]string{"place": `Sam's "Home" & Co`, "map_url": "https://x/?a=1&b=2"}
	got := render(`{"p":"{{place}}","m":"{{map_url}}"}`, vars, jsonEscape)
	want := `{"p":"Sam's \"Home\" & Co","m":"https://x/?a=1&b=2"}`
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
	if got := render("{{place}}", vars, nil); got != vars["place"] {
		t.Fatalf("plain render altered the value: %s", got)
	}
}
