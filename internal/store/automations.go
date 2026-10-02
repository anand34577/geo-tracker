package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// Automation is a geofence rule: at a saved place, on arrive and/or leave, run Actions.
type Automation struct {
	ID          int64           `json:"id"`
	UserID      int64           `json:"-"`
	PlaceID     int64           `json:"place_id"`
	PlaceName   string          `json:"place_name"`
	Name        string          `json:"name"`
	OnArrive    bool            `json:"on_arrive"`
	OnLeave     bool            `json:"on_leave"`
	Enabled     bool            `json:"enabled"`
	CooldownMin int             `json:"cooldown_min"`
	Actions     json.RawMessage `json:"actions"`
	LastFiredAt *int64          `json:"last_fired_at"`
	LastEvent   string          `json:"last_event"`
	LastResult  string          `json:"last_result"`
	CreatedAt   int64           `json:"created_at"`
}

const automationSelect = `SELECT a.id, a.user_id, a.place_id, p.name, a.name, a.on_arrive, a.on_leave, a.enabled, a.cooldown_min,
	a.actions, a.last_fired_at, a.last_event, a.last_result, a.created_at FROM automations a JOIN places p ON p.id = a.place_id`

func scanAutomation(r scanner) (Automation, error) {
	var a Automation
	var actions string
	err := r.Scan(&a.ID, &a.UserID, &a.PlaceID, &a.PlaceName, &a.Name, &a.OnArrive, &a.OnLeave, &a.Enabled, &a.CooldownMin,
		&actions, &a.LastFiredAt, &a.LastEvent, &a.LastResult, &a.CreatedAt)
	a.Actions = json.RawMessage(actions)
	return a, err
}

func (s *Store) queryAutomations(ctx context.Context, where string, args ...any) ([]Automation, error) {
	rows, err := s.R.QueryContext(ctx, automationSelect+` WHERE `+where+` ORDER BY a.name`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Automation{}
	for rows.Next() {
		a, err := scanAutomation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) Automations(ctx context.Context, userID int64) ([]Automation, error) {
	return s.queryAutomations(ctx, "a.user_id = ?", userID)
}

// EnabledAutomationsAt lists the active rules for one place.
func (s *Store) EnabledAutomationsAt(ctx context.Context, userID, placeID int64) ([]Automation, error) {
	return s.queryAutomations(ctx, "a.user_id = ? AND a.place_id = ? AND a.enabled = 1", userID, placeID)
}

func (s *Store) AutomationByID(ctx context.Context, userID, id int64) (*Automation, error) {
	a, err := scanAutomation(s.R.QueryRowContext(ctx, automationSelect+` WHERE a.id = ? AND a.user_id = ?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &a, err
}

func (s *Store) CountAutomations(ctx context.Context, userID int64) (n int, err error) {
	err = s.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM automations WHERE user_id = ?`, userID).Scan(&n)
	return
}

func (s *Store) CreateAutomation(ctx context.Context, a *Automation) error {
	res, err := s.W.ExecContext(ctx, `INSERT INTO automations (user_id, place_id, name, on_arrive, on_leave, enabled, cooldown_min, actions, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, a.UserID, a.PlaceID, a.Name, a.OnArrive, a.OnLeave, a.Enabled, a.CooldownMin, string(a.Actions), now())
	if err == nil {
		a.ID, _ = res.LastInsertId()
	}
	return err
}

func (s *Store) UpdateAutomation(ctx context.Context, a *Automation) error {
	res, err := s.W.ExecContext(ctx, `UPDATE automations SET place_id = ?, name = ?, on_arrive = ?, on_leave = ?, enabled = ?, cooldown_min = ?, actions = ?
		WHERE id = ? AND user_id = ?`, a.PlaceID, a.Name, a.OnArrive, a.OnLeave, a.Enabled, a.CooldownMin, string(a.Actions), a.ID, a.UserID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteAutomation(ctx context.Context, userID, id int64) error {
	_, err := s.W.ExecContext(ctx, `DELETE FROM automations WHERE id = ? AND user_id = ?`, id, userID)
	return err
}

// RecordAutomationRun remembers when a rule last fired, for which event and how it went
// (shown in the UI; the event and time drive the cooldown).
func (s *Store) RecordAutomationRun(ctx context.Context, id int64, event, result string) error {
	_, err := s.W.ExecContext(ctx, `UPDATE automations SET last_fired_at = ?, last_event = ?, last_result = ? WHERE id = ?`, now(), event, result, id)
	return err
}
