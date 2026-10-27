package maps

import "testing"

func TestCustomTemplates(t *testing.T) {
	c := Custom{Name: "Sat", URL: "https://{s}.tiles.example.com/{z}/{x}/{-y}{r}.png?key=abc"}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	if c.Kind != "raster" || c.Subdomains != "abc" || c.MaxZoom != 19 || c.TileSize != 256 || c.ID == "" || c.Proxy {
		t.Fatalf("normalized = %+v", c)
	}
	tiles, tms := c.BrowserTiles()
	if !tms || len(tiles) != 3 || tiles[1] != "https://b.tiles.example.com/{z}/{x}/{y}.png?key=abc" {
		t.Fatalf("browser tiles = %v %v", tiles, tms)
	}
	if got := c.TileURL(3, 1, 2); got != "https://a.tiles.example.com/3/1/5.png?key=abc" {
		t.Fatalf("tile url = %s", got)
	}
	q := Custom{URL: "https://t.example.com/{quadkey}.jpeg"}
	if got := q.TileURL(3, 3, 5); got != "https://t.example.com/213.jpeg" {
		t.Fatalf("quadkey = %s", got)
	}
	w := Custom{URL: "https://wms.example.com/?bbox={bbox-epsg-3857}"}
	if got := w.TileURL(0, 0, 0); got != "https://wms.example.com/?bbox=-20037508.342789244,-20037508.342789244,20037508.342789244,20037508.342789244" {
		t.Fatalf("bbox = %s", got)
	}

	s := Custom{Name: "Style", URL: "https://maps.example.com/style.json"}
	if err := s.Normalize(); err != nil || s.Kind != "style" {
		t.Fatalf("style: %v %+v", err, s)
	}
	for _, bad := range []Custom{
		{Name: "a", Kind: "raster", URL: "https://t.example.com/{z}/{x}.png"},
		{Name: "a", URL: "https://t.example.com/{z}/{x}/{y}/{foo}.png"},
		{Name: "a", URL: "ftp://t.example.com/{z}/{x}/{y}.png"},
		{Name: "a", Kind: "style", URL: "http://insecure.example.com/style.json"},
		{Name: "a", URL: "https://t.example.com/{z}/{x}/{y}.png", MinZoom: 10, MaxZoom: 5},
	} {
		if err := bad.Normalize(); err == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
	if err := NormalizeList([]Custom{{Name: "Same", URL: "https://a.example.com/{z}/{x}/{y}.png"}, {Name: "same", URL: "https://b.example.com/{z}/{x}/{y}.png"}}); err == nil {
		t.Fatal("duplicate names accepted")
	}
}
