package runtimelock

import (
	"embed"
	"io/fs"
)

//go:embed lock.json
var embedded []byte

//go:embed all:wheels
var carried embed.FS

func Embedded() (Lock, error) { return Parse(embedded) }

func Carried() fs.FS {
	wheels, _ := fs.Sub(carried, "wheels")
	return wheels
}
