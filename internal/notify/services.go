package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// telegramAPI is a variable so tests can point it at a local server.
var telegramAPI = "https://api.telegram.org"

func validHTTPURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("enter a full http(s) URL")
	}
	return nil
}

// Do sends one HTTP request through UserHTTP. It fails on transport errors and on HTTP
// status 400 and above; the response body is only read to put a snippet in the error.
func Do(ctx context.Context, method, rawURL string, headers map[string]string, body string) error {
	if err := validHTTPURL(rawURL); err != nil {
		return err
	}
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, r)
	if err != nil {
		return err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if body != "" && req.Header.Get("Content-Type") == "" {
		ct := "text/plain; charset=utf-8"
		if t := strings.TrimSpace(body); strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[") {
			ct = "application/json"
		}
		req.Header.Set("Content-Type", ct)
	}
	req.Header.Set("User-Agent", "GeoTracker")
	resp, err := UserHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	return nil
}

func postJSON(ctx context.Context, rawURL string, headers map[string]string, payload any) error {
	b, _ := json.Marshal(payload)
	if headers == nil {
		headers = map[string]string{}
	}
	headers["Content-Type"] = "application/json"
	return Do(ctx, http.MethodPost, rawURL, headers, string(b))
}

// SendNtfy publishes to an ntfy topic (https://ntfy.sh or a self-hosted server).
func SendNtfy(ctx context.Context, server, topic, token, title, message string, priority int) error {
	if server == "" {
		server = "https://ntfy.sh"
	}
	if topic == "" || strings.ContainsAny(topic, "/ ?#") {
		return errors.New("ntfy needs a topic name (letters, digits, - and _)")
	}
	headers := map[string]string{}
	if token != "" {
		headers["Authorization"] = "Bearer " + token
	}
	payload := map[string]any{"topic": topic, "title": title, "message": message}
	if priority >= 1 && priority <= 5 {
		payload["priority"] = priority
	}
	return postJSON(ctx, strings.TrimRight(server, "/"), headers, payload)
}

// SendTelegram sends a message from your own bot (create one with @BotFather) to a chat id.
func SendTelegram(ctx context.Context, botToken, chatID, text string) error {
	if botToken == "" || chatID == "" {
		return errors.New("Telegram needs a bot token and a chat id")
	}
	if strings.ContainsAny(botToken, "/ ?#") {
		return errors.New("that does not look like a Telegram bot token")
	}
	return postJSON(ctx, telegramAPI+"/bot"+botToken+"/sendMessage", nil, map[string]any{"chat_id": chatID, "text": text, "disable_web_page_preview": true})
}

// SendDiscord posts to a Discord channel webhook.
func SendDiscord(ctx context.Context, webhookURL, text string) error {
	return postJSON(ctx, webhookURL, nil, map[string]string{"content": text})
}

// SendSlack posts to a Slack incoming webhook (also works with Mattermost and Rocket.Chat).
func SendSlack(ctx context.Context, webhookURL, text string) error {
	return postJSON(ctx, webhookURL, nil, map[string]string{"text": text})
}
