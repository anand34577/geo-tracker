package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"geotracker/internal/geo"
	"geotracker/internal/notify"
	"geotracker/internal/store"
)

// Events a user can be notified about. Family geofences (v0.2) reuse the same pipeline.
var notifyEvents = map[string]bool{ // event → on by default
	"place_arrive":  true,
	"place_leave":   false,
	"device_silent": true,
	"battery_low":   true,
	"import_done":   true,
	"family":        true, // invitations and the arrive/leave alerts you set up for family members
	"backup_failed": true, // admins only
}

type notifyPrefs struct {
	Email struct {
		Enabled bool   `json:"enabled"`
		To      string `json:"to"` // empty = the account email
	} `json:"email"`
	Gotify struct {
		Enabled  bool   `json:"enabled"`
		URL      string `json:"url"`
		Token    string `json:"token"`
		Priority int    `json:"priority"`
	} `json:"gotify"`
	Ntfy struct {
		Enabled  bool   `json:"enabled"`
		URL      string `json:"url"` // empty = https://ntfy.sh
		Topic    string `json:"topic"`
		Token    string `json:"token"`
		Priority int    `json:"priority"`
	} `json:"ntfy"`
	Telegram struct {
		Enabled bool   `json:"enabled"`
		Token   string `json:"token"`
		ChatID  string `json:"chat_id"`
	} `json:"telegram"`
	Events       map[string]bool `json:"events"`
	SilentHours  int             `json:"silent_hours"`  // 0 = off
	BatteryBelow int             `json:"battery_below"` // percent
}

func parsePrefs(raw json.RawMessage) notifyPrefs {
	p := notifyPrefs{SilentHours: 12, BatteryBelow: 15}
	p.Gotify.Priority = 5
	p.Ntfy.Priority = 3
	json.Unmarshal(raw, &p)
	if p.Events == nil {
		p.Events = map[string]bool{}
	}
	for e, def := range notifyEvents {
		if _, set := p.Events[e]; !set {
			p.Events[e] = def
		}
	}
	return p
}

// notifyState remembers what each user/device last looked like, to detect changes.
// ponytail: in memory; after a restart the first fix only re-learns state (no false alerts).
type notifyState struct {
	mu          sync.Mutex
	place       map[int64]int64 // user → current place (0 = none)
	battery     map[int64]int   // device → last battery %
	silentAlert map[int64]int64 // device → last_seen_at we already warned about
	rule        map[int64]bool  // family alert rule → subject inside the place
}

func newNotifyState() *notifyState {
	return &notifyState{place: map[int64]int64{}, battery: map[int64]int{}, silentAlert: map[int64]int64{}, rule: map[int64]bool{}}
}

func (s *Server) smtpConfig(st map[string]string) notify.SMTP {
	return notify.SMTP{Host: st["smtp_host"], Port: st["smtp_port"], User: st["smtp_user"], Password: st["smtp_password"], From: st["smtp_from"], Security: st["smtp_security"]}
}

// deliver sends to the user's enabled channels (or only `only`, for tests) and reports per channel.
func (s *Server) deliver(ctx context.Context, u *store.User, p notifyPrefs, title, body, only string) map[string]string {
	out := map[string]string{}
	result := func(ch string, err error) {
		out[ch] = "ok"
		if err != nil {
			out[ch] = err.Error()
			slog.Warn("notification failed", "channel", ch, "user", u.ID, "err", err)
		}
	}
	if (only == "" && p.Email.Enabled) || only == "email" {
		st, err := s.db.Settings(ctx)
		if err == nil {
			to := p.Email.To
			if to == "" {
				to = u.Email
			}
			err = notify.SendEmail(ctx, s.smtpConfig(st), to, title, body+"\n\n— GeoTracker")
		}
		result("email", err)
	}
	if (only == "" && p.Gotify.Enabled) || only == "gotify" {
		result("gotify", notify.SendGotify(ctx, p.Gotify.URL, p.Gotify.Token, title, body, p.Gotify.Priority))
	}
	if (only == "" && p.Ntfy.Enabled) || only == "ntfy" {
		result("ntfy", notify.SendNtfy(ctx, p.Ntfy.URL, p.Ntfy.Topic, p.Ntfy.Token, title, body, p.Ntfy.Priority))
	}
	if (only == "" && p.Telegram.Enabled) || only == "telegram" {
		result("telegram", notify.SendTelegram(ctx, p.Telegram.Token, p.Telegram.ChatID, title+"\n"+body))
	}
	return out
}

// notifyUser sends an event notification in the background if the user wants it.
func (s *Server) notifyUser(userID int64, event, title, body string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		u, err := s.db.UserByID(ctx, userID)
		if err != nil || u.Disabled {
			return
		}
		raw, err := s.db.UserNotify(ctx, userID)
		if err != nil {
			return
		}
		p := parsePrefs(raw)
		if p.Events[event] {
			s.deliver(ctx, u, p, title, body, "")
		}
	}()
}

// checkPointEvents runs after each ingest for the newest fix of a device.
func (s *Server) checkPointEvents(ctx context.Context, userID, deviceID int64, p geo.Point) {
	if time.Since(time.UnixMilli(p.TS)) > 15*time.Minute { // late batch uploads are history, not news
		return
	}
	raw, err := s.db.UserNotify(ctx, userID)
	if err != nil {
		return
	}
	prefs := parsePrefs(raw)
	u, err := s.db.UserByID(ctx, userID)
	if err != nil {
		return
	}
	s.publishFamily(ctx, userID)
	s.checkFamilyAlerts(ctx, u, p)

	// Battery crossing below the threshold.
	if p.Battery != nil && deviceID != 0 { // 0 = browser "send my location", shared by every user
		s.notes.mu.Lock()
		prev, known := s.notes.battery[deviceID]
		s.notes.battery[deviceID] = *p.Battery
		s.notes.mu.Unlock()
		if known && prev >= prefs.BatteryBelow && *p.Battery < prefs.BatteryBelow {
			s.notifyUser(userID, "battery_low", fmt.Sprintf("Battery low: %d%%", *p.Battery),
				fmt.Sprintf("%s's phone is at %d%%. Tracking stops if it runs out.", u.Name, *p.Battery))
		}
	}

	// Arriving at / leaving saved places, with hysteresis so GPS jitter at the edge doesn't flap.
	if p.Accuracy != nil && *p.Accuracy > 100 {
		return
	}
	places, err := s.db.Places(ctx, userID)
	if err != nil {
		return
	}
	byID := map[int64]*store.Place{}
	for i := range places {
		byID[places[i].ID] = &places[i]
	}
	s.notes.mu.Lock()
	prev, known := s.notes.place[userID]
	cur := int64(0)
	if pl := byID[prev]; pl != nil {
		margin := 25.0
		if p.Accuracy != nil {
			margin = math.Max(margin, *p.Accuracy)
		}
		if geo.Distance(p.Lat, p.Lon, pl.Lat, pl.Lon) <= pl.Radius+margin {
			cur = prev
		}
	}
	if cur == 0 {
		if pl := matchPlace(places, p.Lat, p.Lon); pl != nil {
			cur = pl.ID
		}
	}
	s.notes.place[userID] = cur
	s.notes.mu.Unlock()
	if !known || cur == prev {
		return
	}
	if pl := byID[prev]; prev != 0 && pl != nil {
		s.notifyUser(userID, "place_leave", "Left "+pl.Name, u.Name+" left "+pl.Name+".")
		s.runAutomations(u, *pl, "leave", p)
	}
	if pl := byID[cur]; pl != nil {
		s.notifyUser(userID, "place_arrive", "Arrived at "+pl.Name, u.Name+" arrived at "+pl.Name+".")
		s.runAutomations(u, *pl, "arrive", p)
	}
}

// checkSilentDevices warns once when a device stops reporting for the user's chosen time.
func (s *Server) checkSilentDevices(ctx context.Context) {
	users, err := s.db.NotifyUsers(ctx)
	if err != nil {
		return
	}
	now := time.Now().UnixMilli()
	for uid, raw := range users {
		p := parsePrefs(raw)
		if p.SilentHours <= 0 || !p.Events["device_silent"] {
			continue
		}
		devices, err := s.db.Devices(ctx, uid)
		if err != nil {
			continue
		}
		limit := int64(p.SilentHours) * 3_600_000
		for _, d := range devices {
			if d.LastSeenAt == nil || d.Client == "app" {
				continue
			}
			age := now - *d.LastSeenAt
			s.notes.mu.Lock()
			already := s.notes.silentAlert[d.ID] == *d.LastSeenAt
			// Only for devices that went quiet recently; a phone retired months ago isn't news.
			fire := age > limit && age < limit+48*3_600_000 && !already
			if fire {
				s.notes.silentAlert[d.ID] = *d.LastSeenAt
			}
			s.notes.mu.Unlock()
			if fire {
				s.notifyUser(uid, "device_silent", "No location from "+d.Name,
					fmt.Sprintf("%s hasn't sent a location for %d hours. Check that tracking is on and the app can reach the server.", d.Name, p.SilentHours))
			}
		}
	}
}

func (s *Server) notifyAdmins(event, title, body string) {
	ids, err := s.db.AdminIDs(context.Background())
	if err != nil {
		return
	}
	for _, id := range ids {
		s.notifyUser(id, event, title, body)
	}
}

// ── Handlers ─────────────────────────────────────────────────

func (s *Server) getNotify(w http.ResponseWriter, r *http.Request, u *store.User) {
	raw, err := s.db.UserNotify(r.Context(), u.ID)
	if err != nil {
		internal(w, r, err)
		return
	}
	st, err := s.db.Settings(r.Context())
	if err != nil {
		internal(w, r, err)
		return
	}
	p := parsePrefs(raw)
	for _, t := range []*string{&p.Gotify.Token, &p.Ntfy.Token, &p.Telegram.Token} {
		if *t != "" {
			*t = secretMask // tokens are write-only from the browser's point of view
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"prefs": p, "smtp_configured": s.smtpConfig(st).Configured()})
}

func (s *Server) putNotify(w http.ResponseWriter, r *http.Request, u *store.User) {
	var p notifyPrefs
	if !decode(w, r, &p) {
		return
	}
	// A token that comes back masked is unchanged: keep the stored one.
	if raw, err := s.db.UserNotify(r.Context(), u.ID); err == nil {
		old := parsePrefs(raw)
		for _, t := range []struct{ in *string; was string }{{&p.Gotify.Token, old.Gotify.Token}, {&p.Ntfy.Token, old.Ntfy.Token}, {&p.Telegram.Token, old.Telegram.Token}} {
			if *t.in == secretMask {
				*t.in = t.was
			}
		}
	}
	if p.Email.To != "" {
		to, ok := cleanEmail(p.Email.To)
		if !ok {
			fail(w, http.StatusBadRequest, "enter a valid email address")
			return
		}
		p.Email.To = to
	}
	if p.Gotify.Enabled {
		if pu, err := url.Parse(p.Gotify.URL); err != nil || (pu.Scheme != "https" && pu.Scheme != "http") || pu.Host == "" || p.Gotify.Token == "" {
			fail(w, http.StatusBadRequest, "Gotify needs a server URL (http/https) and an app token")
			return
		}
	}
	if p.Ntfy.Enabled {
		if p.Ntfy.Topic == "" || strings.ContainsAny(p.Ntfy.Topic, "/ ?#") {
			fail(w, http.StatusBadRequest, "ntfy needs a topic name (letters, digits, - and _)")
			return
		}
		if pu, err := url.Parse(p.Ntfy.URL); p.Ntfy.URL != "" && (err != nil || (pu.Scheme != "https" && pu.Scheme != "http") || pu.Host == "") {
			fail(w, http.StatusBadRequest, "the ntfy server must be an http(s) address (leave empty for ntfy.sh)")
			return
		}
	}
	if p.Telegram.Enabled && (p.Telegram.Token == "" || p.Telegram.ChatID == "") {
		fail(w, http.StatusBadRequest, "Telegram needs a bot token and a chat id")
		return
	}
	for e := range p.Events {
		if _, ok := notifyEvents[e]; !ok {
			delete(p.Events, e)
		}
	}
	p.Gotify.Priority = min(max(p.Gotify.Priority, 0), 10)
	p.Ntfy.Priority = min(max(p.Ntfy.Priority, 1), 5)
	p.SilentHours = min(max(p.SilentHours, 0), 168)
	p.BatteryBelow = min(max(p.BatteryBelow, 1), 99)
	b, _ := json.Marshal(p)
	if err := s.db.SetUserNotify(r.Context(), u.ID, b); err != nil {
		internal(w, r, err)
		return
	}
	s.getNotify(w, r, u)
}

func (s *Server) testNotify(w http.ResponseWriter, r *http.Request, u *store.User) {
	ch := r.URL.Query().Get("channel")
	if !slices.Contains([]string{"email", "gotify", "ntfy", "telegram"}, ch) {
		fail(w, http.StatusBadRequest, "channel must be email, gotify, ntfy or telegram")
		return
	}
	if !s.logins.allow("notify-test:"+strconv.FormatInt(u.ID, 10), 10, 10*time.Minute) {
		fail(w, http.StatusTooManyRequests, "too many test messages, wait a few minutes")
		return
	}
	raw, err := s.db.UserNotify(r.Context(), u.ID)
	if err != nil {
		internal(w, r, err)
		return
	}
	res := s.deliver(r.Context(), u, parsePrefs(raw), "GeoTracker test", "Notifications are working.", ch)
	if res[ch] != "ok" {
		fail(w, http.StatusBadGateway, res[ch])
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
