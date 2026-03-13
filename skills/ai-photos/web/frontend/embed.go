package frontend

import (
	"embed"
	"io/fs"
)

//go:embed *.html
var assets embed.FS

func Files() fs.FS {
	return assets
}
