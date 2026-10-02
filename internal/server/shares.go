package server

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"geotracker/internal/geo"
	"geotracker/internal/store"
)

// Share links let anyone with the link see a live position or a recorded period,
// without an account. Links always expire; "approx" rounds positions to ~1 km.

func (s *Server) listShares(w http.ResponseWriter, r *http.Request, u *store.User) {
	sh, err := s.db.Shares(r.Context(), u.ID)
	if err != nil {
		internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, sh)
}

func (s *Server) createShare(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		Name       string `json:"name"`
		Kind       string `json:"kind"`
		From       *int64 `json:"from"`
		To         *int64 `json:"to"`
		Precision  string `json:"precision"`
		ExpiresInH int    `json:"expires_in_hours"`
	}
	if !decode(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	switch {
	case req.Name == "" || len(req.Name) > 80:
		fail(w, http.StatusBadRequest, "give the link a name (up to 80 characters)")
		return
	case req.Kind != "live" && req.Kind != "range":
		fail(w, http.StatusBadRequest, "kind must be live or range")
		return
	case req.Kind == "range" && (req.From == nil || req.To == nil || *req.From >= *req.To):
		fail(w, http.StatusBadRequest, "choose the period to share")
		return
	case req.ExpiresInH < 1 || req.ExpiresInH > 24*90:
		fail(w, http.StatusBadRequest, "links expire after 1 hour to 90 days")
		return
	}
	if req.Precision != "approx" {
		req.Precision = "exact"
	}
	if req.Kind == "live" {
		req.From, req.To = nil, nil
	}
	tok := newToken()
	sh := &store.Share{UserID: u.ID, Name: req.Name, Kind: req.Kind, From: req.From, To: req.To, Precision: req.Precision,
		ExpiresAt: time.Now().Add(time.Duration(req.ExpiresInH) * time.Hour).UnixMilli()}
	if err := s.db.CreateShare(r.Context(), sh, hashToken(tok)); err != nil {
		internal(w, r, err)
		return
	}
	s.audit(r, u.ID, "share.create", sh.Kind+": "+sh.Name)
	writeJSON(w, http.StatusCreated, map[string]any{"share": sh, "url": s.baseURL(r) + "/s/" + tok})
}

func (s *Server) deleteShare(w http.ResponseWriter, r *http.Request, u *store.User) {
	if err := s.db.DeleteShare(r.Context(), u.ID, pathID(r)); err != nil {
		internal(w, r, err)
		return
	}
	s.audit(r, u.ID, "share.revoke", strconv.FormatInt(pathID(r), 10))
	w.WriteHeader(http.StatusNoContent)
}

// coarsen rounds to 2 decimals (~1.1 km) for "approximate" links.
func coarsen(v float64) float64 { return math.Round(v*100) / 100 }

// publicShare is the only unauthenticated read endpoint. It returns just what the link grants.
func (s *Server) publicShare(w http.ResponseWriter, r *http.Request) {
	if !s.logins.allow("share:"+s.clientIP(r), 120, time.Minute) {
		fail(w, http.StatusTooManyRequests, "too many requests")
		return
	}
	// The page refreshes live links with ?poll=1; only the first load counts as a view.
	sh, err := s.db.ShareByToken(r.Context(), hashToken(r.PathValue("token")), r.URL.Query().Get("poll") != "1")
	if errors.Is(err, store.ErrNotFound) {
		fail(w, http.StatusNotFound, "this link has expired or was revoked")
		return
	}
	if err != nil {
		internal(w, r, err)
		return
	}
	owner, err := s.db.UserByID(r.Context(), sh.UserID)
	if err != nil || owner.Disabled {
		fail(w, http.StatusNotFound, "this link has expired or was revoked")
		return
	}
	approx := sh.Precision == "approx"
	zones, err := s.privateZones(r.Context(), sh.UserID)
	if err != nil {
		internal(w, r, err)
		return
	}
	hidden := func(lat, lon float64) bool { return matchPlace(zones, lat, lon) != nil }
	pt := func(p geo.Point) []any {
		if approx {
			return []any{p.TS, coarsen(p.Lat), coarsen(p.Lon), nil}
		}
		return []any{p.TS, p.Lat, p.Lon, p.Accuracy}
	}
	from, to := time.Now().Add(-2*time.Hour).UnixMilli(), time.Now().UnixMilli() // live: a short trail
	if sh.Kind == "range" {
		from, to = *sh.From, *sh.To
	}
	var rows [][]any
	total, _ := s.db.CountPoints(r.Context(), sh.UserID, from, to)
	step := max(1, int(math.Ceil(float64(total)/20_000)))
	i := 0
	err = s.db.ForEachPoint(r.Context(), sh.UserID, from, to, 100, func(p geo.Point) error {
		if hidden(p.Lat, p.Lon) {
			return nil
		}
		if i++; (i-1)%step == 0 {
			rows = append(rows, pt(p))
		}
		return nil
	})
	if err != nil {
		internal(w, r, err)
		return
	}
	out := map[string]any{
		"name": sh.Name, "owner": strings.Fields(owner.Name + " ")[0], "kind": sh.Kind, "precision": sh.Precision,
		"expires_at": sh.ExpiresAt, "from": sh.From, "to": sh.To, "points": rows,
	}
	if sh.Kind == "live" {
		if p, err := s.db.LatestPoint(r.Context(), sh.UserID); err == nil && p != nil && !hidden(p.Lat, p.Lon) {
			out["latest"] = pt(*p)
		}
	} else {
		visits, _, err := s.db.Timeline(r.Context(), sh.UserID, from, to)
		if err == nil {
			vs := make([]map[string]any, 0, len(visits))
			for _, v := range visits {
				if hidden(v.Lat, v.Lon) {
					continue
				}
				name := v.Name
				lat, lon := v.Lat, v.Lon
				if approx {
					name, lat, lon = v.City, coarsen(lat), coarsen(lon)
				}
				vs = append(vs, map[string]any{"start": v.Start, "end": v.End, "lat": lat, "lon": lon, "name": name, "city": v.City})
			}
			out["visits"] = vs
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex")
	b, _ := json.Marshal(out)
	w.Header().Set("Content-Type", "application/json")
	w.Write(b)
}
