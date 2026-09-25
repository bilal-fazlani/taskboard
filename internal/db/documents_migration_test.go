package db

import (
	"testing"
	"time"

	"github.com/tcarac/taskboard/internal/models"
)

// TestMigrationAddsDocuments checks migration 008's constraints directly, below
// the store's own rules: exactly one owner, a known format, names unique per
// owner ignoring ASCII case, and documents deleted with their ticket.
func TestMigrationAddsDocuments(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Docs", "DOC")
	tk := seedTicket(t, s, p.ID, "Has docs")
	other := seedTicket(t, s, p.ID, "Other")
	now := time.Now()

	insert := func(id string, ticketID, epicID any, name, format string) error {
		_, err := s.db.Exec(`INSERT INTO documents (id, ticket_id, epic_id, name, format, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, id, ticketID, epicID, name, format, now, now)
		return err
	}

	if err := insert("d1", tk.ID, nil, "Plan", "markdown"); err != nil {
		t.Fatalf("inserting a ticket document: %v", err)
	}
	if err := insert("d2", nil, nil, "Orphan", "markdown"); err == nil {
		t.Fatal("a document with no owner was accepted")
	}
	e, err := s.CreateEpic(models.CreateEpicRequest{ProjectID: p.ID, Name: "Epic"})
	if err != nil {
		t.Fatalf("seeding epic: %v", err)
	}
	if err := insert("d3", tk.ID, e.ID, "Both", "markdown"); err == nil {
		t.Fatal("a document with two owners was accepted")
	}
	if err := insert("d4", nil, e.ID, "Plan", "html"); err != nil {
		t.Fatalf("inserting an epic document: %v", err)
	}
	if err := insert("d5", tk.ID, nil, "Other", "pdf"); err == nil {
		t.Fatal("an unknown format was accepted")
	}
	if err := insert("d6", tk.ID, nil, "PLAN", "markdown"); err == nil {
		t.Fatal("a second name differing only in ASCII case was accepted on one ticket")
	}
	if err := insert("d7", other.ID, nil, "Plan", "markdown"); err != nil {
		t.Fatalf("another ticket reusing a name: %v", err)
	}

	if err := s.DeleteTicket(tk.ID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM documents WHERE id = 'd1'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("after deleting the ticket, its document count = %d, %v", n, err)
	}
}
