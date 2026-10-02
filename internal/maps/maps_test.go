package maps

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestMBTiles(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.mbtiles")
	db, err := sql.Open("sqlite", p)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE metadata (name TEXT, value TEXT);
		CREATE TABLE tiles (zoom_level INTEGER, tile_column INTEGER, tile_row INTEGER, tile_data BLOB);
		INSERT INTO metadata VALUES ('format','pbf'),('name','India'),('maxzoom','14'),('bounds','68,6,97,36'),
		  ('json','{"vector_layers":[{"id":"water"},{"id":"transportation"},{"id":"place"}]}');
		INSERT INTO tiles VALUES (2, 1, 1, x'1f8b00');`) // z2 x1 TMS row 1 == XYZ y 2
	db.Close()
	if err != nil {
		t.Fatal(err)
	}

	m, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if !m.IsVector() || m.Name != "India" || len(m.Bounds) != 4 || len(m.VectorLayers) != 3 {
		t.Fatalf("metadata = %+v", m)
	}
	if _, err := m.Tile(context.Background(), 2, 1, 2); err != nil {
		t.Fatalf("XYZ y=2 should map to TMS row 1: %v", err)
	}
	if _, err := m.Tile(context.Background(), 2, 1, 1); err != ErrNoTile {
		t.Fatalf("missing tile: err = %v", err)
	}
	style := m.Style("http://x/{z}/{x}/{y}", "", false)
	layers := style["layers"].([]any)
	if len(layers) < 10 || style["glyphs"] != nil {
		t.Fatalf("OpenMapTiles style without labels expected, got %d layers", len(layers))
	}
	if n := len(m.Style("http://x/{z}/{x}/{y}", "http://g/{fontstack}/{range}.pbf", true)["layers"].([]any)); n <= len(layers) {
		t.Fatal("labels should add layers when glyphs are configured")
	}
}
