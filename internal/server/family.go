package server

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"geotracker/internal/geo"
	"geotracker/internal/store"
)

// Family groups: people who share their location with each other. Consent-first (ADR-015):
// an invited person shares nothing until they accept and choose what to share, and they
// can always see who sees them, pause, or leave.

func (s *Server) listGroups(w http.ResponseWriter, r *http.Request, u *store.User) {
	gs, err := s.db.GroupsFor(r.Context(), u.ID)
	if err != nil {
		internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, gs)
}

func groupName(w http.ResponseWriter, name string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 60 {
		fail(w, http.StatusBadRequest, "give the group a name (up to 60 characters)")
		return "", false
	}
	return name, true
}

func (s *Server) createGroup(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct{ Name string }
	if !decode(w, r, &req) {
		return
	}
	name, ok := groupName(w, req.Name)
	if !ok {
		return
	}
	id, err := s.db.CreateGroup(r.Context(), name, u.ID)
	if err != nil {
		internal(w, r, err)
		return
	}
	s.audit(r, u.ID, "group.create", name)
	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
}

// requireMember loads the caller's membership; owner=true also requires ownership.
func (s *Server) requireMember(w http.ResponseWriter, r *http.Request, u *store.User, owner bool) (*store.Member, bool) {
	m, err := s.db.Membership(r.Context(), pathID(r), u.ID)
	if errors.Is(err, store.ErrNotFound) || (err == nil && owner && m.Role != "owner") {
		fail(w, http.StatusForbidden, "only the group owner can do that")
		return nil, false
	}
	if err != nil {
		internal(w, r, err)
		return nil, false
	}
	return m, true
}

func (s *Server) renameGroup(w http.ResponseWriter, r *http.Request, u *store.User) {
	if _, ok := s.requireMember(w, r, u, true); !ok {
		return
	}
	var req struct{ Name string }
	if !decode(w, r, &req) {
		return
	}
	name, ok := groupName(w, req.Name)
	if !ok {
		return
	}
	if err := s.db.RenameGroup(r.Context(), pathID(r), name); err != nil {
		internal(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteGroup(w http.ResponseWriter, r *http.Request, u *store.User) {
	if _, ok := s.requireMember(w, r, u, true); !ok {
		return
	}
	if err := s.db.DeleteGroup(r.Context(), pathID(r)); err != nil {
		internal(w, r, err)
		return
	}
	s.audit(r, u.ID, "group.delete", strconv.FormatInt(pathID(r), 10))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) inviteMember(w http.ResponseWriter, r *http.Request, u *store.User) {
	if _, ok := s.requireMember(w, r, u, true); !ok {
		return
	}
	var req struct{ Email string }
	if !decode(w, r, &req) {
		return
	}
	email, _ := cleanEmail(req.Email)
	target, err := s.db.UserByEmail(r.Context(), email)
	if errors.Is(err, store.ErrNotFound) || (err == nil && target.Disabled) {
		fail(w, http.StatusNotFound, "there is no account with that email on this server; ask your admin to create one first")
		return
	}
	if err != nil {
		internal(w, r, err)
		return
	}
	if err := s.db.InviteMember(r.Context(), pathID(r), target.ID, u.ID); errors.Is(err, store.ErrExists) {
		fail(w, http.StatusConflict, target.Name+" is already in this group")
		return
	} else if err != nil {
		internal(w, r, err)
		return
	}
	s.audit(r, u.ID, "group.invite", email)
	s.hub.Publish(target.ID, "family", map[string]string{"reason": "invited"})
	s.notifyUser(target.ID, "family", u.Name+" invited you to a family group", "Open GeoTracker → Family to accept or decline. Nothing is shared until you accept.")
	w.WriteHeader(http.StatusNoContent)
}

// updateMySharing accepts an invitation and/or changes what the caller shares with a group.
func (s *Server) updateMySharing(w http.ResponseWriter, r *http.Request, u *store.User) {
	if _, ok := s.requireMember(w, r, u, false); !ok {
		return
	}
	var m store.Member
	if !decode(w, r, &m) {
		return
	}
	switch m.ShareHistoryD {
	case -1, 0, 1, 7, 30, 365:
	default:
		fail(w, http.StatusBadRequest, "history must be none, 1, 7, 30 or 365 days, or all")
		return
	}
	if m.Precision != "approx" {
		m.Precision = "exact"
	}
	if m.PausedUntil != nil && *m.PausedUntil <= time.Now().UnixMilli() {
		m.PausedUntil = nil
	}
	if err := s.db.UpdateSharing(r.Context(), pathID(r), u.ID, m); err != nil {
		internal(w, r, err)
		return
	}
	s.audit(r, u.ID, "group.sharing", fmt.Sprintf("group %d: live=%v history=%dd %s", pathID(r), m.ShareLive, m.ShareHistoryD, m.Precision))
	s.publishFamily(r.Context(), u.ID)
	w.WriteHeader(http.StatusNoContent)
}

// removeMember: anyone may leave (or decline); owners may remove others.
func (s *Server) removeMember(w http.ResponseWriter, r *http.Request, u *store.User) {
	target, _ := strconv.ParseInt(r.PathValue("uid"), 10, 64)
	if _, ok := s.requireMember(w, r, u, target != u.ID); !ok {
		return
	}
	viewers, _ := s.db.LiveViewers(r.Context(), target)
	if err := s.db.RemoveMember(r.Context(), pathID(r), target); err != nil {
		internal(w, r, err)
		return
	}
	s.audit(r, u.ID, "group.remove", fmt.Sprintf("group %d user %d", pathID(r), target))
	for _, v := range append(viewers, target) {
		s.hub.Publish(v, "family", map[string]string{"reason": "membership"})
	}
	w.WriteHeader(http.StatusNoContent)
}

// publishFamily tells everyone who can see a person that something changed; they refetch
// through the access checks, so the event itself carries no location.
func (s *Server) publishFamily(ctx context.Context, subject int64) {
	viewers, err := s.db.LiveViewers(ctx, subject)
	if err != nil {
		return
	}
	for _, v := range viewers {
		s.hub.Publish(v, "family", map[string]int64{"user_id": subject})
	}
	s.hub.Publish(subject, "family", map[string]int64{"user_id": subject})
}

type familyPerson struct {
	UserID      int64      `json:"user_id"`
	Name        string     `json:"name"`
	Color       string     `json:"color"`
	Live        bool       `json:"live"`
	Approx      bool       `json:"approx"`
	HistoryFrom *int64     `json:"history_from"` // null = no history shared
	Paused      bool       `json:"paused"`
	Point       *geo.Point `json:"point"`
	Place       string     `json:"place,omitempty"`
}

// family lists everyone the caller shares a group with, with what each currently shares.
func (s *Server) family(w http.ResponseWriter, r *http.Request, u *store.User) {
	people, err := s.db.FamilyPeople(r.Context(), u.ID)
	if err != nil {
		internal(w, r, err)
		return
	}
	now := time.Now().UnixMilli()
	out := make([]familyPerson, 0, len(people))
	for _, m := range people {
		acc, err := s.db.AccessTo(r.Context(), u.ID, m.UserID)
		if err != nil {
			internal(w, r, err)
			return
		}
		fp := familyPerson{UserID: m.UserID, Name: m.Name, Color: m.Color, Live: acc.Live, Approx: acc.Approx,
			Paused: m.PausedUntil != nil && *m.PausedUntil > now}
		if acc.From != math.MaxInt64 {
			from := acc.From
			fp.HistoryFrom = &from
		}
		zones, err := s.privateZones(r.Context(), m.UserID)
		if err != nil {
			internal(w, r, err)
			return
		}
		if acc.Live {
			if p, err := s.db.LatestPoint(r.Context(), m.UserID); err == nil && p != nil && matchPlace(zones, p.Lat, p.Lon) == nil {
				fp.Point = maskPoint(*p, acc.Approx)
				// Where they are now: their latest visit if it is still going on.
				if vs, _, err := s.db.Timeline(r.Context(), m.UserID, now-6*3_600_000, now); err == nil && len(vs) > 0 {
					if v := vs[len(vs)-1]; p.TS-v.End < 15*60_000 {
						fp.Place = v.Name
						if acc.Approx {
							fp.Place = v.City
						}
					}
				}
			}
		}
		out = append(out, fp)
	}
	writeJSON(w, http.StatusOK, out)
}

// maskPoint applies approximate precision: ~1 km rounding, no accuracy/speed/altitude.
func maskPoint(p geo.Point, approx bool) *geo.Point {
	if approx {
		p = geo.Point{TS: p.TS, Lat: coarsen(p.Lat), Lon: coarsen(p.Lon), Accuracy: geo.F(1000), Battery: p.Battery}
	}
	return &p
}

// view is what a read endpoint may show of one person's data.
type view struct {
	id, from, to int64
	approx       bool
	zones        []store.Place // the subject's privacy zones; nil when viewing yourself
}

// hidden reports whether a position falls in one of the subject's privacy zones.
func (v view) hidden(lat, lon float64) bool { return matchPlace(v.zones, lat, lon) != nil }

// privateZones lists a person's privacy zones: their location inside them is never shown
// to anyone else (spec §13.2).
func (s *Server) privateZones(ctx context.Context, userID int64) ([]store.Place, error) {
	places, err := s.db.Places(ctx, userID)
	if err != nil {
		return nil, err
	}
	zones := places[:0]
	for _, p := range places {
		if p.Private {
			zones = append(zones, p)
		}
	}
	return zones, nil
}

// subject resolves ?user= for read endpoints: yourself, or a family member within what
// they share. It narrows [from, to] to the visible history.
func (s *Server) subject(w http.ResponseWriter, r *http.Request, u *store.User, from, to int64) (view, bool) {
	q := r.URL.Query().Get("user")
	if q == "" || q == strconv.FormatInt(u.ID, 10) {
		return view{id: u.ID, from: from, to: to}, true
	}
	id, _ := strconv.ParseInt(q, 10, 64)
	acc, err := s.db.AccessTo(r.Context(), u.ID, id)
	if err != nil {
		internal(w, r, err)
		return view{}, false
	}
	if acc.From == math.MaxInt64 {
		fail(w, http.StatusForbidden, "this person doesn't share their history with you")
		return view{}, false
	}
	zones, err := s.privateZones(r.Context(), id)
	if err != nil {
		internal(w, r, err)
		return view{}, false
	}
	return view{id: id, from: max(from, acc.From), to: to, approx: acc.Approx, zones: zones}, true
}

// ── Alerts about family members ──────────────────────────────

func (s *Server) listAlerts(w http.ResponseWriter, r *http.Request, u *store.User) {
	as, err := s.db.AlertsByWatcher(r.Context(), u.ID)
	if err != nil {
		internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, as)
}

func (s *Server) createAlert(w http.ResponseWriter, r *http.Request, u *store.User) {
	var a store.AlertRule
	if !decode(w, r, &a) {
		return
	}
	acc, err := s.db.AccessTo(r.Context(), u.ID, a.SubjectID)
	if err != nil {
		internal(w, r, err)
		return
	}
	if a.SubjectID == u.ID || !acc.Live || acc.Approx {
		fail(w, http.StatusForbidden, "alerts need someone who shares their exact live location with you")
		return
	}
	if _, err := s.db.PlaceByID(r.Context(), u.ID, a.PlaceID); err != nil {
		fail(w, http.StatusBadRequest, "choose one of your saved places")
		return
	}
	if !a.OnArrive && !a.OnLeave {
		fail(w, http.StatusBadRequest, "choose arriving, leaving or both")
		return
	}
	a.WatcherID = u.ID
	if err := s.db.CreateAlert(r.Context(), &a); err != nil {
		internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, a)
}

func (s *Server) deleteAlert(w http.ResponseWriter, r *http.Request, u *store.User) {
	if err := s.db.DeleteAlert(r.Context(), u.ID, pathID(r)); err != nil {
		internal(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// checkFamilyAlerts evaluates watchers' arrive/leave rules for a subject's new fix.
func (s *Server) checkFamilyAlerts(ctx context.Context, subject *store.User, p geo.Point) {
	rules, err := s.db.AlertsBySubject(ctx, subject.ID)
	if err != nil || len(rules) == 0 {
		return
	}
	// Inside a privacy zone nobody learns anything, not even "left School".
	if zones, err := s.privateZones(ctx, subject.ID); err != nil || matchPlace(zones, p.Lat, p.Lon) != nil {
		return
	}
	margin := 25.0
	if p.Accuracy != nil {
		margin = math.Max(margin, *p.Accuracy)
	}
	for _, rule := range rules {
		acc, err := s.db.AccessTo(ctx, rule.WatcherID, subject.ID)
		if err != nil || !acc.Live || acc.Approx { // sharing changed since the rule was made
			continue
		}
		pl, err := s.db.PlaceByID(ctx, rule.WatcherID, rule.PlaceID)
		if err != nil {
			continue
		}
		d := geo.Distance(p.Lat, p.Lon, pl.Lat, pl.Lon)
		s.notes.mu.Lock()
		was, known := s.notes.rule[rule.ID]
		inside := d <= pl.Radius || (was && d <= pl.Radius+margin)
		s.notes.rule[rule.ID] = inside
		s.notes.mu.Unlock()
		if !known || inside == was {
			continue
		}
		if inside && rule.OnArrive {
			s.notifyUser(rule.WatcherID, "family", subject.Name+" arrived at "+pl.Name, subject.Name+" arrived at "+pl.Name+".")
		}
		if !inside && rule.OnLeave {
			s.notifyUser(rule.WatcherID, "family", subject.Name+" left "+pl.Name, subject.Name+" left "+pl.Name+".")
		}
	}
}
