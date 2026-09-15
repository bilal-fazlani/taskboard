// Package livebuild tells the binary installed by `make install`, and release
// binaries, apart from every other build of taskboard.
//
// Only a marked build may open the default database in the OS config dir and
// listen on port 3010 by default. Anything else — make build, make dev,
// go build, go run, go test — must be pointed at a database with --db, so a
// development build cannot reach the live board by accident.
package livebuild

// Mark is "true" only in a marked build. `make install`, `make test-install`
// and .github/workflows/release.yml set it with
//
//	-ldflags "-X github.com/tcarac/taskboard/internal/livebuild.Mark=true"
//
// Nothing else may set it, except tests, which must restore it.
var Mark string

// Enabled reports whether this binary is a marked build.
func Enabled() bool { return Mark == "true" }
