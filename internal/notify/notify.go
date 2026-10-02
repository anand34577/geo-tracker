// Package notify delivers messages by email (SMTP) and Gotify, using only the standard library.
package notify

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/http"
	"net/smtp"
	"strings"
	"syscall"
	"time"
)

// UserHTTP is the client for URLs that users type in (Gotify, Immich). Their servers are
// usually on the LAN, so private addresses stay allowed, but link-local ones are refused:
// that range holds cloud metadata services (169.254.169.254) that leak credentials.
var UserHTTP = &http.Client{
	Timeout: 20 * time.Second,
	Transport: &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{Timeout: 10 * time.Second, Control: func(_, address string, _ syscall.RawConn) error {
			host, _, _ := net.SplitHostPort(address)
			if ip := net.ParseIP(host); ip == nil || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
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

// SendEmail sends a plain-text message. "tls" means implicit TLS (usually port 465),
// "starttls" upgrades a plain connection (usually 587) and refuses to continue without it.
func SendEmail(ctx context.Context, c SMTP, to, subject, body string) error {
	if !c.Configured() {
		return errors.New("SMTP is not configured (Admin → Settings)")
	}
	addr := net.JoinHostPort(c.Host, c.Port)
	d := net.Dialer{Timeout: 15 * time.Second}
	var conn net.Conn
	var err error
	if c.Security == "tls" {
		conn, err = tls.DialWithDialer(&d, "tcp", addr, &tls.Config{ServerName: c.Host})
	} else {
		conn, err = d.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return err
	}
	conn.SetDeadline(time.Now().Add(30 * time.Second))
	cl, err := smtp.NewClient(conn, c.Host)
	if err != nil {
		conn.Close()
		return err
	}
	defer cl.Close()
	if c.Security == "starttls" {
		if ok, _ := cl.Extension("STARTTLS"); !ok {
			return errors.New("server does not offer STARTTLS; choose TLS or None")
		}
		if err := cl.StartTLS(&tls.Config{ServerName: c.Host}); err != nil {
			return err
		}
	}
	if c.User != "" {
		// PlainAuth refuses to send credentials over an unencrypted connection (except to localhost).
		if err := cl.Auth(smtp.PlainAuth("", c.User, c.Password, c.Host)); err != nil {
			return err
		}
	}
	if err := cl.Mail(c.From); err != nil {
		return err
	}
	if err := cl.Rcpt(to); err != nil {
		return err
	}
	w, err := cl.Data()
	if err != nil {
		return err
	}
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n%s\r\n",
		c.From, to, mime.QEncoding.Encode("utf-8", subject), time.Now().Format(time.RFC1123Z), strings.ReplaceAll(body, "\n", "\r\n"))
	if _, err := w.Write([]byte(msg)); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return cl.Quit()
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
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("gotify: HTTP %d", resp.StatusCode)
	}
	return nil
}
