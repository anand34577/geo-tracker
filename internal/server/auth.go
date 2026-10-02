package server

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"

	"geotracker/internal/store"
)

const (
	sessionCookie = "gt_session"
	sessionTTL    = 30 * 24 * time.Hour
	minPassword   = 10
)

// ── Passwords (argon2id, OWASP minimum parameters) ───────────

const argonMem, argonTime, argonThreads = 19 * 1024, 2, 1

var b64 = base64.RawStdEncoding

func HashPassword(pw string) string {
	salt := make([]byte, 16)
	rand.Read(salt)
	key := argon2.IDKey([]byte(pw), salt, argonTime, argonMem, argonThreads, 32)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", argonMem, argonTime, argonThreads, b64.EncodeToString(salt), b64.EncodeToString(key))
}

func checkPassword(hash, pw string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false
	}
	salt, err1 := b64.DecodeString(parts[4])
	key, err2 := b64.DecodeString(parts[5])
	if err1 != nil || err2 != nil {
		return false
	}
	return subtle.ConstantTimeCompare(argon2.IDKey([]byte(pw), salt, t, m, p, uint32(len(key))), key) == 1
}

// dummyHash keeps login timing equal for unknown emails.
var dummyHash = HashPassword("timing-equalizer")

// ── Tokens ───────────────────────────────────────────────────

func newToken() string {
	b := make([]byte, 20)
	rand.Read(b)
	return hex.EncodeToString(b) // alphanumeric: safe as a Traccar device identifier
}

func hashToken(t string) []byte {
	h := sha256.Sum256([]byte(t))
	return h[:]
}

// ── Sessions ─────────────────────────────────────────────────

type handler func(http.ResponseWriter, *http.Request, *store.User)

func (s *Server) user(h handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if u, code, msg := s.bearerUser(r); u != nil { // native apps and scripts
			h(w, r, u)
			return
		} else if code != 0 {
			fail(w, code, msg)
			return
		}
		c, err := r.Cookie(sessionCookie)
		if err != nil {
			fail(w, http.StatusUnauthorized, "not signed in")
			return
		}
		hash := hashToken(c.Value)
		u, exp, err := s.db.SessionUser(r.Context(), hash)
		if err != nil {
			fail(w, http.StatusUnauthorized, "not signed in")
			return
		}
		// Sliding expiry, written at most once a day.
		if newExp := time.Now().Add(sessionTTL); newExp.UnixMilli()-exp > int64(24*time.Hour/time.Millisecond) {
			s.db.TouchSession(r.Context(), hash, newExp.UnixMilli())
			s.setSessionCookie(w, r, c.Value, newExp)
		}
		h(w, r, u)
	}
}

func (s *Server) admin(h handler) http.HandlerFunc {
	return s.user(func(w http.ResponseWriter, r *http.Request, u *store.User) {
		if u.Role != "admin" {
			fail(w, http.StatusForbidden, "admins only")
			return
		}
		h(w, r, u)
	})
}

func (s *Server) setSessionCookie(w http.ResponseWriter, r *http.Request, value string, exp time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: value, Path: "/", Expires: exp,
		HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: isHTTPS(r) || strings.HasPrefix(s.cfg.BaseURL, "https://"),
	})
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, u *store.User) error {
	tok := newToken() + newToken()
	exp := time.Now().Add(sessionTTL)
	if err := s.db.CreateSession(r.Context(), hashToken(tok), u.ID, exp.UnixMilli(), r.UserAgent(), s.clientIP(r)); err != nil {
		return err
	}
	s.setSessionCookie(w, r, tok, exp)
	return nil
}

// ── Rate limiting ────────────────────────────────────────────

// limiter counts attempts per key in a sliding window.
// ponytail: in-memory, per process; fine for a single self-hosted instance.
type limiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

func newLimiter() *limiter { return &limiter{hits: map[string][]time.Time{}} }

func (l *limiter) allow(key string, max int, window time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := time.Now().Add(-window)
	if len(l.hits) > 10_000 {
		l.hits = map[string][]time.Time{} // crude memory bound under attack
	}
	recent := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}
	if len(recent) >= max {
		l.hits[key] = recent
		return false
	}
	l.hits[key] = append(recent, time.Now())
	return true
}

// ── Validation ───────────────────────────────────────────────

func cleanEmail(e string) (string, bool) {
	e = strings.ToLower(strings.TrimSpace(e))
	a, err := mail.ParseAddress(e)
	return e, err == nil && a.Address == e
}

func validateAccount(email, name, password string, requirePassword bool) (string, string) {
	email, ok := cleanEmail(email)
	switch {
	case !ok:
		return "", "enter a valid email address"
	case strings.TrimSpace(name) == "" || len(name) > 80:
		return "", "enter a name (up to 80 characters)"
	case (requirePassword || password != "") && len(password) < minPassword:
		return "", fmt.Sprintf("password must be at least %d characters", minPassword)
	}
	return email, ""
}

// ── Handlers ─────────────────────────────────────────────────

func (s *Server) getSetup(w http.ResponseWriter, r *http.Request) {
	n, err := s.db.CountUsers(r.Context())
	if err != nil {
		internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"needs_setup": n == 0})
}

type accountReq struct {
	Email    string `json:"email"`
	Name     string `json:"name"`
	Password string `json:"password"`
}

func (s *Server) postSetup(w http.ResponseWriter, r *http.Request) {
	var req accountReq
	if !decode(w, r, &req) {
		return
	}
	email, msg := validateAccount(req.Email, req.Name, req.Password, true)
	if msg != "" {
		fail(w, http.StatusBadRequest, msg)
		return
	}
	u, err := s.db.CreateFirstAdmin(r.Context(), email, strings.TrimSpace(req.Name), HashPassword(req.Password))
	if errors.Is(err, store.ErrExists) {
		fail(w, http.StatusConflict, "setup is already complete")
		return
	}
	if err != nil {
		internal(w, r, err)
		return
	}
	if err := s.startSession(w, r, u); err != nil {
		internal(w, r, err)
		return
	}
	s.audit(r, u.ID, "setup", email)
	writeJSON(w, http.StatusCreated, u)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var req accountReq
	if !decode(w, r, &req) {
		return
	}
	email, _ := cleanEmail(req.Email)
	if !s.logins.allow("ip:"+s.clientIP(r), 20, 15*time.Minute) || !s.logins.allow("email:"+email, 10, 15*time.Minute) {
		fail(w, http.StatusTooManyRequests, "too many attempts, try again in 15 minutes")
		return
	}
	u, err := s.db.UserByEmail(r.Context(), email)
	hash := dummyHash
	if err == nil {
		hash = u.PasswordHash
	}
	if !checkPassword(hash, req.Password) || err != nil || u.Disabled {
		s.audit(r, 0, "login.failed", email)
		fail(w, http.StatusUnauthorized, "wrong email or password")
		return
	}
	if err := s.startSession(w, r, u); err != nil {
		internal(w, r, err)
		return
	}
	s.audit(r, u.ID, "login", "password")
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.db.DeleteSession(r.Context(), hashToken(c.Value))
	}
	s.setSessionCookie(w, r, "", time.Unix(0, 0))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getMe(w http.ResponseWriter, r *http.Request, u *store.User) {
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) patchMe(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		Name  *string         `json:"name"`
		Email *string         `json:"email"`
		// Password is required to change the email of an account that has one: the email
		// is where password resets and sign-in linking point, so a stolen session must not move it.
		Password string          `json:"password"`
		Prefs    json.RawMessage `json:"prefs"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Name != nil {
		u.Name = strings.TrimSpace(*req.Name)
	}
	if req.Email != nil {
		if newEmail, _ := cleanEmail(*req.Email); newEmail != u.Email && u.PasswordHash != "!" && !checkPassword(u.PasswordHash, req.Password) {
			fail(w, http.StatusForbidden, "enter your current password to change your email")
			return
		}
		u.Email = *req.Email
	}
	email, msg := validateAccount(u.Email, u.Name, "", false)
	if msg != "" {
		fail(w, http.StatusBadRequest, msg)
		return
	}
	u.Email = email
	if req.Prefs != nil {
		var obj map[string]any
		if json.Unmarshal(req.Prefs, &obj) != nil || len(req.Prefs) > 4096 {
			fail(w, http.StatusBadRequest, "prefs must be a small JSON object")
			return
		}
		u.Prefs = req.Prefs
	}
	if err := s.db.UpdateUser(r.Context(), u); errors.Is(err, store.ErrExists) {
		fail(w, http.StatusConflict, "that email is already in use")
		return
	} else if err != nil {
		internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if !decode(w, r, &req) {
		return
	}
	if !checkPassword(u.PasswordHash, req.Current) {
		fail(w, http.StatusBadRequest, "current password is wrong")
		return
	}
	if len(req.New) < minPassword {
		fail(w, http.StatusBadRequest, fmt.Sprintf("password must be at least %d characters", minPassword))
		return
	}
	var keep []byte
	if c, err := r.Cookie(sessionCookie); err == nil {
		keep = hashToken(c.Value)
	}
	if err := s.db.SetPassword(r.Context(), u.ID, HashPassword(req.New), keep); err != nil {
		internal(w, r, err)
		return
	}
	s.audit(r, u.ID, "password.change", "")
	w.WriteHeader(http.StatusNoContent)
}
