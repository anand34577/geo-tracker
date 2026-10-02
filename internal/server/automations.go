package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"geotracker/internal/geo"
	"geotracker/internal/notify"
	"geotracker/internal/store"
)

// Geofence automations: "when I arrive at / leave this place, do something".
// A rule watches one saved place and runs a list of actions (webhook, notify people,
// ntfy, Telegram, Discord, Slack, email). Rules only ever act for their owner.

type autoAction struct {
	ID      string            `json:"id"`
	Type    string            `json:"type"`
	Message string            `json:"message,omitempty"`
	URL     string            `json:"url,omitempty"`     // webhook, discord, slack; ntfy server
	Method  string            `json:"method,omitempty"`  // webhook
	Body    string            `json:"body,omitempty"`    // webhook
	Headers map[string]string `json:"headers,omitempty"` // webhook
	Topic   string            `json:"topic,omitempty"`   // ntfy
	Token   string            `json:"token,omitempty"`   // ntfy access token, Telegram bot token
	ChatID  string            `json:"chat_id,omitempty"` // Telegram
	To      string            `json:"to,omitempty"`      // email
	Members []int64           `json:"members,omitempty"` // notify_family
}

var actionTypes = []string{"webhook", "notify_me", "notify_family", "ntfy", "telegram", "discord", "slack", "email"}

const (
	maxActions     = 6
	maxAutomations = 50
	defaultMessage = "{{user}} {{verb}} {{place}} at {{time}}."
)

var headerName = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)

// ── Event data and templates ────────────────────────────────

type autoEvent struct {
	Event string // arrive | leave
	User  *store.User
	Place store.Place
	Point geo.Point
}

func (e autoEvent) verb() string {
	if e.Event == "leave" {
		return "left"
	}
	return "arrived at"
}

func (e autoEvent) vars() map[string]string {
	t := time.UnixMilli(e.Point.TS)
	lat, lon := strconv.FormatFloat(e.Point.Lat, 'f', 6, 64), strconv.FormatFloat(e.Point.Lon, 'f', 6, 64)
	return map[string]string{
		"user": e.User.Name, "place": e.Place.Name, "event": e.Event, "verb": e.verb(),
		"time": t.Format("15:04"), "date": t.Format("2006-01-02"), "lat": lat, "lon": lon,
		"map_url": "https://www.openstreetmap.org/?mlat=" + lat + "&mlon=" + lon + "#map=17/" + lat + "/" + lon,
	}
}

// render fills {{placeholders}}. escape is applied to each value (JSON or URL escaping).
func render(tpl string, vars map[string]string, escape func(string) string) string {
	pairs := make([]string, 0, len(vars)*2)
	for k, v := range vars {
		if escape != nil {
			v = escape(v)
		}
		pairs = append(pairs, "{{"+k+"}}", v)
	}
	return strings.NewReplacer(pairs...).Replace(tpl)
}

// jsonEscape escapes s for use inside a JSON string. Unlike json.Marshal it leaves & < > alone,
// so a map link keeps its plain ampersand instead of a unicode escape.
func jsonEscape(s string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.Encode(s)
	out := strings.TrimSuffix(buf.String(), "\n") // Encode appends a newline
	return out[1 : len(out)-1]                    // drop the surrounding quotes
}

// ── Running actions ─────────────────────────────────────────

type actionResult struct {
	Type  string `json:"type"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

func (s *Server) runAction(ctx context.Context, a autoAction, ev autoEvent) error {
	vars := ev.vars()
	msg := render(cmp(a.Message, defaultMessage), vars, nil)
	title := render("{{user}} {{verb}} {{place}}", vars, nil)
	switch a.Type {
	case "webhook":
		method := cmp(a.Method, http.MethodPost)
		body := ""
		switch {
		case method == http.MethodGet:
		case strings.TrimSpace(a.Body) == "":
			b, _ := json.Marshal(map[string]any{"event": ev.Event, "user": ev.User.Name, "place": ev.Place.Name,
				"lat": ev.Point.Lat, "lon": ev.Point.Lon, "ts": ev.Point.TS, "map_url": vars["map_url"]})
			body = string(b)
		case strings.HasPrefix(strings.TrimSpace(a.Body), "{") || strings.HasPrefix(strings.TrimSpace(a.Body), "["):
			body = render(a.Body, vars, jsonEscape)
		default:
			body = render(a.Body, vars, nil)
		}
		headers := map[string]string{}
		for k, v := range a.Headers {
			headers[k] = render(v, vars, nil)
		}
		return notify.Do(ctx, method, render(a.URL, vars, url.QueryEscape), headers, body)
	case "notify_me":
		raw, err := s.db.UserNotify(ctx, ev.User.ID)
		if err != nil {
			return err
		}
		res := s.deliver(ctx, ev.User, parsePrefs(raw), title, msg, "")
		if len(res) == 0 {
			return errors.New("no notification channel is on (Settings → Notifications)")
		}
		for ch, r := range res {
			if r != "ok" {
				return fmt.Errorf("%s: %s", ch, r)
			}
		}
		return nil
	case "notify_family":
		people, err := s.db.FamilyPeople(ctx, ev.User.ID)
		if err != nil {
			return err
		}
		n := 0
		for _, m := range people { // re-checked on every run: someone who left the group gets nothing
			if slices.Contains(a.Members, m.UserID) {
				s.notifyUser(m.UserID, "family", title, msg)
				n++
			}
		}
		if n == 0 {
			return errors.New("none of the chosen people share a group with you any more")
		}
		return nil
	case "ntfy":
		return notify.SendNtfy(ctx, a.URL, a.Topic, a.Token, title, msg, 0)
	case "telegram":
		return notify.SendTelegram(ctx, a.Token, a.ChatID, msg)
	case "discord":
		return notify.SendDiscord(ctx, a.URL, msg)
	case "slack":
		return notify.SendSlack(ctx, a.URL, msg)
	case "email":
		st, err := s.db.Settings(ctx)
		if err != nil {
			return err
		}
		return notify.SendEmail(ctx, s.smtpConfig(st), a.To, title, msg+"\n\n— GeoTracker")
	}
	return errors.New("unknown action")
}

func cmp(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func (s *Server) execute(ctx context.Context, a *store.Automation, ev autoEvent) []actionResult {
	var actions []autoAction
	json.Unmarshal(a.Actions, &actions)
	out := make([]actionResult, 0, len(actions))
	for _, act := range actions {
		actx, cancel := context.WithTimeout(ctx, 20*time.Second)
		err := s.runAction(actx, act, ev)
		cancel()
		r := actionResult{Type: act.Type, OK: err == nil}
		if err != nil {
			r.Error = err.Error()
			slog.Warn("automation action failed", "automation", a.ID, "type", act.Type, "err", err)
		}
		out = append(out, r)
	}
	return out
}

func summarize(rs []actionResult) string {
	parts := make([]string, 0, len(rs))
	for _, r := range rs {
		if r.OK {
			parts = append(parts, r.Type+": ok")
		} else {
			parts = append(parts, r.Type+": "+r.Error)
		}
	}
	out := strings.Join(parts, "; ")
	if len(out) > 400 {
		out = out[:400] + "…"
	}
	return out
}

// runAutomations fires the user's rules for a place transition, in the background.
func (s *Server) runAutomations(u *store.User, pl store.Place, event string, p geo.Point) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		list, err := s.db.EnabledAutomationsAt(ctx, u.ID, pl.ID)
		if err != nil {
			return
		}
		ev := autoEvent{Event: event, User: u, Place: pl, Point: p}
		for i := range list {
			a := &list[i]
			if (event == "arrive" && !a.OnArrive) || (event == "leave" && !a.OnLeave) {
				continue
			}
			// Cooldown only suppresses the same event repeating (GPS flapping at the edge),
			// so an arrive is never swallowed just because the matching leave fired a moment ago.
			if a.LastFiredAt != nil && a.LastEvent == event && time.Since(time.UnixMilli(*a.LastFiredAt)) < time.Duration(a.CooldownMin)*time.Minute {
				continue
			}
			if !s.logins.allow("automation:"+strconv.FormatInt(u.ID, 10), 60, time.Hour) {
				slog.Warn("automation rate limit reached", "user", u.ID)
				return
			}
			s.db.RecordAutomationRun(ctx, a.ID, event, summarize(s.execute(ctx, a, ev)))
		}
	}()
}

// ── Secrets: masked in API responses, kept when the mask comes back ──

func maskURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Path == "" || u.Path == "/") && u.RawQuery == "" {
		return raw
	}
	return u.Scheme + "://" + u.Host + "/" + secretMask
}

func hasURLSecret(t string) bool { return t == "webhook" || t == "discord" || t == "slack" }

func maskAction(a autoAction) autoAction {
	if a.Token != "" {
		a.Token = secretMask
	}
	if hasURLSecret(a.Type) {
		a.URL = maskURL(a.URL)
	}
	if len(a.Headers) > 0 {
		h := make(map[string]string, len(a.Headers))
		for k := range a.Headers {
			h[k] = secretMask
		}
		a.Headers = h
	}
	return a
}

// restoreSecrets swaps masked values from the browser back for the stored ones (matched by action id).
func restoreSecrets(in, old []autoAction) {
	for i := range in {
		var o *autoAction
		for j := range old {
			if old[j].ID != "" && old[j].ID == in[i].ID && old[j].Type == in[i].Type {
				o = &old[j]
			}
		}
		if o == nil {
			continue
		}
		if in[i].Token == secretMask {
			in[i].Token = o.Token
		}
		if hasURLSecret(in[i].Type) && strings.HasSuffix(in[i].URL, "/"+secretMask) {
			in[i].URL = o.URL
		}
		for k, v := range in[i].Headers {
			if v == secretMask {
				in[i].Headers[k] = o.Headers[k]
			}
		}
	}
}

// ── Validation ──────────────────────────────────────────────

func (s *Server) validateActions(ctx context.Context, u *store.User, actions []autoAction) string {
	if len(actions) == 0 || len(actions) > maxActions {
		return fmt.Sprintf("add between 1 and %d actions", maxActions)
	}
	people, err := s.db.FamilyPeople(ctx, u.ID)
	if err != nil {
		return "could not check your family groups"
	}
	seen := map[string]bool{}
	for i := range actions {
		a := &actions[i]
		a.Type, a.URL, a.Topic, a.Token, a.ChatID, a.To = strings.TrimSpace(a.Type), strings.TrimSpace(a.URL), strings.TrimSpace(a.Topic), strings.TrimSpace(a.Token), strings.TrimSpace(a.ChatID), strings.TrimSpace(a.To)
		if a.ID == "" || seen[a.ID] || len(a.ID) > 32 {
			a.ID = newToken()[:10]
		}
		seen[a.ID] = true
		if !slices.Contains(actionTypes, a.Type) {
			return "unknown action type"
		}
		if len(a.Message) > 500 || len(a.Body) > 4000 || len(a.URL) > 500 || len(a.Token) > 300 || len(a.Topic) > 100 || len(a.ChatID) > 60 {
			return "an action has a value that is too long"
		}
		needURL := a.Type == "webhook" || a.Type == "discord" || a.Type == "slack"
		if needURL || (a.Type == "ntfy" && a.URL != "") {
			if pu, err := url.Parse(strings.ReplaceAll(a.URL, "{{", "")); err != nil || (pu.Scheme != "https" && pu.Scheme != "http") || pu.Host == "" {
				return "enter a full http(s) address for the " + a.Type + " action"
			}
		}
		switch a.Type {
		case "webhook":
			a.Method = strings.ToUpper(cmp(strings.TrimSpace(a.Method), http.MethodPost))
			if !slices.Contains([]string{http.MethodGet, http.MethodPost, http.MethodPut}, a.Method) {
				return "webhook method must be GET, POST or PUT"
			}
			if len(a.Headers) > 10 {
				return "a webhook can have up to 10 headers"
			}
			for k, v := range a.Headers {
				if !headerName.MatchString(k) || len(v) > 300 || strings.ContainsAny(v, "\r\n") {
					return "invalid webhook header " + k
				}
			}
		case "ntfy":
			if a.Topic == "" || strings.ContainsAny(a.Topic, "/ ?#") {
				return "ntfy needs a topic name (letters, digits, - and _)"
			}
		case "telegram":
			if a.Token == "" || a.ChatID == "" {
				return "Telegram needs a bot token and a chat id"
			}
		case "email":
			to, ok := cleanEmail(a.To)
			if !ok {
				return "enter a valid email address for the email action"
			}
			a.To = to
		case "notify_family":
			a.Members = slices.DeleteFunc(slices.Compact(slices.Sorted(slices.Values(a.Members))), func(id int64) bool {
				return !slices.ContainsFunc(people, func(m store.Member) bool { return m.UserID == id })
			})
			if len(a.Members) == 0 {
				return "choose at least one person from your family groups"
			}
		}
	}
	return ""
}

// ── Handlers ────────────────────────────────────────────────

type automationOut struct {
	store.Automation
	Actions []autoAction `json:"actions"`
}

func automationJSON(a store.Automation) automationOut {
	var actions []autoAction
	json.Unmarshal(a.Actions, &actions)
	for i := range actions {
		actions[i] = maskAction(actions[i])
	}
	if actions == nil {
		actions = []autoAction{}
	}
	return automationOut{Automation: a, Actions: actions}
}

func (s *Server) listAutomations(w http.ResponseWriter, r *http.Request, u *store.User) {
	list, err := s.db.Automations(r.Context(), u.ID)
	if err != nil {
		internal(w, r, err)
		return
	}
	out := make([]automationOut, len(list))
	for i := range list {
		out[i] = automationJSON(list[i])
	}
	writeJSON(w, http.StatusOK, out)
}

type automationReq struct {
	Name        string       `json:"name"`
	PlaceID     int64        `json:"place_id"`
	OnArrive    bool         `json:"on_arrive"`
	OnLeave     bool         `json:"on_leave"`
	Enabled     bool         `json:"enabled"`
	CooldownMin int          `json:"cooldown_min"`
	Actions     []autoAction `json:"actions"`
}

func (s *Server) saveAutomation(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req automationReq
	if !decode(w, r, &req) {
		return
	}
	ctx := r.Context()
	id := pathID(r) // 0 when creating
	req.Name = strings.TrimSpace(req.Name)
	switch {
	case req.Name == "" || len(req.Name) > 80:
		fail(w, http.StatusBadRequest, "give the automation a name (up to 80 characters)")
		return
	case !req.OnArrive && !req.OnLeave:
		fail(w, http.StatusBadRequest, "choose when it runs: on arrive, on leave or both")
		return
	case req.CooldownMin < 1 || req.CooldownMin > 1440:
		fail(w, http.StatusBadRequest, "the pause between runs must be 1 minute to 24 hours")
		return
	}
	if _, err := s.db.PlaceByID(ctx, u.ID, req.PlaceID); err != nil {
		fail(w, http.StatusBadRequest, "choose one of your saved places")
		return
	}
	var existing *store.Automation
	if id != 0 {
		var err error
		if existing, err = s.db.AutomationByID(ctx, u.ID, id); errors.Is(err, store.ErrNotFound) {
			fail(w, http.StatusNotFound, "automation not found")
			return
		} else if err != nil {
			internal(w, r, err)
			return
		}
		var old []autoAction
		json.Unmarshal(existing.Actions, &old)
		restoreSecrets(req.Actions, old)
	} else if n, _ := s.db.CountAutomations(ctx, u.ID); n >= maxAutomations {
		fail(w, http.StatusBadRequest, fmt.Sprintf("you can have up to %d automations", maxAutomations))
		return
	}
	if msg := s.validateActions(ctx, u, req.Actions); msg != "" {
		fail(w, http.StatusBadRequest, msg)
		return
	}
	raw, _ := json.Marshal(req.Actions)
	a := &store.Automation{ID: id, UserID: u.ID, PlaceID: req.PlaceID, Name: req.Name, OnArrive: req.OnArrive, OnLeave: req.OnLeave,
		Enabled: req.Enabled, CooldownMin: req.CooldownMin, Actions: raw}
	var err error
	status := http.StatusOK
	if id == 0 {
		status = http.StatusCreated
		err = s.db.CreateAutomation(ctx, a)
		s.audit(r, u.ID, "automation.create", req.Name)
	} else {
		err = s.db.UpdateAutomation(ctx, a)
		s.audit(r, u.ID, "automation.update", req.Name)
	}
	if err != nil {
		internal(w, r, err)
		return
	}
	saved, err := s.db.AutomationByID(ctx, u.ID, a.ID)
	if err != nil {
		internal(w, r, err)
		return
	}
	writeJSON(w, status, automationJSON(*saved))
}

func (s *Server) deleteAutomation(w http.ResponseWriter, r *http.Request, u *store.User) {
	if err := s.db.DeleteAutomation(r.Context(), u.ID, pathID(r)); err != nil {
		internal(w, r, err)
		return
	}
	s.audit(r, u.ID, "automation.delete", strconv.FormatInt(pathID(r), 10))
	w.WriteHeader(http.StatusNoContent)
}

// testAutomation runs the actions once with a sample event so people can check them
// without leaving the house. It does not touch the cooldown or last-run record.
func (s *Server) testAutomation(w http.ResponseWriter, r *http.Request, u *store.User) {
	if !s.logins.allow("automation-test:"+strconv.FormatInt(u.ID, 10), 20, 10*time.Minute) {
		fail(w, http.StatusTooManyRequests, "too many tests, wait a few minutes")
		return
	}
	a, err := s.db.AutomationByID(r.Context(), u.ID, pathID(r))
	if errors.Is(err, store.ErrNotFound) {
		fail(w, http.StatusNotFound, "automation not found")
		return
	}
	if err != nil {
		internal(w, r, err)
		return
	}
	pl, err := s.db.PlaceByID(r.Context(), u.ID, a.PlaceID)
	if err != nil {
		internal(w, r, err)
		return
	}
	event := "arrive"
	if !a.OnArrive {
		event = "leave"
	}
	ev := autoEvent{Event: event, User: u, Place: *pl, Point: geo.Point{TS: time.Now().UnixMilli(), Lat: pl.Lat, Lon: pl.Lon}}
	writeJSON(w, http.StatusOK, map[string]any{"event": event, "results": s.execute(r.Context(), a, ev)})
}
