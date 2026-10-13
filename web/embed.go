// Package web embeds the built single-page app (web/dist) into the Go
// binary. `make web` fills dist; without it the binary still builds and
// serves a hint instead of the UI.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Dist returns the built app's files, rooted at dist/.
func Dist() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err) // dist/ is embedded at compile time; Sub cannot fail
	}
	return sub
}
