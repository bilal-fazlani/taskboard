// Package buildinfo says which build of taskboard is running: its version,
// the commit it was built from, and whether it is a development build.
//
// Version and Commit are set at build time with
//
//	-ldflags "-X github.com/tcarac/taskboard/internal/buildinfo.Version=<v>
//	          -X github.com/tcarac/taskboard/internal/buildinfo.Commit=<sha>"
//
// by the Makefile (make build, make dev, make install, make test-install) and
// by .github/workflows/release.yml. A build that sets neither (go build, go
// test) falls back to what the Go toolchain stamped into the binary, and to
// "unknown" when it stamped nothing (go run).
//
// A development build is any build livebuild does not mark: the same line
// that keeps it off the live database (see internal/livebuild).
package buildinfo

import (
	"fmt"
	"runtime/debug"

	"github.com/tcarac/taskboard/internal/livebuild"
)

// Version is the version this binary was built as: the release tag for a
// release binary, `git describe --tags --always --dirty` for a Makefile build.
var Version string

// Commit is the full hash of the commit this binary was built from.
var Commit string

// Unknown stands in for a version or commit nothing recorded.
const Unknown = "unknown"

// Info is the running build, as `taskboard --version` prints it and
// GET /api/version answers it.
type Info struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	// Dev is true for every build but a release binary and the one
	// `make install` builds.
	Dev bool `json:"dev"`
}

// readBuildInfo is debug.ReadBuildInfo; tests replace it.
var readBuildInfo = debug.ReadBuildInfo

// Get reports the running build.
func Get() Info {
	info := Info{Version: Version, Commit: Commit, Dev: !livebuild.Enabled()}
	if info.Version != "" && info.Commit != "" {
		return info
	}
	if bi, ok := readBuildInfo(); ok {
		if info.Version == "" && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			info.Version = bi.Main.Version
		}
		if info.Commit == "" {
			for _, s := range bi.Settings {
				if s.Key == "vcs.revision" {
					info.Commit = s.Value
				}
			}
		}
	}
	if info.Version == "" {
		info.Version = Unknown
	}
	if info.Commit == "" {
		info.Commit = Unknown
	}
	return info
}

// String is the one line `taskboard --version` prints after the name, e.g.
// "v0.2.0 (commit 1f3c…)" or "v0.1.0-12-g1f3c9ab (commit 1f3c…, dev build)".
func (i Info) String() string {
	if i.Dev {
		return fmt.Sprintf("%s (commit %s, dev build)", i.Version, i.Commit)
	}
	return fmt.Sprintf("%s (commit %s)", i.Version, i.Commit)
}
