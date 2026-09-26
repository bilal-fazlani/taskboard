package cli

import (
	"testing"

	"github.com/tcarac/taskboard/internal/buildinfo"
)

// --version needs no database, even from a dev build, and names the build.
func TestVersionFlag(t *testing.T) {
	for _, tc := range []struct {
		name string
		live bool
		want string
	}{
		{"release or make install build", true, "taskboard v0.2.0 (commit 1f3c9ab0d2e4)\n"},
		{"dev build", false, "taskboard v0.2.0 (commit 1f3c9ab0d2e4, dev build)\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setLiveBuild(t, tc.live)
			sandboxHome(t)
			prevV, prevC := buildinfo.Version, buildinfo.Commit
			t.Cleanup(func() { buildinfo.Version, buildinfo.Commit = prevV, prevC })
			buildinfo.Version, buildinfo.Commit = "v0.2.0", "1f3c9ab0d2e4"

			out, err := runCLI(t, "--version")
			if err != nil {
				t.Fatalf("taskboard --version: %v (output %q)", err, out)
			}
			if out != tc.want {
				t.Errorf("taskboard --version = %q, want %q", out, tc.want)
			}
		})
	}
}
