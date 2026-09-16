package db

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tcarac/taskboard/internal/models"
)

// openWatched returns a migrated store on a temp database and a watcher on the
// same file.
func openWatched(t *testing.T) (*Store, string, *Watcher) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "watch.db")
	database, err := OpenAt(path)
	if err != nil {
		t.Fatalf("opening database: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	w, err := OpenWatcher(path, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("opening watcher: %v", err)
	}
	t.Cleanup(func() { w.Close() })
	return NewStore(database), path, w
}

// data_version only moves for commits made on other connections. These checks
// pin down that the watcher's own connection sees commits made through the
// server's pool in this process, as well as through a second pool opened on
// the same file, the way the CLI and MCP processes do.
func TestDataVersionSeesCommitsFromOtherConnections(t *testing.T) {
	store, path, w := openWatched(t)
	ctx := context.Background()

	v0, err := w.dataVersion(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := w.dataVersion(ctx); v != v0 {
		t.Fatalf("data_version moved without a commit: %d -> %d", v0, v)
	}

	seedProject(t, store, "Same pool", "SP")
	v1, err := w.dataVersion(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if v1 == v0 {
		t.Fatalf("a commit through the store's pool did not change data_version (%d)", v0)
	}

	other, err := OpenAt(path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	seedProject(t, NewStore(other), "Other pool", "OP")
	v2, err := w.dataVersion(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if v2 == v1 {
		t.Fatalf("a commit through a second pool did not change data_version (%d)", v1)
	}

	// A read is not a change.
	if _, err := store.ListProjects(""); err != nil {
		t.Fatal(err)
	}
	if v, _ := w.dataVersion(ctx); v != v2 {
		t.Fatalf("a read changed data_version: %d -> %d", v2, v)
	}
}

func TestWatcherRunReportsChangesAndStops(t *testing.T) {
	store, _, w := openWatched(t)

	ctx, cancel := context.WithCancel(context.Background())
	var mu sync.Mutex
	calls := 0
	changed := make(chan struct{}, 10)
	done := make(chan struct{})
	go func() {
		defer close(done)
		w.Run(ctx, func() {
			mu.Lock()
			calls++
			mu.Unlock()
			changed <- struct{}{}
		})
	}()

	// Nothing committed yet: no call.
	select {
	case <-changed:
		t.Fatal("watcher reported a change before any commit")
	case <-time.After(50 * time.Millisecond):
	}

	if _, err := store.CreateProject(models.CreateProjectRequest{Name: "P", Prefix: "P"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-changed:
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not report the commit")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after its context was cancelled")
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Fatalf("expected 1 change call, got %d", calls)
	}
}

// logRecorder collects a watcher's log lines.
type logRecorder struct {
	mu    sync.Mutex
	lines []string
}

func (l *logRecorder) logf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
}

func (l *logRecorder) get() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.lines...)
}

func TestWatcherRecoversFromABrokenConnection(t *testing.T) {
	store, _, w := openWatched(t)
	logs := &logRecorder{}
	w.logf = logs.logf

	// Break the pinned connection: every poll on it now fails with
	// sql.ErrConnDone until the watcher replaces it.
	w.conn.Close()

	ctx, cancel := context.WithCancel(context.Background())
	changed := make(chan struct{}, 10)
	done := make(chan struct{})
	go func() {
		defer close(done)
		w.Run(ctx, func() { changed <- struct{}{} })
	}()
	defer func() {
		cancel()
		<-done
	}()

	// Reconnecting reports a change, since a commit could have been missed.
	select {
	case <-changed:
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not recover and report a change")
	}

	// Commits after the recovery are reported again.
	seedProject(t, store, "After", "AFT")
	select {
	case <-changed:
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not report a commit after recovering")
	}

	lines := logs.get()
	if len(lines) != 2 || !strings.Contains(lines[0], "sql: connection is already closed") ||
		!strings.Contains(lines[1], "reconnected after 1 failures") {
		t.Fatalf("unexpected log lines: %q", lines)
	}
}

func TestWatcherKeepsRetryingWithRateLimitedLogs(t *testing.T) {
	_, _, w := openWatched(t)
	logs := &logRecorder{}
	w.logf = logs.logf
	w.logEvery = 150 * time.Millisecond

	// A directory cannot be opened as a database, so every reopen fails.
	w.path = t.TempDir()
	w.conn.Close()

	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	done := make(chan struct{})
	go func() {
		defer close(done)
		w.Run(ctx, func() { calls++ })
	}()
	time.Sleep(700 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return while failing")
	}

	if calls != 0 {
		t.Fatalf("a watcher that never reconnected reported %d changes", calls)
	}
	lines := logs.get()
	if len(lines) < 2 {
		t.Fatalf("expected the failure to be logged again after logEvery, got %q", lines)
	}
	var attempts int
	if _, err := fmt.Sscanf(lines[len(lines)-1][strings.Index(lines[len(lines)-1], "(failed"):], "(failed %d times", &attempts); err != nil {
		t.Fatalf("parsing %q: %v", lines[len(lines)-1], err)
	}
	if attempts <= len(lines) {
		t.Fatalf("every one of %d attempts was logged (%d lines): %q", attempts, len(lines), lines)
	}
	if w.conn != nil {
		t.Fatal("a failed watcher should hold no connection")
	}
}
