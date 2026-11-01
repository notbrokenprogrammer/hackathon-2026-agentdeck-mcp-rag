//go:build webdist

package web

import (
	"embed"
	"io/fs"
)

// Build the frontend into web/dist first, then: go build -tags webdist ./cmd/game-server
//
//go:embed dist
var dist embed.FS

var Files = func() fs.FS {
	f, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return f
}()
