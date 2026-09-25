package cli

import (
	"database/sql"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
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
	}
	root.PersistentFlags().StringVar(&dbPath, "db", "", "path to SQLite database file (default: OS config dir, live build only)")

	startCmd := &cobra.Command{
		Use:   "start",
		Short: "Start the web UI server",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !foreground {
				return daemonize(cmd.OutOrStdout(), port)
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
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			srv := server.New(db.NewStore(database), webFS)
			return srv.ListenAndServe(ctx, port, path)
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

			pid, err := readPID(pidPath)
			if err != nil {
				return fmt.Errorf("taskboard is not running")
			}

			process, err := os.FindProcess(pid)
			if err != nil {
				os.Remove(pidPath)
				return fmt.Errorf("taskboard is not running")
			}

			if err := process.Signal(syscall.Signal(0)); err != nil {
				os.Remove(pidPath)
				return fmt.Errorf("taskboard is not running (stale pid file removed)")
			}

			if err := process.Signal(syscall.SIGTERM); err != nil {
				os.Remove(pidPath)
				return fmt.Errorf("failed to stop taskboard: %w", err)
			}

			os.Remove(pidPath)
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
				var answer string
				fmt.Scanln(&answer)
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

	if pid, err := readPID(pidPath); err == nil {
		if p, err := os.FindProcess(pid); err == nil {
			if err := p.Signal(syscall.Signal(0)); err == nil {
				return fmt.Errorf("taskboard is already running (pid %d)", pid)
			}
		}
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
