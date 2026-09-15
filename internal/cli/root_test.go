package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tcarac/taskboard/internal/db"
	"github.com/tcarac/taskboard/internal/livebuild"
)

// setLiveBuild marks or unmarks this test binary as the live build for one test.
func setLiveBuild(t *testing.T, live bool) {
	t.Helper()
	prev := livebuild.Mark
	livebuild.Mark = ""
	if live {
		livebuild.Mark = "true"
	}
	t.Cleanup(func() { livebuild.Mark = prev })
}

// sandboxHome points HOME and the XDG config dir at a temp dir, so that even a
// broken guard could only ever create files there, never in the real config dir.
func sandboxHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	return home
}

func runCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	root := NewRootCmd(nil)
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func TestUnmarkedBuildWithoutDBRefuses(t *testing.T) {
	setLiveBuild(t, false)
	home := sandboxHome(t)

	for _, args := range [][]string{
		{"project", "list"},
		{"ticket", "list"},
		{"label", "list"},
		{"clear", "--force"},
		{"mcp"},
		{"stop"},
	} {
		_, err := runCLI(t, args...)
		if err == nil {
			t.Fatalf("%v: expected an error without --db", args)
		}
		if !errors.Is(err, db.ErrNoDefaultDB) || !strings.Contains(err.Error(), "--db") {
			t.Fatalf("%v: error should name --db, got: %v", args, err)
		}
	}

	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("refused commands still created files in the config dir: %v", entries)
	}
}

func TestUnmarkedBuildWithDBWorks(t *testing.T) {
	setLiveBuild(t, false)
	sandboxHome(t)
	path := filepath.Join(t.TempDir(), "dev.db")

	if _, err := runCLI(t, "--db", path, "project", "create", "Dev", "--prefix", "DEV"); err != nil {
		t.Fatalf("project create with --db: %v", err)
	}
	if _, err := runCLI(t, "--db", path, "project", "list"); err != nil {
		t.Fatalf("project list with --db: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("database not created at --db path: %v", err)
	}
}

func TestHelpNeedsNoDB(t *testing.T) {
	setLiveBuild(t, false)
	sandboxHome(t)
	if _, err := runCLI(t, "--help"); err != nil {
		t.Fatalf("--help on an unmarked build: %v", err)
	}
}

func TestStartDefaultPort(t *testing.T) {
	for _, tc := range []struct {
		live bool
		want string
	}{
		{live: false, want: "3011"},
		{live: true, want: "3010"},
	} {
		setLiveBuild(t, tc.live)
		root := NewRootCmd(nil)
		start, _, err := root.Find([]string{"start"})
		if err != nil {
			t.Fatal(err)
		}
		if got := start.Flags().Lookup("port").DefValue; got != tc.want {
			t.Errorf("live=%v: start --port defaults to %s, want %s", tc.live, got, tc.want)
		}
	}
}

func TestPIDFileFollowsDB(t *testing.T) {
	setLiveBuild(t, false)
	t.Cleanup(func() { dbPath = "" })
	pidFor := func(path string) string {
		t.Helper()
		dbPath = path
		got, err := pidFilePath()
		if err != nil {
			t.Fatalf("--db %s: %v", path, err)
		}
		return got
	}

	for path, want := range map[string]string{
		"/work/.tmp/dev.db": "/work/.tmp/dev.db.pid",
		"./.tmp/x.db":       "./.tmp/x.db.pid",
		"/work/board":       "/work/board.pid",
	} {
		if got := pidFor(path); got != want {
			t.Errorf("--db %s: pid file %s, want %s", path, got, want)
		}
	}

	// Two different databases must never share a pid file, or `stop` on one
	// would stop the server of the other.
	for _, pair := range [][2]string{
		{"./.tmp/x", "./.tmp/x.db"},
		{"./.tmp/x.db", "./.tmp/x.db.pid"},
	} {
		if a, b := pidFor(pair[0]), pidFor(pair[1]); a == b {
			t.Errorf("--db %s and --db %s share the pid file %s", pair[0], pair[1], a)
		}
	}
}

func TestPIDFileUnmarkedWithoutDBRefuses(t *testing.T) {
	setLiveBuild(t, false)
	dbPath = ""
	if _, err := pidFilePath(); !errors.Is(err, db.ErrNoDefaultDB) {
		t.Fatalf("expected ErrNoDefaultDB, got %v", err)
	}
}

// The live build's pid file must stay where the currently running live server
// wrote it: taskboard.pid next to taskboard.db in the OS config dir. The path
// is only computed, never opened.
func TestPIDFileLiveBuildKeepsDefaultLocation(t *testing.T) {
	setLiveBuild(t, true)
	home := sandboxHome(t)
	dbPath = ""

	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(configDir, home) {
		t.Fatalf("config dir %s escaped the sandbox %s", configDir, home)
	}
	got, err := pidFilePath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(configDir, "taskboard", "taskboard.pid"); got != want {
		t.Fatalf("pid file %s, want %s", got, want)
	}
}
