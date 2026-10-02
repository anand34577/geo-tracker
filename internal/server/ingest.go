package server

import (
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"geotracker/internal/geo"
	"geotracker/internal/ingest"
	"geotracker/internal/store"
)

// ingestToken finds the device token wherever each app can put it: a Bearer header
// (Overland, scripts), Basic-auth password (OwnTracks), ?token= (GPSLogger) or ?id= (Traccar/OsmAnd).
func ingestToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(h[7:])
	}
	if _, pw, ok := r.BasicAuth(); ok {
		return pw
	}
	q := r.URL.Query()
	if t := q.Get("token"); t != "" {
		return t
	}
	return q.Get("id")
}

func (s *Server) ingest(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Authenticate before parsing so unknown callers can't make us chew through 10 MB.
		// Traccar JSON carries its token in the body, so it is checked after parsing.
		token := ingestToken(r)
		lookup := func() (userID, deviceID int64, ok bool) {
			uid, did, scopes, err := s.db.TokenLookup(r.Context(), hashToken(token))
			if errors.Is(err, store.ErrNotFound) || (err == nil && !strings.Contains(scopes, "ingest")) {
				fail(w, http.StatusUnauthorized, "unknown device token")
				return 0, 0, false
			}
			if err != nil {
				internal(w, r, err)
				return 0, 0, false
			}
			return uid, did, true
		}
		var userID, deviceID int64
		if token != "" {
			var ok bool
			if userID, deviceID, ok = lookup(); !ok {
				return
			}
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 10<<20))
		if err != nil {
			fail(w, http.StatusRequestEntityTooLarge, "payload too large")
			return
		}
		var pts []geo.Point
		switch kind {
		case "owntracks":
			pts, err = ingest.OwnTracks(body)
		case "overland":
			pts, err = ingest.Overland(body)
		case "colota":
			if len(body) == 0 { // Colota's optional GET mode sends query parameters
				pts, err = ingest.Params(r.URL.Query(), false)
			} else {
				pts, err = ingest.Colota(body)
			}
		case "json":
			pts, err = ingest.JSON(body)
		case "gpslogger", "osmand":
			ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if kind == "osmand" && ct == "application/json" {
				var dev string
				pts, dev, err = ingest.Traccar(body)
				if token == "" {
					token = dev
				}
				break
			}
			vals := r.URL.Query()
			if form, ferr := url.ParseQuery(string(body)); ferr == nil {
				for k, v := range form {
					vals[k] = v
				}
			}
			pts, err = ingest.Params(vals, kind == "osmand")
		}
		if err != nil {
			fail(w, http.StatusBadRequest, "could not parse location: "+err.Error())
			return
		}

		if userID == 0 {
			var ok bool
			if userID, deviceID, ok = lookup(); !ok {
				return
			}
		}

		valid := pts[:0]
		for _, p := range pts {
			if p.Valid() {
				valid = append(valid, p)
			}
		}
		added, err := s.db.InsertPoints(r.Context(), userID, deviceID, 0, valid)
		if err != nil {
			internal(w, r, err)
			return
		}
		var battery *int
		if len(valid) > 0 {
			// Batches are not always in time order; the live position is the newest fix.
			latest := valid[0]
			for _, p := range valid[1:] {
				if p.TS > latest.TS {
					latest = p
				}
			}
			battery = latest.Battery
			s.hub.Publish(userID, "point", map[string]any{"device_id": deviceID, "point": latest})
			s.checkPointEvents(r.Context(), userID, deviceID, latest)
		}
		if err := s.db.TouchDevice(r.Context(), deviceID, battery); err != nil {
			slog.Warn("touch device", "err", err)
		}
		if added > 0 {
			s.wake(s.wakeTimeline)
		}

		switch kind {
		case "owntracks":
			writeJSON(w, http.StatusOK, []any{}) // OwnTracks expects a JSON array
		case "overland":
			writeJSON(w, http.StatusOK, map[string]string{"result": "ok"}) // Overland deletes the batch only on this
		default:
			writeJSON(w, http.StatusOK, map[string]int{"received": len(pts), "added": added})
		}
	}
}
