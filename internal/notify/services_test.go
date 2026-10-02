package notify

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServices(t *testing.T) {
	var gotPath, gotAuth, gotBody, gotType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotPath, gotAuth, gotBody, gotType = r.URL.Path, r.Header.Get("Authorization"), string(b), r.Header.Get("Content-Type")
		if r.URL.Path == "/fail" {
			http.Error(w, "nope", 500)
		}
	}))
	defer srv.Close()
	ctx := context.Background()

	if err := SendNtfy(ctx, srv.URL, "family", "tk", "Arrived", "Sam is home", 4); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer tk" || !strings.Contains(gotBody, `"topic":"family"`) || !strings.Contains(gotBody, `"priority":4`) {
		t.Fatalf("ntfy: %q %q", gotAuth, gotBody)
	}
	if err := SendNtfy(ctx, srv.URL, "bad topic", "", "t", "m", 0); err == nil {
		t.Fatal("a topic with a space must be rejected")
	}

	old := telegramAPI
	telegramAPI = srv.URL
	defer func() { telegramAPI = old }()
	if err := SendTelegram(ctx, "123:abc", "42", "hi"); err != nil || gotPath != "/bot123:abc/sendMessage" || !strings.Contains(gotBody, `"chat_id":"42"`) {
		t.Fatalf("telegram: %v %s %s", err, gotPath, gotBody)
	}
	if err := SendDiscord(ctx, srv.URL+"/hook", "hello"); err != nil || gotBody != `{"content":"hello"}` || gotType != "application/json" {
		t.Fatalf("discord: %v %s", err, gotBody)
	}
	if err := SendSlack(ctx, srv.URL+"/hook", "hello"); err != nil || gotBody != `{"text":"hello"}` {
		t.Fatalf("slack: %v %s", err, gotBody)
	}
	if err := Do(ctx, "POST", srv.URL+"/fail", nil, "{}"); err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("a 500 must surface as an error, got %v", err)
	}
	if err := Do(ctx, "GET", "ftp://x", nil, ""); err == nil {
		t.Fatal("a non-http URL must be rejected")
	}
}
