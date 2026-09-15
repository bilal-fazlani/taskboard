package db

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tcarac/taskboard/internal/livebuild"
)

// These tests only compute the default path; they never open it.

func TestDefaultDBPathRefusedOnUnmarkedBuild(t *testing.T) {
	prev := livebuild.Mark
	livebuild.Mark = ""
	t.Cleanup(func() { livebuild.Mark = prev })
	// Should the guard ever regress, Open below would create a database here,
	// not in the real config dir.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	if _, err := DefaultDBPath(); !errors.Is(err, ErrNoDefaultDB) {
		t.Fatalf("expected ErrNoDefaultDB, got %v", err)
	}
	if _, err := Open(); !errors.Is(err, ErrNoDefaultDB) {
		t.Fatalf("Open on an unmarked build: expected ErrNoDefaultDB, got %v", err)
	}
	if !strings.Contains(ErrNoDefaultDB.Error(), "--db") {
		t.Fatalf("error should name --db: %v", ErrNoDefaultDB)
	}
}

func TestDefaultDBPathOnLiveBuild(t *testing.T) {
	prev := livebuild.Mark
	livebuild.Mark = "true"
	t.Cleanup(func() { livebuild.Mark = prev })

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(configDir, home) {
		t.Fatalf("config dir %s escaped the sandbox %s", configDir, home)
	}
	got, err := DefaultDBPath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(configDir, "taskboard", "taskboard.db"); got != want {
		t.Fatalf("DefaultDBPath() = %s, want %s", got, want)
	}
}
