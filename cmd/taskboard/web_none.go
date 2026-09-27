//go:build !frontend

package main

import "io/fs"

// Without -tags frontend the binary has no web UI, so go vet, go test, go build
// and go run work on a fresh checkout before the web app is ever built. Such a
// binary says so on start and at every web path (see internal/server).
func webFS() fs.FS { return nil }
