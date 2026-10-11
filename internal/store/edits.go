package store

import (
	"context"
	"database/sql"
	"slices"
)

// ── Visit corrections ────────────────────────────────────────

// VisitEdit is the user's correction of one detected visit (see migration 0009).
type VisitEdit struct {
	Start   int64  `json:"start"`
	Name    string `json:"name"`
	PlaceID int64  `json:"place_id"` // 0 = automatic
	NoPlace bool   `json:"no_place"`
	Hidden  bool   `json:"hidden"`
	MergeTo int64  `json:"merge_to"`
}

func (e VisitEdit) empty() bool {
	return e.Name == "" && e.PlaceID == 0 && !e.NoPlace && !e.Hidden && e.MergeTo == 0
}

// visitSlack matches an edit to a recomputed visit starting within 2 minutes of it.
const visitSlack = 120_000

// VisitEditAt returns the edit for the visit starting near `start` (zero value if none).
func (s *Store) VisitEditAt(ctx context.Context, userID, start int64) (VisitEdit, error) {
	var e VisitEdit
	var place sql.NullInt64
	err := s.R.QueryRowContext(ctx, `SELECT start_ts, name, place_id, no_place, hidden, merge_to FROM visit_edits
		WHERE user_id = ? AND start_ts BETWEEN ? AND ? ORDER BY ABS(start_ts - ?) LIMIT 1`,
		userID, start-visitSlack, start+visitSlack, start).Scan(&e.Start, &e.Name, &place, &e.NoPlace, &e.Hidden, &e.MergeTo)
	if err == sql.ErrNoRows {
		return VisitEdit{Start: start}, nil
	}
	e.PlaceID = place.Int64
	return e, err
}

// SaveVisitEdit stores a correction; an edit that changes nothing is removed.
func (s *Store) SaveVisitEdit(ctx context.Context, userID int64, e VisitEdit) error {
	tx, err := s.W.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM visit_edits WHERE user_id = ? AND start_ts BETWEEN ? AND ?`, userID, e.Start-visitSlack, e.Start+visitSlack); err != nil {
		return err
	}
	if !e.empty() {
		if _, err := tx.ExecContext(ctx, `INSERT INTO visit_edits (user_id, start_ts, name, place_id, no_place, hidden, merge_to) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			userID, e.Start, e.Name, nullID(e.PlaceID), e.NoPlace, e.Hidden, e.MergeTo); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// applyVisitEdits applies the user's corrections to visits (sorted by start) and drops the
// trips a merge swallowed. ponytail: a merge is only seen when its first visit is in the
// queried range; merges spanning a range boundary show unmerged on the far side.
func (s *Store) applyVisitEdits(ctx context.Context, userID int64, visits []VisitRow, trips []TripRow) ([]VisitRow, []TripRow, error) {
	if len(visits) == 0 {
		return visits, trips, nil
	}
	rows, err := s.R.QueryContext(ctx, `SELECT start_ts, name, place_id, no_place, hidden, merge_to FROM visit_edits WHERE user_id = ? AND start_ts BETWEEN ? AND ?`,
		userID, visits[0].Start-visitSlack, visits[len(visits)-1].Start+visitSlack)
	if err != nil {
		return nil, nil, err
	}
	var edits []VisitEdit
	for rows.Next() {
		var e VisitEdit
		var place sql.NullInt64
		if err := rows.Scan(&e.Start, &e.Name, &place, &e.NoPlace, &e.Hidden, &e.MergeTo); err != nil {
			rows.Close()
			return nil, nil, err
		}
		e.PlaceID = place.Int64
		edits = append(edits, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil || len(edits) == 0 {
		return visits, trips, err
	}

	var swallowed [][2]int64 // [from, to] spans absorbed by merges
	out := visits[:0:0]
	for _, v := range visits {
		if n := len(swallowed); n > 0 && v.Start > swallowed[n-1][0] && v.Start <= swallowed[n-1][1] {
			continue // absorbed by the merged visit before it
		}
		for _, e := range edits {
			if d := v.Start - e.Start; d < -visitSlack || d > visitSlack {
				continue
			}
			v.Edited = true
			v.CustomName, v.PinnedPlace, v.NoPlace = e.Name, e.PlaceID, e.NoPlace
			if e.Hidden {
				v.Hidden = true
			}
			if e.MergeTo > v.End {
				swallowed = append(swallowed, [2]int64{v.End, e.MergeTo})
				v.MergedTo, v.End = e.MergeTo, e.MergeTo
			}
		}
		if !v.Hidden {
			out = append(out, v)
		}
	}
	trips = slices.DeleteFunc(trips, func(t TripRow) bool {
		for _, sw := range swallowed {
			if t.Start >= sw[0] && t.Start < sw[1] {
				return true
			}
		}
		return false
	})
	return out, trips, nil
}

// ── Device health ────────────────────────────────────────────

type DeviceFix struct {
	TS      int64
	Lat     float64
	Lon     float64
	Battery *int
}

// DeviceFixes returns one device's fixes in [from, to], oldest first.
func (s *Store) DeviceFixes(ctx context.Context, userID, deviceID, from, to int64) ([]DeviceFix, error) {
	rows, err := s.R.QueryContext(ctx, `SELECT ts, lat, lon, battery FROM points WHERE device_id = ? AND user_id = ? AND ts BETWEEN ? AND ? ORDER BY ts`,
		deviceID, userID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DeviceFix
	for rows.Next() {
		var f DeviceFix
		if err := rows.Scan(&f.TS, &f.Lat, &f.Lon, &f.Battery); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// LastFixBefore is the device's latest fix before ts, to tell whether a range starts mid-gap.
func (s *Store) LastFixBefore(ctx context.Context, userID, deviceID, ts int64) (*DeviceFix, error) {
	var f DeviceFix
	err := s.R.QueryRowContext(ctx, `SELECT ts, lat, lon, battery FROM points WHERE device_id = ? AND user_id = ? AND ts < ? ORDER BY ts DESC LIMIT 1`,
		deviceID, userID, ts).Scan(&f.TS, &f.Lat, &f.Lon, &f.Battery)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &f, err
}
