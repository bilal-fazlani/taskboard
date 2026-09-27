package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/tcarac/taskboard/internal/db"
	"github.com/tcarac/taskboard/internal/models"
)

func getConfigJSON(t *testing.T, url string) map[string]any {
	t.Helper()
	resp, err := http.Get(url + "/api/config")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/config: status %d (body %s)", resp.StatusCode, raw)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("GET /api/config: %v (body %s)", err, raw)
	}
	return got
}

// /api/config gives the install's stale threshold and lease, in seconds, as
// the database holds them: a change another process makes (the CLI's
// settings set) shows at once, with no restart.
func TestConfigAPIGivesTheAgentTimings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.db")
	open := func() *db.Store {
		database, err := db.OpenAt(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { database.Close() })
		return db.NewStore(database)
	}
	ts := httptest.NewServer(New(open(), nil))
	t.Cleanup(ts.Close)

	got := getConfigJSON(t, ts.URL)
	if len(got) != 2 || got["staleAfterSeconds"] != float64(1800) || got["leaseSeconds"] != float64(7200) {
		t.Fatalf("default config = %v, want staleAfterSeconds 1800 and leaseSeconds 7200", got)
	}

	staleAfter, lease := 45*time.Minute, 3*time.Hour
	if _, err := open().UpdateAgentSettings(models.UpdateAgentSettingsRequest{StaleAfter: &staleAfter, Lease: &lease}); err != nil {
		t.Fatal(err)
	}
	got = getConfigJSON(t, ts.URL)
	if got["staleAfterSeconds"] != float64(2700) || got["leaseSeconds"] != float64(10800) {
		t.Fatalf("config = %v, want staleAfterSeconds 2700 and leaseSeconds 10800", got)
	}
}
