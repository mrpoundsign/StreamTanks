package web

import (
	"embed"
	"io/fs"
)

//go:embed public/*
var embeddedPublic embed.FS

// FS returns an fs.FS rooted at the embedded public/ directory.
func FS() (fs.FS, error) {
	return fs.Sub(embeddedPublic, "public")
}
