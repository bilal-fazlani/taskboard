package db

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"
)

const (
	// DefaultWatchInterval is how often a Watcher polls for changes.
	DefaultWatchInterval = 250 * time.Millisecond

	// maxWatchBackoff caps the wait between attempts to reopen a failed
	// watcher connection.
	maxWatchBackoff = 5 * time.Second

	// watchLogEvery rate-limits the log while the watcher keeps failing.
	watchLogEvery = time.Minute
)

// Watcher notices commits to a database file made by any connection, in this
// process or another one (the CLI and the MCP server write from their own
// processes).
//
// It polls PRAGMA data_version, whose value changes whenever a connection
// other than the one asking commits a change. The watcher therefore owns a
// connection that nothing else uses: its own *sql.DB with a single, pinned
// connection, opened with the same settings as OpenAt. Writes through the
// server's own pool happen on other connections, so they are seen too.
type Watcher struct {
	path     string
	interval time.Duration
	db       *sql.DB
	conn     *sql.Conn
	last     int64
	closed   chan struct{}

	// logf and logEvery are fields so tests can observe and shorten them.
	logf     func(format string, args ...any)
	logEvery time.Duration
}

// OpenWatcher opens a dedicated connection to the database at dbPath and
// records its current data_version, so any commit after OpenWatcher returns is
// reported by Run. The database must already exist and be migrated (OpenAt
// does that). An interval of zero or less means DefaultWatchInterval.
func OpenWatcher(dbPath string, interval time.Duration) (*Watcher, error) {
	if interval <= 0 {
		interval = DefaultWatchInterval
	}
	w := &Watcher{
		path:     dbPath,
		interval: interval,
		closed:   make(chan struct{}),
		logf:     log.Printf,
		logEvery: watchLogEvery,
	}
	if err := w.open(context.Background()); err != nil {
		return nil, err
	}
	return w, nil
}

// open connects and reads the baseline data_version.
func (w *Watcher) open(ctx context.Context) error {
	database, err := sql.Open("sqlite", dsn(w.path))
	if err != nil {
		return fmt.Errorf("opening watcher database: %w", err)
	}
	database.SetMaxOpenConns(1)
	conn, err := database.Conn(ctx)
	if err != nil {
		database.Close()
		return fmt.Errorf("opening watcher connection: %w", err)
	}
	w.db, w.conn = database, conn
	if w.last, err = w.dataVersion(ctx); err != nil {
		w.release()
		return err
	}
	return nil
}

// release closes the current connection and its pool, if any.
func (w *Watcher) release() {
	if w.conn != nil {
		w.conn.Close()
		w.conn = nil
	}
	if w.db != nil {
		w.db.Close()
		w.db = nil
	}
}

func (w *Watcher) dataVersion(ctx context.Context) (int64, error) {
	var v int64
	if err := w.conn.QueryRowContext(ctx, "PRAGMA data_version").Scan(&v); err != nil {
		return 0, fmt.Errorf("reading data_version: %w", err)
	}
	return v, nil
}

// Run polls until ctx is done and calls onChange, from Run's goroutine, once
// per poll that found new commits. Several commits between two polls produce
// one call. Run does not close the watcher, and Close must not be called while
// Run is running.
//
// A failed poll drops the connection. Run then reopens it, backing off from
// the poll interval up to maxWatchBackoff between attempts, and calls onChange
// once it is back, because a commit made in the meantime cannot be told apart
// from the new connection's baseline. The first failure is logged, then at
// most one line per logEvery while it keeps failing, and the recovery.
func (w *Watcher) Run(ctx context.Context, onChange func()) {
	timer := time.NewTimer(w.interval)
	defer timer.Stop()

	var (
		failures int
		backoff  time.Duration
		lastLog  time.Time
	)
	fail := func(err error) {
		failures++
		backoff = min(max(w.interval, 2*backoff), maxWatchBackoff)
		if failures == 1 || time.Since(lastLog) >= w.logEvery {
			w.logf("watcher: %v (failed %d times, retrying in %v)", err, failures, backoff)
			lastLog = time.Now()
		}
		timer.Reset(backoff)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}

		if w.conn == nil {
			if err := w.open(ctx); err != nil {
				if ctx.Err() != nil {
					return
				}
				fail(err)
				continue
			}
			w.logf("watcher: reconnected after %d failures", failures)
			failures, backoff = 0, 0
			onChange()
			timer.Reset(w.interval)
			continue
		}

		v, err := w.dataVersion(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			w.release()
			fail(err)
			continue
		}
		if v != w.last {
			w.last = v
			onChange()
		}
		timer.Reset(w.interval)
	}
}

// Close releases the watcher's connection. It is safe to call more than once.
func (w *Watcher) Close() error {
	w.release()
	select {
	case <-w.closed:
	default:
		close(w.closed)
	}
	return nil
}

// Closed reports whether Close has been called.
func (w *Watcher) Closed() bool {
	select {
	case <-w.closed:
		return true
	default:
		return false
	}
}
