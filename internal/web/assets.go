package web

import (
	"embed"
	"fmt"
	"io/fs"
)

//go:embed all:dist
var embeddedDist embed.FS

func DistFS() (fs.FS, error) {
	dist, err := fs.Sub(embeddedDist, "dist")
	if err != nil {
		return nil, fmt.Errorf("open embedded frontend: %w", err)
	}
	if _, err := fs.Stat(dist, "index.html"); err != nil {
		return nil, fmt.Errorf("embedded frontend missing index.html: %w", err)
	}
	return dist, nil
}
