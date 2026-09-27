package db

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tcarac/taskboard/internal/models"
)

// agentsMigration is the migration that adds sessions, agents, the agent
// holding a ticket, and requests for user input. These tests check its
// constraints directly, below any store method: the store's own rules come
// with the methods that use the tables.
const agentsMigration = "018_agents_sessions_requests.sql"

func assertAgentsSchema(t *testing.T, database *sql.DB) {
	t.Helper()
	for _, obj := range []struct{ kind, name string }{
		{"table", "sessions"},
		{"table", "agents"},
		{"table", "ticket_requests"},
		{"index", "idx_agents_session_id"},
		{"index", "idx_tickets_agent_id"},
		{"index", "idx_ticket_requests_one_open"},
		{"index", "idx_ticket_requests_ticket"},
		{"index", "idx_ticket_requests_agent_id"},
	} {
		var n int
		if err := database.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = ? AND name = ?`, obj.kind, obj.name).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("%s %s missing after migrating", obj.kind, obj.name)
		}
	}
	var n int
	if err := database.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('tickets') WHERE name = 'agent_id'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("tickets.agent_id missing after migrating (%d, %v)", n, err)
	}
}

// A fresh database runs the migration right after 017.
func TestFreshDatabaseMigratesAgentsAfterTypedLinks(t *testing.T) {
	s := newTestStore(t)
	rows, err := s.db.Query(`SELECT version FROM schema_migrations ORDER BY rowid`)
	if err != nil {
		t.Fatal(err)
	}
	var applied []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatal(err)
		}
		applied = append(applied, v)
	}
	rows.Close()
	links, agents := -1, -1
	for i, v := range applied {
		switch v {
		case typedLinksMigration:
			links = i
		case agentsMigration:
			agents = i
		}
	}
	if links < 0 || agents != links+1 {
		t.Fatalf("migrations applied %v: want %s right after %s", applied, agentsMigration, typedLinksMigration)
	}
	assertAgentsSchema(t, s.db)
}

// A database with tickets from before the migration keeps every ticket as it
// was, with no holding agent and no requests, and keeps working.
func TestMigrationKeepsExistingTickets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacy := openLegacyDB(t, path, agentsMigration)
	execOrFail(t, legacy, `INSERT INTO projects (id, name, prefix) VALUES ('p1', 'Billing', 'BILL')`)
	execOrFail(t, legacy, `INSERT INTO tickets (id, project_id, number, title, description, status, priority)
		VALUES ('t1', 'p1', 1, 'Invoice export', 'Export invoices as CSV', 'in_progress', 'high'),
		       ('t2', 'p1', 2, 'Tax rules', '', 'agent_review', 'low')`)
	execOrFail(t, legacy, `INSERT INTO ticket_dependencies (ticket_id, blocked_by_id, kind, note) VALUES ('t2', 't1', 'conflict_only', 'store.go')`)
	execOrFail(t, legacy, `INSERT INTO subtasks (id, ticket_id, title, completed, position) VALUES ('st1', 't1', 'Write it', 1, 0)`)
	if err := legacy.Close(); err != nil {
		t.Fatalf("closing legacy database: %v", err)
	}

	database, err := OpenAt(path)
	if err != nil {
		t.Fatalf("migrating legacy database: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	assertAgentsSchema(t, database)

	s := NewStore(database)
	got, err := s.GetTicket("t1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Invoice export" || got.Description != "Export invoices as CSV" || got.Status != "in_progress" ||
		got.Priority != "high" || len(got.Subtasks) != 1 || !got.Subtasks[0].Completed {
		t.Fatalf("t1 after migrating: %+v", got)
	}
	blocked, err := s.GetTicket("t2")
	if err != nil {
		t.Fatal(err)
	}
	if blocked.Status != "agent_review" || len(blocked.DependsOn) != 1 ||
		blocked.DependsOn[0].Kind != models.DependencyConflictOnly || blocked.DependsOn[0].Note != "store.go" {
		t.Fatalf("t2 after migrating: %+v", blocked)
	}
	for _, id := range []string{"t1", "t2"} {
		if agent, held := ticketAgent(t, s, id); held {
			t.Fatalf("%s is held by %q after migrating, want no agent", id, agent)
		}
	}
	var n int
	if err := database.QueryRow(`SELECT COUNT(*) FROM ticket_requests`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("ticket_requests after migrating: %d rows, %v; want none", n, err)
	}
	if _, err := s.MoveTicket("t1", models.MoveTicketRequest{Status: models.StatusDone}); err != nil {
		t.Fatalf("moving an existing ticket after migrating: %v", err)
	}
}

// mustFail runs query and fails the test if the database accepts it.
func mustFail(t *testing.T, s *Store, what, query string, args ...any) {
	t.Helper()
	if _, err := s.db.Exec(query, args...); err == nil {
		t.Fatalf("%s was accepted", what)
	}
}

func insertSession(s *Store, id, vendor, vendorSessionID string) error {
	_, err := s.db.Exec(`INSERT INTO sessions (id, vendor, vendor_session_id, machine, resume_command, created_at)
		VALUES (?, ?, ?, 'bilal-mbp', 'claude --resume '||?, ?)`, id, vendor, vendorSessionID, vendorSessionID, stamp(time.Now()))
	return err
}

// A session stores its vendor, the vendor's session ID, the machine, the
// resume command and an optional web link; vendor and session ID together
// name one session and neither may be empty.
func TestSessionsStoreTheirLinkAndAreUniquePerVendor(t *testing.T) {
	s := newTestStore(t)

	if err := insertSession(s, "s1", "claude_code", "3da2c294"); err != nil {
		t.Fatalf("inserting a session: %v", err)
	}
	var machine, resume, webURL string
	if err := s.db.QueryRow(`SELECT machine, resume_command, web_url FROM sessions WHERE id = 's1'`).
		Scan(&machine, &resume, &webURL); err != nil {
		t.Fatal(err)
	}
	if machine != "bilal-mbp" || resume != "claude --resume 3da2c294" || webURL != "" {
		t.Fatalf("session read back machine %q resume %q web %q, want bilal-mbp, the command, and no web link", machine, resume, webURL)
	}
	if _, err := s.db.Exec(`INSERT INTO sessions (id, vendor, vendor_session_id, machine, resume_command, web_url, created_at)
		VALUES ('s2', 'codex', 'abc', 'bilal-mbp', 'codex resume abc', 'https://chatgpt.com/codex/tasks/abc', ?)`, stamp(time.Now())); err != nil {
		t.Fatalf("inserting a session with a web link: %v", err)
	}

	if err := insertSession(s, "s3", "claude_code", "3da2c294"); err == nil {
		t.Fatal("a second session with the same vendor and session ID was accepted")
	}
	if err := insertSession(s, "s4", "codex", "3da2c294"); err != nil {
		t.Fatalf("the same session ID from another vendor: %v", err)
	}
	if err := insertSession(s, "s5", "", "empty-vendor"); err == nil {
		t.Fatal("a session with no vendor was accepted")
	}
	if err := insertSession(s, "s6", "claude_code", ""); err == nil {
		t.Fatal("a session with no session ID was accepted")
	}
}

func insertAgent(s *Store, id, sessionID, role, provider string) error {
	now := stamp(time.Now())
	_, err := s.db.Exec(`INSERT INTO agents (id, session_id, role, model, provider, created_at, last_seen_at)
		VALUES (?, ?, ?, 'claude-opus-5-5', ?, ?, ?)`, id, sessionID, role, provider, now, now)
	return err
}

// An agent is a worker in one session, with a role, a model and a provider,
// free text checked against models.Providers. Several agents share a
// session, and a session that still has agents cannot be deleted.
func TestAgentsAreWorkersInASession(t *testing.T) {
	s := newTestStore(t)
	if err := insertSession(s, "s1", "claude_code", "3da2c294"); err != nil {
		t.Fatal(err)
	}

	if err := insertAgent(s, "a1", "s1", "orchestrator", "anthropic"); err != nil {
		t.Fatalf("inserting the main agent: %v", err)
	}
	if err := insertAgent(s, "a2", "s1", "implementer", "anthropic"); err != nil {
		t.Fatalf("inserting a subagent in the same session: %v", err)
	}
	var role, model, provider string
	var created, lastSeen time.Time
	if err := s.db.QueryRow(`SELECT role, model, provider, created_at, last_seen_at FROM agents WHERE id = 'a2'`).
		Scan(&role, &model, &provider, &created, &lastSeen); err != nil {
		t.Fatal(err)
	}
	if role != "implementer" || model != "claude-opus-5-5" || provider != "anthropic" || created.IsZero() || lastSeen.IsZero() {
		t.Fatalf("agent read back role %q model %q provider %q created %v last seen %v", role, model, provider, created, lastSeen)
	}

	// The provider is free text in the table; models.Providers is the check,
	// so a provider it doesn't name yet needs no change to the table.
	if models.ValidProvider("mistral") {
		t.Fatal("mistral is a valid provider, want it unknown")
	}
	if err := insertAgent(s, "a3", "s1", "reviewer", "mistral"); err != nil {
		t.Fatalf("the table refused a provider it should leave to models: %v", err)
	}
	if err := insertAgent(s, "a4", "no-such-session", "reviewer", "anthropic"); err == nil {
		t.Fatal("an agent in no session was accepted")
	}
	mustFail(t, s, "an agent with no session", `INSERT INTO agents (id, role, model, provider, created_at, last_seen_at)
		VALUES ('a5', 'reviewer', 'm', 'other', '', '')`)

	mustFail(t, s, "deleting a session that has agents", `DELETE FROM sessions WHERE id = 's1'`)
}

func ticketAgent(t *testing.T, s *Store, ticketID string) (string, bool) {
	t.Helper()
	var agentID *string
	if err := s.db.QueryRow(`SELECT agent_id FROM tickets WHERE id = ?`, ticketID).Scan(&agentID); err != nil {
		t.Fatalf("reading tickets.agent_id: %v", err)
	}
	if agentID == nil {
		return "", false
	}
	return *agentID, true
}

// A ticket is held by at most one agent, none when created. The agent must
// exist, and deleting it frees the ticket without deleting the ticket.
func TestTicketsCarryAnOptionalHoldingAgent(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	tk := seedTicket(t, s, p.ID, "Invoice export")
	if id, held := ticketAgent(t, s, tk.ID); held {
		t.Fatalf("a new ticket is held by %q, want no agent", id)
	}

	if err := insertSession(s, "s1", "claude_code", "3da2c294"); err != nil {
		t.Fatal(err)
	}
	if err := insertAgent(s, "a1", "s1", "implementer", "anthropic"); err != nil {
		t.Fatal(err)
	}
	execOrFail(t, s.db, `UPDATE tickets SET agent_id = 'a1' WHERE id = ?`, tk.ID)
	if id, held := ticketAgent(t, s, tk.ID); !held || id != "a1" {
		t.Fatalf("ticket held by %q (%v), want a1", id, held)
	}
	mustFail(t, s, "a ticket held by an agent that does not exist",
		`UPDATE tickets SET agent_id = 'no-such-agent' WHERE id = ?`, tk.ID)

	execOrFail(t, s.db, `DELETE FROM agents WHERE id = 'a1'`)
	if id, held := ticketAgent(t, s, tk.ID); held {
		t.Fatalf("after deleting its agent the ticket is held by %q, want no agent", id)
	}
	if got, err := s.GetTicket(tk.ID); err != nil || got.Title != "Invoice export" {
		t.Fatalf("the ticket after its agent was deleted: %+v, %v", got, err)
	}
}

// Only a request puts a ticket in needs_user_input, so no write sets it,
// and the refusal says how a ticket gets there; a list filters on it, and
// the board has a column for it.
func TestStoreWritesRefuseNeedsUserInput(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	tk := seedTicket(t, s, p.ID, "Invoice export")
	status := models.StatusNeedsUserInput

	_, err := s.CreateTicket(models.CreateTicketRequest{ProjectID: p.ID, Title: "Asks", Status: status})
	wantRejected(t, err, "creating a ticket in needs_user_input")
	_, err = s.MoveTicket(tk.ID, models.MoveTicketRequest{Status: status})
	wantRejected(t, err, "moving a ticket to needs_user_input")
	if !strings.Contains(err.Error(), "request user input") {
		t.Errorf("moving to needs_user_input: %v; want it to say a request puts a ticket there", err)
	}
	_, err = s.UpdateTicket(tk.ID, models.UpdateTicketRequest{Status: &status})
	wantRejected(t, err, "updating a ticket to needs_user_input")
	if list, err := s.ListTickets(models.TicketFilter{Statuses: []string{status}}); err != nil || len(list) != 0 {
		t.Fatalf("filtering on needs_user_input: %+v, %v; want no tickets and no error", list, err)
	}

	board, err := s.GetBoard(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	has := false
	for _, c := range board.Columns {
		has = has || c.Status == status
	}
	if !has {
		t.Fatal("the board has no needs_user_input column")
	}
}

// seedAsker adds a session with one agent, "asker", to make requests.
func seedAsker(t *testing.T, s *Store) {
	t.Helper()
	if err := insertSession(s, "s1", "claude_code", "3da2c294"); err != nil {
		t.Fatal(err)
	}
	if err := insertAgent(s, "asker", "s1", "implementer", "anthropic"); err != nil {
		t.Fatal(err)
	}
}

// insertRequest adds a request made by the agent "asker".
func insertRequest(s *Store, id, ticketID, typ, choices string) error {
	_, err := s.db.Exec(`INSERT INTO ticket_requests (id, ticket_id, agent_id, type, prompt, choices, created_at)
		VALUES (?, ?, 'asker', ?, 'Ship it?', ?, ?)`, id, ticketID, typ, choices, stamp(time.Now()))
	return err
}

// A request names the agent that made it, which must exist. That record
// outlives a takeover of the ticket, and deleting the agent fails while it
// has requests.
func TestTicketRequestsNameTheAgentThatAsked(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	tk := seedTicket(t, s, p.ID, "Invoice export")
	seedAsker(t, s)

	mustFail(t, s, "a request with no agent", `INSERT INTO ticket_requests (id, ticket_id, type, prompt, created_at)
		VALUES ('r0', ?, 'question', 'Hm?', ?)`, tk.ID, stamp(time.Now()))
	mustFail(t, s, "a request naming an unknown agent", `INSERT INTO ticket_requests (id, ticket_id, agent_id, type, prompt, created_at)
		VALUES ('r0', ?, 'no-such-agent', 'question', 'Hm?', ?)`, tk.ID, stamp(time.Now()))

	execOrFail(t, s.db, `UPDATE tickets SET agent_id = 'asker' WHERE id = ?`, tk.ID)
	if err := insertRequest(s, "r1", tk.ID, models.UserInputApproval, `["approved","declined"]`); err != nil {
		t.Fatalf("inserting a request: %v", err)
	}

	// Another agent takes the ticket over; the request still names the asker.
	if err := insertAgent(s, "taker", "s1", "implementer", "anthropic"); err != nil {
		t.Fatal(err)
	}
	execOrFail(t, s.db, `UPDATE tickets SET agent_id = 'taker' WHERE id = ?`, tk.ID)
	var asker string
	if err := s.db.QueryRow(`SELECT agent_id FROM ticket_requests WHERE id = 'r1'`).Scan(&asker); err != nil || asker != "asker" {
		t.Fatalf("after a takeover the request names %q, %v; want asker", asker, err)
	}

	mustFail(t, s, "deleting an agent that has requests", `DELETE FROM agents WHERE id = 'asker'`)
	execOrFail(t, s.db, `DELETE FROM agents WHERE id = 'taker'`)
	if held, ok := ticketAgent(t, s, tk.ID); ok {
		t.Fatalf("after deleting its holder the ticket is held by %q, want no agent", held)
	}
}

func answerRequest(s *Store, id, answer, by string) error {
	_, err := s.db.Exec(`UPDATE ticket_requests SET answer = ?, answered_by = ?, answered_at = ? WHERE id = ?`,
		answer, by, stamp(time.Now()), id)
	return err
}

// A request for user input stores its ticket, type, prompt, choices (a JSON
// array), and once answered the answer, who answered and when, all three
// together. A ticket has at most one unanswered request, and its requests go
// with it. The table takes any type: the list lives in models.
func TestTicketRequestsKeepOneOpenPerTicket(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	tk := seedTicket(t, s, p.ID, "Invoice export")
	other := seedTicket(t, s, p.ID, "Tax rules")
	seedAsker(t, s)

	if err := insertRequest(s, "r1", tk.ID, models.UserInputApproval, `["approved","declined"]`); err != nil {
		t.Fatalf("inserting an approval request: %v", err)
	}
	var typ, prompt, choices string
	var answer, answeredBy *string
	var answeredAt *time.Time
	if err := s.db.QueryRow(`SELECT type, prompt, choices, answer, answered_by, answered_at FROM ticket_requests WHERE id = 'r1'`).
		Scan(&typ, &prompt, &choices, &answer, &answeredBy, &answeredAt); err != nil {
		t.Fatal(err)
	}
	if typ != "approval" || prompt != "Ship it?" || choices != `["approved","declined"]` || answer != nil || answeredBy != nil || answeredAt != nil {
		t.Fatalf("open request read back type %q prompt %q choices %s answer %v by %v at %v", typ, prompt, choices, answer, answeredBy, answeredAt)
	}

	if err := insertRequest(s, "r2", tk.ID, models.UserInputQuestion, `[]`); err == nil {
		t.Fatal("a second unanswered request on one ticket was accepted")
	}
	if err := insertRequest(s, "r3", other.ID, models.UserInputQuestion, `[]`); err != nil {
		t.Fatalf("another ticket's own open request: %v", err)
	}

	// Answer, who answered and when go together.
	mustFail(t, s, "an answer with no time or person", `UPDATE ticket_requests SET answer = 'yes' WHERE id = 'r1'`)
	mustFail(t, s, "an answer with no person",
		`UPDATE ticket_requests SET answer = 'yes', answered_at = ? WHERE id = 'r1'`, stamp(time.Now()))
	mustFail(t, s, "an answer time with no answer",
		`UPDATE ticket_requests SET answered_by = 'bilal', answered_at = ? WHERE id = 'r1'`, stamp(time.Now()))
	if err := answerRequest(s, "r1", "approved", "bilal"); err != nil {
		t.Fatalf("answering: %v", err)
	}
	if err := s.db.QueryRow(`SELECT answer, answered_by, answered_at FROM ticket_requests WHERE id = 'r1'`).
		Scan(&answer, &answeredBy, &answeredAt); err != nil {
		t.Fatal(err)
	}
	if answer == nil || *answer != "approved" || answeredBy == nil || *answeredBy != "bilal" || answeredAt == nil {
		t.Fatalf("answered request read back answer %v by %v at %v", answer, answeredBy, answeredAt)
	}
	if err := insertRequest(s, "r4", tk.ID, models.UserInputQuestion, `[]`); err != nil {
		t.Fatalf("a new request once the open one is answered: %v", err)
	}

	// Choices default to none and must be a JSON array.
	execOrFail(t, s.db, `DELETE FROM ticket_requests WHERE id = 'r3'`)
	execOrFail(t, s.db, `INSERT INTO ticket_requests (id, ticket_id, agent_id, type, prompt, created_at)
		VALUES ('r5', ?, 'asker', 'question', 'Which one?', ?)`, other.ID, stamp(time.Now()))
	if err := s.db.QueryRow(`SELECT choices FROM ticket_requests WHERE id = 'r5'`).Scan(&choices); err != nil || choices != "[]" {
		t.Fatalf("default choices = %q, %v; want []", choices, err)
	}
	execOrFail(t, s.db, `DELETE FROM ticket_requests WHERE id = 'r5'`)
	for _, bad := range []string{`approved,declined`, `{"a":1}`, `"approved"`} {
		if err := insertRequest(s, "bad", other.ID, models.UserInputQuestion, bad); err == nil {
			t.Fatalf("choices %s were accepted, want a JSON array only", bad)
		}
	}

	// The type is free text in the table; models.UserInputTypes is the check.
	if models.ValidUserInputType("hand_off") {
		t.Fatal("hand_off is a valid type of user input, want it unknown")
	}
	if err := insertRequest(s, "r6", other.ID, "hand_off", `[]`); err != nil {
		t.Fatalf("the table refused a type it should leave to models: %v", err)
	}
	mustFail(t, s, "a request on no ticket", `INSERT INTO ticket_requests (id, ticket_id, agent_id, type, prompt, created_at)
		VALUES ('r7', 'no-such-ticket', 'asker', 'question', 'Hm?', ?)`, stamp(time.Now()))

	if err := s.DeleteTicket(tk.ID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM ticket_requests WHERE ticket_id = ?`, tk.ID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("after deleting the ticket it has %d requests, %v; want none", n, err)
	}
}
