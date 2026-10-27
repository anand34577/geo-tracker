// Package notify delivers messages by email (SMTP) and Gotify, using only the standard library.
package notify

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

// UserHTTP is the client for URLs that users type in (Gotify, Immich). Their servers are
// usually on the LAN, so private addresses stay allowed, but link-local ones are refused:
// that range holds cloud metadata services (169.254.169.254) that leak credentials.
//
// BlockPrivate additionally refuses loopback and private-range targets. Turn it on (GT_BLOCK_PRIVATE_TARGETS)
// for multi-user servers, where any account could otherwise probe the host's own network.
var BlockPrivate atomic.Bool

// blocked reports whether a dial target is off limits.
func blocked(ip net.IP) bool {
	if ip == nil || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() ||
		ip.Equal(net.IPv4(100, 100, 100, 200)) { // Alibaba Cloud metadata, inside the CGNAT range
		return true
	}
	return BlockPrivate.Load() && (ip.IsLoopback() || ip.IsPrivate())
}

// Scrub removes the request URL from transport errors: URLs carry secrets (Telegram bot tokens,
// Discord and Slack webhook paths) and errors end up in logs and in the UI.
func Scrub(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return fmt.Errorf("%s request failed: %w", strings.ToLower(ue.Op), ue.Err)
	}
	return err
}

var UserHTTP = &http.Client{
	Timeout: 20 * time.Second,
	Transport: &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{Timeout: 10 * time.Second, Control: func(_, address string, _ syscall.RawConn) error {
			host, _, _ := net.SplitHostPort(address)
			if ip := net.ParseIP(host); blocked(ip) {
				return fmt.Errorf("address %s is not allowed", host)
			}
			return nil
		}}).DialContext,
		TLSHandshakeTimeout: 10 * time.Second,
	},
}

type SMTP struct {
	Host, Port, User, Password, From string
	Security                         string // starttls | tls | none
}

func (c SMTP) Configured() bool { return c.Host != "" && c.From != "" }

// plainAuth is SMTP AUTH PLAIN without net/smtp's "encrypted connections only" rule. It is used
// only when the admin explicitly chose no encryption (a trusted local relay).
type plainAuth struct{ user, password string }

func (a plainAuth) Start(*smtp.ServerInfo) (string, []byte, error) {
	return "PLAIN", []byte("\x00" + a.user + "\x00" + a.password), nil
}

func (a plainAuth) Next(_ []byte, more bool) ([]byte, error) {
	if more {
		return nil, errors.New("unexpected server challenge")
	}
	return nil, nil
}

// heloName is how we introduce ourselves; some relays reject the default "localhost".
func heloName() string {
	if h, err := os.Hostname(); err == nil && h != "" && !strings.ContainsAny(h, " _") {
		return h
	}
	return "localhost"
}

// SendEmail sends a plain-text message. "tls" means implicit TLS (usually port 465),
// "starttls" upgrades a plain connection (usually 587) and refuses to continue without it,
// "none" talks plain SMTP (a local relay, usually port 25).
func SendEmail(ctx context.Context, c SMTP, to, subject, body string) error {
	if !c.Configured() {
		return errors.New("SMTP is not configured (Admin → Settings)")
	}
	// The envelope needs bare addresses: "GeoTracker <gt@example.com>" is only valid in the header.
	from, err := mail.ParseAddress(c.From)
	if err != nil {
		return errors.New("the sender address in Admin → Settings is not valid")
	}
	rcpt, err := mail.ParseAddress(to)
	if err != nil {
		return fmt.Errorf("invalid recipient address %q", to)
	}
	port := c.Port
	if port == "" {
		port = map[string]string{"tls": "465", "none": "25"}[c.Security]
		if port == "" {
			port = "587"
		}
	}
	addr := net.JoinHostPort(c.Host, port)
	d := net.Dialer{Timeout: 15 * time.Second}
	var conn net.Conn
	if c.Security == "tls" {
		conn, err = (&tls.Dialer{NetDialer: &d, Config: &tls.Config{ServerName: c.Host}}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = d.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("cannot reach the mail server at %s: %w", addr, err)
	}
	conn.SetDeadline(time.Now().Add(30 * time.Second))
	cl, err := smtp.NewClient(conn, c.Host)
	if err != nil {
		conn.Close()
		return err
	}
	defer cl.Close()
	if err := cl.Hello(heloName()); err != nil {
		return err
	}
	if c.Security == "starttls" {
		if ok, _ := cl.Extension("STARTTLS"); !ok {
			return errors.New("server does not offer STARTTLS; choose TLS or None")
		}
		if err := cl.StartTLS(&tls.Config{ServerName: c.Host}); err != nil {
			return err
		}
	}
	if c.User != "" {
		if ok, _ := cl.Extension("AUTH"); ok {
			var a smtp.Auth = smtp.PlainAuth("", c.User, c.Password, c.Host)
			if c.Security == "none" {
				a = plainAuth{c.User, c.Password} // the admin chose an unencrypted relay on purpose
			}
			if err := cl.Auth(a); err != nil {
				return fmt.Errorf("mail server rejected the login: %w", err)
			}
		} else if c.Security != "none" {
			return errors.New("mail server does not accept a login; clear the SMTP username")
		}
		// An unencrypted relay that offers no AUTH accepts mail by network address: send without a login.
	}
	if err := cl.Mail(from.Address); err != nil {
		return fmt.Errorf("mail server refused the sender: %w", err)
	}
	if err := cl.Rcpt(rcpt.Address); err != nil {
		return fmt.Errorf("mail server refused the recipient: %w", err)
	}
	w, err := cl.Data()
	if err != nil {
		return err
	}
	domain := from.Address[strings.LastIndexByte(from.Address, '@')+1:]
	id := make([]byte, 12)
	rand.Read(id)
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMessage-ID: <%x@%s>\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n%s\r\n",
		from.String(), rcpt.String(), mime.QEncoding.Encode("utf-8", subject), time.Now().Format(time.RFC1123Z), id, domain, crlf(body))
	if _, err := w.Write([]byte(msg)); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return cl.Quit()
}

// crlf normalizes line endings for SMTP (net/smtp's DATA writer escapes leading dots itself).
func crlf(body string) string {
	return strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\n", "\r\n")
}

// SendGotify posts a message to a Gotify server using an application token.
func SendGotify(ctx context.Context, server, token, title, message string, priority int) error {
	if server == "" || token == "" {
		return errors.New("Gotify server URL and app token are required")
	}
	b, _ := json.Marshal(map[string]any{"title": title, "message": message, "priority": priority})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(server, "/")+"/message", bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gotify-Key", token)
	resp, err := UserHTTP.Do(req)
	if err != nil {
		return Scrub(err)
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("gotify: HTTP %d", resp.StatusCode)
	}
	return nil
}
