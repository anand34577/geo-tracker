// Package server is the HTTP API, ingest endpoints, embedded web UI and background workers.
package server

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/netip"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"geotracker/internal/geo"
	"geotracker/internal/geocode"
	"geotracker/internal/maps"
	"geotracker/internal/store"
)

type Config struct {
	DataDir        string
	BaseURL        string // public URL, e.g. https://track.example.com
	Version        string
	TrustedProxies []netip.Prefix
	Web            fs.FS // built frontend
}

type Server struct {
	cfg          Config
	db           *store.Store
	hub          *Hub
	geo          *geocode.Client
	logins       *limiter
	wakeTimeline chan struct{}
	wakeImports  chan struct{}
	wakeExports  chan struct{}
	started      time.Time
	notes        *notifyState
	oidc         oidcCache
	mapMu        sync.Mutex
	mb           *maps.MBTiles // offline basemap, opened on demand

	// Restart receives a value when a staged restore needs the process restarted.
	Restart chan struct{}
}

func New(cfg Config, db *store.Store) *Server {
	return &Server{
		cfg:          cfg,
		db:           db,
		hub:          NewHub(),
		geo:          &geocode.Client{HTTP: &http.Client{Timeout: 15 * time.Second}, UserAgent: "GeoTracker/" + cfg.Version + " (self-hosted; " + cfg.BaseURL + ")"},
		logins:       newLimiter(),
		wakeTimeline: make(chan struct{}, 1),
		wakeImports:  make(chan struct{}, 1),
		wakeExports:  make(chan struct{}, 1),
		started:      time.Now(),
		notes:        newNotifyState(),
		Restart:      make(chan struct{}, 1),
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	handle := mux.HandleFunc

	handle("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := s.db.R.PingContext(r.Context()); err != nil {
			fail(w, http.StatusServiceUnavailable, "database unavailable")
			return
		}
		w.Write([]byte("ok"))
	})

	// Setup & auth
	handle("GET /api/v1/setup", s.getSetup)
	handle("POST /api/v1/setup", s.postSetup)
	handle("POST /api/v1/auth/login", s.login)
	handle("POST /api/v1/auth/logout", s.logout)
	handle("GET /api/v1/auth/methods", s.authMethods)
	handle("GET /api/v1/auth/oidc/login", s.oidcLogin)
	handle("GET /api/v1/auth/oidc/callback", s.oidcCallback)
	handle("POST /api/v1/auth/token", s.appLogin)
	handle("GET /api/v1/me/notifications", s.user(s.getNotify))
	handle("PUT /api/v1/me/notifications", s.user(s.putNotify))
	handle("POST /api/v1/me/notifications/test", s.user(s.testNotify))
	handle("GET /api/v1/me", s.user(s.getMe))
	handle("PATCH /api/v1/me", s.user(s.patchMe))
	handle("POST /api/v1/me/password", s.user(s.changePassword))
	handle("GET /api/v1/config", s.user(s.getConfig))

	// Location data
	handle("GET /api/v1/points", s.user(s.getPoints))
	handle("DELETE /api/v1/points", s.user(s.deletePoints))
	handle("GET /api/v1/points/latest", s.user(s.latestPoint))
	handle("GET /api/v1/stats", s.user(s.stats))
	handle("GET /api/v1/timeline", s.user(s.getTimeline))
	handle("POST /api/v1/timeline/rebuild", s.user(s.rebuildTimeline))
	handle("GET /api/v1/live", s.user(s.live))

	// Family
	handle("GET /api/v1/family", s.user(s.family))
	handle("GET /api/v1/groups", s.user(s.listGroups))
	handle("POST /api/v1/groups", s.user(s.createGroup))
	handle("PATCH /api/v1/groups/{id}", s.user(s.renameGroup))
	handle("DELETE /api/v1/groups/{id}", s.user(s.deleteGroup))
	handle("POST /api/v1/groups/{id}/members", s.user(s.inviteMember))
	handle("PUT /api/v1/groups/{id}/me", s.user(s.updateMySharing))
	handle("DELETE /api/v1/groups/{id}/members/{uid}", s.user(s.removeMember))
	handle("GET /api/v1/alerts", s.user(s.listAlerts))
	handle("POST /api/v1/alerts", s.user(s.createAlert))
	handle("DELETE /api/v1/alerts/{id}", s.user(s.deleteAlert))

	// Security, retention, corrections
	handle("GET /api/v1/me/sessions", s.user(s.listSessions))
	handle("DELETE /api/v1/me/sessions/{sid}", s.user(s.revokeSession))
	handle("GET /api/v1/me/retention", s.user(s.getRetention))
	handle("PUT /api/v1/me/retention", s.user(s.putRetention))
	handle("PUT /api/v1/trips/mode", s.user(s.setTripMode))
	handle("POST /api/v1/points", s.user(s.postPoint))
	handle("GET /api/v1/admin/audit", s.admin(s.auditLog))
	handle("GET /api/v1/me/integrations", s.user(s.getIntegrations))
	handle("PUT /api/v1/me/integrations", s.user(s.putIntegrations))
	handle("GET /api/v1/photos", s.user(s.photos))
	handle("GET /api/v1/photos/{pid}/thumb", s.user(s.photoThumb))

	// Geofence automations
	handle("GET /api/v1/automations", s.user(s.listAutomations))
	handle("POST /api/v1/automations", s.user(s.saveAutomation))
	handle("PUT /api/v1/automations/{id}", s.user(s.saveAutomation))
	handle("DELETE /api/v1/automations/{id}", s.user(s.deleteAutomation))
	handle("POST /api/v1/automations/{id}/test", s.user(s.testAutomation))

	// Insights & sharing
	handle("GET /api/v1/days", s.user(s.days))
	handle("GET /api/v1/insights", s.user(s.insights))
	handle("GET /api/v1/shares", s.user(s.listShares))
	handle("POST /api/v1/shares", s.user(s.createShare))
	handle("DELETE /api/v1/shares/{id}", s.user(s.deleteShare))
	handle("GET /api/v1/public/shares/{token}", s.publicShare)

	// Maps & geocoding
	handle("GET /api/v1/map/style/{id}", s.user(s.mapStyle))
	handle("GET /api/v1/map/tiles/{z}/{x}/{y}", s.user(s.mapTile))
	handle("GET /api/v1/geocode/search", s.user(s.geocodeSearch))
	handle("GET /api/v1/geocode/reverse", s.user(s.geocodeReverse))

	// Places
	handle("GET /api/v1/places", s.user(s.listPlaces))
	handle("POST /api/v1/places", s.user(s.savePlace))
	handle("PUT /api/v1/places/{id}", s.user(s.savePlace))
	handle("DELETE /api/v1/places/{id}", s.user(s.deletePlace))

	// Devices
	handle("GET /api/v1/devices", s.user(s.listDevices))
	handle("POST /api/v1/devices", s.user(s.createDevice))
	handle("POST /api/v1/devices/{id}/token", s.user(s.rotateDeviceToken))
	handle("DELETE /api/v1/devices/{id}", s.user(s.deleteDevice))

	// Import / export
	handle("GET /api/v1/imports", s.user(s.listImports))
	handle("POST /api/v1/imports", s.user(s.uploadImport))
	handle("DELETE /api/v1/imports/{id}", s.user(s.deleteImport))
	handle("GET /api/v1/export", s.user(s.export))
	handle("GET /api/v1/exports", s.user(s.listExports))
	handle("POST /api/v1/exports", s.user(s.createExport))
	handle("GET /api/v1/exports/{id}/download", s.user(s.downloadExport))
	handle("DELETE /api/v1/exports/{id}", s.user(s.deleteExport))

	// Admin
	handle("GET /api/v1/admin/users", s.admin(s.listUsers))
	handle("POST /api/v1/admin/users", s.admin(s.createUser))
	handle("PATCH /api/v1/admin/users/{id}", s.admin(s.updateUser))
	handle("DELETE /api/v1/admin/users/{id}", s.admin(s.deleteUser))
	handle("GET /api/v1/admin/settings", s.admin(s.getSettings))
	handle("PUT /api/v1/admin/settings", s.admin(s.putSettings))
	handle("GET /api/v1/admin/system", s.admin(s.system))
	handle("GET /api/v1/admin/backups", s.admin(s.listBackups))
	handle("POST /api/v1/admin/backups", s.admin(s.createBackup))
	handle("POST /api/v1/admin/backups/upload", s.admin(s.restoreUpload))
	handle("GET /api/v1/admin/backups/{name}", s.admin(s.downloadBackup))
	handle("DELETE /api/v1/admin/backups/{name}", s.admin(s.deleteBackup))
	handle("POST /api/v1/admin/backups/{name}/restore", s.admin(s.restoreBackup))

	handle("/api/", func(w http.ResponseWriter, r *http.Request) { fail(w, http.StatusNotFound, "no such endpoint") })

	// Ingest: stable forever, phones are configured once and forgotten (§11).
	handle("POST /ingest/owntracks", s.ingest("owntracks"))
	handle("POST /ingest/overland", s.ingest("overland"))
	handle("/ingest/colota", s.ingest("colota"))
	handle("/ingest/gpslogger", s.ingest("gpslogger"))
	handle("/ingest/osmand", s.ingest("osmand"))
	handle("POST /ingest/json", s.ingest("json"))

	mux.Handle("/", spa(s.cfg.Web))

	// Stdlib CSRF defence (Go 1.25+): rejects cross-origin browser writes via
	// Sec-Fetch-Site/Origin; phone apps send neither header and pass through.
	h := http.NewCrossOriginProtection().Handler(mux)
	return securityHeaders(logRequests(h))
}

// ── Helpers ──────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// fail writes an RFC 9457 problem response.
func fail(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"title": msg, "status": status})
}

func internal(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("request failed", "path", r.URL.Path, "err", err)
	fail(w, http.StatusInternalServerError, "internal error")
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(v); err != nil {
		fail(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}

func pathID(r *http.Request) int64 {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id
}

// timeRange parses ?from=&to= (unix ms or RFC 3339); missing bounds mean "all time".
func timeRange(r *http.Request) (from, to int64, ok bool) {
	from, to = 0, math.MaxInt64
	q := r.URL.Query()
	if v := q.Get("from"); v != "" {
		if from, ok = geo.ParseTime(v); !ok {
			return 0, 0, false
		}
	}
	if v := q.Get("to"); v != "" {
		if to, ok = geo.ParseTime(v); !ok {
			return 0, 0, false
		}
	}
	return from, to, from <= to
}

func (s *Server) clientIP(r *http.Request) string {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	if ip, err := netip.ParseAddr(host); err == nil {
		for _, p := range s.cfg.TrustedProxies {
			if p.Contains(ip) {
				if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
					parts := strings.Split(xff, ",")
					return strings.TrimSpace(parts[len(parts)-1])
				}
			}
		}
	}
	return host
}

func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

// baseURL is the configured public URL, or a best guess from the request.
func (s *Server) baseURL(r *http.Request) string {
	if s.cfg.BaseURL != "" {
		return strings.TrimRight(s.cfg.BaseURL, "/")
	}
	scheme := "http"
	if isHTTPS(r) {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// ── Middleware ───────────────────────────────────────────────

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Unwrap lets http.ResponseController reach Flush for SSE.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(sw, r)
		// Never log query strings: they can carry coordinates and device tokens.
		level := slog.LevelDebug
		if sw.status >= 500 {
			level = slog.LevelError
		}
		slog.Log(r.Context(), level, "http", "method", r.Method, "path", r.URL.Path, "status", sw.status, "ms", time.Since(start).Milliseconds())
	})
}

func securityHeaders(next http.Handler) http.Handler {
	csp := strings.Join([]string{
		"default-src 'self'",
		"script-src 'self'",
		"style-src 'self' 'unsafe-inline'", // MapLibre sets inline styles
		"img-src 'self' data: blob: https:",
		"font-src 'self' data: https:",
		"connect-src 'self' https:", // map tiles/styles come from a configurable host
		"worker-src 'self' blob:",
		"frame-ancestors 'none'",
		"base-uri 'self'",
		"form-action 'self'",
	}, "; ")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Permissions-Policy", "geolocation=(self), camera=(), microphone=()")
		if isHTTPS(r) {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}

// spa serves the embedded frontend, falling back to index.html for client-side routes.
func spa(web fs.FS) http.Handler {
	files := http.FileServerFS(web)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p == "" || p == "index.html" {
			p = "."
		}
		if _, err := fs.Stat(web, p); errors.Is(err, fs.ErrNotExist) {
			if path.Ext(p) != "" { // a missing asset, not a route
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Cache-Control", "no-cache")
			http.ServeFileFS(w, r, web, "index.html")
			return
		}
		if strings.HasPrefix(p, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	})
}
