package notify

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGotify(t *testing.T) {
	var got map[string]any
	var key string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key = r.Header.Get("X-Gotify-Key")
		json.NewDecoder(r.Body).Decode(&got)
	}))
	defer srv.Close()
	if err := SendGotify(context.Background(), srv.URL+"/", "tok", "Hi", "Arrived", 5); err != nil {
		t.Fatal(err)
	}
	if key != "tok" || got["title"] != "Hi" || got["priority"] != float64(5) {
		t.Fatalf("key=%q body=%v", key, got)
	}
}

// fakeSMTP accepts one message and returns its DATA section.
func fakeSMTP(t *testing.T) (addr string, data chan string) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	data = make(chan string, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		r := bufio.NewReader(c)
		say := func(s string) { c.Write([]byte(s + "\r\n")) }
		say("220 fake")
		var body strings.Builder
		inData := false
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			if inData {
				if line == ".\r\n" {
					inData = false
					data <- body.String()
					say("250 ok")
					continue
				}
				body.WriteString(line)
				continue
			}
			switch cmd := strings.ToUpper(strings.Fields(line)[0]); cmd {
			case "EHLO", "HELO", "MAIL", "RCPT":
				say("250 ok")
			case "DATA":
				inData = true
				say("354 go")
			case "QUIT":
				say("221 bye")
				return
			default:
				say("502 no")
			}
		}
	}()
	return ln.Addr().String(), data
}

func TestSendEmail(t *testing.T) {
	addr, data := fakeSMTP(t)
	host, port, _ := net.SplitHostPort(addr)
	cfg := SMTP{Host: host, Port: port, From: "gt@example.com", Security: "none"}
	if err := SendEmail(context.Background(), cfg, "ana@example.com", "Arrived at Home", "Ana arrived at Home."); err != nil {
		t.Fatal(err)
	}
	msg := <-data
	if !strings.Contains(msg, "To: ana@example.com") || !strings.Contains(msg, "Ana arrived at Home.") {
		t.Fatalf("message = %q", msg)
	}
}

func TestUserHTTPRefusesLinkLocal(t *testing.T) {
	_, err := UserHTTP.Get("http://169.254.169.254/latest/meta-data/")
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("metadata address reachable: %v", err)
	}
}
