//go:build frontend

package main

import (
	"embed"
	"io/fs"
)

// The web UI, copied here by `make frontend`. make build, make dev, make
// install and the release workflow all build with -tags frontend, so a missing
// or empty web build fails the compile instead of shipping a binary without
// the UI. index.html is named on its own so a copy without it fails too.
//
//go:embed web/dist web/dist/index.html
var webEmbed embed.FS

func webFS() fs.FS {
	sub, err := fs.Sub(webEmbed, "web/dist")
	if err != nil {
		panic(err)
	}
	return sub
}
