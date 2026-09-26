package buildinfo

import (
	"runtime/debug"
	"testing"

	"github.com/tcarac/taskboard/internal/livebuild"
)

// set gives Version, Commit, livebuild.Mark and the toolchain's build info
// the values of one test, restoring them afterwards.
func set(t *testing.T, version, commit string, live bool, stamped *debug.BuildInfo) {
	t.Helper()
	prevV, prevC, prevM, prevR := Version, Commit, livebuild.Mark, readBuildInfo
	t.Cleanup(func() { Version, Commit, livebuild.Mark, readBuildInfo = prevV, prevC, prevM, prevR })
	Version, Commit = version, commit
	livebuild.Mark = ""
	if live {
		livebuild.Mark = "true"
	}
	readBuildInfo = func() (*debug.BuildInfo, bool) { return stamped, stamped != nil }
}

func TestGetUsesBuildTimeValues(t *testing.T) {
	stamped := &debug.BuildInfo{
		Main:     debug.Module{Version: "v9.9.9"},
		Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "ffff"}},
	}
	set(t, "v0.2.0", "1f3c9ab", true, stamped)

	got := Get()
	want := Info{Version: "v0.2.0", Commit: "1f3c9ab", Dev: false}
	if got != want {
		t.Fatalf("Get() = %+v, want %+v", got, want)
	}
	if s := got.String(); s != "v0.2.0 (commit 1f3c9ab)" {
		t.Errorf("String() = %q", s)
	}
}

func TestGetSaysDevBuild(t *testing.T) {
	set(t, "v0.1.0-3-g1f3c9ab", "1f3c9ab", false, nil)

	got := Get()
	if !got.Dev {
		t.Fatalf("Get().Dev = false for an unmarked build")
	}
	if s := got.String(); s != "v0.1.0-3-g1f3c9ab (commit 1f3c9ab, dev build)" {
		t.Errorf("String() = %q", s)
	}
}

func TestGetFallsBackToToolchainStamp(t *testing.T) {
	stamped := &debug.BuildInfo{
		Main:     debug.Module{Version: "v0.1.1-0.20260926000000-1f3c9ab00000+dirty"},
		Settings: []debug.BuildSetting{{Key: "vcs", Value: "git"}, {Key: "vcs.revision", Value: "1f3c9ab00000"}},
	}
	set(t, "", "", false, stamped)

	got := Get()
	want := Info{Version: "v0.1.1-0.20260926000000-1f3c9ab00000+dirty", Commit: "1f3c9ab00000", Dev: true}
	if got != want {
		t.Fatalf("Get() = %+v, want %+v", got, want)
	}
}

func TestGetUnknownWhenNothingRecorded(t *testing.T) {
	set(t, "", "", false, &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}})

	got := Get()
	want := Info{Version: Unknown, Commit: Unknown, Dev: true}
	if got != want {
		t.Fatalf("Get() = %+v, want %+v", got, want)
	}
}
