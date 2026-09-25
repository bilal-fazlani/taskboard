package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// spawnSleeper starts a real, harmless process that is never named
// "taskboard" (so the real isTaskboardBinary correctly says no), for tests
// of the "process is gone" and "process isn't taskboard" cases. It reaps the
// process in the background as soon as it exits, so processAlive's
// Signal(0) check reliably reports it gone right after it actually dies
// rather than while it lingers as an unreaped zombie (which a bare
// cmd.Start without a concurrent Wait would leave behind, and which
// Signal(0) — unlike a real signal — still succeeds against). Cleanup kills
// it (a no-op if it has already exited) and waits for the reaper.
func spawnSleeper(t *testing.T) *os.Process {
	t.Helper()
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting sleep: %v", err)
	}
	done := make(chan struct{})
	go func() {
		cmd.Wait()
		close(done)
	}()
	t.Cleanup(func() {
		cmd.Process.Kill()
		<-done
	})
	return cmd.Process
}

// spawnExitedProcess starts and waits out a process, handing back a pid that
// is now guaranteed to belong to nothing.
func spawnExitedProcess(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatalf("running true: %v", err)
	}
	return cmd.Process.Pid
}

func TestRemoveOwnPIDFileRemovesOwnPID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "taskboard.pid")
	if err := writePID(path, os.Getpid()); err != nil {
		t.Fatal(err)
	}
	removeOwnPIDFile(path)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("own pid file not removed: %v", err)
	}
}

func TestRemoveOwnPIDFileLeavesForeignPID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "taskboard.pid")
	// Some other pid, guaranteed not to be ours.
	if err := writePID(path, os.Getpid()+1); err != nil {
		t.Fatal(err)
	}
	removeOwnPIDFile(path)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("pid file for a different pid was removed: %v", err)
	}
}

// TestForegroundServerRemovesOwnPIDFileOnCleanExit covers ACP-50's first
// subtask: the server removes its own pid file on the way out, the way
// daemonize's child would leave it there forever otherwise once it exits any
// way other than through `stop`.
func TestForegroundServerRemovesOwnPIDFileOnCleanExit(t *testing.T) {
	setLiveBuild(t, false)
	sandboxHome(t)
	t.Cleanup(func() { dbPath = "" })
	dbPath = filepath.Join(t.TempDir(), "dev.db")

	pidPath, err := pidFilePath()
	if err != nil {
		t.Fatal(err)
	}
	// Mimic what daemonize does: write the pid file for this very process
	// before the foreground server runs.
	if err := writePID(pidPath, os.Getpid()); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runForeground(ctx, nil, 0) }()

	// Give ListenAndServe a moment to open its listener before asking it to
	// stop; port 0 always binds, so this is not racing a port conflict.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runForeground: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runForeground did not return after its context was cancelled")
	}

	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatalf("pid file still exists after a clean exit: %v", err)
	}
}

// TestRunForegroundRemovesPIDFileEvenIfDatabaseFailsToOpen covers review
// finding 5: removeOwnPIDFile is deferred before the database ever opens, so
// a daemon that never gets as far as a working database still removes its
// pid file, rather than leaving one behind for a `start` or `stop` that runs
// later to mistake for a server that is still running.
func TestRunForegroundRemovesPIDFileEvenIfDatabaseFailsToOpen(t *testing.T) {
	setLiveBuild(t, false)
	sandboxHome(t)
	t.Cleanup(func() { dbPath = "" })
	// A directory can never be opened as the sqlite database file, so
	// db.OpenAt fails before runForeground ever gets to ListenAndServe.
	dbPath = t.TempDir()

	pidPath, err := pidFilePath()
	if err != nil {
		t.Fatal(err)
	}
	if err := writePID(pidPath, os.Getpid()); err != nil {
		t.Fatal(err)
	}

	if err := runForeground(context.Background(), nil, 0); err == nil {
		t.Fatal("expected opening a directory as the database to fail")
	}

	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatalf("pid file still exists after the database failed to open: %v", err)
	}
}

func TestCheckExistingDaemonProcessGone(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "taskboard.pid")
	gonePID := spawnExitedProcess(t)
	if err := writePID(pidPath, gonePID); err != nil {
		t.Fatal(err)
	}

	pid, running, err := checkExistingDaemon(pidPath)
	if err != nil {
		t.Fatalf("checkExistingDaemon: %v", err)
	}
	if running {
		t.Fatalf("a gone process was reported running (pid %d)", pid)
	}
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatalf("stale pid file not removed: %v", err)
	}
}

func TestCheckExistingDaemonForeignProcess(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "taskboard.pid")
	sleeper := spawnSleeper(t)
	if err := writePID(pidPath, sleeper.Pid); err != nil {
		t.Fatal(err)
	}

	pid, running, err := checkExistingDaemon(pidPath)
	if err != nil {
		t.Fatalf("checkExistingDaemon: %v", err)
	}
	if running {
		t.Fatalf("an unrelated process was reported as taskboard running (pid %d)", pid)
	}
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatalf("stale pid file (foreign process) not removed: %v", err)
	}
	if !processAlive(sleeper.Pid) {
		t.Fatal("checkExistingDaemon must never signal a process that is not taskboard, but the process is gone")
	}
}

// TestCheckExistingDaemonMCPProcess covers review finding 3: several MCP
// sessions may run `taskboard mcp` at once, from the very same binary. A pid
// running that, not the server, must count as foreign to `start`, never
// signaled.
func TestCheckExistingDaemonMCPProcess(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "taskboard.pid")
	mcpProcess := spawnHelper(t, "graceful", "mcp")
	if err := writePID(pidPath, mcpProcess.Pid); err != nil {
		t.Fatal(err)
	}

	pid, running, err := checkExistingDaemon(pidPath)
	if err != nil {
		t.Fatalf("checkExistingDaemon: %v", err)
	}
	if running {
		t.Fatalf("an mcp process was reported as the running daemon (pid %d)", pid)
	}
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatalf("stale pid file (mcp process) not removed: %v", err)
	}
	if !processAlive(mcpProcess.Pid) {
		t.Fatal("checkExistingDaemon must never signal an mcp process, but the process is gone")
	}
}

// TestCheckExistingDaemonTaskboard is the positive-path test review finding
// 4 asked for: a real process — literally a copy of this test binary,
// renamed "taskboard" and run with the daemon's own "start" argument shape
// (see spawnHelper) — recognised by the real, unstubbed
// processCommandLine/identifyProcess pipeline, rather than an override
// standing in for one.
func TestCheckExistingDaemonTaskboard(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "taskboard.pid")
	daemon := spawnHelper(t, "graceful", "start", "--port", "0", "--foreground")
	if err := writePID(pidPath, daemon.Pid); err != nil {
		t.Fatal(err)
	}

	pid, running, err := checkExistingDaemon(pidPath)
	if err != nil {
		t.Fatalf("checkExistingDaemon: %v", err)
	}
	if !running || pid != daemon.Pid {
		t.Fatalf("a running taskboard daemon was not detected: running=%v pid=%d", running, pid)
	}
	if _, err := os.Stat(pidPath); err != nil {
		t.Fatalf("pid file for a live daemon must not be removed: %v", err)
	}
}

// TestCheckExistingDaemonUnknown covers review finding 1: when ps cannot
// report a live pid's command at all, that pid must be left alone — neither
// treated as the running daemon nor as safe to discard the pid file for —
// since it might still be the real daemon.
func TestCheckExistingDaemonUnknown(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "taskboard.pid")
	sleeper := spawnSleeper(t)
	prev := processCommandLine
	processCommandLine = func(int) (string, error) {
		return "", fmt.Errorf("simulated: ps did not answer")
	}
	t.Cleanup(func() { processCommandLine = prev })
	if err := writePID(pidPath, sleeper.Pid); err != nil {
		t.Fatal(err)
	}

	_, running, err := checkExistingDaemon(pidPath)
	if err == nil {
		t.Fatal("expected an error when a live pid's identity cannot be determined")
	}
	if running {
		t.Fatal("an unknown process must never be reported as the running daemon")
	}
	if _, err := os.Stat(pidPath); err != nil {
		t.Fatalf("pid file must be kept when a live pid's identity is unknown: %v", err)
	}
	if !processAlive(sleeper.Pid) {
		t.Fatal("checkExistingDaemon must never signal a pid whose identity is unknown")
	}
}

func TestStopDaemonProcessGone(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "taskboard.pid")
	gonePID := spawnExitedProcess(t)
	if err := writePID(pidPath, gonePID); err != nil {
		t.Fatal(err)
	}

	_, err := stopDaemon(pidPath, time.Second, 10*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "stale pid file removed") {
		t.Fatalf("expected a stale pid file error, got %v", err)
	}
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatalf("stale pid file not removed: %v", err)
	}
}

func TestStopDaemonForeignProcess(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "taskboard.pid")
	sleeper := spawnSleeper(t)
	if err := writePID(pidPath, sleeper.Pid); err != nil {
		t.Fatal(err)
	}

	_, err := stopDaemon(pidPath, time.Second, 10*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "belongs to another process") {
		t.Fatalf("expected a foreign-process error, got %v", err)
	}
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatalf("stale pid file (foreign process) not removed: %v", err)
	}
	if !processAlive(sleeper.Pid) {
		t.Fatal("stopDaemon must never signal a process that is not taskboard, but the process is gone")
	}
}

// TestStopDaemonMCPProcess covers review finding 3 on the `stop` side: if
// the daemon's pid was reused by a `taskboard mcp` process, stop must treat
// it as foreign (remove the stale pid file, never signal it), not as the
// server it was asked to stop.
func TestStopDaemonMCPProcess(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "taskboard.pid")
	mcpProcess := spawnHelper(t, "graceful", "mcp")
	if err := writePID(pidPath, mcpProcess.Pid); err != nil {
		t.Fatal(err)
	}

	_, err := stopDaemon(pidPath, time.Second, 10*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "belongs to another process") {
		t.Fatalf("expected a foreign-process error, got %v", err)
	}
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatalf("stale pid file (mcp process) not removed: %v", err)
	}
	if !processAlive(mcpProcess.Pid) {
		t.Fatal("stopDaemon must never signal an mcp process, but the process is gone")
	}
}

// TestStopDaemonUnknown covers review finding 1 on the `stop` side.
func TestStopDaemonUnknown(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "taskboard.pid")
	sleeper := spawnSleeper(t)
	prev := processCommandLine
	processCommandLine = func(int) (string, error) {
		return "", fmt.Errorf("simulated: ps did not answer")
	}
	t.Cleanup(func() { processCommandLine = prev })
	if err := writePID(pidPath, sleeper.Pid); err != nil {
		t.Fatal(err)
	}

	_, err := stopDaemon(pidPath, time.Second, 10*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "cannot tell") {
		t.Fatalf("expected an unknown-identity error, got %v", err)
	}
	if _, err := os.Stat(pidPath); err != nil {
		t.Fatalf("pid file must be kept when a live pid's identity is unknown: %v", err)
	}
	if !processAlive(sleeper.Pid) {
		t.Fatal("stopDaemon must never signal a pid whose identity is unknown")
	}
}

func TestStopDaemonWaitsForExit(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "taskboard.pid")
	process := spawnHelper(t, "graceful", "start", "--port", "0", "--foreground")
	if err := writePID(pidPath, process.Pid); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	pid, err := stopDaemon(pidPath, 2*time.Second, 20*time.Millisecond)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("stopDaemon: %v", err)
	}
	if pid != process.Pid {
		t.Fatalf("pid = %d, want %d", pid, process.Pid)
	}
	if elapsed < 150*time.Millisecond {
		t.Fatalf("stopDaemon returned after %s, before the process could have exited", elapsed)
	}
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatalf("pid file not removed after a clean stop: %v", err)
	}
}

func TestStopDaemonTimesOut(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "taskboard.pid")
	process := spawnHelper(t, "stubborn", "start", "--port", "0", "--foreground")
	if err := writePID(pidPath, process.Pid); err != nil {
		t.Fatal(err)
	}

	_, err := stopDaemon(pidPath, 150*time.Millisecond, 20*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "did not stop") {
		t.Fatalf("expected a timeout error, got %v", err)
	}
	if !strings.Contains(err.Error(), "kill -9") {
		t.Fatalf("timeout error should suggest a next step, got %v", err)
	}
	if _, err := os.Stat(pidPath); err != nil {
		t.Fatalf("pid file removed even though the process never stopped: %v", err)
	}
	if !processAlive(process.Pid) {
		t.Fatal("the process should still be alive; stopDaemon must not have killed it outright")
	}
}

// TestIsTaskboardBinaryAcceptsOwnExecutableName covers review finding 2: a
// release tarball binary (taskboard-darwin-arm64), a renamed dev build or a
// symlink is still recognised, since it is this process's own executable
// name, even though it is not literally "taskboard".
func TestIsTaskboardBinaryAcceptsOwnExecutableName(t *testing.T) {
	prev := ownExecutablePath
	ownExecutablePath = func() string { return "/opt/tb/taskboard-darwin-arm64" }
	t.Cleanup(func() { ownExecutablePath = prev })

	if !isTaskboardBinary(binaryName) {
		t.Error("the canonical \"taskboard\" name must still be accepted")
	}
	if !isTaskboardBinary("taskboard-darwin-arm64") {
		t.Error("a release-named binary matching our own executable's name was not recognised")
	}
	if isTaskboardBinary("some-other-program") {
		t.Error("an unrelated binary name was accepted")
	}
}

// TestStopCLI covers the stop command's wiring end to end: a stale pid file
// and a successful stop, both through NewRootCmd rather than stopDaemon
// directly.
func TestStopCLI(t *testing.T) {
	setLiveBuild(t, false)
	sandboxHome(t)
	t.Cleanup(func() { dbPath = "" })

	t.Run("not running", func(t *testing.T) {
		dbPath = filepath.Join(t.TempDir(), "dev.db")
		out, err := runCLI(t, "--db", dbPath, "stop")
		if err == nil || !strings.Contains(err.Error(), "taskboard is not running") {
			t.Fatalf("expected a not-running error, got out=%q err=%v", out, err)
		}
	})

	t.Run("successful stop", func(t *testing.T) {
		dbPath = filepath.Join(t.TempDir(), "dev.db")
		pidPath := dbPath + ".pid"

		process := spawnHelper(t, "graceful", "start", "--port", "0", "--foreground")
		if err := writePID(pidPath, process.Pid); err != nil {
			t.Fatal(err)
		}

		out, err := runCLI(t, "--db", dbPath, "stop")
		if err != nil {
			t.Fatalf("stop: %v", err)
		}
		if want := fmt.Sprintf("Taskboard stopped (pid %d)", process.Pid); !strings.Contains(out, want) {
			t.Fatalf("output %q does not contain %q", out, want)
		}
		if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
			t.Fatalf("pid file not removed: %v", err)
		}
	})
}

// TestIsDaemonCommandLine covers the shapes ps can print for a pid: only the
// exact invocation daemonize runs counts as the server, even when the
// executable's path or the --db path contains spaces, and a command that
// merely has "start" among its arguments never does.
func TestIsDaemonCommandLine(t *testing.T) {
	prev := ownExecutablePath
	ownExecutablePath = func() string { return "/opt/Taskboard Builds/taskboard-darwin-arm64" }
	t.Cleanup(func() { ownExecutablePath = prev })

	for _, tc := range []struct {
		line string
		want bool
	}{
		{"/usr/local/bin/taskboard start --port 3010 --foreground", true},
		{"/usr/local/bin/taskboard --db /work/.tmp/dev.db start --port 3011 --foreground", true},
		{"/Users/b/Library/Application Support/tb/taskboard start --port 3010 --foreground", true},
		{"/Users/b/My Tools/taskboard --db /Users/b/My Data/start here.db start --port 3011 --foreground", true},
		// This process's own executable, however it is named.
		{"/opt/Taskboard Builds/taskboard-darwin-arm64 start --port 3010 --foreground", true},
		{"/elsewhere/taskboard-darwin-arm64 --db /x.db start --port 3010 --foreground", true},

		// Other taskboard commands, from the very same binary.
		{"/usr/local/bin/taskboard ticket create --title start", false},
		{"/usr/local/bin/taskboard --db /x.db ticket create --title start --project ACP", false},
		{"/usr/local/bin/taskboard mcp", false},
		{"/usr/local/bin/taskboard --db /x/my start here.db mcp", false},
		{"/Users/b/Library/Application Support/tb/taskboard --db /x/my start here.db mcp", false},
		// daemonize's own short-lived parent, not the server it starts.
		{"/usr/local/bin/taskboard --db /x.db start", false},
		{"/usr/local/bin/taskboard start --port 3011", false},
		// Not the order daemonize uses, or something after --foreground.
		{"/usr/local/bin/taskboard start --foreground --port 3010", false},
		{"/usr/local/bin/taskboard start --port 3010 --foreground extra", false},
		{"/usr/local/bin/taskboard start --port x --foreground", false},
		// --db with no path.
		{"/usr/local/bin/taskboard --db  start --port 3010 --foreground", false},

		// Other programs, and argv[0] that is not an absolute path.
		{"/usr/bin/python3 start --port 3010 --foreground", false},
		{"/usr/bin/python3 --db /x.db start --port 3010 --foreground", false},
		{"/usr/local/bin/taskboard-helper start --port 3010 --foreground", false},
		{"taskboard start --port 3010 --foreground", false},
		{"sleep 30", false},
		{"", false},
	} {
		if got := isDaemonCommandLine(tc.line); got != tc.want {
			t.Errorf("isDaemonCommandLine(%q) = %v, want %v", tc.line, got, tc.want)
		}
	}
}

// TestDaemonInDirectoryWithSpace runs the real, unstubbed check against a
// daemon whose executable sits in a directory whose name contains a space
// (like macOS's "Application Support"), with a --db path that contains both
// a space and the word "start". `start` must see it running, and `stop`
// must stop it rather than take it for a stranger and orphan it.
func TestDaemonInDirectoryWithSpace(t *testing.T) {
	root := t.TempDir()
	bin := copyHelperTo(t, filepath.Join(root, "Application Support", "tb"), "taskboard")
	db := filepath.Join(root, "my data", "start here.db")
	pidPath := filepath.Join(t.TempDir(), "taskboard.pid")

	daemon := spawnHelperAt(t, bin, "graceful", "--db", db, "start", "--port", "0", "--foreground")
	if err := writePID(pidPath, daemon.Pid); err != nil {
		t.Fatal(err)
	}

	pid, running, err := checkExistingDaemon(pidPath)
	if err != nil {
		t.Fatalf("checkExistingDaemon: %v", err)
	}
	if !running || pid != daemon.Pid {
		t.Fatalf("a daemon under a path with a space was not detected: running=%v pid=%d", running, pid)
	}

	stopped, err := stopDaemon(pidPath, 2*time.Second, 20*time.Millisecond)
	if err != nil {
		t.Fatalf("stopDaemon: %v", err)
	}
	if stopped != daemon.Pid {
		t.Fatalf("stopped pid %d, want %d", stopped, daemon.Pid)
	}
	if processAlive(daemon.Pid) {
		t.Fatal("stopDaemon returned but the daemon is still running")
	}
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatalf("pid file not removed after a clean stop: %v", err)
	}
}

// TestNonDaemonTaskboardCommandsAreForeign runs the real, unstubbed check
// against taskboard processes that have "start" among their arguments but
// are not the server: a CLI command with "start" as a value, and an MCP
// server whose --db path contains "start". Neither `start` nor `stop` may
// signal them; both treat the pid file as stale.
func TestNonDaemonTaskboardCommandsAreForeign(t *testing.T) {
	spacedBin := copyHelperTo(t, filepath.Join(t.TempDir(), "Application Support"), "taskboard")
	for _, tc := range []struct {
		name string
		bin  string
		args []string
	}{
		{"ticket create --title start", helperBinaryPath, []string{"ticket", "create", "--title", "start"}},
		{"mcp with start in its db path", helperBinaryPath, []string{"--db", "/x/my start here.db", "mcp"}},
		{"mcp under a path with a space", spacedBin, []string{"--db", "/x/my start here.db", "mcp"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			process := spawnHelperAt(t, tc.bin, "graceful", tc.args...)

			pidPath := filepath.Join(t.TempDir(), "taskboard.pid")
			if err := writePID(pidPath, process.Pid); err != nil {
				t.Fatal(err)
			}
			if _, running, err := checkExistingDaemon(pidPath); err != nil || running {
				t.Fatalf("checkExistingDaemon: running=%v err=%v, want not running", running, err)
			}
			if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
				t.Fatalf("start: stale pid file not removed: %v", err)
			}

			if err := writePID(pidPath, process.Pid); err != nil {
				t.Fatal(err)
			}
			_, err := stopDaemon(pidPath, time.Second, 10*time.Millisecond)
			if err == nil || !strings.Contains(err.Error(), "belongs to another process") {
				t.Fatalf("expected a foreign-process error, got %v", err)
			}
			if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
				t.Fatalf("stop: stale pid file not removed: %v", err)
			}
			if !processAlive(process.Pid) {
				t.Fatal("a taskboard process that is not the server was signaled")
			}
		})
	}
}
