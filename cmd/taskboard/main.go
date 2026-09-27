package main

import "github.com/tcarac/taskboard/internal/cli"

// webFS is the embedded web UI with -tags frontend (web_embed.go) and nil
// without it (web_none.go).
func main() {
	cli.Execute(webFS())
}
