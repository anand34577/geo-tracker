package server

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"geotracker/internal/store"
)

// Native apps (e.g. a future GeoTracker Android app) sign in once and then use a bearer
// token for both the REST API and location upload. The token belongs to a device entry,
// so users see and revoke app sessions under Settings → Devices like any phone.

// bearerUser authenticates `Authorization: Bearer <token>` for the REST API.
// Device (ingest-only) tokens are refused: a lost tracker phone must not read history.
func (s *Server) bearerUser(r *http.Request) (*store.User, int, string) {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return nil, 0, ""
	}
	uid, _, scopes, err := s.db.TokenLookup(r.Context(), hashToken(strings.TrimSpace(h[7:])))
	if err != nil {
		return nil, http.StatusUnauthorized, "invalid or revoked token"
	}
	need := "write"
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		need = "read"
	}
	if !strings.Contains(scopes, need) {
		return nil, http.StatusForbidden, "this token may not " + need + " (device tokens can only upload locations)"
	}
	u, err := s.db.UserByID(r.Context(), uid)
	if err != nil || u.Disabled {
		return nil, http.StatusUnauthorized, "invalid or revoked token"
	}
	return u, 0, ""
}

// appLogin exchanges email + password for an app token (scopes read, write, ingest).
func (s *Server) appLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email      string `json:"email"`
		Password   string `json:"password"`
		DeviceName string `json:"device_name"`
	}
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
		s.audit(r, 0, "login.failed", email+" (app)")
		fail(w, http.StatusUnauthorized, "wrong email or password")
		return
	}
	name := strings.TrimSpace(req.DeviceName)
	if name == "" || len(name) > 60 {
		name = "App"
	}
	tok := newToken()
	d, err := s.db.CreateDeviceWithScopes(r.Context(), u.ID, name, "app", hashToken(tok), "read,write,ingest")
	if err != nil {
		internal(w, r, err)
		return
	}
	s.audit(r, u.ID, "login", "app: "+name)
	writeJSON(w, http.StatusCreated, map[string]any{"token": tok, "user": u, "device": d})
}

// ── Geocoding ────────────────────────────────────────────────

func (s *Server) geocodeSearch(w http.ResponseWriter, r *http.Request, u *store.User) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) < 3 || len(q) > 200 {
		fail(w, http.StatusBadRequest, "type at least 3 characters")
		return
	}
	// Public geocoders allow ~1 request/second; keep each user well inside that.
	if !s.logins.allow("geocode:"+strconv.FormatInt(u.ID, 10), 30, time.Minute) {
		fail(w, http.StatusTooManyRequests, "searching too fast, wait a moment")
		return
	}
	st, err := s.db.Settings(r.Context())
	if err != nil {
		internal(w, r, err)
		return
	}
	lang := r.Header.Get("Accept-Language")
	hits, err := s.geo.Search(r.Context(), st["geocoder"], st["geocoder_url"], q, lang)
	if err != nil {
		fail(w, http.StatusBadGateway, "address search failed: "+err.Error())
		return
	}
	if hits == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	writeJSON(w, http.StatusOK, hits)
}

// geocodeReverse names a coordinate, using the shared cache first.
func (s *Server) geocodeReverse(w http.ResponseWriter, r *http.Request, u *store.User) {
	lat, e1 := strconv.ParseFloat(r.URL.Query().Get("lat"), 64)
	lon, e2 := strconv.ParseFloat(r.URL.Query().Get("lon"), 64)
	if e1 != nil || e2 != nil || lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		fail(w, http.StatusBadRequest, "invalid coordinates")
		return
	}
	st, err := s.db.Settings(r.Context())
	if err != nil {
		internal(w, r, err)
		return
	}
	if st["geocoder"] == "none" {
		writeJSON(w, http.StatusOK, map[string]string{})
		return
	}
	if !s.logins.allow("geocode:"+strconv.FormatInt(u.ID, 10), 30, time.Minute) {
		fail(w, http.StatusTooManyRequests, "too many lookups, wait a moment")
		return
	}
	res, err := s.geo.Reverse(r.Context(), st["geocoder"], st["geocoder_url"], lat, lon)
	if err != nil {
		fail(w, http.StatusBadGateway, "lookup failed: "+err.Error())
		return
	}
	s.db.InsertGeocode(r.Context(), store.Geocode{Lat: lat, Lon: lon, Provider: st["geocoder"], Name: res.Name, Address: res.Address, City: res.City, CountryCode: res.CountryCode})
	writeJSON(w, http.StatusOK, map[string]string{"name": res.Name, "address": res.Address, "city": res.City, "country": res.CountryCode})
}
