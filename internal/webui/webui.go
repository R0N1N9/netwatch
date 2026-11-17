// Package webui embeds the NetWatch GUI (Cyberpunk-styled web front-end) so the
// whole console ships inside one static binary.
package webui

import (
	"embed"
	"io/fs"
)

//go:embed assets
var assets embed.FS

// FS returns the front-end file system rooted at the asset directory.
func FS() fs.FS {
	sub, err := fs.Sub(assets, "assets")
	if err != nil {
		panic(err)
	}
	return sub
}
