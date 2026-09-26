package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/tcarac/taskboard/internal/buildinfo"
	"github.com/tcarac/taskboard/internal/livebuild"
)

// setBuild gives the running binary a version, commit and live mark for one
// test, as the Makefile's and release.yml's -ldflags would.
func setBuild(t *testing.T, version, commit string, live bool) {
	t.Helper()
	prevV, prevC, prevM := buildinfo.Version, buildinfo.Commit, livebuild.Mark
	t.Cleanup(func() { buildinfo.Version, buildinfo.Commit, livebuild.Mark = prevV, prevC, prevM })
	buildinfo.Version, buildinfo.Commit = version, commit
	livebuild.Mark = ""
	if live {
		livebuild.Mark = "true"
	}
}

func TestVersionAPI(t *testing.T) {
	for _, tc := range []struct {
		name string
		live bool
		want string
	}{
		{"release or make install build", true, `{"version":"v0.2.0","commit":"1f3c9ab0d2e4","dev":false}`},
		{"dev build", false, `{"version":"v0.2.0","commit":"1f3c9ab0d2e4","dev":true}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setBuild(t, "v0.2.0", "1f3c9ab0d2e4", tc.live)
			r := serve(t)

			raw, status := rawGET(t, r.url+"/api/version")
			if status != http.StatusOK {
				t.Fatalf("GET /api/version: status = %d, want 200 (body %s)", status, raw)
			}
			var got, want map[string]any
			if err := json.Unmarshal([]byte(raw), &got); err != nil {
				t.Fatalf("GET /api/version: %v (body %s)", err, raw)
			}
			if err := json.Unmarshal([]byte(tc.want), &want); err != nil {
				t.Fatal(err)
			}
			if len(got) != len(want) || got["version"] != want["version"] || got["commit"] != want["commit"] || got["dev"] != want["dev"] {
				t.Errorf("GET /api/version = %s, want %s", raw, tc.want)
			}
		})
	}
}
