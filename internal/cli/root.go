package cli

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/tcarac/taskboard/internal/buildinfo"
	"github.com/tcarac/taskboard/internal/db"
	"github.com/tcarac/taskboard/internal/mcp"
	"github.com/tcarac/taskboard/internal/server"
	"github.com/tcarac/taskboard/internal/weburl"
)

var (
	port       int
	foreground bool
	dbPath     string
)

// defaultPort keeps development builds off the live server's port. The ports
// themselves live in weburl, which also builds links against them.
func defaultPort() int { return weburl.DefaultPort() }

func NewRootCmd(webFS fs.FS) *cobra.Command {
	root := &cobra.Command{
		Use:   "taskboard",
		Short: "Local project management with Kanban UI and MCP server",
		// --version prints the build: its version, commit and whether it is a
		// dev build (internal/buildinfo).
		Version: buildinfo.Get().String(),
	}
	root.SetVersionTemplate("taskboard {{.Version}}\n")
	root.PersistentFlags().StringVar(&dbPath, "db", "", "path to SQLite database file (default: OS config dir, live build only)")

	startCmd := &cobra.Command{
		Use:   "start",
		Short: "Start the web UI server",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !foreground {
				return daemonize(cmd.OutOrStdout(), port)
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return runForeground(ctx, webFS, port)
		},
	}
	startCmd.Flags().IntVarP(&port, "port", "p", defaultPort(), "port to listen on")
	startCmd.Flags().BoolVar(&foreground, "foreground", false, "run in foreground instead of as a daemon")

	stopCmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the running taskboard server",
		RunE: func(cmd *cobra.Command, args []string) error {
			pidPath, err := pidFilePath()
			if err != nil {
				return err
			}

			pid, err := stopDaemon(pidPath, stopTimeout, stopPollInterval)
			if err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Taskboard stopped (pid %d)\n", pid)
			return nil
		},
	}

	mcpCmd := &cobra.Command{
		Use:   "mcp",
		Short: "Start MCP stdio server for AI assistants",
		RunE: func(cmd *cobra.Command, args []string) error {
			database, err := openDB()
			if err != nil {
				return fmt.Errorf("opening database: %w", err)
			}
			store := db.NewStore(database)
			srv := mcp.NewServer(store)
			return srv.Run()
		},
	}

	clearCmd := &cobra.Command{
		Use:   "clear",
		Short: "Delete all data from the database (keeps schema intact)",
		RunE: func(cmd *cobra.Command, args []string) error {
			force, _ := cmd.Flags().GetBool("force")
			if !force {
				fmt.Fprint(cmd.OutOrStdout(), "This will delete all projects, tickets, and labels. Continue? [y/N] ")
				answer, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
				answer = strings.TrimSpace(answer)
				if answer != "y" && answer != "Y" {
					fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
					return nil
				}
			}

			store, err := openStore()
			if err != nil {
				return fmt.Errorf("opening database: %w", err)
			}
			if err := store.ClearData(); err != nil {
				return fmt.Errorf("clearing data: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "All data cleared.")
			return nil
		},
	}
	clearCmd.Flags().BoolP("force", "f", false, "skip confirmation prompt")

	root.AddCommand(startCmd, stopCmd, mcpCmd, clearCmd)
	root.AddCommand(projectCommands())
	root.AddCommand(ticketCommands())
	root.AddCommand(labelCommands())
	root.AddCommand(epicCommands())
	root.AddCommand(documentCommands())

	return root
}

func Execute(webFS fs.FS) {
	if err := NewRootCmd(webFS).Execute(); err != nil {
		os.Exit(1)
	}
}

// effectiveDBPath is the database this invocation uses: --db, or else the
// default path, which only the live build may resolve.
func effectiveDBPath() (string, error) {
	if dbPath != "" {
		return dbPath, nil
	}
	return db.DefaultDBPath()
}

func openDB() (*sql.DB, error) {
	path, err := effectiveDBPath()
	if err != nil {
		return nil, err
	}
	return db.OpenAt(path)
}

func openStore() (*db.Store, error) {
	database, err := openDB()
	if err != nil {
		return nil, err
	}
	return db.NewStore(database), nil
}

func daemonize(w io.Writer, port int) error {
	pidPath, err := pidFilePath()
	if err != nil {
		return err
	}

	pid, running, err := checkExistingDaemon(pidPath)
	if err != nil {
		return err
	}
	if running {
		return fmt.Errorf("taskboard is already running (pid %d)", pid)
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding executable: %w", err)
	}

	daemonArgs := []string{"start", "--port", strconv.Itoa(port), "--foreground"}
	if dbPath != "" {
		daemonArgs = append([]string{"--db", dbPath}, daemonArgs...)
	}
	cmd := exec.Command(exe, daemonArgs...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting daemon: %w", err)
	}

	if err := writePID(pidPath, cmd.Process.Pid); err != nil {
		return fmt.Errorf("writing pid file: %w", err)
	}

	fmt.Fprintf(w, "Taskboard running at http://localhost:%d (pid %d)\n", port, cmd.Process.Pid)
	return nil
}

// pidFilePath sits next to the database the server uses, so `stop` only reaches
// a server started on the same database. An explicit --db gets "<db>.pid",
// which no other database path can share. The default database keeps
// taskboard.pid in the OS config dir, where the live server's pid file has
// always been.
func pidFilePath() (string, error) {
	if dbPath != "" {
		return dbPath + ".pid", nil
	}
	path, err := db.DefaultDBPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(path), "taskboard.pid"), nil
}

func writePID(path string, pid int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strconv.Itoa(pid)), 0o644)
}

func readPID(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(data)))
}

// runForeground opens the database and serves until ctx is done (a signal, in
// production), removing the pid file it owns on the way out. The removal is
// deferred before the database ever opens, so a daemon that never gets as
// far as a working database (a bad --db path, a locked file) still removes
// its pid file rather than leaving one behind for a `start` or `stop` that
// runs later to mistake for a server that is still running.
func runForeground(ctx context.Context, webFS fs.FS, port int) error {
	if pidPath, err := pidFilePath(); err == nil {
		defer removeOwnPIDFile(pidPath)
	}

	path, err := effectiveDBPath()
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	database, err := db.OpenAt(path)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer database.Close()

	srv := server.New(db.NewStore(database), webFS)
	return srv.ListenAndServe(ctx, port, path)
}

// removeOwnPIDFile removes the pid file at path only if it still names this
// process. A pid file that now names someone else — a later daemon that
// overwrote it, or one this process never wrote in the first place — is left
// alone.
func removeOwnPIDFile(path string) {
	pid, err := readPID(path)
	if err != nil || pid != os.Getpid() {
		return
	}
	os.Remove(path)
}

// binaryName is what `stop` and `start` look for in a pid's running command,
// so a pid a stale pid file names, once reused by an unrelated program, is
// never signaled. It matches the name every build (make build, make install,
// go build, go run) produces (see Makefile); a release tarball or a renamed
// build is covered separately by ownExecutablePath, since it is not this.
const binaryName = "taskboard"

// stopTimeout bounds how long `stop` waits for the signaled process to exit
// before giving up without removing the pid file. Shutdown itself can take up
// to the server's own shutdown timeout (5s) to let in-flight requests finish,
// so this must comfortably exceed that.
var stopTimeout = 10 * time.Second

// stopPollInterval is how often `stop` checks whether the process it signaled
// has exited yet.
var stopPollInterval = 50 * time.Millisecond

// processAlive reports whether pid names a running process.
func processAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}

// processCommandLine is pid's full command line (argv joined by spaces, e.g.
// "/usr/local/bin/taskboard start --port 3010 --foreground"), read via ps.
// -ww keeps ps from cutting a long line short. The line is returned whole,
// never split on whitespace, since the executable's path or the --db path
// may contain spaces (see isDaemonCommandLine). A var so tests can replace
// it, to simulate ps being unable to answer (a permissions error, a
// transient race) without needing a real such failure.
var processCommandLine = func(pid int) (string, error) {
	out, err := exec.Command("ps", "-ww", "-p", strconv.Itoa(pid), "-o", "args=").Output()
	if err != nil {
		return "", err
	}
	line := strings.TrimSpace(string(out))
	if line == "" {
		return "", fmt.Errorf("ps reported no command for pid %d", pid)
	}
	return line, nil
}

// ownExecutablePath is this process's own executable's path. A build is not
// always literally named "taskboard": a release tarball's binary is
// "taskboard-darwin-arm64", and a renamed dev build or a symlink could be
// anything else. daemonize starts the server with this same path as its
// argv[0]. A var so tests can override it without needing to rename or move
// the test binary itself.
var ownExecutablePath = func() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return exe
}

// isTaskboardBinary reports whether name, a running process's executable
// base name, could be a taskboard build: the name every build target
// produces, or this process's own executable's base name (see
// ownExecutablePath).
func isTaskboardBinary(name string) bool {
	if name == binaryName {
		return true
	}
	own := ownExecutablePath()
	return own != "" && name == filepath.Base(own)
}

// daemonArgsTail matches the end of the command line daemonize starts the
// server with: "start --port <n> --foreground", --foreground last.
var daemonArgsTail = regexp.MustCompile(` start --port [0-9]+ --foreground$`)

// isDaemonCommandLine reports whether line, a process's command line as ps
// prints it, has the exact shape daemonize runs:
//
//	<exe> [--db <path>] start --port <n> --foreground
//
// where <exe> is an absolute path (daemonize uses os.Executable) whose base
// name is a taskboard build's (see isTaskboardBinary). Anything else is not
// the server: `mcp`, which several concurrent AI sessions may each run from
// the very same binary, a CLI command that merely has "start" among its
// arguments, or some other program.
//
// ps joins argv with spaces and no quoting, and both <exe> and <path> may
// contain spaces themselves, so the line is never split on whitespace.
// Instead the fixed tail is matched first, and what comes before it is tried
// as <exe> alone, then as <exe> --db <path> at each " --db " in it.
func isDaemonCommandLine(line string) bool {
	tail := daemonArgsTail.FindStringIndex(line)
	if tail == nil {
		return false
	}
	head := line[:tail[0]]
	if isTaskboardExecutable(head) {
		return true
	}
	const dbFlag = " --db "
	for i := 0; i < len(head); i++ {
		if strings.HasPrefix(head[i:], dbFlag) && len(head) > i+len(dbFlag) && isTaskboardExecutable(head[:i]) {
			return true
		}
	}
	return false
}

// isTaskboardExecutable reports whether exe, a candidate argv[0], is an
// absolute path to a taskboard build, as daemonize's own argv[0] always is.
func isTaskboardExecutable(exe string) bool {
	return filepath.IsAbs(exe) && isTaskboardBinary(filepath.Base(exe))
}

// processIdentity classifies what a pid found in a pid file actually is, so
// start and stop know whether it is safe to signal.
type processIdentity int

const (
	// processUnknown means ps could not answer at all. It must never be
	// treated as license to signal the pid or to discard the pid file:
	// unlike processForeign, it does not mean the pid is definitely not
	// taskboard, only that this check could not tell either way.
	processUnknown processIdentity = iota
	// processForeign means the pid is definitely not this program's own
	// server: a different binary, or taskboard running some other
	// subcommand (like `mcp`) rather than the foreground server.
	processForeign
	// processDaemon means the pid is this program running its foreground
	// server, the way daemonize starts it.
	processDaemon
)

// identifyProcess classifies a live pid by its running command: taskboard's
// own foreground server, something else entirely, or unknown when ps itself
// could not answer.
func identifyProcess(pid int) processIdentity {
	line, err := processCommandLine(pid)
	if err != nil {
		return processUnknown
	}
	if !isDaemonCommandLine(line) {
		return processForeign
	}
	return processDaemon
}

// checkExistingDaemon reports whether pidPath names a taskboard daemon that
// is still running. A stale pid file — its process gone, or its pid reused
// by some unrelated program (or by taskboard running something other than
// the server) since — is removed on the spot and reported as not running,
// without ever signaling whatever that pid now belongs to. When a live pid's
// identity cannot be determined at all (err != nil), the pid file is left
// exactly as it is, since treating an unreadable process as either "safe to
// start over" or "definitely running" could both be wrong.
func checkExistingDaemon(pidPath string) (pid int, running bool, err error) {
	pid, readErr := readPID(pidPath)
	if readErr != nil {
		return 0, false, nil
	}
	if !processAlive(pid) {
		os.Remove(pidPath)
		return 0, false, nil
	}
	switch identifyProcess(pid) {
	case processDaemon:
		return pid, true, nil
	case processForeign:
		os.Remove(pidPath)
		return 0, false, nil
	default: // processUnknown
		return pid, false, fmt.Errorf("cannot tell whether pid %d (in %s) is taskboard; leaving the pid file in place", pid, pidPath)
	}
}

// stopDaemon signals pidPath's process to stop and waits, up to timeout, for
// it to exit before removing the pid file and returning its pid. It never
// signals a pid that is stale — its process already gone, or no longer
// taskboard's own server, since some unrelated program reused that pid (or
// taskboard itself is running something other than the server there) —
// instead removing the pid file on the spot and reporting the daemon as not
// running. If the process does not exit within timeout, or its identity
// cannot be determined at all, the pid file is left in place rather than
// guessed at.
func stopDaemon(pidPath string, timeout, pollInterval time.Duration) (int, error) {
	pid, err := readPID(pidPath)
	if err != nil {
		return 0, fmt.Errorf("taskboard is not running")
	}

	if !processAlive(pid) {
		os.Remove(pidPath)
		return 0, fmt.Errorf("taskboard is not running (stale pid file removed)")
	}

	switch identifyProcess(pid) {
	case processForeign:
		os.Remove(pidPath)
		return 0, fmt.Errorf("taskboard is not running (pid %d belongs to another process; stale pid file removed)", pid)
	case processUnknown:
		return 0, fmt.Errorf("cannot tell whether pid %d is taskboard; leaving the pid file in place, try again", pid)
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		os.Remove(pidPath)
		return 0, fmt.Errorf("taskboard is not running")
	}

	if err := process.Signal(syscall.SIGTERM); err != nil {
		os.Remove(pidPath)
		return 0, fmt.Errorf("failed to stop taskboard: %w", err)
	}

	if !waitForExit(pid, timeout, pollInterval) {
		return pid, fmt.Errorf("taskboard (pid %d) did not stop within %s; if it is stuck, try `kill -9 %d`", pid, timeout, pid)
	}

	os.Remove(pidPath)
	return pid, nil
}

// waitForExit polls, at pollInterval, until pid is no longer running or
// timeout elapses, reporting which happened.
func waitForExit(pid int, timeout, pollInterval time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if !processAlive(pid) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(pollInterval)
	}
}
