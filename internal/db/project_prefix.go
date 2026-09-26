package db

import (
	"database/sql"
	"fmt"
)

// checkPrefixFree refuses a project prefix that another project already uses,
// ignoring letter case: every prefix resolver matches case-insensitively, so
// "GLOW" and "glow" on two projects would make keys like GLOW-1 and the
// --project flag ambiguous. The comparison is SQLite's LOWER(), the same one
// the resolvers and migration 013's unique index use, so all three agree on
// what counts as the same prefix. exceptID is the project being renamed, which
// may change the case of its own prefix; it is "" on create.
//
// Callers run it in the transaction that writes the prefix. The index is the
// backstop; this is what turns a clash into a clear ErrInvalidInput.
func checkPrefixFree(q dbtx, prefix, exceptID string) error {
	var name, taken string
	err := q.QueryRow(
		"SELECT name, prefix FROM projects WHERE LOWER(prefix) = LOWER(?) AND id <> ? LIMIT 1",
		prefix, exceptID,
	).Scan(&name, &taken)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("checking project prefix %q: %w", prefix, err)
	}
	if taken == prefix {
		return invalidInput("project prefix %q is already used by project %q", prefix, name)
	}
	return invalidInput("project prefix %q is already used by project %q as %q; prefixes must differ by more than letter case",
		prefix, name, taken)
}
