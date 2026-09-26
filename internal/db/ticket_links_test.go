package db

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/tcarac/taskboard/internal/models"
)

// typedLinksMigration is the migration that adds dependency kinds and
// surfaced-from links.
const typedLinksMigration = "017_typed_ticket_links.sql"

// kinds maps each ref's key to its kind.
func kinds(refs []models.TicketRef) map[string]string {
	out := map[string]string{}
	for _, r := range refs {
		out[r.Key] = r.Kind
	}
	return out
}

func sameKinds(t *testing.T, what string, got []models.TicketRef, want map[string]string) {
	t.Helper()
	g := kinds(got)
	if len(g) != len(want) {
		t.Fatalf("%s = %v, want %v", what, g, want)
	}
	for k, v := range want {
		if g[k] != v {
			t.Fatalf("%s = %v, want %v", what, g, want)
		}
	}
}

func wantRejected(t *testing.T, err error, what string) {
	t.Helper()
	var invalid *ErrInvalidInput
	if !errors.As(err, &invalid) {
		t.Fatalf("%s: err = %v, want ErrInvalidInput", what, err)
	}
}

func TestMigrationGivesExistingDependenciesNeedsWork(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacy := openLegacyDB(t, path, typedLinksMigration)
	execOrFail(t, legacy, `INSERT INTO projects (id, name, prefix) VALUES ('p1', 'Billing', 'BILL')`)
	execOrFail(t, legacy, `INSERT INTO tickets (id, project_id, number, title) VALUES ('t1', 'p1', 1, 'Blocker'), ('t2', 'p1', 2, 'Dependent')`)
	execOrFail(t, legacy, `INSERT INTO ticket_dependencies (ticket_id, blocked_by_id) VALUES ('t2', 't1')`)
	// Delivery (migration 015) sits under this one and keeps its rows.
	execOrFail(t, legacy, `INSERT INTO ticket_delivery (ticket_id, branch) VALUES ('t2', 'acp-2-dependent')`)
	if err := legacy.Close(); err != nil {
		t.Fatalf("closing legacy database: %v", err)
	}

	database, err := OpenAt(path)
	if err != nil {
		t.Fatalf("migrating legacy database: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	var kind string
	if err := database.QueryRow(`SELECT kind FROM ticket_dependencies WHERE ticket_id = 't2'`).Scan(&kind); err != nil {
		t.Fatalf("reading kind: %v", err)
	}
	if kind != models.DependencyNeedsWork {
		t.Fatalf("existing dependency's kind = %q, want %q", kind, models.DependencyNeedsWork)
	}

	s := NewStore(database)
	got, err := s.GetTicket("t2")
	if err != nil {
		t.Fatalf("GetTicket: %v", err)
	}
	if got.SurfacedFrom != nil || len(got.Surfaced) != 0 {
		t.Fatalf("existing ticket has surfaced links %+v / %+v, want none", got.SurfacedFrom, got.Surfaced)
	}
	sameKindsAndNotes(t, "dependsOn", got.DependsOn, map[string]string{"BILL-1": models.DependencyNeedsWork + "|"})
	if got.Delivery == nil || got.Delivery.Branch != "acp-2-dependent" {
		t.Fatalf("delivery = %+v after migrating, want the branch set before it", got.Delivery)
	}

	// The column only takes the two kinds.
	if _, err := database.Exec(`UPDATE ticket_dependencies SET kind = 'sometimes'`); err == nil {
		t.Fatal("kind accepted a value outside the two kinds")
	}
}

// dep is a dependsOn entry for the tests.
func dep(ticket, kind, note string) models.DependencyInput {
	return models.DependencyInput{Ticket: ticket, Kind: kind, Note: note}
}

// kindsAndNotes maps each ref's key to "kind|note".
func kindsAndNotes(refs []models.TicketRef) map[string]string {
	out := map[string]string{}
	for _, r := range refs {
		out[r.Key] = r.Kind + "|" + r.Note
	}
	return out
}

func sameKindsAndNotes(t *testing.T, what string, got []models.TicketRef, want map[string]string) {
	t.Helper()
	g := kindsAndNotes(got)
	if len(g) != len(want) {
		t.Fatalf("%s = %v, want %v", what, g, want)
	}
	for k, v := range want {
		if g[k] != v {
			t.Fatalf("%s = %v, want %v", what, g, want)
		}
	}
}

// A fresh database runs every migration in order, typed links after
// delivery (015) and the project journal (016), and ends with the tables and
// columns all three need.
func TestFreshDatabaseMigratesDeliveryJournalThenTypedLinks(t *testing.T) {
	s := newTestStore(t)
	rows, err := s.db.Query(`SELECT version FROM schema_migrations ORDER BY version`)
	if err != nil {
		t.Fatalf("reading schema_migrations: %v", err)
	}
	var versions []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatal(err)
		}
		versions = append(versions, v)
	}
	rows.Close()
	delivery, journal, links := -1, -1, -1
	for i, v := range versions {
		switch v {
		case "015_ticket_delivery.sql":
			delivery = i
		case "016_project_journal.sql":
			journal = i
		case typedLinksMigration:
			links = i
		}
	}
	if delivery < 0 || journal < 0 || links < 0 || !(delivery < journal && journal < links) {
		t.Fatalf("migrations = %v, want 015_ticket_delivery.sql, 016_project_journal.sql, then %s", versions, typedLinksMigration)
	}
	for _, q := range []string{
		`SELECT kind, note FROM ticket_dependencies`,
		`SELECT ticket_id, source_id FROM ticket_surfaced_from`,
		`SELECT ticket_id, branch, worktree, pr_url FROM ticket_delivery`,
		`SELECT ticket_id, repo, sha, position FROM ticket_landed_commits`,
		`SELECT COUNT(*) FROM project_journal_entries`,
	} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
}

func TestDependencyKindsAndNotesSetAndRead(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	seedTicket(t, s, p.ID, "Needed work")                        // BILL-1
	seedTicket(t, s, p.ID, "Same files")                         // BILL-2
	dependent, err := s.CreateTicket(models.CreateTicketRequest{ // BILL-3
		ProjectID: p.ID, Title: "Dependent",
		DependsOn: []models.DependencyInput{
			dep("BILL-1", "", ""),
			dep(" bill-2 ", " Conflict_Only ", "  internal/db/store.go, web/src/api/client.ts  "),
		},
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	want := map[string]string{
		"BILL-1": models.DependencyNeedsWork + "|",
		"BILL-2": models.DependencyConflictOnly + "|internal/db/store.go, web/src/api/client.ts",
	}
	sameKindsAndNotes(t, "created dependsOn", dependent.DependsOn, want)

	// The other end reads the same kind and note in blocks.
	blocker, err := s.GetTicket(dependent.DependsOn[1].ID)
	if err != nil {
		t.Fatalf("GetTicket: %v", err)
	}
	sameKindsAndNotes(t, "BILL-2 blocks", blocker.Blocks, map[string]string{"BILL-3": want["BILL-2"]})

	// Lists carry them too.
	list, err := s.ListTickets(models.TicketFilter{ProjectID: p.ID})
	if err != nil {
		t.Fatalf("ListTickets: %v", err)
	}
	for _, tk := range list {
		if tk.ID == dependent.ID {
			sameKindsAndNotes(t, "listed dependsOn", tk.DependsOn, want)
		}
	}
}

func TestDependsOnReplacesTheWholeList(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	seedTicket(t, s, p.ID, "One")   // BILL-1
	seedTicket(t, s, p.ID, "Two")   // BILL-2
	seedTicket(t, s, p.ID, "Three") // BILL-3
	dependent, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "Dependent",
		DependsOn: []models.DependencyInput{dep("BILL-1", "conflict_only", "store.go"), dep("BILL-2", "", "")},
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}

	// An entry without a kind or note needs work and has no note, whatever
	// the dependency had before: every write is the whole list.
	got, err := s.UpdateTicket(dependent.ID, models.UpdateTicketRequest{DependsOn: models.DependOn("BILL-1", "BILL-3")})
	if err != nil {
		t.Fatalf("UpdateTicket: %v", err)
	}
	sameKindsAndNotes(t, "after a plain update", got.DependsOn, map[string]string{
		"BILL-1": models.DependencyNeedsWork + "|", "BILL-3": models.DependencyNeedsWork + "|",
	})

	// A kind and note given set them; a note is allowed on either kind.
	got, err = s.UpdateTicket(dependent.ID, models.UpdateTicketRequest{DependsOn: []models.DependencyInput{
		dep("BILL-1", "needs_work", "needs the schema"), dep("BILL-2", "conflict_only", "mcp.go"),
	}})
	if err != nil {
		t.Fatalf("UpdateTicket: %v", err)
	}
	sameKindsAndNotes(t, "after explicit kinds", got.DependsOn, map[string]string{
		"BILL-1": models.DependencyNeedsWork + "|needs the schema", "BILL-2": models.DependencyConflictOnly + "|mcp.go",
	})

	// The same ticket twice, the same way, counts once.
	got, err = s.UpdateTicket(dependent.ID, models.UpdateTicketRequest{DependsOn: []models.DependencyInput{
		dep("BILL-2", "conflict_only", "mcp.go"), dep(got.DependsOn[1].ID, "conflict_only", " mcp.go "),
	}})
	if err != nil {
		t.Fatalf("UpdateTicket: %v", err)
	}
	sameKindsAndNotes(t, "named twice", got.DependsOn, map[string]string{"BILL-2": models.DependencyConflictOnly + "|mcp.go"})

	// An empty list clears them.
	got, err = s.UpdateTicket(dependent.ID, models.UpdateTicketRequest{DependsOn: []models.DependencyInput{}})
	if err != nil {
		t.Fatalf("UpdateTicket: %v", err)
	}
	if len(got.DependsOn) != 0 {
		t.Fatalf("dependsOn = %+v after [], want none", got.DependsOn)
	}
}

func TestBadDependencyAppliesNothing(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	seedTicket(t, s, p.ID, "One") // BILL-1
	seedTicket(t, s, p.ID, "Two") // BILL-2
	dependent, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "Dependent", DependsOn: []models.DependencyInput{dep("BILL-1", "conflict_only", "store.go")},
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}

	for name, entries := range map[string][]models.DependencyInput{
		"an unknown kind":       {dep("BILL-2", "sometimes", "")},
		"no ticket":             {dep("  ", "conflict_only", "")},
		"two kinds":             {dep("BILL-2", "conflict_only", ""), dep("BILL-2", "needs_work", "")},
		"two notes":             {dep("BILL-2", "", "a"), dep("BILL-2", "", "b")},
		"the ticket itself":     {dep("BILL-3", "", "")},
		"an unknown ticket key": {dep("BILL-99", "", "")},
	} {
		_, err := s.UpdateTicket(dependent.ID, models.UpdateTicketRequest{Title: strPtr("Changed"), DependsOn: entries})
		wantRejected(t, err, "UpdateTicket with "+name)
		if name != "the ticket itself" {
			_, err = s.CreateTicket(models.CreateTicketRequest{ProjectID: p.ID, Title: "New", DependsOn: entries})
			wantRejected(t, err, "CreateTicket with "+name)
		}
	}

	got, err := s.GetTicket(dependent.ID)
	if err != nil {
		t.Fatalf("GetTicket: %v", err)
	}
	if got.Title != "Dependent" {
		t.Fatalf("title = %q: a rejected update applied part of itself", got.Title)
	}
	sameKindsAndNotes(t, "dependsOn", got.DependsOn, map[string]string{"BILL-1": models.DependencyConflictOnly + "|store.go"})
	list, err := s.ListTickets(models.TicketFilter{ProjectID: p.ID})
	if err != nil {
		t.Fatalf("ListTickets: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("%d tickets, want 3: a rejected create left a ticket behind", len(list))
	}
}

func TestConflictOnlyDependencyStillHoldsReady(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	blocker := seedTicket(t, s, p.ID, "Same files")
	if _, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "Waits", DependsOn: []models.DependencyInput{dep(blocker.ID, "conflict_only", "")},
	}); err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	ready, err := s.ListTickets(models.TicketFilter{ProjectID: p.ID, Ready: true})
	if err != nil {
		t.Fatalf("ListTickets: %v", err)
	}
	if len(ready) != 1 || ready[0].ID != blocker.ID {
		t.Fatalf("ready = %d tickets, want only the blocker: a conflict-only dependency still waits", len(ready))
	}
}

func TestSurfacedFromSetAndRead(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Agent control plane", "ACP")
	auth := seedProject(t, s, "Auth", "AUTH")
	source := seedTicket(t, s, p.ID, "Hardening run") // ACP-1
	found, err := s.CreateTicket(models.CreateTicketRequest{
		ProjectID: p.ID, Title: "Found during the run", SurfacedFrom: strPtr("acp-1"),
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	if found.SurfacedFrom == nil || found.SurfacedFrom.ID != source.ID || found.SurfacedFrom.Key != "ACP-1" ||
		found.SurfacedFrom.Title != "Hardening run" || found.SurfacedFrom.Status != models.StatusTodo || found.SurfacedFrom.Kind != "" {
		t.Fatalf("surfacedFrom = %+v, want ACP-1 without a kind", found.SurfacedFrom)
	}

	// Across projects, by id.
	other, err := s.CreateTicket(models.CreateTicketRequest{ProjectID: auth.ID, Title: "Elsewhere", SurfacedFrom: strPtr(source.ID)})
	if err != nil {
		t.Fatalf("CreateTicket across projects: %v", err)
	}

	// The source reads what it surfaced.
	got, err := s.GetTicket(source.ID)
	if err != nil {
		t.Fatalf("GetTicket: %v", err)
	}
	sameKinds(t, "surfaced", got.Surfaced, map[string]string{"ACP-2": "", "AUTH-1": ""})
	if got.SurfacedFrom != nil {
		t.Fatalf("source surfacedFrom = %+v, want none", got.SurfacedFrom)
	}

	// Lists carry surfacedFrom, and never surfaced.
	list, err := s.ListTickets(models.TicketFilter{ProjectID: p.ID})
	if err != nil {
		t.Fatalf("ListTickets: %v", err)
	}
	for _, tk := range list {
		switch tk.ID {
		case found.ID:
			if tk.SurfacedFrom == nil || tk.SurfacedFrom.Key != "ACP-1" {
				t.Fatalf("listed surfacedFrom = %+v, want ACP-1", tk.SurfacedFrom)
			}
		case source.ID:
			if tk.SurfacedFrom != nil || tk.Surfaced != nil {
				t.Fatalf("listed source = %+v / %+v, want no links", tk.SurfacedFrom, tk.Surfaced)
			}
		}
	}

	// An update that leaves it out keeps it.
	got, err = s.UpdateTicket(found.ID, models.UpdateTicketRequest{Title: strPtr("Renamed")})
	if err != nil {
		t.Fatalf("UpdateTicket: %v", err)
	}
	if got.SurfacedFrom == nil {
		t.Fatal("an update without surfacedFrom dropped the link")
	}

	// It can be moved to another ticket, and removed with "" or "none".
	second := seedTicket(t, s, p.ID, "Second run") // ACP-3
	got, err = s.UpdateTicket(found.ID, models.UpdateTicketRequest{SurfacedFrom: strPtr("ACP-3")})
	if err != nil {
		t.Fatalf("UpdateTicket: %v", err)
	}
	if got.SurfacedFrom == nil || got.SurfacedFrom.ID != second.ID {
		t.Fatalf("surfacedFrom = %+v, want ACP-3", got.SurfacedFrom)
	}
	for _, clear := range []string{"", " None "} {
		got, err = s.UpdateTicket(found.ID, models.UpdateTicketRequest{SurfacedFrom: strPtr("ACP-3")})
		if err != nil {
			t.Fatalf("UpdateTicket: %v", err)
		}
		got, err = s.UpdateTicket(found.ID, models.UpdateTicketRequest{SurfacedFrom: strPtr(clear)})
		if err != nil {
			t.Fatalf("UpdateTicket clearing with %q: %v", clear, err)
		}
		if got.SurfacedFrom != nil {
			t.Fatalf("surfacedFrom = %+v after clearing with %q, want none", got.SurfacedFrom, clear)
		}
	}

	// Deleting the source removes the link.
	if err := s.DeleteTicket(source.ID); err != nil {
		t.Fatalf("DeleteTicket: %v", err)
	}
	got, err = s.GetTicket(other.ID)
	if err != nil {
		t.Fatalf("GetTicket: %v", err)
	}
	if got.SurfacedFrom != nil {
		t.Fatalf("surfacedFrom = %+v after its source was deleted, want none", got.SurfacedFrom)
	}
}

func TestBadSurfacedFromAppliesNothing(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Agent control plane", "ACP")
	root := seedTicket(t, s, p.ID, "Root") // ACP-1
	child, err := s.CreateTicket(models.CreateTicketRequest{ProjectID: p.ID, Title: "Child", SurfacedFrom: strPtr("ACP-1")})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	grandchild, err := s.CreateTicket(models.CreateTicketRequest{ProjectID: p.ID, Title: "Grandchild", SurfacedFrom: strPtr("ACP-2")})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}

	for name, value := range map[string]string{
		"itself":          "ACP-1",
		"its child":       "ACP-2",
		"its grandchild":  grandchild.ID,
		"an unknown key":  "ACP-99",
		"only spaces":     "   ",
		"a malformed key": "not a key",
	} {
		_, err := s.UpdateTicket(root.ID, models.UpdateTicketRequest{Title: strPtr("Changed"), SurfacedFrom: strPtr(value)})
		wantRejected(t, err, "surfaced from "+name)
	}
	_, err = s.CreateTicket(models.CreateTicketRequest{ProjectID: p.ID, Title: "New", SurfacedFrom: strPtr("ACP-99")})
	wantRejected(t, err, "CreateTicket surfaced from an unknown key")

	got, err := s.GetTicket(root.ID)
	if err != nil {
		t.Fatalf("GetTicket: %v", err)
	}
	if got.Title != "Root" || got.SurfacedFrom != nil {
		t.Fatalf("root = %q surfaced from %+v: a rejected update applied part of itself", got.Title, got.SurfacedFrom)
	}
	if len(got.Surfaced) != 1 || got.Surfaced[0].ID != child.ID {
		t.Fatalf("root surfaced = %+v, want only the child", got.Surfaced)
	}
	list, err := s.ListTickets(models.TicketFilter{ProjectID: p.ID})
	if err != nil {
		t.Fatalf("ListTickets: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("%d tickets, want 3: a rejected create left a ticket behind", len(list))
	}
}
