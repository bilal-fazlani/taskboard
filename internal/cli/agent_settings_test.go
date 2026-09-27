package cli

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tcarac/taskboard/internal/db"
	"github.com/tcarac/taskboard/internal/models"
)

// settingsDB points --db at a fresh throwaway database for one test.
func settingsDB(t *testing.T) string {
	t.Helper()
	setLiveBuild(t, false)
	sandboxHome(t)
	t.Cleanup(func() { dbPath = "" })
	return filepath.Join(t.TempDir(), "dev.db")
}

// A new install has the defaults, 30m and 2h.
func TestSettingsGetShowsTheDefaults(t *testing.T) {
	path := settingsDB(t)
	out, err := runCLI(t, "--db", path, "settings", "get")
	if err != nil {
		t.Fatalf("settings get: %v\n%s", err, out)
	}
	if out != "stale-after  30m\nlease        2h\n" {
		t.Fatalf("settings get printed %q", out)
	}
}

// A value set through the CLI is stored in the database, so another store
// on the same file, as the server or an MCP server would open, uses it to
// judge staleness.
func TestSettingsSetIsWhatEveryStoreOnTheFileUses(t *testing.T) {
	path := settingsDB(t)
	if out, err := runCLI(t, "--db", path, "settings", "set", "stale-after", "45m"); err != nil ||
		!strings.Contains(out, "stale-after  45m") {
		t.Fatalf("settings set stale-after 45m: %v\n%s", err, out)
	}
	if out, err := runCLI(t, "--db", path, "settings", "set", "lease", "3h"); err != nil || !strings.Contains(out, "lease        3h") {
		t.Fatalf("settings set lease 3h: %v\n%s", err, out)
	}

	database, err := db.OpenAt(path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	store := db.NewStore(database)
	settings, err := store.AgentSettings()
	if err != nil || settings.StaleAfter != 45*time.Minute || settings.Lease != 3*time.Hour {
		t.Fatalf("another store reads %+v, %v; want 45m and 3h", settings, err)
	}

	a, err := store.IdentifyAgent(models.IdentifyAgentRequest{Vendor: "claude_code", VendorSessionID: "s1",
		Role: "implementer", Model: "claude-opus-5-5", Provider: models.ProviderAnthropic})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		ago   time.Duration
		stale bool
	}{{40 * time.Minute, false}, {46 * time.Minute, true}} {
		if _, err := database.Exec(`UPDATE agents SET last_seen_at = ? WHERE id = ?`,
			time.Now().Add(-tc.ago).UTC().Format("2006-01-02T15:04:05.000000000Z"), a.ID); err != nil {
			t.Fatal(err)
		}
		got, err := store.GetAgent(a.ID)
		if err != nil || got.Stale != tc.stale {
			t.Errorf("unseen for %v with stale-after 45m: stale = %v (%v), want %v", tc.ago, got.Stale, err, tc.stale)
		}
	}
}

func TestSettingsSetRefusesABadNameOrDuration(t *testing.T) {
	path := settingsDB(t)
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"stale-after", "soon"}, `"soon" is not a duration`},
		{[]string{"lease", "0s"}, "lease is 0s: it must be a positive whole number of seconds"},
		{[]string{"stale-after", "1.5s"}, "stale-after is 1.5s"},
		{[]string{"timeout", "5m"}, `"timeout" is not a setting: use one of stale-after, lease`},
	} {
		out, err := runCLI(t, append([]string{"--db", path, "settings", "set"}, tc.args...)...)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("settings set %v: %v\n%s\nwant an error containing %q", tc.args, err, out, tc.want)
		}
	}
	out, err := runCLI(t, "--db", path, "settings", "get")
	if err != nil || out != "stale-after  30m\nlease        2h\n" {
		t.Fatalf("after refused sets, settings get = %q, %v; want the defaults", out, err)
	}
}
