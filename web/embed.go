//go:build !webdist

// Package web embeds the current offline client without a Node/Vite dependency.
package web

import "embed"

//go:embed index.html css src vendor
var Files embed.FS
