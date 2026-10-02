package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Export is a data export built in the background.
type Export struct {
	ID         int64  `json:"id"`
	UserID     int64  `json:"-"`
	Format     string `json:"format"`
	From       *int64 `json:"from"`
	To         *int64 `json:"to"`
	Status     string `json:"status"` // queued | running | done | failed
	Size       int64  `json:"size"`
	Error      string `json:"error,omitempty"`
	CreatedAt  int64  `json:"created_at"`
	FinishedAt *int64 `json:"finished_at"`
}

const exportCols = `id, user_id, format, from_ts, to_ts, status, size, error, created_at, finished_at`

func scanExport(r scanner) (*Export, error) {
	var e Export
	err := r.Scan(&e.ID, &e.UserID, &e.Format, &e.From, &e.To, &e.Status, &e.Size, &e.Error, &e.CreatedAt, &e.FinishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &e, err
}

// MaxActiveExports caps queued+running exports per user.
const MaxActiveExports = 3

// CreateExport queues an export; ErrExists means the user already has too many waiting.
func (s *Store) CreateExport(ctx context.Context, userID int64, format string, from, to *int64) (int64, error) {
	res, err := s.W.ExecContext(ctx, `INSERT INTO exports (user_id, format, from_ts, to_ts, status, created_at)
		SELECT ?1, ?2, ?3, ?4, 'queued', ?5 WHERE (SELECT COUNT(*) FROM exports WHERE user_id = ?1 AND status IN ('queued','running')) < ?6`,
		userID, format, from, to, now(), MaxActiveExports)
	if err != nil {
		return 0, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return 0, ErrExists
	}
	return res.LastInsertId()
}

func (s *Store) Exports(ctx context.Context, userID int64) ([]*Export, error) {
	rows, err := s.R.QueryContext(ctx, `SELECT `+exportCols+` FROM exports WHERE user_id = ? ORDER BY id DESC LIMIT 20`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Export{}
	for rows.Next() {
		e, err := scanExport(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) ExportByID(ctx context.Context, userID, id int64) (*Export, error) {
	return scanExport(s.R.QueryRowContext(ctx, `SELECT `+exportCols+` FROM exports WHERE id = ? AND user_id = ?`, id, userID))
}

func (s *Store) NextExport(ctx context.Context) (*Export, error) {
	e, err := scanExport(s.R.QueryRowContext(ctx, `SELECT `+exportCols+` FROM exports WHERE status = 'queued' ORDER BY id LIMIT 1`))
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	return e, err
}

func (s *Store) UpdateExport(ctx context.Context, e *Export) error {
	_, err := s.W.ExecContext(ctx, `UPDATE exports SET status = ?, size = ?, error = ?, finished_at = ? WHERE id = ?`,
		e.Status, e.Size, e.Error, e.FinishedAt, e.ID)
	return err
}

func (s *Store) DeleteExport(ctx context.Context, userID, id int64) error {
	res, err := s.W.ExecContext(ctx, `DELETE FROM exports WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// RequeueExports restarts exports interrupted by a shutdown.
func (s *Store) RequeueExports(ctx context.Context) error {
	_, err := s.W.ExecContext(ctx, `UPDATE exports SET status = 'queued' WHERE status = 'running'`)
	return err
}

// PruneExports deletes exports older than keep and returns the ids still alive, so the
// caller can remove orphaned files.
func (s *Store) PruneExports(ctx context.Context, keep time.Duration) (map[int64]bool, error) {
	if _, err := s.W.ExecContext(ctx, `DELETE FROM exports WHERE status != 'running' AND created_at < ?`, time.Now().Add(-keep).UnixMilli()); err != nil {
		return nil, err
	}
	rows, err := s.R.QueryContext(ctx, `SELECT id FROM exports`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	live := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		live[id] = true
	}
	return live, rows.Err()
}
