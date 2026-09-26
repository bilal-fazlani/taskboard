package db

import (
	"testing"
	"time"
)

// TestListStatusChangesBreaksTiesByRowid pins the newest-first tie-break
// documented on ListStatusChanges's query: writes are serialised
// (_txlock=immediate) and rowid follows commit order, so two rows that land
// on the same created_at instant still come back in the order they were
// written, later first. Every other store test relies on its writes getting
// different timestamps, so none of them would catch this ordering breaking.
func TestListStatusChangesBreaksTiesByRowid(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Agent Control Plane", "ACP")
	tk := seedTicket(t, s, p.ID, "Tied history")

	// The store always stamps "now" on write, so pin both rows to the same
	// instant directly rather than trying to land two API calls in the same
	// nanosecond.
	same := stamp(time.Now())
	if _, err := s.db.Exec(
		`INSERT INTO ticket_status_changes (id, ticket_id, from_status, to_status, note, created_at)
		VALUES (?, ?, 'todo', 'in_progress', 'first', ?)`,
		newID(), tk.ID, same,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(
		`INSERT INTO ticket_status_changes (id, ticket_id, from_status, to_status, note, created_at)
		VALUES (?, ?, 'in_progress', 'agent_review', 'second', ?)`,
		newID(), tk.ID, same,
	); err != nil {
		t.Fatal(err)
	}

	assertHistory(t, s, tk.ID,
		"in_progress>agent_review:second",
		"todo>in_progress:first",
		">todo:",
	)
}
