package db

import "time"

// sortableTimeFormat is the fixed-width, UTC layout every created_at /
// updated_at value is written in: an RFC 3339 timestamp with nanosecond
// precision padded to a constant width (Go's usual "9" fractional-second
// verb trims trailing zeros, which would leave rows with different string
// lengths). Every stamp this format produces is the same length and always
// UTC, so a plain text ORDER BY or MAX() over the column compares the same
// way the instants themselves do, whatever timezone or DST offset the
// writer's clock was in.
//
// modernc.org/sqlite already parses a TEXT value in a DATE/DATETIME/TIMESTAMP
// column back into a time.Time on read (its conn.go parseTime falls back to
// trying this shape once the trailing "Z" is stripped), so reading a
// created_at/updated_at column needs no change: only writes go through
// stamp.
//
// Migration 011 rewrote every row already in the database into this shape:
// Go's default `time.Time.String()` text (a local offset, a zone name, and
// sometimes a monotonic-clock suffix), which is everything this app had
// ever written, and ISO 8601 text, which nothing here writes but which the
// migration also converts rather than risk corrupting. A row in neither
// shape is left exactly as it was.
const sortableTimeFormat = "2006-01-02T15:04:05.000000000Z"

// stamp renders t as a sortable UTC string for a created_at/updated_at
// column. It is safe to call with a time.Time in any location.
func stamp(t time.Time) string {
	return t.UTC().Format(sortableTimeFormat)
}
