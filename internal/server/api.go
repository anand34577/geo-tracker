package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"geotracker/internal/geo"
	"geotracker/internal/maps"
	"geotracker/internal/notify"
	"geotracker/internal/store"
	"geotracker/internal/transfer"
)

func (s *Server) getConfig(w http.ResponseWriter, r *http.Request, u *store.User) {
	st, err := s.db.Settings(r.Context())
	if err != nil {
		internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version":         s.cfg.Version,
		"base_url":        s.baseURL(r),
		"basemaps":        s.basemaps(r, st),
		"basemap_default": st["map_default"],
		"geocoder":        st["geocoder"] != "none",
	})
}

// ── Points ───────────────────────────────────────────────────

// getPoints streams [ts, lat, lon, acc, speed, alt, batt] rows. Long ranges are
// thinned to at most ?max points so the browser stays fast.
// ponytail: every-nth thinning; switch to Douglas-Peucker if shapes look too coarse.
func (s *Server) getPoints(w http.ResponseWriter, r *http.Request, u *store.User) {
	from, to, ok := timeRange(r)
	if !ok {
		fail(w, http.StatusBadRequest, "invalid from/to")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("max"))
	if limit <= 0 || limit > 200_000 {
		limit = 50_000
	}
	maxAcc := 100.0
	if r.URL.Query().Get("raw") == "1" {
		maxAcc = 0
	}
	v, ok := s.subject(w, r, u, from, to)
	if !ok {
		return
	}
	total, err := s.db.CountPoints(r.Context(), v.id, v.from, v.to, maxAcc)
	if err != nil {
		internal(w, r, err)
		return
	}
	step := max(1, int(math.Ceil(float64(total)/float64(limit))))

	w.Header().Set("Content-Type", "application/json")
	bw := bufio.NewWriterSize(w, 64<<10)
	// "total" is written last: the count of points the caller may see (privacy zones excluded),
	// which is only known once the stream has been filtered. The database count above is just for thinning.
	bw.WriteString(`{"points":[`)
	i, n := 0, 0
	err = s.db.ForEachPoint(r.Context(), v.id, v.from, v.to, maxAcc, func(p geo.Point) error {
		if v.hidden(p.Lat, p.Lon) {
			return nil
		}
		i++
		if (i-1)%step != 0 {
			return nil
		}
		if n > 0 {
			bw.WriteByte(',')
		}
		n++
		row, _ := json.Marshal([]any{p.TS, p.Lat, p.Lon, p.Accuracy, p.Speed, p.Altitude, p.Battery})
		if v.approx {
			row, _ = json.Marshal([]any{p.TS, coarsen(p.Lat), coarsen(p.Lon), nil, nil, nil, nil})
		}
		_, err := bw.Write(row)
		return err
	})
	if err != nil {
		// Headers and part of the body are already out. Ending cleanly would hand the client valid-looking
		// but cut-short JSON, so log and abort the connection and let it see a failed request instead.
		slog.Error("stream points", "path", r.URL.Path, "err", err)
		panic(http.ErrAbortHandler)
	}
	fmt.Fprintf(bw, `],"step":%d,"total":%d}`, step, i)
	bw.Flush()
}

func (s *Server) deletePoints(w http.ResponseWriter, r *http.Request, u *store.User) {
	from, to, ok := timeRange(r)
	if !ok || r.URL.Query().Get("from") == "" || r.URL.Query().Get("to") == "" {
		fail(w, http.StatusBadRequest, "from and to are required")
		return
	}
	n, err := s.db.DeletePoints(r.Context(), u.ID, from, to)
	if err != nil {
		internal(w, r, err)
		return
	}
	s.wake(s.wakeTimeline)
	s.audit(r, u.ID, "points.delete", fmt.Sprintf("%d points", n))
	writeJSON(w, http.StatusOK, map[string]int64{"deleted": n})
}

func (s *Server) latestPoint(w http.ResponseWriter, r *http.Request, u *store.User) {
	p, err := s.db.LatestPoint(r.Context(), u.ID)
	if err != nil {
		internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) stats(w http.ResponseWriter, r *http.Request, u *store.User) {
	st, err := s.db.PointStats(r.Context(), u.ID)
	if err != nil {
		internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// ── Timeline ─────────────────────────────────────────────────

type visitOut struct {
	store.VisitRow
	PlaceID   int64  `json:"place_id,omitempty"`
	PlaceName string `json:"place_name,omitempty"`
	PlaceIcon string `json:"place_icon,omitempty"`
}

// matchPlace returns the nearest saved place whose radius contains (lat, lon).
func matchPlace(places []store.Place, lat, lon float64) *store.Place {
	var best *store.Place
	bestD := math.Inf(1)
	for i := range places {
		if d := geo.Distance(lat, lon, places[i].Lat, places[i].Lon); d <= places[i].Radius && d < bestD {
			best, bestD = &places[i], d
		}
	}
	return best
}

// visitPlace is the saved place a visit belongs to: the user's pick when they corrected it,
// otherwise the nearest place containing it. (A family member's pick names *their* place,
// which isn't among the viewer's places, so the viewer gets the automatic match.)
func visitPlace(places []store.Place, v store.VisitRow) *store.Place {
	if v.NoPlace {
		return nil
	}
	for i := range places {
		if v.PinnedPlace != 0 && places[i].ID == v.PinnedPlace {
			return &places[i]
		}
	}
	return matchPlace(places, v.Lat, v.Lon)
}

func (s *Server) getTimeline(w http.ResponseWriter, r *http.Request, u *store.User) {
	from, to, ok := timeRange(r)
	if !ok {
		fail(w, http.StatusBadRequest, "invalid from/to")
		return
	}
	sv, ok := s.subject(w, r, u, from, to)
	if !ok {
		return
	}
	visits, trips, err := s.db.Timeline(r.Context(), sv.id, sv.from, sv.to)
	if err != nil {
		internal(w, r, err)
		return
	}
	// Someone else's visits are labelled with *your* saved places ("Grandma's house").
	places, err := s.db.Places(r.Context(), u.ID)
	if err != nil {
		internal(w, r, err)
		return
	}
	out := make([]visitOut, 0, len(visits))
	var hiddenAt [][2]int64
	for _, v := range visits {
		if sv.hidden(v.Lat, v.Lon) {
			hiddenAt = append(hiddenAt, [2]int64{v.Start, v.End})
			continue
		}
		if sv.approx {
			v.Name, v.Address, v.Lat, v.Lon, v.Radius, v.CustomName = v.City, "", coarsen(v.Lat), coarsen(v.Lon), 1000, ""
		}
		o := visitOut{VisitRow: v}
		if p := visitPlace(places, v); p != nil && !sv.approx {
			o.PlaceID, o.PlaceName, o.PlaceIcon = p.ID, p.Name, p.Icon
		}
		out = append(out, o)
	}
	// A trip that starts or ends inside a privacy zone would reveal when the person left or arrived.
	const slack = 2 * 60_000
	shown := trips[:0]
	for _, t := range trips {
		hide := false
		for _, h := range hiddenAt {
			if (t.Start >= h[0]-slack && t.Start <= h[1]+slack) || (t.End >= h[0]-slack && t.End <= h[1]+slack) {
				hide = true
				break
			}
		}
		if !hide {
			shown = append(shown, t)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"visits": out, "trips": shown})
}

func (s *Server) rebuildTimeline(w http.ResponseWriter, r *http.Request, u *store.User) {
	if err := s.db.MarkDirty(r.Context(), u.ID, 0); err != nil {
		internal(w, r, err)
		return
	}
	s.wake(s.wakeTimeline)
	w.WriteHeader(http.StatusAccepted)
}

// ── Places ───────────────────────────────────────────────────

type placeOut struct {
	store.Place
	Visits    int    `json:"visits"`
	TotalMs   int64  `json:"total_ms"`
	LastVisit *int64 `json:"last_visit"`
}

func (s *Server) listPlaces(w http.ResponseWriter, r *http.Request, u *store.User) {
	places, err := s.db.Places(r.Context(), u.ID)
	if err != nil {
		internal(w, r, err)
		return
	}
	visits, err := s.db.VisitCenters(r.Context(), u.ID)
	if err != nil {
		internal(w, r, err)
		return
	}
	out := make([]placeOut, len(places))
	idx := map[int64]int{}
	for i, p := range places {
		out[i] = placeOut{Place: p}
		idx[p.ID] = i
	}
	for _, v := range visits {
		if p := visitPlace(places, v); p != nil {
			o := &out[idx[p.ID]]
			o.Visits++
			o.TotalMs += v.End - v.Start
			if o.LastVisit == nil || v.End > *o.LastVisit {
				end := v.End
				o.LastVisit = &end
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) savePlace(w http.ResponseWriter, r *http.Request, u *store.User) {
	var p store.Place
	if !decode(w, r, &p) {
		return
	}
	p.Name = strings.TrimSpace(p.Name)
	if p.Icon == "" {
		p.Icon = "map-pin"
	}
	if p.Radius == 0 {
		p.Radius = 75
	}
	switch {
	case p.Name == "" || len(p.Name) > 80:
		fail(w, http.StatusBadRequest, "enter a name (up to 80 characters)")
		return
	case !(geo.Point{TS: time.Now().UnixMilli(), Lat: p.Lat, Lon: p.Lon}).Valid():
		fail(w, http.StatusBadRequest, "invalid coordinates")
		return
	case p.Radius < 10 || p.Radius > 5000:
		fail(w, http.StatusBadRequest, "radius must be between 10 and 5000 meters")
		return
	case len(p.Icon) > 32:
		fail(w, http.StatusBadRequest, "invalid icon")
		return
	}
	var err error
	if r.Method == http.MethodPut {
		p.ID = pathID(r)
		err = s.db.UpdatePlace(r.Context(), u.ID, &p)
	} else {
		err = s.db.CreatePlace(r.Context(), u.ID, &p)
	}
	if errors.Is(err, store.ErrNotFound) {
		fail(w, http.StatusNotFound, "place not found")
		return
	}
	if err != nil {
		internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) deletePlace(w http.ResponseWriter, r *http.Request, u *store.User) {
	if err := s.db.DeletePlace(r.Context(), u.ID, pathID(r)); errors.Is(err, store.ErrNotFound) {
		fail(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		internal(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── Devices ──────────────────────────────────────────────────

var clients = []string{"colota", "owntracks", "overland", "gpslogger", "traccar", "homeassistant", "other"}

func (s *Server) listDevices(w http.ResponseWriter, r *http.Request, u *store.User) {
	ds, err := s.db.Devices(r.Context(), u.ID)
	if err != nil {
		internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ds)
}

func (s *Server) createDevice(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct{ Name, Client string }
	if !decode(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Name) > 60 || !slices.Contains(clients, req.Client) {
		fail(w, http.StatusBadRequest, "enter a device name and pick an app")
		return
	}
	tok := newToken()
	d, err := s.db.CreateDevice(r.Context(), u.ID, req.Name, req.Client, hashToken(tok))
	if err != nil {
		internal(w, r, err)
		return
	}
	s.audit(r, u.ID, "device.create", req.Name)
	writeJSON(w, http.StatusCreated, map[string]any{"device": d, "token": tok})
}

func (s *Server) rotateDeviceToken(w http.ResponseWriter, r *http.Request, u *store.User) {
	tok := newToken()
	err := s.db.RotateDeviceToken(r.Context(), u.ID, pathID(r), hashToken(tok))
	if errors.Is(err, store.ErrNotFound) {
		fail(w, http.StatusNotFound, "device not found")
		return
	}
	if err != nil {
		internal(w, r, err)
		return
	}
	s.audit(r, u.ID, "device.token", strconv.FormatInt(pathID(r), 10))
	writeJSON(w, http.StatusOK, map[string]string{"token": tok})
}

func (s *Server) deleteDevice(w http.ResponseWriter, r *http.Request, u *store.User) {
	if err := s.db.DeleteDevice(r.Context(), u.ID, pathID(r)); errors.Is(err, store.ErrNotFound) {
		fail(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		internal(w, r, err)
		return
	}
	s.audit(r, u.ID, "device.delete", strconv.FormatInt(pathID(r), 10))
	w.WriteHeader(http.StatusNoContent)
}

// ── Import / export ──────────────────────────────────────────

var importExts = []string{".zip", ".json", ".geojson", ".gpx", ".csv", ".rec"}

const maxUpload = 8 << 30 // ponytail: fixed 8 GiB cap; make configurable if someone needs more

func (s *Server) listImports(w http.ResponseWriter, r *http.Request, u *store.User) {
	is, err := s.db.Imports(r.Context(), u.ID)
	if err != nil {
		internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, is)
}

// receiveFile streams the multipart "file" field to dir without buffering it in memory.
func receiveFile(w http.ResponseWriter, r *http.Request, dir string) (tmp, filename string, err error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload)
	reader, err := r.MultipartReader()
	if err != nil {
		return "", "", err
	}
	for {
		part, err := reader.NextPart()
		if err != nil {
			return "", "", errors.New("no file uploaded")
		}
		if part.FormName() != "file" {
			continue
		}
		filename = filepath.Base(part.FileName())
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return "", "", err
		}
		f, err := os.CreateTemp(dir, "upload-*")
		if err != nil {
			return "", "", err
		}
		_, err = io.Copy(f, part)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			os.Remove(f.Name())
			return "", "", err
		}
		return f.Name(), filename, nil
	}
}

func (s *Server) importPath(id int64) string {
	return filepath.Join(s.cfg.DataDir, "imports", strconv.FormatInt(id, 10))
}

func (s *Server) uploadImport(w http.ResponseWriter, r *http.Request, u *store.User) {
	tmp, name, err := receiveFile(w, r, filepath.Join(s.cfg.DataDir, "imports"))
	if err != nil {
		fail(w, http.StatusBadRequest, "upload failed: "+err.Error())
		return
	}
	if !slices.Contains(importExts, strings.ToLower(filepath.Ext(name))) {
		os.Remove(tmp)
		fail(w, http.StatusBadRequest, "unsupported file type; use "+strings.Join(importExts, " "))
		return
	}
	id, err := s.db.CreateImport(r.Context(), u.ID, name)
	if err == nil {
		err = os.Rename(tmp, s.importPath(id))
	}
	if err != nil {
		os.Remove(tmp)
		internal(w, r, err)
		return
	}
	s.wake(s.wakeImports)
	s.audit(r, u.ID, "import.upload", name)
	writeJSON(w, http.StatusAccepted, map[string]int64{"id": id})
}

func (s *Server) deleteImport(w http.ResponseWriter, r *http.Request, u *store.User) {
	err := s.db.DeleteImport(r.Context(), u.ID, pathID(r))
	if errors.Is(err, store.ErrNotFound) {
		fail(w, http.StatusConflict, "import not found or still running")
		return
	}
	if err != nil {
		internal(w, r, err)
		return
	}
	os.Remove(s.importPath(pathID(r)))
	s.wake(s.wakeTimeline)
	s.audit(r, u.ID, "import.undo", strconv.FormatInt(pathID(r), 10))
	w.WriteHeader(http.StatusNoContent)
}

// writeExport streams one export format to w. Used by the direct download and the background job.
func (s *Server) writeExport(ctx context.Context, w io.Writer, u *store.User, format string, from, to int64) error {
	points := func(fn func(geo.Point) error) error {
		return s.db.ForEachPoint(ctx, u.ID, from, to, 0, fn)
	}
	switch format {
	case "gpx":
		return transfer.WriteGPX(w, "GeoTracker – "+u.Name, points)
	case "geojson":
		return transfer.WriteGeoJSON(w, points)
	case "csv":
		return transfer.WriteCSV(w, points)
	case "native":
		places, err := s.db.Places(ctx, u.ID)
		if err != nil {
			return err
		}
		visits, trips, err := s.db.Timeline(ctx, u.ID, from, to)
		if err != nil {
			return err
		}
		tp := make([]transfer.Place, len(places))
		for i, p := range places {
			tp[i] = transfer.Place{Name: p.Name, Icon: p.Icon, Lat: p.Lat, Lon: p.Lon, Radius: p.Radius, Private: p.Private}
		}
		return transfer.WriteNative(w, s.cfg.Version, u.Email, points, tp, anys(visits), anys(trips))
	}
	return errors.New("unknown export format")
}

// export streams an export straight to the browser (scripts and small ranges). The UI uses
// background exports (exports.go) so large archives don't depend on one long request.
func (s *Server) export(w http.ResponseWriter, r *http.Request, u *store.User) {
	format := r.URL.Query().Get("format")
	meta, ok := transfer.Formats[format]
	from, to, okRange := timeRange(r)
	if !ok || !okRange {
		fail(w, http.StatusBadRequest, "choose a format (native, gpx, geojson, csv) and a valid range")
		return
	}
	name := fmt.Sprintf("geotracker-%s.%s", time.Now().Format("2006-01-02"), meta[0])
	w.Header().Set("Content-Type", meta[1])
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	s.audit(r, u.ID, "export", format)
	tw := &wroteTracker{ResponseWriter: w}
	if err := s.writeExport(r.Context(), tw, u, format, from, to); err != nil {
		if tw.wrote {
			// Part of the file is already on its way: a JSON error appended to it would look like a
			// finished download. Cut the connection so the browser reports the download as failed.
			slog.Error("export failed mid-stream", "err", err)
			panic(http.ErrAbortHandler)
		}
		internal(w, r, err)
	}
}

// wroteTracker records whether any body bytes have been sent.
type wroteTracker struct {
	http.ResponseWriter
	wrote bool
}

func (t *wroteTracker) Write(b []byte) (int, error) {
	t.wrote = true
	return t.ResponseWriter.Write(b)
}

func (t *wroteTracker) Flush() {
	if f, ok := t.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func anys[T any](xs []T) []any {
	out := make([]any, len(xs))
	for i, x := range xs {
		out[i] = x
	}
	return out
}

// ── Admin: users ─────────────────────────────────────────────

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request, _ *store.User) {
	us, err := s.db.Users(r.Context())
	if err != nil {
		internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, us)
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request, me *store.User) {
	var req struct {
		accountReq
		Role string `json:"role"`
	}
	if !decode(w, r, &req) {
		return
	}
	email, msg := validateAccount(req.Email, req.Name, req.Password, true)
	if req.Role != "admin" {
		req.Role = "user"
	}
	if msg != "" {
		fail(w, http.StatusBadRequest, msg)
		return
	}
	u, err := s.db.CreateUser(r.Context(), email, strings.TrimSpace(req.Name), HashPassword(req.Password), req.Role)
	if errors.Is(err, store.ErrExists) {
		fail(w, http.StatusConflict, "a user with that email already exists")
		return
	}
	if err != nil {
		internal(w, r, err)
		return
	}
	s.audit(r, me.ID, "user.create", u.Email)
	writeJSON(w, http.StatusCreated, u)
}

// wouldOrphan reports whether demoting/disabling/deleting target leaves no active admin.
func (s *Server) wouldOrphan(r *http.Request, target *store.User) bool {
	if target.Role != "admin" || target.Disabled {
		return false
	}
	n, err := s.db.CountActiveAdmins(r.Context())
	return err != nil || n <= 1
}

func (s *Server) updateUser(w http.ResponseWriter, r *http.Request, me *store.User) {
	target, err := s.db.UserByID(r.Context(), pathID(r))
	if errors.Is(err, store.ErrNotFound) {
		fail(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		internal(w, r, err)
		return
	}
	var req struct {
		Name     *string `json:"name"`
		Email    *string `json:"email"`
		Role     *string `json:"role"`
		Disabled *bool   `json:"disabled"`
		Password string  `json:"password"`
	}
	if !decode(w, r, &req) {
		return
	}
	demote := req.Role != nil && *req.Role != "admin"
	disable := req.Disabled != nil && *req.Disabled
	if (demote || disable) && s.wouldOrphan(r, target) {
		fail(w, http.StatusConflict, "this is the last admin; make someone else admin first")
		return
	}
	if disable && target.ID == me.ID {
		fail(w, http.StatusConflict, "you cannot disable yourself")
		return
	}
	if req.Name != nil {
		target.Name = strings.TrimSpace(*req.Name)
	}
	if req.Email != nil {
		target.Email = *req.Email
	}
	if req.Role != nil {
		target.Role = map[bool]string{true: "admin", false: "user"}[*req.Role == "admin"]
	}
	if req.Disabled != nil {
		target.Disabled = *req.Disabled
	}
	email, msg := validateAccount(target.Email, target.Name, req.Password, false)
	if msg != "" {
		fail(w, http.StatusBadRequest, msg)
		return
	}
	target.Email = email
	if err := s.db.UpdateUser(r.Context(), target); errors.Is(err, store.ErrExists) {
		fail(w, http.StatusConflict, "that email is already in use")
		return
	} else if err != nil {
		internal(w, r, err)
		return
	}
	if req.Password != "" {
		if err := s.db.SetPassword(r.Context(), target.ID, HashPassword(req.Password), nil); err != nil {
			internal(w, r, err)
			return
		}
	}
	s.audit(r, me.ID, "user.update", target.Email)
	writeJSON(w, http.StatusOK, target)
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request, me *store.User) {
	target, err := s.db.UserByID(r.Context(), pathID(r))
	if errors.Is(err, store.ErrNotFound) {
		fail(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		internal(w, r, err)
		return
	}
	if target.ID == me.ID || s.wouldOrphan(r, target) {
		fail(w, http.StatusConflict, "you cannot delete yourself or the last admin")
		return
	}
	if err := s.db.DeleteUser(r.Context(), target.ID); err != nil {
		internal(w, r, err)
		return
	}
	s.audit(r, me.ID, "user.delete", target.Email)
	w.WriteHeader(http.StatusNoContent)
}

// ── Admin: settings & system ─────────────────────────────────

const secretMask = "********"

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request, _ *store.User) {
	st, err := s.db.Settings(r.Context())
	if err != nil {
		internal(w, r, err)
		return
	}
	for k := range store.SecretSettings {
		if st[k] != "" {
			st[k] = secretMask // secrets are write-only from the browser's point of view
		}
	}
	// Read-only helpers for the admin UI.
	out := map[string]any{"oidc_callback_url": s.oidcCallbackURL(r)}
	for k, v := range st {
		if !store.InternalSettings[k] {
			out[k] = v
		}
	}
	if _, err := s.mbtiles(st); err != nil {
		out["map_mbtiles_error"] = err.Error()
	}
	writeJSON(w, http.StatusOK, out)
}

// readOnlySettings are sent by GET /admin/settings for display only; a PUT that echoes them back is fine.
var readOnlySettings = map[string]bool{"oidc_callback_url": true, "map_mbtiles_error": true}

var builtinBasemaps = []string{"auto", "liberty", "bright", "osm", "topo", "offline"}

func isURL(v string) bool {
	u, err := url.Parse(v)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != ""
}

// checkSetting validates one value and returns it normalized, or a message for the admin.
func checkSetting(k, v string) (string, string) {
	bad := func(msg string) (string, string) { return "", msg }
	switch k {
	case "map_default":
		// Checked against the (possibly just edited) custom map list in putSettings.
		if len(v) == 0 || len(v) > 20 {
			return bad("choose a default map")
		}
	case "map_custom":
		if v == "" {
			v = "[]"
		}
		var list []maps.Custom
		if err := json.Unmarshal([]byte(v), &list); err != nil {
			return bad("custom maps: invalid list")
		}
		if err := maps.NormalizeList(list); err != nil {
			return bad("custom maps: " + err.Error())
		}
		if list == nil {
			list = []maps.Custom{}
		}
		b, _ := json.Marshal(list)
		return string(b), ""
	case "map_mbtiles":
		if v == "" {
			return v, ""
		}
		mb, err := maps.Open(v)
		if err != nil {
			return bad("cannot open that .mbtiles file on the server (check the path; with Docker, mount the folder into the container)")
		}
		mb.Close()
	case "map_glyphs":
		if v != "" && !isURL(strings.NewReplacer("{fontstack}", "x", "{range}", "x").Replace(v)) {
			return bad("label fonts: enter an http(s) URL with {fontstack} and {range}, or leave empty")
		}
	case "smtp_host", "smtp_user", "smtp_password", "oidc_client_id", "oidc_client_secret", "oidc_label":
		if len(v) > 500 {
			return bad(k + ": too long")
		}
	case "smtp_port":
		if n, err := strconv.Atoi(v); v != "" && (err != nil || n <= 0 || n >= 65536) {
			return bad("SMTP port must be between 1 and 65535")
		}
	case "smtp_from":
		if _, err := mail.ParseAddress(v); v != "" && err != nil {
			return bad("sender address: use name@example.com or Name <name@example.com>")
		}
	case "smtp_security":
		if v != "starttls" && v != "tls" && v != "none" {
			return bad("invalid SMTP security")
		}
	case "oidc_enabled", "oidc_auto_register":
		if v != "true" && v != "false" {
			return bad("invalid value for " + k)
		}
	case "oidc_issuer":
		if v != "" && !isURL(v) {
			return bad("SSO issuer must be an http(s) URL")
		}
		v = strings.TrimRight(v, "/")
	case "geocoder":
		if v != "nominatim" && v != "photon" && v != "none" {
			return bad("invalid geocoder")
		}
	case "backup_hour":
		if n, err := strconv.Atoi(v); err != nil || n < -1 || n > 23 {
			return bad("backup hour must be between 0 and 23 (or off)")
		}
	case "backup_keep":
		if n, err := strconv.Atoi(v); err != nil || n < 1 || n > 365 {
			return bad("keep between 1 and 365 backups")
		}
	case "geocoder_url":
		if v != "" && !isURL(v) {
			return bad("geocoder URL must be an http(s) URL")
		}
	case "map_style_light", "map_style_dark":
		if maps.IsTileTemplate(v) {
			return bad("that is a tile URL ({z}/{x}/{y}); add it under Custom maps instead of as a style")
		}
		if !isURL(v) {
			return bad("map style must be an http(s) URL to a style JSON")
		}
	default:
		return bad("unknown setting " + k)
	}
	return v, ""
}

func (s *Server) putSettings(w http.ResponseWriter, r *http.Request, u *store.User) {
	var in map[string]string
	if !decode(w, r, &in) {
		return
	}
	cur, err := s.db.Settings(r.Context())
	if err != nil {
		internal(w, r, err)
		return
	}
	kv := map[string]string{}
	for k, v := range in {
		if readOnlySettings[k] || store.InternalSettings[k] {
			continue // display-only or server-managed: never written from the browser
		}
		v = strings.TrimSpace(v)
		if store.SecretSettings[k] && v == secretMask {
			continue // unchanged secret
		}
		if k == "map_mbtiles" {
			v = strings.Trim(v, `"`) // pasted Windows paths often come quoted
		}
		if _, known := store.DefaultSettings[k]; known && cur[k] == v {
			continue // unchanged: don't re-validate (an unreachable .mbtiles path must not block saving SMTP)
		}
		nv, msg := checkSetting(k, v)
		if msg != "" {
			fail(w, http.StatusBadRequest, msg)
			return
		}
		kv[k] = nv
	}
	merged := map[string]string{}
	for k, v := range cur {
		merged[k] = v
	}
	for k, v := range kv {
		merged[k] = v
	}
	// Cross-field checks against the settings as they will be after this save.
	if def := merged["map_default"]; !slices.Contains(builtinBasemaps, def) {
		if _, ok := maps.FindCustom(maps.ParseCustom(merged["map_custom"]), def); !ok {
			if _, changed := kv["map_default"]; changed {
				fail(w, http.StatusBadRequest, "the default map does not exist")
				return
			}
			kv["map_default"] = "auto" // its custom map was just removed
		}
	}
	if merged["oidc_enabled"] == "true" && (merged["oidc_issuer"] == "" || merged["oidc_client_id"] == "") {
		fail(w, http.StatusBadRequest, "single sign-on needs an issuer URL and a client ID")
		return
	}
	if len(kv) == 0 {
		s.getSettings(w, r, u)
		return
	}
	s.oidc.mu.Lock()
	s.oidc.c = nil // re-run discovery with the new settings
	s.oidc.mu.Unlock()
	keys := make([]string, 0, len(kv))
	for k := range kv {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	s.audit(r, u.ID, "settings.update", strings.Join(keys, ", "))
	if err := s.db.SetSettings(r.Context(), kv); err != nil {
		internal(w, r, err)
		return
	}
	s.getSettings(w, r, u)
}

func (s *Server) system(w http.ResponseWriter, r *http.Request, _ *store.User) {
	counts, err := s.db.Counts(r.Context())
	if err != nil {
		internal(w, r, err)
		return
	}
	backups, _ := s.db.Backups()
	var last *store.BackupFile
	if len(backups) > 0 {
		last = &backups[0]
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version":     s.cfg.Version,
		"go":          runtime.Version(),
		"os":          runtime.GOOS + "/" + runtime.GOARCH,
		"started":     s.started.UnixMilli(),
		"db_size":     s.db.DBSize(),
		"counts":      counts,
		"data_dir":    s.cfg.DataDir,
		"base_url":    s.baseURL(r),
		"https":       strings.HasPrefix(s.baseURL(r), "https://"),
		"last_backup": last,
		"backups":     len(backups),
		// Whether webhooks, notification services and Immich may reach loopback/LAN
		// addresses (GT_BLOCK_PRIVATE_TARGETS). The admin UI warns when it should be on.
		"outbound_private_blocked": notify.BlockPrivate.Load(),
	})
}

// ── Admin: backups ───────────────────────────────────────────

func (s *Server) listBackups(w http.ResponseWriter, r *http.Request, _ *store.User) {
	bs, err := s.db.Backups()
	if err != nil {
		internal(w, r, err)
		return
	}
	if bs == nil {
		bs = []store.BackupFile{}
	}
	writeJSON(w, http.StatusOK, bs)
}

func (s *Server) createBackup(w http.ResponseWriter, r *http.Request, u *store.User) {
	name := "manual-" + time.Now().Format("20060102-150405") + ".db.gz"
	if err := s.db.Backup(r.Context(), filepath.Join(s.cfg.DataDir, "backups", name)); err != nil {
		internal(w, r, err)
		return
	}
	s.audit(r, u.ID, "backup.create", name)
	writeJSON(w, http.StatusCreated, map[string]string{"name": name})
}

func (s *Server) downloadBackup(w http.ResponseWriter, r *http.Request, _ *store.User) {
	p, ok := s.db.BackupPath(r.PathValue("name"))
	if !ok {
		fail(w, http.StatusNotFound, "backup not found")
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="`+filepath.Base(p)+`"`)
	http.ServeFile(w, r, p)
}

func (s *Server) deleteBackup(w http.ResponseWriter, r *http.Request, u *store.User) {
	p, ok := s.db.BackupPath(r.PathValue("name"))
	if !ok {
		fail(w, http.StatusNotFound, "backup not found")
		return
	}
	if err := os.Remove(p); err != nil {
		internal(w, r, err)
		return
	}
	s.audit(r, u.ID, "backup.delete", filepath.Base(p))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) restoreBackup(w http.ResponseWriter, r *http.Request, u *store.User) {
	p, ok := s.db.BackupPath(r.PathValue("name"))
	if !ok {
		fail(w, http.StatusNotFound, "backup not found")
		return
	}
	s.stageAndRestart(w, r, u.ID, p)
}

func (s *Server) restoreUpload(w http.ResponseWriter, r *http.Request, u *store.User) {
	tmp, _, err := receiveFile(w, r, filepath.Join(s.cfg.DataDir, "imports"))
	if err != nil {
		fail(w, http.StatusBadRequest, "upload failed: "+err.Error())
		return
	}
	defer os.Remove(tmp)
	s.stageAndRestart(w, r, u.ID, tmp)
}

func (s *Server) stageAndRestart(w http.ResponseWriter, r *http.Request, actorID int64, src string) {
	if err := store.StageRestore(r.Context(), s.cfg.DataDir, src); err != nil {
		fail(w, http.StatusBadRequest, "not a valid GeoTracker backup: "+err.Error())
		return
	}
	s.audit(r, actorID, "backup.restore", filepath.Base(src))
	writeJSON(w, http.StatusAccepted, map[string]bool{"restarting": true})
	go func() {
		time.Sleep(500 * time.Millisecond) // let the response reach the browser
		s.Restart <- struct{}{}
	}()
}
