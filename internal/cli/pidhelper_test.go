package cli

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// pidHelperEnv, when set on this test binary's own environment, tells it to
// act as a stand-in taskboard process (see runPIDHelper) instead of running
// the test suite (see TestMain). It exists so tests can exercise the real,
// unstubbed processCommandLine/identifyProcess pipeline against a process ps
// genuinely reports as "taskboard" — literally a copy of this test binary,
// renamed, running under whatever argv a test needs (see spawnHelper) —
// rather than an override standing in for one.
const pidHelperEnv = "TASKBOARD_TEST_PID_HELPER"

// helperBinaryPath is a copy of this test binary, renamed "taskboard", built
// once for the whole test run (see TestMain).
var helperBinaryPath string

func TestMain(m *testing.M) {
	if mode := os.Getenv(pidHelperEnv); mode != "" {
		os.Exit(runPIDHelper(mode))
	}

	dir, err := os.MkdirTemp("", "taskboard-test-helper")
	if err != nil {
		fmt.Fprintln(os.Stderr, "taskboard test helper: mkdir:", err)
		os.Exit(1)
	}
	self, err := os.Executable()
	if err != nil {
		os.RemoveAll(dir)
		fmt.Fprintln(os.Stderr, "taskboard test helper: os.Executable:", err)
		os.Exit(1)
	}
	helperBinaryPath = filepath.Join(dir, "taskboard")
	if err := copyExecutable(self, helperBinaryPath); err != nil {
		os.RemoveAll(dir)
		fmt.Fprintln(os.Stderr, "taskboard test helper: copy:", err)
		os.Exit(1)
	}

	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func copyExecutable(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o755)
}

// helperMaxLifetime bounds how long a helper lives if nothing ever kills it,
// for instance when the test binary itself is killed before its cleanups run.
const helperMaxLifetime = 2 * time.Minute

// runPIDHelper makes this process stand in for a taskboard process in a
// test: it reports itself ready over stdout once its SIGTERM behavior is
// installed, then waits for SIGTERM (or to be killed outright in cleanup).
//
// Both modes receive SIGTERM through signal.Notify and wait on that channel.
// Ignoring it with signal.Ignore and then blocking in `select {}` is not
// safe: nothing is left that the runtime counts as able to wake the process,
// so Go sometimes aborts it with "all goroutines are asleep - deadlock!".
func runPIDHelper(mode string) int {
	if mode != "graceful" && mode != "stubborn" {
		fmt.Fprintln(os.Stderr, "taskboard test helper: unknown mode", mode)
		return 2
	}
	sigterm := make(chan os.Signal, 1)
	signal.Notify(sigterm, syscall.SIGTERM)
	fmt.Println("ready")

	giveUp := time.After(helperMaxLifetime)
	for {
		select {
		case <-sigterm:
			if mode == "graceful" {
				// Like the real server since ACP-3: takes a little while
				// (200ms stands in for that) to shut down after SIGTERM,
				// rather than dying immediately.
				time.Sleep(200 * time.Millisecond)
				return 0
			}
			// stubborn: drain the signal and keep running, for tests of
			// stopDaemon's timeout.
		case <-giveUp:
			fmt.Fprintln(os.Stderr, "taskboard test helper: never stopped, exiting after", helperMaxLifetime)
			return 3
		}
	}
}

// spawnHelper starts helperBinaryPath — a copy of this test binary, so ps
// genuinely reports its command name as "taskboard" — with args, so ps
// reports whatever invocation shape a test needs (typically the daemon's own
// "start --port N --foreground", or "mcp" to stand in for an MCP server
// sharing the same binary). mode selects its SIGTERM behavior (see
// runPIDHelper).
//
// It blocks until the process reports itself ready, so a caller about to
// signal it never races its startup. That read happens before the
// background Wait below ever starts: Wait closes the stdout pipe as soon as
// it sees the process exit, and reading after that would race losing
// whatever had not been read yet.
func spawnHelper(t *testing.T, mode string, args ...string) *os.Process {
	t.Helper()
	return spawnHelperAt(t, helperBinaryPath, mode, args...)
}

// spawnHelperAt is spawnHelper, but runs the helper binary from bin, for
// tests that need it at a particular path (see copyHelperTo).
func spawnHelperAt(t *testing.T, bin, mode string, args ...string) *os.Process {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), pidHelperEnv+"="+mode)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting helper: %v", err)
	}

	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() || scanner.Text() != "ready" {
		cmd.Process.Kill()
		cmd.Wait()
		t.Fatalf("helper did not report ready: got %q, scan err %v", scanner.Text(), scanner.Err())
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

// copyHelperTo copies the helper binary to dir/name, creating dir, and
// returns its path, for tests that need the "daemon" to run from a path
// shaped a particular way (a directory whose name has a space, say). dir
// should live under t.TempDir(), which removes it.
func copyHelperTo(t *testing.T, dir, name string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := copyExecutable(helperBinaryPath, path); err != nil {
		t.Fatalf("copying helper to %s: %v", path, err)
	}
	return path
}
