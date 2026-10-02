// Package web embeds the built frontend (run `npm run build` in this directory first).
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

func Dist() fs.FS {
	sub, _ := fs.Sub(dist, "dist")
	return sub
}
