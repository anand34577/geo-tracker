package store

import (
	"context"
	"path/filepath"
	"testing"

	"geotracker/internal/geo"
)

func TestPointsDedupeBackupRestore(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	u, err := s.CreateFirstAdmin(ctx, "a@example.com", "A", "x")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateFirstAdmin(ctx, "b@example.com", "B", "x"); err != ErrExists {
		t.Fatalf("second setup: err = %v, want ErrExists", err)
	}

	pts := []geo.Point{{TS: 1_700_000_000_000, Lat: 1, Lon: 1, Accuracy: geo.F(5)}, {TS: 1_700_000_060_000, Lat: 1.001, Lon: 1}}
	if n, err := s.InsertPoints(ctx, u.ID, 0, 0, pts); err != nil || n != 2 {
		t.Fatalf("insert: n=%d err=%v", n, err)
	}
	if n, _ := s.InsertPoints(ctx, u.ID, 0, 0, pts); n != 0 {
		t.Fatalf("duplicate insert added %d", n)
	}
	dirty, err := s.TakeDirty(ctx)
	if err != nil || dirty[u.ID] != pts[0].TS {
		t.Fatalf("dirty = %v, err = %v", dirty, err)
	}

	backup := filepath.Join(dir, "backups", "geotracker-test.db.gz")
	if err := s.Backup(ctx, backup); err != nil {
		t.Fatal(err)
	}
	s.DeletePoints(ctx, u.ID, 0, 1<<62)
	if err := StageRestore(ctx, dir, backup); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s, err = Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	st, _ := s.PointStats(ctx, u.ID)
	if st.Count != 2 {
		t.Fatalf("after restore: %d points, want 2", st.Count)
	}
	files, _ := s.Backups()
	if len(files) != 2 { // the test backup + the automatic pre-restore safety backup
		t.Fatalf("backups = %+v", files)
	}
}
