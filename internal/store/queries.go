package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"
	"time"

	"geotracker/internal/geo"
	"geotracker/internal/timeline"
)

var (
	ErrNotFound = errors.New("not found")
	ErrExists   = errors.New("already exists")
)

func now() int64 { return time.Now().UnixMilli() }

// nullID maps 0 to SQL NULL for optional foreign keys.
func nullID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint")
}

type scanner interface{ Scan(...any) error }

// ── Users ────────────────────────────────────────────────────

type User struct {
	ID           int64           `json:"id"`
	Email        string          `json:"email"`
	Name         string          `json:"name"`
	Role         string          `json:"role"`
	Prefs        json.RawMessage `json:"prefs"`
	Disabled     bool            `json:"disabled"`
	CreatedAt    int64           `json:"created_at"`
	PasswordHash string          `json:"-"`
}

const userCols = `id, email, name, role, prefs, disabled_at IS NOT NULL, created_at, password_hash`

func scanUser(row scanner) (*User, error) {
	u := &User{}
	var prefs string
	err := row.Scan(&u.ID, &u.Email, &u.Name, &u.Role, &prefs, &u.Disabled, &u.CreatedAt, &u.PasswordHash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	u.Prefs = json.RawMessage(prefs)
	return u, err
}

func (s *Store) CountUsers(ctx context.Context) (n int, err error) {
	err = s.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return
}

func (s *Store) CreateUser(ctx context.Context, email, name, hash, role string) (*User, error) {
	res, err := s.W.ExecContext(ctx, `INSERT INTO users (email, name, password_hash, role, created_at) VALUES (?, ?, ?, ?, ?)`,
		email, name, hash, role, now())
	if isUnique(err) {
		return nil, ErrExists
	}
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.UserByID(ctx, id)
}

// CreateFirstAdmin succeeds only while there are no users, so setup cannot be replayed.
func (s *Store) CreateFirstAdmin(ctx context.Context, email, name, hash string) (*User, error) {
	res, err := s.W.ExecContext(ctx, `INSERT INTO users (email, name, password_hash, role, created_at)
		SELECT ?, ?, ?, 'admin', ? WHERE NOT EXISTS (SELECT 1 FROM users)`, email, name, hash, now())
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrExists
	}
	id, _ := res.LastInsertId()
	return s.UserByID(ctx, id)
}

func (s *Store) UserByID(ctx context.Context, id int64) (*User, error) {
	return scanUser(s.R.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE id = ?`, id))
}

func (s *Store) UserByEmail(ctx context.Context, email string) (*User, error) {
	return scanUser(s.R.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE email = ?`, email))
}

func (s *Store) Users(ctx context.Context) ([]*User, error) {
	rows, err := s.R.QueryContext(ctx, `SELECT `+userCols+` FROM users ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) UpdateUser(ctx context.Context, u *User) error {
	_, err := s.W.ExecContext(ctx, `UPDATE users SET email = ?, name = ?, role = ?, prefs = ?,
		disabled_at = CASE WHEN ? THEN COALESCE(disabled_at, ?) END WHERE id = ?`,
		u.Email, u.Name, u.Role, string(u.Prefs), u.Disabled, now(), u.ID)
	if isUnique(err) {
		return ErrExists
	}
	if err == nil && u.Disabled {
		_, err = s.W.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, u.ID)
	}
	return err
}

// SetPassword changes a password and signs out every other session.
func (s *Store) SetPassword(ctx context.Context, userID int64, hash string, keepSession []byte) error {
	if _, err := s.W.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, hash, userID); err != nil {
		return err
	}
	_, err := s.W.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ? AND id_hash IS NOT ?`, userID, keepSession)
	return err
}

// DeleteUser deletes an account with all its data. Groups it owned pass to the
// longest-standing remaining member, and groups left empty are removed.
func (s *Store) DeleteUser(ctx context.Context, id int64) error {
	tx, err := s.W.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM groups WHERE NOT EXISTS (SELECT 1 FROM group_members m WHERE m.group_id = groups.id)`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE group_members SET role = 'owner' WHERE rowid IN (
		SELECT (SELECT m.rowid FROM group_members m WHERE m.group_id = g.id ORDER BY m.status = 'active' DESC, m.joined_at LIMIT 1)
		FROM groups g WHERE NOT EXISTS (SELECT 1 FROM group_members o WHERE o.group_id = g.id AND o.role = 'owner'))`); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) CountActiveAdmins(ctx context.Context) (n int, err error) {
	err = s.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE role = 'admin' AND disabled_at IS NULL`).Scan(&n)
	return
}

// ── Sessions ─────────────────────────────────────────────────

func (s *Store) CreateSession(ctx context.Context, hash []byte, userID, expires int64, ua, ip string) error {
	_, err := s.W.ExecContext(ctx, `INSERT INTO sessions (id_hash, user_id, expires_at, user_agent, ip, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		hash, userID, expires, ua, ip, now())
	return err
}

// SessionUser returns the active user of a valid session and the session's expiry.
func (s *Store) SessionUser(ctx context.Context, hash []byte) (*User, int64, error) {
	var exp int64
	row := s.R.QueryRowContext(ctx, `SELECT s.expires_at, `+prefixed("u.", userCols)+`
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.id_hash = ? AND s.expires_at > ? AND u.disabled_at IS NULL`, hash, now())
	u := &User{}
	var prefs string
	err := row.Scan(&exp, &u.ID, &u.Email, &u.Name, &u.Role, &prefs, &u.Disabled, &u.CreatedAt, &u.PasswordHash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, 0, ErrNotFound
	}
	u.Prefs = json.RawMessage(prefs)
	return u, exp, err
}

func prefixed(p, cols string) string {
	parts := strings.Split(cols, ", ")
	for i := range parts {
		parts[i] = p + parts[i]
	}
	return strings.Join(parts, ", ")
}

func (s *Store) TouchSession(ctx context.Context, hash []byte, expires int64) error {
	_, err := s.W.ExecContext(ctx, `UPDATE sessions SET expires_at = ? WHERE id_hash = ?`, expires, hash)
	return err
}

func (s *Store) DeleteSession(ctx context.Context, hash []byte) error {
	_, err := s.W.ExecContext(ctx, `DELETE FROM sessions WHERE id_hash = ?`, hash)
	return err
}

func (s *Store) PurgeSessions(ctx context.Context) error {
	_, err := s.W.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < ?`, now())
	return err
}

// ── Devices & tokens ─────────────────────────────────────────

type Device struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Client      string `json:"client"`
	LastSeenAt  *int64 `json:"last_seen_at"`
	LastBattery *int   `json:"last_battery"`
	CreatedAt   int64  `json:"created_at"`
}

// CreateDevice creates a device with an ingest-only token: a lost phone cannot read history.
func (s *Store) CreateDevice(ctx context.Context, userID int64, name, client string, tokenHash []byte) (*Device, error) {
	return s.CreateDeviceWithScopes(ctx, userID, name, client, tokenHash, "ingest")
}

// CreateDeviceWithScopes also backs app sign-ins, whose tokens may read and write via the API.
func (s *Store) CreateDeviceWithScopes(ctx context.Context, userID int64, name, client string, tokenHash []byte, scopes string) (*Device, error) {
	tx, err := s.W.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	t := now()
	res, err := tx.ExecContext(ctx, `INSERT INTO devices (user_id, name, client, created_at) VALUES (?, ?, ?, ?)`, userID, name, client, t)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	if _, err := tx.ExecContext(ctx, `INSERT INTO tokens (user_id, device_id, name, hash, scopes, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		userID, id, name, tokenHash, scopes, t); err != nil {
		return nil, err
	}
	return &Device{ID: id, Name: name, Client: client, CreatedAt: t}, tx.Commit()
}

func (s *Store) Devices(ctx context.Context, userID int64) ([]Device, error) {
	rows, err := s.R.QueryContext(ctx, `SELECT id, name, client, last_seen_at, last_battery, created_at FROM devices WHERE user_id = ? ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Device{}
	for rows.Next() {
		var d Device
		if err := rows.Scan(&d.ID, &d.Name, &d.Client, &d.LastSeenAt, &d.LastBattery, &d.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// RotateDeviceToken replaces a device's token, e.g. after the phone was reset.
func (s *Store) RotateDeviceToken(ctx context.Context, userID, deviceID int64, tokenHash []byte) error {
	res, err := s.W.ExecContext(ctx, `UPDATE tokens SET hash = ?, last_used_at = NULL WHERE device_id = ? AND user_id = ?`, tokenHash, deviceID, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteDevice(ctx context.Context, userID, id int64) error {
	_, err := s.W.ExecContext(ctx, `DELETE FROM devices WHERE id = ? AND user_id = ?`, id, userID)
	return err
}

// TokenLookup resolves a token hash to its active owner.
func (s *Store) TokenLookup(ctx context.Context, hash []byte) (userID, deviceID int64, scopes string, err error) {
	var dev sql.NullInt64
	err = s.R.QueryRowContext(ctx, `SELECT t.user_id, t.device_id, t.scopes FROM tokens t JOIN users u ON u.id = t.user_id
		WHERE t.hash = ? AND u.disabled_at IS NULL`, hash).Scan(&userID, &dev, &scopes)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return userID, dev.Int64, scopes, err
}

func (s *Store) TouchDevice(ctx context.Context, deviceID int64, battery *int) error {
	t := now()
	_, err := s.W.ExecContext(ctx, `UPDATE devices SET last_seen_at = ?, last_battery = COALESCE(?, last_battery) WHERE id = ?`, t, battery, deviceID)
	if err == nil {
		_, err = s.W.ExecContext(ctx, `UPDATE tokens SET last_used_at = ? WHERE device_id = ?`, t, deviceID)
	}
	return err
}

// ── Points ───────────────────────────────────────────────────

// InsertPoints stores points (duplicates by user+timestamp are skipped) and marks the
// timeline stale from the earliest new point. Returns how many were actually added.
func (s *Store) InsertPoints(ctx context.Context, userID, deviceID, importID int64, pts []geo.Point) (int, error) {
	if len(pts) == 0 {
		return 0, nil
	}
	tx, err := s.W.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO points (user_id, device_id, import_id, ts, lat, lon, accuracy, altitude, speed, bearing, battery)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()
	added := 0
	minTS := int64(math.MaxInt64)
	for _, p := range pts {
		res, err := stmt.ExecContext(ctx, userID, nullID(deviceID), nullID(importID), p.TS, p.Lat, p.Lon, p.Accuracy, p.Altitude, p.Speed, p.Bearing, p.Battery)
		if err != nil {
			return 0, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			added++
			minTS = min(minTS, p.TS)
		}
	}
	if added > 0 {
		if err := markDirty(ctx, tx, userID, minTS); err != nil {
			return 0, err
		}
	}
	return added, tx.Commit()
}

type execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func markDirty(ctx context.Context, db execer, userID, from int64) error {
	_, err := db.ExecContext(ctx, `UPDATE users SET timeline_dirty_from = MIN(COALESCE(timeline_dirty_from, ?1), ?1) WHERE id = ?2`, from, userID)
	return err
}

func (s *Store) MarkDirty(ctx context.Context, userID, from int64) error {
	return markDirty(ctx, s.W, userID, from)
}

// MarkAllDirty is used at startup so an interrupted rebuild is always finished.
func (s *Store) MarkAllDirty(ctx context.Context) error {
	_, err := s.W.ExecContext(ctx, `UPDATE users SET timeline_dirty_from = COALESCE(timeline_dirty_from,
		(SELECT MAX(start_ts) FROM visits WHERE user_id = users.id), 0)`)
	return err
}

// ForEachPoint streams a user's points in [from, to] in time order.
// maxAcc > 0 skips fixes less accurate than that many meters.
func (s *Store) ForEachPoint(ctx context.Context, userID, from, to int64, maxAcc float64, fn func(geo.Point) error) error {
	q := `SELECT ts, lat, lon, accuracy, altitude, speed, bearing, battery FROM points WHERE user_id = ? AND ts BETWEEN ? AND ?`
	if maxAcc > 0 {
		q += ` AND (accuracy IS NULL OR accuracy <= ` + strconv.FormatFloat(maxAcc, 'f', -1, 64) + `)`
	}
	rows, err := s.R.QueryContext(ctx, q+` ORDER BY ts`, userID, from, to)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var p geo.Point
		if err := rows.Scan(&p.TS, &p.Lat, &p.Lon, &p.Accuracy, &p.Altitude, &p.Speed, &p.Bearing, &p.Battery); err != nil {
			return err
		}
		if err := fn(p); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (s *Store) CountPoints(ctx context.Context, userID, from, to int64) (n int, err error) {
	err = s.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM points WHERE user_id = ? AND ts BETWEEN ? AND ?`, userID, from, to).Scan(&n)
	return
}

func (s *Store) LatestPoint(ctx context.Context, userID int64) (*geo.Point, error) {
	var p geo.Point
	err := s.R.QueryRowContext(ctx, `SELECT ts, lat, lon, accuracy, altitude, speed, bearing, battery FROM points
		WHERE user_id = ? ORDER BY ts DESC LIMIT 1`, userID).
		Scan(&p.TS, &p.Lat, &p.Lon, &p.Accuracy, &p.Altitude, &p.Speed, &p.Bearing, &p.Battery)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &p, err
}

type PointStats struct {
	Count int    `json:"count"`
	First *int64 `json:"first"`
	Last  *int64 `json:"last"`
}

func (s *Store) PointStats(ctx context.Context, userID int64) (st PointStats, err error) {
	err = s.R.QueryRowContext(ctx, `SELECT COUNT(*), MIN(ts), MAX(ts) FROM points WHERE user_id = ?`, userID).Scan(&st.Count, &st.First, &st.Last)
	return
}

func (s *Store) DeletePoints(ctx context.Context, userID, from, to int64) (int64, error) {
	res, err := s.W.ExecContext(ctx, `DELETE FROM points WHERE user_id = ? AND ts BETWEEN ? AND ?`, userID, from, to)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		err = markDirty(ctx, s.W, userID, from)
	}
	return n, err
}

// ── Timeline (derived) ───────────────────────────────────────

// TakeDirty returns users whose timelines are stale and clears the markers atomically.
// Points arriving during the rebuild set the marker again, so nothing is lost.
func (s *Store) TakeDirty(ctx context.Context) (map[int64]int64, error) {
	tx, err := s.W.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id, timeline_dirty_from FROM users WHERE timeline_dirty_from IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	out := map[int64]int64{}
	for rows.Next() {
		var id, from int64
		if err := rows.Scan(&id, &from); err != nil {
			rows.Close()
			return nil, err
		}
		out[id] = from
	}
	rows.Close()
	if len(out) == 0 {
		return out, nil
	}
	if _, err := tx.ExecContext(ctx, `UPDATE users SET timeline_dirty_from = NULL`); err != nil {
		return nil, err
	}
	return out, tx.Commit()
}

// WindowStart is where a rebuild must begin: the start of the last visit at or before
// the first changed point, since new points may extend that visit.
func (s *Store) WindowStart(ctx context.Context, userID, dirtyFrom int64) (int64, error) {
	var start sql.NullInt64
	err := s.R.QueryRowContext(ctx, `SELECT MAX(start_ts) FROM visits WHERE user_id = ? AND start_ts <= ?`, userID, dirtyFrom).Scan(&start)
	return start.Int64, err
}

// ReplaceTimeline swaps all derived visits/trips from `from` onward in one transaction.
func (s *Store) ReplaceTimeline(ctx context.Context, userID, from int64, visits []timeline.Visit, trips []timeline.Trip) error {
	tx, err := s.W.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM visits WHERE user_id = ? AND start_ts >= ?`, userID, from); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM trips WHERE user_id = ? AND start_ts >= ?`, userID, from); err != nil {
		return err
	}
	vs, err := tx.PrepareContext(ctx, `INSERT INTO visits (user_id, start_ts, end_ts, lat, lon, radius_m, points) VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer vs.Close()
	for _, v := range visits {
		if _, err := vs.ExecContext(ctx, userID, v.Start, v.End, v.Lat, v.Lon, v.Radius, v.Points); err != nil {
			return err
		}
	}
	ts, err := tx.PrepareContext(ctx, `INSERT INTO trips (user_id, start_ts, end_ts, distance_m, mode, confidence) VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer ts.Close()
	for _, t := range trips {
		if _, err := ts.ExecContext(ctx, userID, t.Start, t.End, t.Distance, t.Mode, t.Confidence); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type VisitRow struct {
	ID int64 `json:"id"`
	timeline.Visit
	Name    string `json:"name,omitempty"`
	Address string `json:"address,omitempty"`
	City    string `json:"city,omitempty"`
	Country string `json:"country,omitempty"`
}

type TripRow struct {
	ID int64 `json:"id"`
	timeline.Trip
	Corrected bool `json:"corrected,omitempty"` // mode set by the user
}

// Timeline returns visits and trips overlapping [from, to].
func (s *Store) Timeline(ctx context.Context, userID, from, to int64) ([]VisitRow, []TripRow, error) {
	rows, err := s.R.QueryContext(ctx, `SELECT v.id, v.start_ts, v.end_ts, v.lat, v.lon, v.radius_m, v.points,
		COALESCE(g.name, ''), COALESCE(g.address, ''), COALESCE(g.city, ''), COALESCE(g.country_code, '')
		FROM visits v LEFT JOIN geocode_cache g ON g.id = v.geocode_id
		WHERE v.user_id = ? AND v.end_ts >= ? AND v.start_ts <= ? ORDER BY v.start_ts`, userID, from, to)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	visits := []VisitRow{}
	for rows.Next() {
		var v VisitRow
		if err := rows.Scan(&v.ID, &v.Start, &v.End, &v.Lat, &v.Lon, &v.Radius, &v.Points, &v.Name, &v.Address, &v.City, &v.Country); err != nil {
			return nil, nil, err
		}
		visits = append(visits, v)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	trows, err := s.R.QueryContext(ctx, `SELECT id, start_ts, end_ts, distance_m, mode, confidence FROM trips
		WHERE user_id = ? AND end_ts >= ? AND start_ts <= ? ORDER BY start_ts`, userID, from, to)
	if err != nil {
		return nil, nil, err
	}
	defer trows.Close()
	trips := []TripRow{}
	for trows.Next() {
		var t TripRow
		if err := trows.Scan(&t.ID, &t.Start, &t.End, &t.Distance, &t.Mode, &t.Confidence); err != nil {
			return nil, nil, err
		}
		trips = append(trips, t)
	}
	if err := trows.Err(); err != nil {
		return nil, nil, err
	}
	return visits, trips, s.applyTripModes(ctx, userID, trips)
}

type LatLon struct {
	ID       int64
	Lat, Lon float64
}

func (s *Store) UngeocodedVisits(ctx context.Context, limit int) ([]LatLon, error) {
	rows, err := s.R.QueryContext(ctx, `SELECT id, lat, lon FROM visits WHERE geocode_id IS NULL ORDER BY start_ts DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LatLon
	for rows.Next() {
		var v LatLon
		if err := rows.Scan(&v.ID, &v.Lat, &v.Lon); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// degrees per meter of latitude; longitude spans shrink with cos(lat).
const degPerM = 1.0 / 111_320

func bbox(lat, lon, m float64) (lat1, lat2, lon1, lon2 float64) {
	dLat := m * degPerM
	dLon := dLat / math.Max(0.01, math.Cos(lat*math.Pi/180))
	return lat - dLat, lat + dLat, lon - dLon, lon + dLon
}

// GeocodeNear finds a cached reverse-geocode result within m meters.
func (s *Store) GeocodeNear(ctx context.Context, lat, lon, m float64) (int64, bool, error) {
	a, b, c, d := bbox(lat, lon, m)
	rows, err := s.R.QueryContext(ctx, `SELECT id, lat, lon FROM geocode_cache WHERE lat BETWEEN ? AND ? AND lon BETWEEN ? AND ?`, a, b, c, d)
	if err != nil {
		return 0, false, err
	}
	defer rows.Close()
	best, bestD := int64(0), math.Inf(1)
	for rows.Next() {
		var g LatLon
		if err := rows.Scan(&g.ID, &g.Lat, &g.Lon); err != nil {
			return 0, false, err
		}
		if dd := geo.Distance(lat, lon, g.Lat, g.Lon); dd <= m && dd < bestD {
			best, bestD = g.ID, dd
		}
	}
	return best, best != 0, rows.Err()
}

type Geocode struct {
	Lat, Lon                         float64
	Provider                         string
	Name, Address, City, CountryCode string
}

func (s *Store) InsertGeocode(ctx context.Context, g Geocode) (int64, error) {
	res, err := s.W.ExecContext(ctx, `INSERT INTO geocode_cache (lat, lon, provider, name, address, city, country_code, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		g.Lat, g.Lon, g.Provider, g.Name, g.Address, g.City, g.CountryCode, now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) SetVisitGeocode(ctx context.Context, visitID, geocodeID int64) error {
	_, err := s.W.ExecContext(ctx, `UPDATE visits SET geocode_id = ? WHERE id = ?`, geocodeID, visitID)
	return err
}

// ── Places ───────────────────────────────────────────────────

type Place struct {
	ID        int64   `json:"id"`
	Name      string  `json:"name"`
	Icon      string  `json:"icon"`
	Lat       float64 `json:"lat"`
	Lon       float64 `json:"lon"`
	Radius    float64 `json:"radius"`
	Private   bool    `json:"private"` // privacy zone: hidden from everyone else
	CreatedAt int64   `json:"created_at"`
}

func (s *Store) Places(ctx context.Context, userID int64) ([]Place, error) {
	rows, err := s.R.QueryContext(ctx, `SELECT id, name, icon, lat, lon, radius_m, private, created_at FROM places WHERE user_id = ? ORDER BY name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Place{}
	for rows.Next() {
		var p Place
		if err := rows.Scan(&p.ID, &p.Name, &p.Icon, &p.Lat, &p.Lon, &p.Radius, &p.Private, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) CreatePlace(ctx context.Context, userID int64, p *Place) error {
	p.CreatedAt = now()
	res, err := s.W.ExecContext(ctx, `INSERT INTO places (user_id, name, icon, lat, lon, radius_m, private, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, p.Name, p.Icon, p.Lat, p.Lon, p.Radius, p.Private, p.CreatedAt)
	if err == nil {
		p.ID, _ = res.LastInsertId()
	}
	return err
}

func (s *Store) UpdatePlace(ctx context.Context, userID int64, p *Place) error {
	res, err := s.W.ExecContext(ctx, `UPDATE places SET name = ?, icon = ?, lat = ?, lon = ?, radius_m = ?, private = ? WHERE id = ? AND user_id = ?`,
		p.Name, p.Icon, p.Lat, p.Lon, p.Radius, p.Private, p.ID, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeletePlace(ctx context.Context, userID, id int64) error {
	_, err := s.W.ExecContext(ctx, `DELETE FROM places WHERE id = ? AND user_id = ?`, id, userID)
	return err
}

// VisitCenters returns every visit's time span and center, for matching visits to places.
// ponytail: matching happens in Go, not SQL; fine for decades of data (visits are ~10/day).
func (s *Store) VisitCenters(ctx context.Context, userID int64) ([]timeline.Visit, error) {
	rows, err := s.R.QueryContext(ctx, `SELECT start_ts, end_ts, lat, lon FROM visits WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []timeline.Visit
	for rows.Next() {
		var v timeline.Visit
		if err := rows.Scan(&v.Start, &v.End, &v.Lat, &v.Lon); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ── Imports ──────────────────────────────────────────────────

type Import struct {
	ID         int64  `json:"id"`
	UserID     int64  `json:"-"`
	Filename   string `json:"filename"`
	Format     string `json:"format"`
	Status     string `json:"status"`
	Added      int    `json:"added"`
	Duplicates int    `json:"duplicates"`
	Rejected   int    `json:"rejected"`
	Error      string `json:"error,omitempty"`
	CreatedAt  int64  `json:"created_at"`
	FinishedAt *int64 `json:"finished_at"`
}

const importCols = `id, user_id, filename, format, status, added, duplicates, rejected, error, created_at, finished_at`

func scanImport(r scanner) (*Import, error) {
	var i Import
	err := r.Scan(&i.ID, &i.UserID, &i.Filename, &i.Format, &i.Status, &i.Added, &i.Duplicates, &i.Rejected, &i.Error, &i.CreatedAt, &i.FinishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &i, err
}

func (s *Store) CreateImport(ctx context.Context, userID int64, filename string) (int64, error) {
	res, err := s.W.ExecContext(ctx, `INSERT INTO imports (user_id, filename, status, created_at) VALUES (?, ?, 'queued', ?)`, userID, filename, now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) NextImport(ctx context.Context) (*Import, error) {
	i, err := scanImport(s.R.QueryRowContext(ctx, `SELECT `+importCols+` FROM imports WHERE status = 'queued' ORDER BY id LIMIT 1`))
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	return i, err
}

func (s *Store) UpdateImport(ctx context.Context, i *Import) error {
	_, err := s.W.ExecContext(ctx, `UPDATE imports SET format = ?, status = ?, added = ?, duplicates = ?, rejected = ?, error = ?, finished_at = ? WHERE id = ?`,
		i.Format, i.Status, i.Added, i.Duplicates, i.Rejected, i.Error, i.FinishedAt, i.ID)
	return err
}

// RequeueImports restarts imports interrupted by a shutdown; dedupe makes re-running safe.
func (s *Store) RequeueImports(ctx context.Context) error {
	_, err := s.W.ExecContext(ctx, `UPDATE imports SET status = 'queued' WHERE status = 'running'`)
	return err
}

func (s *Store) Imports(ctx context.Context, userID int64) ([]*Import, error) {
	rows, err := s.R.QueryContext(ctx, `SELECT `+importCols+` FROM imports WHERE user_id = ? ORDER BY id DESC LIMIT 100`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Import{}
	for rows.Next() {
		i, err := scanImport(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// DeleteImport undoes an import: its points go with it (FK cascade) and the timeline is rebuilt.
func (s *Store) DeleteImport(ctx context.Context, userID, id int64) error {
	tx, err := s.W.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var minTS sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT MIN(ts) FROM points WHERE import_id = ? AND user_id = ?`, id, userID).Scan(&minTS); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM imports WHERE id = ? AND user_id = ? AND status != 'running'`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if minTS.Valid {
		if err := markDirty(ctx, tx, userID, minTS.Int64); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ── Settings ─────────────────────────────────────────────────

// DefaultSettings lists every instance setting with its default (ADR-011).
var DefaultSettings = map[string]string{
	"map_style_light": "https://tiles.openfreemap.org/styles/positron",
	"map_style_dark":  "https://tiles.openfreemap.org/styles/dark",
	"geocoder":        "nominatim", // nominatim | photon | none
	"geocoder_url":    "",          // empty = provider's public instance
	"backup_hour":     "3",         // local server hour, -1 disables
	"backup_keep":     "7",

	"map_default": "auto",                                                        // basemap id users get unless they pick another
	"map_mbtiles": "",                                                            // path to a local .mbtiles file for offline maps
	"map_glyphs":  "https://tiles.openfreemap.org/fonts/{fontstack}/{range}.pbf", // label fonts; empty = no labels (fully offline)

	"smtp_host":     "",
	"smtp_port":     "587",
	"smtp_user":     "",
	"smtp_password": "",
	"smtp_from":     "",
	"smtp_security": "starttls", // starttls | tls | none

	"oidc_enabled":       "false",
	"oidc_issuer":        "",
	"oidc_client_id":     "",
	"oidc_client_secret": "",
	"oidc_label":         "Single sign-on",
	"oidc_auto_register": "false", // create accounts for unknown users on first SSO login
}

// SecretSettings are never sent back to the browser in clear text.
var SecretSettings = map[string]bool{"smtp_password": true, "oidc_client_secret": true}

func (s *Store) Settings(ctx context.Context) (map[string]string, error) {
	out := map[string]string{}
	for k, v := range DefaultSettings {
		out[k] = v
	}
	rows, err := s.R.QueryContext(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		if _, known := DefaultSettings[k]; known {
			out[k] = v
		}
	}
	return out, rows.Err()
}

func (s *Store) SetSettings(ctx context.Context, kv map[string]string) error {
	tx, err := s.W.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for k, v := range kv {
		if _, err := tx.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value`, k, v); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ── System ───────────────────────────────────────────────────

type Counts struct {
	Users  int `json:"users"`
	Points int `json:"points"`
	Visits int `json:"visits"`
	Trips  int `json:"trips"`
}

func (s *Store) Counts(ctx context.Context) (c Counts, err error) {
	err = s.R.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM users), (SELECT COUNT(*) FROM points),
		(SELECT COUNT(*) FROM visits), (SELECT COUNT(*) FROM trips)`).Scan(&c.Users, &c.Points, &c.Visits, &c.Trips)
	return
}

// ── OIDC & notifications ─────────────────────────────────────

func (s *Store) UserByOIDC(ctx context.Context, sub string) (*User, error) {
	return scanUser(s.R.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE oidc_sub = ?`, sub))
}

func (s *Store) LinkOIDC(ctx context.Context, userID int64, sub string) error {
	_, err := s.W.ExecContext(ctx, `UPDATE users SET oidc_sub = ? WHERE id = ?`, sub, userID)
	return err
}

// CreateOIDCUser creates an SSO-only account ("!" is never a valid password hash).
func (s *Store) CreateOIDCUser(ctx context.Context, email, name, sub string) (*User, error) {
	res, err := s.W.ExecContext(ctx, `INSERT INTO users (email, name, password_hash, role, oidc_sub, created_at) VALUES (?, ?, '!', 'user', ?, ?)`,
		email, name, sub, now())
	if isUnique(err) {
		return nil, ErrExists
	}
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.UserByID(ctx, id)
}

func (s *Store) UserNotify(ctx context.Context, userID int64) (json.RawMessage, error) {
	var raw string
	err := s.R.QueryRowContext(ctx, `SELECT notify FROM users WHERE id = ?`, userID).Scan(&raw)
	return json.RawMessage(raw), err
}

func (s *Store) SetUserNotify(ctx context.Context, userID int64, raw json.RawMessage) error {
	_, err := s.W.ExecContext(ctx, `UPDATE users SET notify = ? WHERE id = ?`, string(raw), userID)
	return err
}

// NotifyUsers lists active users who configured notifications.
func (s *Store) NotifyUsers(ctx context.Context) (map[int64]json.RawMessage, error) {
	rows, err := s.R.QueryContext(ctx, `SELECT id, notify FROM users WHERE notify != '{}' AND disabled_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]json.RawMessage{}
	for rows.Next() {
		var id int64
		var raw string
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		out[id] = json.RawMessage(raw)
	}
	return out, rows.Err()
}

func (s *Store) AdminIDs(ctx context.Context) ([]int64, error) {
	rows, err := s.R.QueryContext(ctx, `SELECT id FROM users WHERE role = 'admin' AND disabled_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ── Share links ──────────────────────────────────────────────

type Share struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"-"`
	Name      string `json:"name"`
	Kind      string `json:"kind"` // live | range
	From      *int64 `json:"from"`
	To        *int64 `json:"to"`
	Precision string `json:"precision"` // exact | approx
	ExpiresAt int64  `json:"expires_at"`
	Views     int    `json:"views"`
	CreatedAt int64  `json:"created_at"`
}

const shareCols = `id, user_id, name, kind, from_ts, to_ts, precision, expires_at, views, created_at`

func scanShare(r scanner) (*Share, error) {
	var s Share
	err := r.Scan(&s.ID, &s.UserID, &s.Name, &s.Kind, &s.From, &s.To, &s.Precision, &s.ExpiresAt, &s.Views, &s.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &s, err
}

func (s *Store) CreateShare(ctx context.Context, sh *Share, tokenHash []byte) error {
	sh.CreatedAt = now()
	res, err := s.W.ExecContext(ctx, `INSERT INTO share_links (user_id, token_hash, name, kind, from_ts, to_ts, precision, expires_at, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sh.UserID, tokenHash, sh.Name, sh.Kind, sh.From, sh.To, sh.Precision, sh.ExpiresAt, sh.CreatedAt)
	if err == nil {
		sh.ID, _ = res.LastInsertId()
	}
	return err
}

func (s *Store) Shares(ctx context.Context, userID int64) ([]*Share, error) {
	rows, err := s.R.QueryContext(ctx, `SELECT `+shareCols+` FROM share_links WHERE user_id = ? ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Share{}
	for rows.Next() {
		sh, err := scanShare(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sh)
	}
	return out, rows.Err()
}

// ShareByToken returns an unexpired link; countView records an opening (not a live refresh).
func (s *Store) ShareByToken(ctx context.Context, hash []byte, countView bool) (*Share, error) {
	sh, err := scanShare(s.R.QueryRowContext(ctx, `SELECT `+shareCols+` FROM share_links WHERE token_hash = ? AND expires_at > ?`, hash, now()))
	if err == nil && countView {
		s.W.ExecContext(ctx, `UPDATE share_links SET views = views + 1 WHERE id = ?`, sh.ID)
	}
	return sh, err
}

func (s *Store) DeleteShare(ctx context.Context, userID, id int64) error {
	_, err := s.W.ExecContext(ctx, `DELETE FROM share_links WHERE id = ? AND user_id = ?`, id, userID)
	return err
}

// ── Insights ─────────────────────────────────────────────────

// QuarterHourCounts returns point counts per 15-minute bucket (bucket = ts / 900000), for calendar views.
// 15 minutes, not hours: every real timezone offset is a multiple of 15 minutes (India +5:30, Nepal +5:45),
// so a bucket never straddles local midnight.
func (s *Store) QuarterHourCounts(ctx context.Context, userID, from, to int64) (map[int64]int, error) {
	rows, err := s.R.QueryContext(ctx, `SELECT ts / 900000, COUNT(*) FROM points WHERE user_id = ? AND ts BETWEEN ? AND ? GROUP BY 1`, userID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int{}
	for rows.Next() {
		var h int64
		var n int
		if err := rows.Scan(&h, &n); err != nil {
			return nil, err
		}
		out[h] = n
	}
	return out, rows.Err()
}
