package server

import (
	"net/http"
	"slices"
	"strconv"
	"time"

	"geotracker/internal/geo"
	"geotracker/internal/store"
)

// audit records security-relevant actions (spec §14). Never coordinates.
func (s *Server) audit(r *http.Request, actorID int64, action, target string) {
	s.db.Audit(r.Context(), actorID, action, target, s.clientIP(r))
}

func (s *Server) auditLog(w http.ResponseWriter, r *http.Request, _ *store.User) {
	before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
	entries, err := s.db.AuditLog(r.Context(), before, 100)
	if err != nil {
		internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

// ── Sessions ─────────────────────────────────────────────────

func (s *Server) listSessions(w http.ResponseWriter, r *http.Request, u *store.User) {
	var current []byte
	if c, err := r.Cookie(sessionCookie); err == nil {
		current = hashToken(c.Value)
	}
	ss, err := s.db.Sessions(r.Context(), u.ID, current)
	if err != nil {
		internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ss)
}

func (s *Server) revokeSession(w http.ResponseWriter, r *http.Request, u *store.User) {
	if err := s.db.RevokeSession(r.Context(), u.ID, r.PathValue("sid")); err != nil {
		fail(w, http.StatusNotFound, "session not found")
		return
	}
	s.audit(r, u.ID, "session.revoke", "")
	w.WriteHeader(http.StatusNoContent)
}

// ── Retention ────────────────────────────────────────────────

var retentionChoices = []int{0, 30, 90, 180, 365, 730, 1825}

func (s *Server) getRetention(w http.ResponseWriter, r *http.Request, u *store.User) {
	days, err := s.db.Retention(r.Context(), u.ID)
	if err != nil {
		internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"days": days})
}

func (s *Server) putRetention(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct{ Days int }
	if !decode(w, r, &req) {
		return
	}
	if !slices.Contains(retentionChoices, req.Days) {
		fail(w, http.StatusBadRequest, "choose one of the offered retention periods")
		return
	}
	if err := s.db.SetRetention(r.Context(), u.ID, req.Days); err != nil {
		internal(w, r, err)
		return
	}
	s.audit(r, u.ID, "retention.set", strconv.Itoa(req.Days)+" days")
	writeJSON(w, http.StatusOK, map[string]int{"days": req.Days})
}

// ── Timeline corrections ─────────────────────────────────────

var tripModes = []string{"walk", "cycle", "drive", "train", "flight", "unknown"}

func (s *Server) setTripMode(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		Start int64  `json:"start"`
		Mode  string `json:"mode"`
	}
	if !decode(w, r, &req) {
		return
	}
	if !slices.Contains(tripModes, req.Mode) || req.Start <= 0 {
		fail(w, http.StatusBadRequest, "unknown transport mode")
		return
	}
	if err := s.db.SetTripMode(r.Context(), u.ID, req.Start, req.Mode); err != nil {
		internal(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── "Send my location now" from the browser ──────────────────

func (s *Server) postPoint(w http.ResponseWriter, r *http.Request, u *store.User) {
	var p geo.Point
	if !decode(w, r, &p) {
		return
	}
	if p.TS == 0 {
		p.TS = time.Now().UnixMilli()
	}
	if !p.Valid() {
		fail(w, http.StatusBadRequest, "invalid location")
		return
	}
	if _, err := s.db.InsertPoints(r.Context(), u.ID, 0, 0, []geo.Point{p}); err != nil {
		internal(w, r, err)
		return
	}
	s.hub.Publish(u.ID, "point", map[string]any{"device_id": 0, "point": p})
	s.checkPointEvents(r.Context(), u.ID, 0, p)
	s.wake(s.wakeTimeline)
	writeJSON(w, http.StatusCreated, p)
}
