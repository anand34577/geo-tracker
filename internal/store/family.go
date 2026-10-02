package store

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"math"
	"time"
)

// ── Family groups ────────────────────────────────────────────

type Member struct {
	UserID        int64  `json:"user_id"`
	Name          string `json:"name"`
	Email         string `json:"email"`
	Role          string `json:"role"`   // owner | member
	Status        string `json:"status"` // invited | active
	ShareLive     bool   `json:"share_live"`
	ShareHistoryD int    `json:"share_history_days"` // 0 none, -1 all
	Precision     string `json:"precision"`          // exact | approx
	PausedUntil   *int64 `json:"paused_until"`
	Color         string `json:"color"`
	JoinedAt      *int64 `json:"joined_at"`
}

type Group struct {
	ID        int64    `json:"id"`
	Name      string   `json:"name"`
	CreatedAt int64    `json:"created_at"`
	Members   []Member `json:"members"`
}

const memberCols = `m.user_id, u.name, u.email, m.role, m.status, m.share_live, m.share_history_d, m.precision, m.paused_until, m.color, m.joined_at`

func scanMember(r scanner) (Member, error) {
	var m Member
	err := r.Scan(&m.UserID, &m.Name, &m.Email, &m.Role, &m.Status, &m.ShareLive, &m.ShareHistoryD, &m.Precision, &m.PausedUntil, &m.Color, &m.JoinedAt)
	return m, err
}

// memberColors is a colorblind-friendly categorical palette for people on the map.
var memberColors = []string{"#2563eb", "#db2777", "#16a34a", "#ea580c", "#7c3aed", "#0891b2", "#ca8a04", "#dc2626"}

func (s *Store) CreateGroup(ctx context.Context, name string, ownerID int64) (int64, error) {
	tx, err := s.W.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	t := now()
	res, err := tx.ExecContext(ctx, `INSERT INTO groups (name, created_by, created_at) VALUES (?, ?, ?)`, name, ownerID, t)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	// The creator chose to start a family group, so they share live (no history) until they change it.
	if _, err := tx.ExecContext(ctx, `INSERT INTO group_members (group_id, user_id, role, status, share_live, share_history_d, color, joined_at, created_at)
		VALUES (?, ?, 'owner', 'active', 1, 0, ?, ?, ?)`, id, ownerID, memberColors[0], t, t); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

// GroupsFor returns the user's groups (active or invited) with all members.
func (s *Store) GroupsFor(ctx context.Context, userID int64) ([]Group, error) {
	rows, err := s.R.QueryContext(ctx, `SELECT g.id, g.name, g.created_at FROM groups g JOIN group_members m ON m.group_id = g.id
		WHERE m.user_id = ? ORDER BY g.created_at`, userID)
	if err != nil {
		return nil, err
	}
	groups := []Group{}
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.ID, &g.Name, &g.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		groups = append(groups, g)
	}
	rows.Close()
	for i := range groups {
		mrows, err := s.R.QueryContext(ctx, `SELECT `+memberCols+` FROM group_members m JOIN users u ON u.id = m.user_id
			WHERE m.group_id = ? AND u.disabled_at IS NULL ORDER BY m.role DESC, u.name`, groups[i].ID)
		if err != nil {
			return nil, err
		}
		for mrows.Next() {
			m, err := scanMember(mrows)
			if err != nil {
				mrows.Close()
				return nil, err
			}
			groups[i].Members = append(groups[i].Members, m)
		}
		mrows.Close()
	}
	return groups, nil
}

func (s *Store) Membership(ctx context.Context, groupID, userID int64) (*Member, error) {
	m, err := scanMember(s.R.QueryRowContext(ctx, `SELECT `+memberCols+` FROM group_members m JOIN users u ON u.id = m.user_id
		WHERE m.group_id = ? AND m.user_id = ?`, groupID, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &m, err
}

func (s *Store) InviteMember(ctx context.Context, groupID, userID, invitedBy int64) error {
	var n int
	s.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM group_members WHERE group_id = ?`, groupID).Scan(&n)
	_, err := s.W.ExecContext(ctx, `INSERT INTO group_members (group_id, user_id, role, status, color, invited_by, created_at) VALUES (?, ?, 'member', 'invited', ?, ?, ?)`,
		groupID, userID, memberColors[n%len(memberColors)], invitedBy, now())
	if isUnique(err) { // primary-key conflicts are reported as UNIQUE constraint failures
		return ErrExists
	}
	return err
}

// UpdateSharing stores a member's own sharing choices; accepting an invite activates them.
func (s *Store) UpdateSharing(ctx context.Context, groupID, userID int64, m Member) error {
	res, err := s.W.ExecContext(ctx, `UPDATE group_members SET status = 'active', joined_at = COALESCE(joined_at, ?),
		share_live = ?, share_history_d = ?, precision = ?, paused_until = ? WHERE group_id = ? AND user_id = ?`,
		now(), m.ShareLive, m.ShareHistoryD, m.Precision, m.PausedUntil, groupID, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) RenameGroup(ctx context.Context, groupID int64, name string) error {
	_, err := s.W.ExecContext(ctx, `UPDATE groups SET name = ? WHERE id = ?`, name, groupID)
	return err
}

func (s *Store) DeleteGroup(ctx context.Context, groupID int64) error {
	_, err := s.W.ExecContext(ctx, `DELETE FROM groups WHERE id = ?`, groupID)
	return err
}

// RemoveMember removes someone (or lets them leave). The group disappears with its last
// member, and if the owner leaves, the longest-standing active member takes over.
func (s *Store) RemoveMember(ctx context.Context, groupID, userID int64) error {
	tx, err := s.W.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM group_members WHERE group_id = ? AND user_id = ?`, groupID, userID); err != nil {
		return err
	}
	// Alerts about each other no longer make sense once they share no group.
	if _, err := tx.ExecContext(ctx, `DELETE FROM alert_rules WHERE (watcher_id = ?1 OR subject_id = ?1) AND NOT EXISTS (
		SELECT 1 FROM group_members a JOIN group_members b ON a.group_id = b.group_id
		WHERE a.user_id = alert_rules.watcher_id AND b.user_id = alert_rules.subject_id)`, userID); err != nil {
		return err
	}
	var owners, members int
	tx.QueryRowContext(ctx, `SELECT COUNT(*) FILTER (WHERE role = 'owner'), COUNT(*) FROM group_members WHERE group_id = ?`, groupID).Scan(&owners, &members)
	switch {
	case members == 0:
		if _, err := tx.ExecContext(ctx, `DELETE FROM groups WHERE id = ?`, groupID); err != nil {
			return err
		}
	case owners == 0:
		if _, err := tx.ExecContext(ctx, `UPDATE group_members SET role = 'owner' WHERE group_id = ?1 AND user_id = (
			SELECT user_id FROM group_members WHERE group_id = ?1 ORDER BY status = 'active' DESC, joined_at LIMIT 1)`, groupID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Access is what a viewer may see of a subject, combined over all groups they share.
type Access struct {
	Live   bool  // current position
	From   int64 // earliest visible timestamp; math.MaxInt64 = no history
	Approx bool  // positions rounded to ~1 km
}

// AccessTo is the single place that decides what one person may see of another (spec §13.5).
func (s *Store) AccessTo(ctx context.Context, viewer, subject int64) (Access, error) {
	if viewer == subject {
		return Access{Live: true, From: 0}, nil
	}
	rows, err := s.R.QueryContext(ctx, `SELECT b.share_live, b.share_history_d, b.precision, b.paused_until
		FROM group_members a JOIN group_members b ON a.group_id = b.group_id
		JOIN users u ON u.id = b.user_id
		WHERE a.user_id = ? AND a.status = 'active' AND b.user_id = ? AND b.status = 'active' AND u.disabled_at IS NULL`, viewer, subject)
	if err != nil {
		return Access{}, err
	}
	defer rows.Close()
	acc := Access{From: math.MaxInt64, Approx: true}
	found := false
	t := now()
	for rows.Next() {
		var live bool
		var days int
		var precision string
		var paused sql.NullInt64
		if err := rows.Scan(&live, &days, &precision, &paused); err != nil {
			return Access{}, err
		}
		if paused.Valid && paused.Int64 > t {
			continue // ghost mode hides everything while active
		}
		found = true
		acc.Live = acc.Live || live
		switch {
		case days < 0:
			acc.From = 0
		case days > 0:
			acc.From = min(acc.From, time.UnixMilli(t).AddDate(0, 0, -days).UnixMilli())
		}
		if precision == "exact" {
			acc.Approx = false
		}
	}
	if !found {
		return Access{From: math.MaxInt64}, rows.Err()
	}
	return acc, rows.Err()
}

// LiveViewers lists who currently gets a subject's live position.
func (s *Store) LiveViewers(ctx context.Context, subject int64) ([]int64, error) {
	rows, err := s.R.QueryContext(ctx, `SELECT DISTINCT a.user_id FROM group_members a JOIN group_members b ON a.group_id = b.group_id
		WHERE b.user_id = ?1 AND a.user_id != ?1 AND a.status = 'active' AND b.status = 'active' AND b.share_live = 1
		AND (b.paused_until IS NULL OR b.paused_until < ?2)`, subject, now())
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

// FamilyPeople lists everyone the viewer shares an active group with.
func (s *Store) FamilyPeople(ctx context.Context, viewer int64) ([]Member, error) {
	rows, err := s.R.QueryContext(ctx, `SELECT `+memberCols+` FROM group_members m JOIN users u ON u.id = m.user_id
		WHERE m.status = 'active' AND u.disabled_at IS NULL AND m.user_id != ?1 AND m.group_id IN (
			SELECT group_id FROM group_members WHERE user_id = ?1 AND status = 'active')
		GROUP BY m.user_id ORDER BY u.name`, viewer)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Member{}
	for rows.Next() {
		m, err := scanMember(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ── Alert rules ──────────────────────────────────────────────

type AlertRule struct {
	ID        int64  `json:"id"`
	WatcherID int64  `json:"-"`
	SubjectID int64  `json:"subject_id"`
	Subject   string `json:"subject_name"`
	PlaceID   int64  `json:"place_id"`
	Place     string `json:"place_name"`
	OnArrive  bool   `json:"on_arrive"`
	OnLeave   bool   `json:"on_leave"`
}

func (s *Store) CreateAlert(ctx context.Context, a *AlertRule) error {
	res, err := s.W.ExecContext(ctx, `INSERT INTO alert_rules (watcher_id, subject_id, place_id, on_arrive, on_leave, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		a.WatcherID, a.SubjectID, a.PlaceID, a.OnArrive, a.OnLeave, now())
	if err == nil {
		a.ID, _ = res.LastInsertId()
	}
	return err
}

const alertSelect = `SELECT a.id, a.watcher_id, a.subject_id, u.name, a.place_id, p.name, a.on_arrive, a.on_leave
	FROM alert_rules a JOIN users u ON u.id = a.subject_id JOIN places p ON p.id = a.place_id`

func (s *Store) queryAlerts(ctx context.Context, where string, arg int64) ([]AlertRule, error) {
	rows, err := s.R.QueryContext(ctx, alertSelect+` WHERE `+where+` ORDER BY u.name`, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AlertRule{}
	for rows.Next() {
		var a AlertRule
		if err := rows.Scan(&a.ID, &a.WatcherID, &a.SubjectID, &a.Subject, &a.PlaceID, &a.Place, &a.OnArrive, &a.OnLeave); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) AlertsByWatcher(ctx context.Context, watcher int64) ([]AlertRule, error) {
	return s.queryAlerts(ctx, "a.watcher_id = ?", watcher)
}

func (s *Store) AlertsBySubject(ctx context.Context, subject int64) ([]AlertRule, error) {
	return s.queryAlerts(ctx, "a.subject_id = ?", subject)
}

func (s *Store) DeleteAlert(ctx context.Context, watcher, id int64) error {
	_, err := s.W.ExecContext(ctx, `DELETE FROM alert_rules WHERE id = ? AND watcher_id = ?`, id, watcher)
	return err
}

func (s *Store) PlaceByID(ctx context.Context, userID, id int64) (*Place, error) {
	var p Place
	err := s.R.QueryRowContext(ctx, `SELECT id, name, icon, lat, lon, radius_m, created_at FROM places WHERE id = ? AND user_id = ?`, id, userID).
		Scan(&p.ID, &p.Name, &p.Icon, &p.Lat, &p.Lon, &p.Radius, &p.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &p, err
}

// ── Trip mode corrections ────────────────────────────────────

func (s *Store) SetTripMode(ctx context.Context, userID, start int64, mode string) error {
	_, err := s.W.ExecContext(ctx, `INSERT INTO trip_modes (user_id, start_ts, mode) VALUES (?, ?, ?)
		ON CONFLICT (user_id, start_ts) DO UPDATE SET mode = excluded.mode`, userID, start, mode)
	return err
}

// applyTripModes overrides detected modes with the user's corrections. Trips are
// recomputed, so a correction matches the trip starting within 2 minutes of it.
func (s *Store) applyTripModes(ctx context.Context, userID int64, trips []TripRow) error {
	if len(trips) == 0 {
		return nil
	}
	const slack = 120_000
	rows, err := s.R.QueryContext(ctx, `SELECT start_ts, mode FROM trip_modes WHERE user_id = ? AND start_ts BETWEEN ? AND ?`,
		userID, trips[0].Start-slack, trips[len(trips)-1].Start+slack)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var start int64
		var mode string
		if err := rows.Scan(&start, &mode); err != nil {
			return err
		}
		for i := range trips {
			if d := trips[i].Start - start; d >= -slack && d <= slack {
				trips[i].Mode, trips[i].Confidence, trips[i].Corrected = mode, 1, true
			}
		}
	}
	return rows.Err()
}

// ── Audit log ────────────────────────────────────────────────

type AuditEntry struct {
	ID     int64  `json:"id"`
	TS     int64  `json:"ts"`
	Actor  string `json:"actor"`
	Action string `json:"action"`
	Target string `json:"target"`
	IP     string `json:"ip"`
}

func (s *Store) Audit(ctx context.Context, actorID int64, action, target, ip string) {
	s.W.ExecContext(ctx, `INSERT INTO audit_log (ts, actor_id, action, target, ip) VALUES (?, ?, ?, ?, ?)`, now(), nullID(actorID), action, target, ip)
}

// AuditLog pages backwards in time; before = 0 starts at the newest entry.
func (s *Store) AuditLog(ctx context.Context, before int64, limit int) ([]AuditEntry, error) {
	if before <= 0 {
		before = math.MaxInt64
	}
	rows, err := s.R.QueryContext(ctx, `SELECT a.id, a.ts, COALESCE(u.name, 'system'), a.action, a.target, a.ip FROM audit_log a
		LEFT JOIN users u ON u.id = a.actor_id WHERE a.id < ? ORDER BY a.id DESC LIMIT ?`, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuditEntry{}
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.TS, &e.Actor, &e.Action, &e.Target, &e.IP); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// PruneAudit keeps one year of history.
func (s *Store) PruneAudit(ctx context.Context) {
	s.W.ExecContext(ctx, `DELETE FROM audit_log WHERE ts < ?`, time.Now().AddDate(-1, 0, 0).UnixMilli())
}

// ── Sessions ─────────────────────────────────────────────────

type SessionInfo struct {
	ID        string `json:"id"` // hex of the stored hash; never the cookie itself
	UserAgent string `json:"user_agent"`
	IP        string `json:"ip"`
	CreatedAt int64  `json:"created_at"`
	ExpiresAt int64  `json:"expires_at"`
	Current   bool   `json:"current"`
}

func (s *Store) Sessions(ctx context.Context, userID int64, current []byte) ([]SessionInfo, error) {
	rows, err := s.R.QueryContext(ctx, `SELECT id_hash, COALESCE(user_agent, ''), COALESCE(ip, ''), created_at, expires_at FROM sessions
		WHERE user_id = ? AND expires_at > ? ORDER BY created_at DESC`, userID, now())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SessionInfo{}
	for rows.Next() {
		var h []byte
		var si SessionInfo
		if err := rows.Scan(&h, &si.UserAgent, &si.IP, &si.CreatedAt, &si.ExpiresAt); err != nil {
			return nil, err
		}
		si.ID, si.Current = hex.EncodeToString(h), string(h) == string(current)
		out = append(out, si)
	}
	return out, rows.Err()
}

func (s *Store) RevokeSession(ctx context.Context, userID int64, id string) error {
	h, err := hex.DecodeString(id)
	if err != nil {
		return ErrNotFound
	}
	_, err = s.W.ExecContext(ctx, `DELETE FROM sessions WHERE id_hash = ? AND user_id = ?`, h, userID)
	return err
}

// ── Retention ────────────────────────────────────────────────

func (s *Store) Retention(ctx context.Context, userID int64) (days int, err error) {
	err = s.R.QueryRowContext(ctx, `SELECT retention_days FROM users WHERE id = ?`, userID).Scan(&days)
	return
}

func (s *Store) SetRetention(ctx context.Context, userID int64, days int) error {
	_, err := s.W.ExecContext(ctx, `UPDATE users SET retention_days = ? WHERE id = ?`, days, userID)
	return err
}

// ApplyRetention deletes each user's raw points older than their retention period.
// Derived visits and trips are deliberately left alone (no rebuild is triggered): they were
// computed while the points existed and stay correct. A manual full rebuild would drop them.
func (s *Store) ApplyRetention(ctx context.Context) (int64, error) {
	rows, err := s.R.QueryContext(ctx, `SELECT id, retention_days FROM users WHERE retention_days > 0`)
	if err != nil {
		return 0, err
	}
	type pol struct {
		id   int64
		days int
	}
	var pols []pol
	for rows.Next() {
		var p pol
		if err := rows.Scan(&p.id, &p.days); err != nil {
			rows.Close()
			return 0, err
		}
		pols = append(pols, p)
	}
	rows.Close()
	var total int64
	for _, p := range pols {
		cutoff := time.Now().AddDate(0, 0, -p.days).UnixMilli()
		res, err := s.W.ExecContext(ctx, `DELETE FROM points WHERE user_id = ? AND ts < ?`, p.id, cutoff)
		if err != nil {
			return total, err
		}
		n, _ := res.RowsAffected()
		total += n
	}
	return total, nil
}

// ── Integrations ─────────────────────────────────────────────

func (s *Store) Integrations(ctx context.Context, userID int64) (string, error) {
	var raw string
	err := s.R.QueryRowContext(ctx, `SELECT integrations FROM users WHERE id = ?`, userID).Scan(&raw)
	return raw, err
}

func (s *Store) SetIntegrations(ctx context.Context, userID int64, raw string) error {
	_, err := s.W.ExecContext(ctx, `UPDATE users SET integrations = ? WHERE id = ?`, raw, userID)
	return err
}
