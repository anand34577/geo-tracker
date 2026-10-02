// Package store owns the SQLite database: opening, migrations, backups, restores and all queries.
package store

import (
	"bufio"
	"compress/gzip"
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure Go, no CGO (ADR-002)
)

//go:embed migrations/*.sql
var migrationFS embed.FS

const (
	dbFile      = "geotracker.db"
	restoreFile = "restore.db" // staged by StageRestore, swapped in on next start
)

// Store has one writer connection (SQLite allows one writer) and a small reader pool,
// so long reads such as exports never block ingestion.
type Store struct {
	W, R *sql.DB
	Dir  string
}

const pragmas = "?_pragma=busy_timeout(10000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=cache_size(-32000)"

func Open(ctx context.Context, dir string) (*Store, error) {
	if err := os.MkdirAll(filepath.Join(dir, "backups"), 0o750); err != nil {
		return nil, err
	}
	if err := applyStagedRestore(ctx, dir); err != nil {
		return nil, fmt.Errorf("restore: %w", err)
	}
	p := filepath.Join(dir, dbFile)
	w, err := sql.Open("sqlite", p+pragmas+"&_txlock=immediate")
	if err != nil {
		return nil, err
	}
	w.SetMaxOpenConns(1)
	r, err := sql.Open("sqlite", p+pragmas)
	if err != nil {
		w.Close()
		return nil, err
	}
	r.SetMaxOpenConns(4)
	s := &Store{W: w, R: r, Dir: dir}
	if err := s.migrate(ctx); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	s.R.Close()
	return s.W.Close()
}

// migrate applies embedded forward-only migrations, backing up first when upgrading (ADR-017).
func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.W.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL)`); err != nil {
		return err
	}
	var cur int
	if err := s.W.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&cur); err != nil {
		return err
	}
	files, _ := fs.Glob(migrationFS, "migrations/*.sql")
	sort.Strings(files)
	backedUp := false
	for _, f := range files {
		v, err := strconv.Atoi(strings.SplitN(path.Base(f), "_", 2)[0])
		if err != nil {
			return fmt.Errorf("bad migration name %s", f)
		}
		if v <= cur {
			continue
		}
		if cur > 0 && !backedUp {
			name := fmt.Sprintf("pre-upgrade-v%d-%s.db.gz", cur, time.Now().Format("20060102-150405"))
			if err := s.Backup(ctx, filepath.Join(s.Dir, "backups", name)); err != nil {
				return fmt.Errorf("pre-upgrade backup: %w", err)
			}
			backedUp = true
		}
		body, _ := migrationFS.ReadFile(f)
		tx, err := s.W.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: %w", f, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations VALUES (?, ?)`, v, now()); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		slog.Info("applied migration", "file", path.Base(f))
	}
	return nil
}

// Backup writes a consistent, verified, gzipped snapshot while the app keeps running.
func (s *Store) Backup(ctx context.Context, dest string) error {
	return snapshot(ctx, s.R, dest)
}

func snapshot(ctx context.Context, db *sql.DB, dest string) error {
	tmp := dest + ".tmp"
	os.Remove(tmp)
	defer os.Remove(tmp)
	if _, err := db.ExecContext(ctx, `VACUUM INTO ?`, tmp); err != nil {
		return err
	}
	if err := checkDB(ctx, tmp); err != nil {
		return fmt.Errorf("verification failed: %w", err)
	}
	return gzipFile(tmp, dest)
}

func checkDB(ctx context.Context, p string) error {
	db, err := sql.Open("sqlite", p)
	if err != nil {
		return err
	}
	defer db.Close()
	var res string
	if err := db.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&res); err != nil {
		return err
	}
	if res != "ok" {
		return errors.New(res)
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE name = 'points'`).Scan(&n); err != nil || n == 0 {
		return errors.New("not a GeoTracker database")
	}
	return nil
}

func gzipFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	part := dst + ".part"
	out, err := os.Create(part)
	if err != nil {
		return err
	}
	zw := gzip.NewWriter(out)
	_, err = io.Copy(zw, in)
	if cerr := zw.Close(); err == nil {
		err = cerr
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(part)
		return err
	}
	return os.Rename(part, dst)
}

// StageRestore validates a backup (.db or .db.gz) and stages it; it replaces the
// live database on the next start. ponytail: restart-to-restore avoids hot-swapping
// open connections; Docker/systemd restart policies make it one click.
func StageRestore(ctx context.Context, dir, src string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	br := bufio.NewReader(in)
	var r io.Reader = br
	if magic, _ := br.Peek(2); len(magic) == 2 && magic[0] == 0x1f && magic[1] == 0x8b {
		zr, err := gzip.NewReader(br)
		if err != nil {
			return err
		}
		r = zr
	}
	part := filepath.Join(dir, restoreFile+".part")
	out, err := os.Create(part)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, r)
	out.Close()
	if err == nil {
		err = checkDB(ctx, part)
	}
	if err != nil {
		os.Remove(part)
		return err
	}
	return os.Rename(part, filepath.Join(dir, restoreFile))
}

func applyStagedRestore(ctx context.Context, dir string) error {
	staged := filepath.Join(dir, restoreFile)
	if _, err := os.Stat(staged); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	live := filepath.Join(dir, dbFile)
	if _, err := os.Stat(live); err == nil {
		old, err := sql.Open("sqlite", live)
		if err != nil {
			return err
		}
		name := "pre-restore-" + time.Now().Format("20060102-150405") + ".db.gz"
		err = snapshot(ctx, old, filepath.Join(dir, "backups", name))
		old.Close()
		if err != nil {
			return fmt.Errorf("safety backup of current database: %w", err)
		}
	}
	for _, suffix := range []string{"-wal", "-shm", ""} {
		if err := os.Remove(live + suffix); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	slog.Warn("restored database from staged backup")
	return os.Rename(staged, live)
}

type BackupFile struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	Created int64  `json:"created"`
}

func (s *Store) Backups() ([]BackupFile, error) {
	entries, err := os.ReadDir(filepath.Join(s.Dir, "backups"))
	if err != nil {
		return nil, err
	}
	var out []BackupFile
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || e.IsDir() || !strings.HasSuffix(e.Name(), ".db.gz") {
			continue
		}
		out = append(out, BackupFile{e.Name(), info.Size(), info.ModTime().UnixMilli()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created > out[j].Created })
	return out, nil
}

// BackupPath resolves a backup name safely inside the backups directory.
func (s *Store) BackupPath(name string) (string, bool) {
	if name != filepath.Base(name) || !strings.HasSuffix(name, ".db.gz") {
		return "", false
	}
	p := filepath.Join(s.Dir, "backups", name)
	_, err := os.Stat(p)
	return p, err == nil
}

// Rotate keeps the newest `keep` scheduled backups and the newest 3 safety backups.
// ponytail: simple count-based rotation; add daily/weekly/monthly tiers if users ask.
func (s *Store) Rotate(keep int) {
	files, _ := s.Backups()
	seen := map[string]int{}
	for _, f := range files {
		kind, limit := "safety", 3
		if strings.HasPrefix(f.Name, "geotracker-") {
			kind, limit = "scheduled", keep
		} else if strings.HasPrefix(f.Name, "manual-") {
			continue // made by an admin on purpose; theirs to delete
		}
		seen[kind]++
		if seen[kind] > limit {
			os.Remove(filepath.Join(s.Dir, "backups", f.Name))
		}
	}
}

func (s *Store) DBSize() int64 {
	var total int64
	for _, suffix := range []string{"", "-wal"} {
		if st, err := os.Stat(filepath.Join(s.Dir, dbFile+suffix)); err == nil {
			total += st.Size()
		}
	}
	return total
}
