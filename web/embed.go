package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distribution embed.FS

var Dist = mustSub(distribution, "dist")

func mustSub(source fs.FS, directory string) fs.FS {
	sub, err := fs.Sub(source, directory)
	if err != nil {
		panic(err)
	}
	return sub
}
