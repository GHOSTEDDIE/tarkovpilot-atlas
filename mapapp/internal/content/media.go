package content

import (
	"embed"
	"io/fs"
)

//go:embed media/*
var embeddedMedia embed.FS

func MediaFS() fs.FS {
	sub, err := fs.Sub(embeddedMedia, "media")
	if err != nil {
		panic(err)
	}
	return sub
}
