package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/tcarac/taskboard/internal/models"
)

// entriesFixture is a project with an epic and a ticket, and an agent in a
// session, for entries to sit on and be written by.
type entriesFixture struct {
	s       *Store
	project *models.Project
	epic    *models.Epic
	ticket  *models.Ticket
	agent   string
	session string
}

func newEntriesFixture(t *testing.T) entriesFixture {
	t.Helper()
	s := newTestStore(t)
	f := entriesFixture{s: s, agent: "a1", session: "s1"}
	f.project = seedProject(t, s, "Agent Control Plane", "ACP")
	f.epic = seedEpic(t, s, f.project.ID, "Write-back")
	f.ticket = seedTicket(t, s, f.project.ID, "Entries in the store")
	if err := insertSession(s, f.session, "claude_code", "3da2c294"); err != nil {
		t.Fatal(err)
	}
	if err := insertAgent(s, f.agent, f.session, "implementer", models.ProviderAnthropic); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f entriesFixture) onProject() models.EntryOwner {
	return models.EntryOwner{ProjectID: f.project.ID}
}

func (f entriesFixture) onEpic() models.EntryOwner {
	return models.EntryOwner{EpicID: f.epic.ID}
}

func (f entriesFixture) onTicket() models.EntryOwner {
	return models.EntryOwner{TicketID: f.ticket.ID}
}

func (f entriesFixture) owners() map[string]models.EntryOwner {
	return map[string]models.EntryOwner{"project": f.onProject(), "epic": f.onEpic(), "ticket": f.onTicket()}
}

func mustCreateEntry(t *testing.T, s *Store, req models.CreateEntryRequest) *models.Entry {
	t.Helper()
	e, err := s.CreateEntry(req)
	if err != nil {
		t.Fatalf("creating %s entry %q: %v", req.Type, req.Text, err)
	}
	return e
}

func wantInvalidContaining(t *testing.T, err error, want string) {
	t.Helper()
	var invalid *ErrInvalidInput
	if !errors.As(err, &invalid) || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want an ErrInvalidInput containing %q", err, want)
	}
}

func listCurrent(t *testing.T, s *Store, owner models.EntryOwner) models.EntryPage {
	t.Helper()
	page, err := s.ListEntries(owner, models.EntryFilter{}, "", EntryMaxLimit)
	if err != nil {
		t.Fatal(err)
	}
	return page
}

func ids(entries []models.Entry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.ID
	}
	return out
}

// learning is a learning on owner written by the fixture's agent.
func (f entriesFixture) learning(owner models.EntryOwner, text string) models.CreateEntryRequest {
	return models.CreateEntryRequest{EntryOwner: owner, Type: models.EntryLearning, Text: text, AgentID: f.agent}
}

// An entry is stored with its type, its one owner, its author (an agent,
// whose session is the agent's, or the person by name) and its text, trimmed,
// and reads back the same, on a project, an epic and a ticket alike.
func TestEntryStoredWithTypeOwnerAuthorAndText(t *testing.T) {
	f := newEntriesFixture(t)
	for kind, owner := range f.owners() {
		t.Run(kind, func(t *testing.T) {
			before := time.Now().UTC()
			byAgent := mustCreateEntry(t, f.s, models.CreateEntryRequest{EntryOwner: owner, Type: " learning ",
				Text: "\n The sqlite3 shell opens with foreign keys off. \n", AgentID: " " + f.agent + " "})
			byPerson := mustCreateEntry(t, f.s, models.CreateEntryRequest{EntryOwner: owner, Type: models.EntryNote,
				Text: "Check the migration on a copy.", AuthorName: "  Bilal  "})
			after := time.Now().UTC()

			if byAgent.ID == "" || byAgent.EntryOwner != owner || byAgent.Type != models.EntryLearning ||
				byAgent.Text != "The sqlite3 shell opens with foreign keys off." || byAgent.AgentID != f.agent || byAgent.AuthorName != "" {
				t.Fatalf("agent's entry = %+v", byAgent)
			}
			if byAgent.CreatedAt.Before(before) || byAgent.CreatedAt.After(after) {
				t.Fatalf("createdAt %v, want between %v and %v", byAgent.CreatedAt, before, after)
			}
			if byPerson.AuthorName != "Bilal" || byPerson.AgentID != "" || byPerson.Type != models.EntryNote {
				t.Fatalf("person's entry = %+v", byPerson)
			}
			// The session is reached through the agent.
			var session string
			if err := f.s.db.QueryRow(`SELECT a.session_id FROM entries e JOIN agents a ON a.id = e.agent_id WHERE e.id = ?`,
				byAgent.ID).Scan(&session); err != nil || session != f.session {
				t.Fatalf("session of the agent's entry = %q, %v; want %s", session, err, f.session)
			}

			got, err := f.s.GetEntry(byAgent.ID)
			if err != nil || got == nil || !reflect.DeepEqual(*got, *byAgent) {
				t.Fatalf("GetEntry = %+v, %v; want %+v", got, err, byAgent)
			}
			page := listCurrent(t, f.s, owner)
			if fmt.Sprint(ids(page.Entries)) != fmt.Sprint([]string{byPerson.ID, byAgent.ID}) || page.Total != 2 {
				t.Fatalf("%s's entries = %v (total %d), want the note then the learning", kind, ids(page.Entries), page.Total)
			}
		})
	}
	// A project's entries are its own, not its epics' or tickets'.
	if page := listCurrent(t, f.s, models.EntryOwner{ProjectID: "acp"}); page.Total != 2 {
		t.Fatalf("project entries by prefix: total %d, want 2", page.Total)
	}
	key := fmt.Sprintf("ACP-%d", f.ticket.Number)
	if page := listCurrent(t, f.s, models.EntryOwner{TicketID: strings.ToLower(key)}); page.Total != 2 {
		t.Fatalf("ticket entries by key: total %d, want 2", page.Total)
	}
}

// A ticket-only type (hand-off, proof, review) is refused on a project and
// an epic, and taken on a ticket.
func TestTicketOnlyEntryTypesRefusedElsewhere(t *testing.T) {
	f := newEntriesFixture(t)
	req := func(owner models.EntryOwner, typ string) models.CreateEntryRequest {
		r := models.CreateEntryRequest{EntryOwner: owner, Type: typ, Text: "Stopped after the migration.", AgentID: f.agent}
		if typ == models.EntryReview {
			r.Verdict = models.ReviewApprove
		}
		return r
	}
	for _, typ := range models.TicketOnlyEntryTypes {
		for kind, owner := range map[string]models.EntryOwner{"project": f.onProject(), "epic": f.onEpic()} {
			_, err := f.s.CreateEntry(req(owner, typ))
			wantInvalidContaining(t, err, fmt.Sprintf("a %s entry goes on a ticket only, not on a %s", typ, kind))
		}
		mustCreateEntry(t, f.s, req(f.onTicket(), typ))
	}
	if n := countRows(t, f.s, `SELECT COUNT(*) FROM entries WHERE ticket_id IS NULL`); n != 0 {
		t.Fatalf("%d refused entries were written", n)
	}
}

// A bad type, text, owner or author is the caller's mistake and writes
// nothing.
func TestCreateEntryRejectsBadInput(t *testing.T) {
	f := newEntriesFixture(t)
	good := f.learning(f.onTicket(), "text")
	cases := []struct {
		name string
		edit func(r *models.CreateEntryRequest)
		want string
	}{
		{"unknown type", func(r *models.CreateEntryRequest) { r.Type = "comment" }, `type "comment" is not an entry type`},
		{"type spelled otherwise", func(r *models.CreateEntryRequest) { r.Type = "hand-off" }, "not an entry type"},
		{"blank text", func(r *models.CreateEntryRequest) { r.Text = " \n " }, "text is required"},
		{"no owner", func(r *models.CreateEntryRequest) { r.EntryOwner = models.EntryOwner{} }, "exactly one project, epic or ticket"},
		{"two owners", func(r *models.CreateEntryRequest) { r.EpicID = f.epic.ID }, "exactly one project, epic or ticket"},
		{"unknown project", func(r *models.CreateEntryRequest) { r.EntryOwner = models.EntryOwner{ProjectID: "NOPE"} }, "project not found"},
		{"unknown epic", func(r *models.CreateEntryRequest) { r.EntryOwner = models.EntryOwner{EpicID: "nope"} }, "epic not found"},
		{"unknown ticket", func(r *models.CreateEntryRequest) { r.EntryOwner = models.EntryOwner{TicketID: "ACP-999"} }, "no ticket matches"},
		{"no author", func(r *models.CreateEntryRequest) { r.AgentID = "" }, "author is required"},
		{"both authors", func(r *models.CreateEntryRequest) { r.AuthorName = "Bilal" }, "not both"},
		{"unknown agent", func(r *models.CreateEntryRequest) { r.AgentID = "ghost" }, `agentId "ghost" is not an agent`},
		{"long name", func(r *models.CreateEntryRequest) {
			r.AgentID, r.AuthorName = "", strings.Repeat("a", EntryAuthorNameMaxLength+1)
		}, "at most 100"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := good
			c.edit(&r)
			_, err := f.s.CreateEntry(r)
			wantInvalidContaining(t, err, c.want)
		})
	}
	if n := countRows(t, f.s, `SELECT COUNT(*) FROM entries`); n != 0 {
		t.Fatalf("%d entries written by refused creates", n)
	}
}

// Below the store, the table itself keeps each entry on one owner with one
// author, and never lets an entry be edited, whatever writes to the file.
func TestEntriesTableKeepsOneOwnerOneAuthorAndNoEdits(t *testing.T) {
	f := newEntriesFixture(t)
	e := mustCreateEntry(t, f.s, f.learning(f.onTicket(), "original"))
	now := stamp(time.Now())
	mustFail(t, f.s, "an entry on no owner",
		`INSERT INTO entries (id, type, text, author_name, created_at) VALUES ('x', 'note', 't', 'Bilal', ?)`, now)
	mustFail(t, f.s, "an entry on two owners",
		`INSERT INTO entries (id, project_id, epic_id, type, text, author_name, created_at) VALUES ('x', ?, ?, 'note', 't', 'Bilal', ?)`,
		f.project.ID, f.epic.ID, now)
	mustFail(t, f.s, "an entry with two authors",
		`INSERT INTO entries (id, project_id, type, text, agent_id, author_name, created_at) VALUES ('x', ?, 'learning', 't', ?, 'Bilal', ?)`,
		f.project.ID, f.agent, now)
	mustFail(t, f.s, "an entry with no author",
		`INSERT INTO entries (id, project_id, type, text, created_at) VALUES ('x', ?, 'learning', 't', ?)`, f.project.ID, now)
	for _, set := range []string{"text = 'rewritten'", "type = 'decision'", "ticket_id = NULL, project_id = '" + f.project.ID + "'"} {
		_, err := f.s.db.Exec(`UPDATE entries SET `+set+` WHERE id = ?`, e.ID)
		if err == nil || !strings.Contains(err.Error(), "entries are never edited") {
			t.Fatalf("UPDATE SET %s: error = %v, want it refused", set, err)
		}
	}
	if got, _ := f.s.GetEntry(e.ID); got.Text != "original" || got.Type != models.EntryLearning {
		t.Fatalf("entry after refused edits = %+v", got)
	}
	// An agent that wrote an entry cannot be deleted from under it.
	mustFail(t, f.s, "deleting an agent that wrote an entry", `DELETE FROM agents WHERE id = ?`, f.agent)
}

// A new entry can replace an earlier one of its type on the same owner.
// Reads leave the replaced entry out unless asked for it; asked, it comes
// with the id of the entry that replaced it, and it can still be opened.
func TestReplacedEntryLeftOutOfReads(t *testing.T) {
	f := newEntriesFixture(t)
	first := mustCreateEntry(t, f.s, f.learning(f.onEpic(), "Tests need a GOCACHE of their own."))
	other := mustCreateEntry(t, f.s, f.learning(f.onEpic(), "Ports 3011 and up."))
	second := f.learning(f.onEpic(), "Tests share the GOCACHE; never clean it.")
	second.Replaces = " " + first.ID + " "
	replacement := mustCreateEntry(t, f.s, second)
	if replacement.Replaces != first.ID {
		t.Fatalf("replacement.Replaces = %q, want %s", replacement.Replaces, first.ID)
	}

	page := listCurrent(t, f.s, f.onEpic())
	if want := []string{replacement.ID, other.ID}; fmt.Sprint(ids(page.Entries)) != fmt.Sprint(want) || page.Total != 2 {
		t.Fatalf("current entries = %v (total %d), want %v", ids(page.Entries), page.Total, want)
	}
	all, err := f.s.ListEntries(f.onEpic(), models.EntryFilter{IncludeReplaced: true}, "", EntryMaxLimit)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{replacement.ID, other.ID, first.ID}; fmt.Sprint(ids(all.Entries)) != fmt.Sprint(want) || all.Total != 3 {
		t.Fatalf("all entries = %v (total %d), want %v", ids(all.Entries), all.Total, want)
	}
	if all.Entries[2].ReplacedBy != replacement.ID || all.Entries[0].ReplacedBy != "" {
		t.Fatalf("replacedBy = %q and %q, want %s on the replaced entry only", all.Entries[2].ReplacedBy, all.Entries[0].ReplacedBy, replacement.ID)
	}
	opened, err := f.s.GetEntry(first.ID)
	if err != nil || opened == nil || opened.Text != first.Text || opened.ReplacedBy != replacement.ID {
		t.Fatalf("GetEntry of the replaced entry = %+v, %v", opened, err)
	}
	// A chain: replacing the replacement leaves only the newest current.
	third := f.learning(f.onEpic(), "Each agent gets its own GOCACHE under its scratch directory.")
	third.Replaces = replacement.ID
	newest := mustCreateEntry(t, f.s, third)
	if page := listCurrent(t, f.s, f.onEpic()); fmt.Sprint(ids(page.Entries)) != fmt.Sprint([]string{newest.ID, other.ID}) {
		t.Fatalf("current entries after a second replace = %v", ids(page.Entries))
	}
}

// An entry replaces only an existing, not yet replaced entry of its own
// type on its own owner.
func TestReplaceRules(t *testing.T) {
	f := newEntriesFixture(t)
	onEpic := mustCreateEntry(t, f.s, f.learning(f.onEpic(), "on the epic"))
	proof := mustCreateEntry(t, f.s, models.CreateEntryRequest{EntryOwner: f.onTicket(), Type: models.EntryProof,
		Text: "go test ./... passed", AgentID: f.agent})
	replaced := mustCreateEntry(t, f.s, f.learning(f.onTicket(), "old"))
	r := f.learning(f.onTicket(), "new")
	r.Replaces = replaced.ID
	replacement := mustCreateEntry(t, f.s, r)

	cases := []struct{ name, replaces, want string }{
		{"unknown", "nope", `replaces "nope" is not an entry`},
		{"another owner", onEpic.ID, "is an entry on another epic: it must be on this ticket"},
		{"another type", proof.ID, "is a proof entry: an entry replaces one of its own type (learning)"},
		{"already replaced", replaced.ID, fmt.Sprintf("already replaced by %q", replacement.ID)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := f.learning(f.onTicket(), "again")
			r.Replaces = c.replaces
			_, err := f.s.CreateEntry(r)
			wantInvalidContaining(t, err, c.want)
		})
	}
	mustFail(t, f.s, "a second replacement of one entry",
		`INSERT INTO entries (id, ticket_id, type, text, agent_id, replaces_id, created_at) VALUES ('x', ?, 'learning', 't', ?, ?, ?)`,
		f.ticket.ID, f.agent, replaced.ID, stamp(time.Now()))
}

// A decision records its source: an agent's own, or the person's (a
// correction) even when an agent writes it; one the person writes is the
// person's. No other type has a source.
func TestDecisionsRecordTheirSource(t *testing.T) {
	f := newEntriesFixture(t)
	decision := func(source, agent, person string) models.CreateEntryRequest {
		return models.CreateEntryRequest{EntryOwner: f.onEpic(), Type: models.EntryDecision,
			Text: "Types are a list in models, not a CHECK.", Source: source, AgentID: agent, AuthorName: person}
	}
	for _, c := range []struct{ source, agent, person string }{
		{models.DecisionSourceAgent, f.agent, ""},
		{models.DecisionSourcePerson, f.agent, ""},
		{models.DecisionSourcePerson, "", "Bilal"},
	} {
		e := mustCreateEntry(t, f.s, decision(c.source, c.agent, c.person))
		got, err := f.s.GetEntry(e.ID)
		if err != nil || got.Source != c.source || got.AgentID != c.agent || got.AuthorName != c.person {
			t.Fatalf("decision read back as %+v, %v; want source %s", got, err, c.source)
		}
	}
	_, err := f.s.CreateEntry(decision("", f.agent, ""))
	wantInvalidContaining(t, err, "a decision's source is required: agent or person")
	_, err = f.s.CreateEntry(decision("bilal", f.agent, ""))
	wantInvalidContaining(t, err, "a decision's source is required")
	_, err = f.s.CreateEntry(decision(models.DecisionSourceAgent, "", "Bilal"))
	wantInvalidContaining(t, err, "a decision the person writes has the person as its source")
	learning := f.learning(f.onEpic(), "text")
	learning.Source = models.DecisionSourceAgent
	_, err = f.s.CreateEntry(learning)
	wantInvalidContaining(t, err, "source is for a decision only, not a learning")
}

// A note is the person's; it is open until an agent marks it handled, which
// records the agent and when, once. A note can point at an entry on its own
// owner, which is how the person challenges that entry.
func TestNotesOpenUntilHandled(t *testing.T) {
	f := newEntriesFixture(t)
	decision := mustCreateEntry(t, f.s, models.CreateEntryRequest{EntryOwner: f.onTicket(), Type: models.EntryDecision,
		Text: "Findings as JSON.", Source: models.DecisionSourceAgent, AgentID: f.agent})
	note := mustCreateEntry(t, f.s, models.CreateEntryRequest{EntryOwner: f.onTicket(), Type: models.EntryNote,
		Text: "Why JSON and not columns?", AuthorName: "Bilal", About: decision.ID})
	if !note.Open() || note.About != decision.ID || note.HandledBy != "" {
		t.Fatalf("new note = %+v, want open and about the decision", note)
	}

	before := time.Now().UTC()
	handled, err := f.s.MarkNoteHandled(note.ID, f.agent)
	if err != nil {
		t.Fatal(err)
	}
	if handled.Open() || handled.HandledBy != f.agent || handled.HandledAt == nil || handled.HandledAt.Before(before) ||
		handled.Text != note.Text || handled.About != decision.ID {
		t.Fatalf("handled note = %+v", handled)
	}
	_, err = f.s.MarkNoteHandled(note.ID, f.agent)
	wantInvalidContaining(t, err, "already handled")
	_, err = f.s.MarkNoteHandled(decision.ID, f.agent)
	wantInvalidContaining(t, err, "only a note is handled")
	_, err = f.s.MarkNoteHandled("nope", f.agent)
	wantInvalidContaining(t, err, "entry not found")
	other := mustCreateEntry(t, f.s, models.CreateEntryRequest{EntryOwner: f.onProject(), Type: models.EntryNote,
		Text: "Keep the skill lean.", AuthorName: "Bilal"})
	_, err = f.s.MarkNoteHandled(other.ID, "ghost")
	wantInvalidContaining(t, err, `agentId "ghost" is not an agent`)
	_, err = f.s.MarkNoteHandled(other.ID, " ")
	wantInvalidContaining(t, err, "agentId is required")
	if got, _ := f.s.GetEntry(other.ID); !got.Open() {
		t.Fatalf("note after refused marks = %+v, want it still open", got)
	}

	// Below the store: a handled note stays handled, and handled_by and
	// handled_at go together.
	mustFail(t, f.s, "reopening a handled note", `UPDATE entries SET handled_by = NULL, handled_at = NULL WHERE id = ?`, note.ID)
	mustFail(t, f.s, "handled_by without handled_at", `UPDATE entries SET handled_by = ? WHERE id = ?`, f.agent, other.ID)
	mustFail(t, f.s, "deleting an agent that handled a note", `DELETE FROM agents WHERE id = ?`, f.agent)

	cases := []struct {
		name string
		req  models.CreateEntryRequest
		want string
	}{
		{"note by an agent", models.CreateEntryRequest{EntryOwner: f.onTicket(), Type: models.EntryNote, Text: "t", AgentID: f.agent},
			"a note is the person's"},
		{"about on another owner", models.CreateEntryRequest{EntryOwner: f.onEpic(), Type: models.EntryNote, Text: "t",
			AuthorName: "Bilal", About: decision.ID}, "is an entry on another ticket: it must be on this epic"},
		{"about an unknown entry", models.CreateEntryRequest{EntryOwner: f.onTicket(), Type: models.EntryNote, Text: "t",
			AuthorName: "Bilal", About: "nope"}, `about "nope" is not an entry`},
		{"about on a learning", models.CreateEntryRequest{EntryOwner: f.onTicket(), Type: models.EntryLearning, Text: "t",
			AgentID: f.agent, About: decision.ID}, "about is for a note only"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := f.s.CreateEntry(c.req)
			wantInvalidContaining(t, err, c.want)
		})
	}
}

// A note the person revises by replacing it is open once, as the revision:
// the replaced note is not open, cannot be marked handled, and is left out
// of the current notes, so readers never count the revised note twice.
func TestReplacedNoteIsNotOpen(t *testing.T) {
	f := newEntriesFixture(t)
	first := mustCreateEntry(t, f.s, models.CreateEntryRequest{EntryOwner: f.onTicket(), Type: models.EntryNote,
		Text: "Use port 3011.", AuthorName: "Bilal"})
	revised := mustCreateEntry(t, f.s, models.CreateEntryRequest{EntryOwner: f.onTicket(), Type: models.EntryNote,
		Text: "Use port 3012; 3011 is taken.", AuthorName: "Bilal", Replaces: first.ID})

	replaced, err := f.s.GetEntry(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if replaced.Open() || replaced.ReplacedBy != revised.ID {
		t.Fatalf("replaced note = %+v, want not open and replaced by %s", replaced, revised.ID)
	}
	if !revised.Open() {
		t.Fatalf("revised note = %+v, want open", revised)
	}
	countOpen := func(entries []models.Entry) int {
		n := 0
		for _, e := range entries {
			if e.Open() {
				n++
			}
		}
		return n
	}
	notes, err := f.s.ListEntries(f.onTicket(), models.EntryFilter{Types: []string{models.EntryNote}}, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(ids(notes.Entries)) != fmt.Sprint([]string{revised.ID}) || notes.Total != 1 || countOpen(notes.Entries) != 1 {
		t.Fatalf("current notes = %v (total %d), want only the revision, open", ids(notes.Entries), notes.Total)
	}
	all, err := f.s.ListEntries(f.onTicket(), models.EntryFilter{Types: []string{models.EntryNote}, IncludeReplaced: true}, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if all.Total != 2 || countOpen(all.Entries) != 1 {
		t.Fatalf("all notes: total %d, %d open; want 2 and 1", all.Total, countOpen(all.Entries))
	}

	_, err = f.s.MarkNoteHandled(first.ID, f.agent)
	wantInvalidContaining(t, err, fmt.Sprintf("is replaced by %q, so it is not open", revised.ID))
	if got, _ := f.s.GetEntry(first.ID); got.HandledAt != nil {
		t.Fatalf("replaced note after a refused mark = %+v, want it unhandled", got)
	}
	handled, err := f.s.MarkNoteHandled(revised.ID, f.agent)
	if err != nil || handled.Open() || handled.HandledBy != f.agent {
		t.Fatalf("handling the revision = %+v, %v", handled, err)
	}
}

// A review entry carries its verdict, its finding counts with every severity
// counted, its one-line summary as its text, and the name of the ticket's
// document holding the full report.
func TestReviewEntriesCarryVerdictFindingsAndReport(t *testing.T) {
	f := newEntriesFixture(t)
	if _, err := f.s.CreateDocument(models.CreateDocumentRequest{TicketID: f.ticket.ID, Name: "Review 1", Content: "# Review"}); err != nil {
		t.Fatal(err)
	}
	review := func(edit func(r *models.CreateEntryRequest)) models.CreateEntryRequest {
		r := models.CreateEntryRequest{EntryOwner: f.onTicket(), Type: models.EntryReview, AgentID: f.agent,
			Text: "Two majors in the migration.", Verdict: models.ReviewChanges,
			Findings: map[string]int{models.SeverityMajor: 2, models.SeverityNit: 1}, ReportDocument: "review 1.md"}
		if edit != nil {
			edit(&r)
		}
		return r
	}
	e := mustCreateEntry(t, f.s, review(nil))
	got, err := f.s.GetEntry(e.ID)
	if err != nil {
		t.Fatal(err)
	}
	wantFindings := map[string]int{"blocker": 0, "major": 2, "minor": 0, "nit": 1}
	if got.Verdict != models.ReviewChanges || !reflect.DeepEqual(got.Findings, wantFindings) ||
		got.Text != "Two majors in the migration." || got.ReportDocument != "Review 1" {
		t.Fatalf("review read back as %+v, want verdict changes, findings %v, report Review 1", got, wantFindings)
	}
	approve := mustCreateEntry(t, f.s, review(func(r *models.CreateEntryRequest) {
		r.Verdict, r.Findings, r.ReportDocument, r.Text = models.ReviewApprove, nil, "", "Clean."
	}))
	if !reflect.DeepEqual(approve.Findings, map[string]int{"blocker": 0, "major": 0, "minor": 0, "nit": 0}) || approve.ReportDocument != "" {
		t.Fatalf("approve with no findings = %+v", approve)
	}

	cases := []struct {
		name string
		edit func(r *models.CreateEntryRequest)
		want string
	}{
		{"no verdict", func(r *models.CreateEntryRequest) { r.Verdict = "" }, "a review's verdict is required: approve or changes"},
		{"unknown verdict", func(r *models.CreateEntryRequest) { r.Verdict = "APPROVE" }, "a review's verdict is required"},
		{"unknown severity", func(r *models.CreateEntryRequest) { r.Findings = map[string]int{"critical": 1} }, `"critical" is not one of blocker, major, minor, nit`},
		{"negative count", func(r *models.CreateEntryRequest) { r.Findings = map[string]int{"minor": -1} }, "never negative"},
		{"summary over lines", func(r *models.CreateEntryRequest) { r.Text = "Two majors.\nFirst: ..." }, "one-line summary"},
		{"unknown report", func(r *models.CreateEntryRequest) { r.ReportDocument = "Review 2" }, `no document called "Review 2"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := f.s.CreateEntry(review(c.edit))
			wantInvalidContaining(t, err, c.want)
		})
	}
	for _, edit := range []func(r *models.CreateEntryRequest){
		func(r *models.CreateEntryRequest) { r.Verdict = models.ReviewApprove },
		func(r *models.CreateEntryRequest) { r.Findings = map[string]int{} },
		func(r *models.CreateEntryRequest) { r.ReportDocument = "Review 1" },
	} {
		r := f.learning(f.onTicket(), "t")
		edit(&r)
		_, err := f.s.CreateEntry(r)
		wantInvalidContaining(t, err, "are for a review only, not a learning")
	}
}

// Reads page newest first through nextBefore, and narrow by type; a bad
// limit, type or before is the caller's mistake.
func TestListEntriesPagesAndFilters(t *testing.T) {
	f := newEntriesFixture(t)
	var written []string
	for i := 0; i < 5; i++ {
		written = append(written, mustCreateEntry(t, f.s, f.learning(f.onTicket(), fmt.Sprintf("l%d", i))).ID)
	}
	proof := mustCreateEntry(t, f.s, models.CreateEntryRequest{EntryOwner: f.onTicket(), Type: models.EntryProof, Text: "p", AgentID: f.agent})
	elsewhere := mustCreateEntry(t, f.s, f.learning(f.onEpic(), "elsewhere"))

	var got []string
	before := ""
	for {
		page, err := f.s.ListEntries(f.onTicket(), models.EntryFilter{}, before, 4)
		if err != nil {
			t.Fatal(err)
		}
		if page.Total != 6 {
			t.Fatalf("total %d, want 6", page.Total)
		}
		got = append(got, ids(page.Entries)...)
		if !page.HasMore {
			break
		}
		before = page.NextBefore
	}
	want := append([]string{proof.ID}, written[4], written[3], written[2], written[1], written[0])
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("paged = %v, want %v", got, want)
	}

	proofs, err := f.s.ListEntries(f.onTicket(), models.EntryFilter{Types: []string{models.EntryProof, models.EntryProof}}, "", 10)
	if err != nil || fmt.Sprint(ids(proofs.Entries)) != fmt.Sprint([]string{proof.ID}) || proofs.Total != 1 {
		t.Fatalf("proofs = %v (total %d), %v", ids(proofs.Entries), proofs.Total, err)
	}

	_, err = f.s.ListEntries(f.onTicket(), models.EntryFilter{}, "", 0)
	wantInvalidContaining(t, err, "limit must be between 1 and 100")
	_, err = f.s.ListEntries(f.onTicket(), models.EntryFilter{Types: []string{"comment"}}, "", 10)
	wantInvalidContaining(t, err, `type "comment" is not an entry type`)
	_, err = f.s.ListEntries(f.onTicket(), models.EntryFilter{}, elsewhere.ID, 10)
	wantInvalidContaining(t, err, "is not an entry on this ticket")
	_, err = f.s.ListEntries(models.EntryOwner{}, models.EntryFilter{}, "", 10)
	wantInvalidContaining(t, err, "exactly one project, epic or ticket")
	empty := listCurrent(t, f.s, f.onProject())
	if empty.Entries == nil || empty.Total != 0 || empty.HasMore {
		t.Fatalf("no entries = %+v, want an empty, non-nil page", empty)
	}
}

// A deleted project's entries, and its epics' and tickets', are gone from
// reads and writes but stay in the file; deleting an epic or a ticket takes
// its entries with it, replacements and notes pointing at them included.
func TestEntriesFollowTheirOwner(t *testing.T) {
	f := newEntriesFixture(t)
	old := mustCreateEntry(t, f.s, f.learning(f.onTicket(), "old"))
	r := f.learning(f.onTicket(), "new")
	r.Replaces = old.ID
	mustCreateEntry(t, f.s, r)
	mustCreateEntry(t, f.s, models.CreateEntryRequest{EntryOwner: f.onTicket(), Type: models.EntryNote, Text: "why?",
		AuthorName: "Bilal", About: old.ID})
	onEpic := mustCreateEntry(t, f.s, f.learning(f.onEpic(), "on the epic"))
	if err := f.s.DeleteTicket(f.ticket.ID); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, f.s, `SELECT COUNT(*) FROM entries WHERE ticket_id = ?`, f.ticket.ID); n != 0 {
		t.Fatalf("%d entries left after deleting their ticket", n)
	}
	if _, err := f.s.DeleteEpic(f.epic.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.s.GetEntry(onEpic.ID); got != nil {
		t.Fatalf("entry of a deleted epic = %+v", got)
	}

	project := mustCreateEntry(t, f.s, f.learning(f.onProject(), "on the project"))
	ticket := seedTicket(t, f.s, f.project.ID, "Another")
	onTicket := mustCreateEntry(t, f.s, f.learning(models.EntryOwner{TicketID: ticket.ID}, "on the ticket"))
	if err := f.s.DeleteProject(f.project.ID); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{project.ID, onTicket.ID} {
		if got, err := f.s.GetEntry(id); got != nil || err != nil {
			t.Fatalf("GetEntry in a deleted project = %+v, %v; want nil", got, err)
		}
	}
	_, err := f.s.ListEntries(f.onProject(), models.EntryFilter{}, "", 10)
	wantInvalidContaining(t, err, "project not found")
	_, err = f.s.CreateEntry(f.learning(models.EntryOwner{TicketID: ticket.ID}, "more"))
	wantInvalidContaining(t, err, "no ticket matches")
	if n := countRows(t, f.s, `SELECT COUNT(*) FROM entries`); n != 2 {
		t.Fatalf("%d entries in the file after deleting their project, want 2 (deleting archives)", n)
	}
	if err := f.s.ClearData(); err != nil {
		t.Fatalf("ClearData with entries: %v", err)
	}
}

// Writing an entry and marking a note handled are commits the change
// watcher sees, so live views refresh on them as on any other change.
func TestEntryWritesReachTheWatcher(t *testing.T) {
	s, _, w := openWatched(t)
	ctx := context.Background()
	p := seedProject(t, s, "Agent Control Plane", "ACP")
	if err := insertSession(s, "s1", "claude_code", "3da2c294"); err != nil {
		t.Fatal(err)
	}
	if err := insertAgent(s, "a1", "s1", "implementer", models.ProviderAnthropic); err != nil {
		t.Fatal(err)
	}
	v0, err := w.dataVersion(ctx)
	if err != nil {
		t.Fatal(err)
	}
	note := mustCreateEntry(t, s, models.CreateEntryRequest{EntryOwner: models.EntryOwner{ProjectID: p.ID},
		Type: models.EntryNote, Text: "Keep it lean.", AuthorName: "Bilal"})
	v1, err := w.dataVersion(ctx)
	if err != nil || v1 == v0 {
		t.Fatalf("data_version after writing an entry = %d (was %d), %v; want it moved", v1, v0, err)
	}
	if _, err := s.MarkNoteHandled(note.ID, "a1"); err != nil {
		t.Fatal(err)
	}
	if v2, err := w.dataVersion(ctx); err != nil || v2 == v1 {
		t.Fatalf("data_version after marking a note handled = %d (was %d), %v; want it moved", v2, v1, err)
	}
}

const entriesMigration = "019_entries.sql"

func assertEntriesSchema(t *testing.T, database *sql.DB) {
	t.Helper()
	for _, obj := range []struct {
		kind, name string
		want       int
	}{
		{"table", "entries", 1},
		{"index", "idx_entries_project", 1},
		{"index", "idx_entries_epic", 1},
		{"index", "idx_entries_ticket", 1},
		{"index", "idx_entries_replaces", 1},
		{"trigger", "entries_never_edited", 1},
		{"table", "project_journal_entries", 0},
	} {
		var n int
		if err := database.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = ? AND name = ?`, obj.kind, obj.name).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != obj.want {
			t.Fatalf("%s %s: %d after migrating, want %d", obj.kind, obj.name, n, obj.want)
		}
	}
}

// Migrating a database with journal entries moves each one into its
// project's entries as a decision of the person's, keeping its id, text,
// author and time and the journal's order, a deleted project's too, and
// drops the journal's table; a blank author, which only raw SQL could have
// written, becomes "unknown" rather than failing the migration. The journal
// reads the same before and after.
func TestMigrationMovesJournalIntoProjectEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacy := openLegacyDB(t, path, entriesMigration)
	execOrFail(t, legacy, `INSERT INTO projects (id, name, prefix) VALUES ('p1', 'Agent Control Plane', 'ACP')`)
	execOrFail(t, legacy, `INSERT INTO projects (id, name, prefix, status) VALUES ('p2', 'Gone', 'GONE', 'archived')`)
	// j2 and j3 share an instant: rowid keeps j3 the later one.
	execOrFail(t, legacy, `INSERT INTO project_journal_entries (id, project_id, author, text, created_at) VALUES
		('j1', 'p1', 'orchestrator', 'Decision: rewrite the North Star.', '2026-09-27T11:51:14.039858000Z'),
		('j2', 'p1', 'Bilal', 'Correction: ordered by the vision.
Second line.', '2026-09-27T11:51:14.047291000Z'),
		('j3', 'p1', 'reviewer', 'Tied with j2, written after it.', '2026-09-27T11:51:14.047291000Z'),
		('j4', 'p2', 'orchestrator', 'In a deleted project.', '2026-09-27T12:00:00.000000000Z'),
		('j5', 'p2', '  ', 'Blank author, written behind the store.', '2026-09-27T12:00:01.000000000Z')`)
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	database, err := OpenAt(path)
	if err != nil {
		t.Fatalf("migrating: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	assertEntriesSchema(t, database)

	rows, err := database.Query(`SELECT id, project_id, type, source, author_name, COALESCE(agent_id, ''), text, created_at || ''
		FROM entries ORDER BY rowid`)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for rows.Next() {
		var id, project, typ, source, author, agent, text, at string
		if err := rows.Scan(&id, &project, &typ, &source, &author, &agent, &text, &at); err != nil {
			t.Fatal(err)
		}
		got = append(got, strings.Join([]string{id, project, typ, source, author, agent, text, at}, "|"))
	}
	rows.Close()
	want := []string{
		"j1|p1|decision|person|orchestrator||Decision: rewrite the North Star.|2026-09-27T11:51:14.039858000Z",
		"j2|p1|decision|person|Bilal||Correction: ordered by the vision.\nSecond line.|2026-09-27T11:51:14.047291000Z",
		"j3|p1|decision|person|reviewer||Tied with j2, written after it.|2026-09-27T11:51:14.047291000Z",
		"j4|p2|decision|person|orchestrator||In a deleted project.|2026-09-27T12:00:00.000000000Z",
		"j5|p2|decision|person|unknown||Blank author, written behind the store.|2026-09-27T12:00:01.000000000Z",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("entries after migrating:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	s := NewStore(database)
	page, err := s.ListJournal("ACP", "", JournalDefaultLimit)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(entryTexts(page.Entries)) != fmt.Sprint([]string{"Tied with j2, written after it.",
		"Correction: ordered by the vision.\nSecond line.", "Decision: rewrite the North Star."}) || page.Total != 3 {
		t.Fatalf("journal after migrating = %q (total %d)", entryTexts(page.Entries), page.Total)
	}
	if a := page.Entries[1]; a.ID != "j2" || a.Author != "Bilal" || a.CreatedAt.Format(time.RFC3339Nano) != "2026-09-27T11:51:14.047291Z" {
		t.Fatalf("migrated journal entry = %+v", a)
	}
	// The migrated file takes new entries, and still refuses edits.
	appendEntry(t, s, "ACP", "Bilal", "After the move.")
	if _, err := database.Exec(`UPDATE entries SET text = 'x' WHERE id = 'j1'`); err == nil {
		t.Fatal("editing a migrated entry was accepted")
	}
	var fkErrors int
	if err := database.QueryRow(`SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&fkErrors); err != nil || fkErrors != 0 {
		t.Fatalf("foreign key check after migrating: %d violations, %v", fkErrors, err)
	}
}

// A journal entry is a project entry: an agent's current project entries
// show in the journal under its role, and a replaced one is left out.
func TestJournalReadsCurrentProjectEntries(t *testing.T) {
	f := newEntriesFixture(t)
	old := appendEntry(t, f.s, "ACP", "Bilal", "Ports from 3011.")
	r := models.CreateEntryRequest{EntryOwner: f.onProject(), Type: models.EntryDecision, Text: "Ports from 3012.",
		AgentID: f.agent, Source: models.DecisionSourcePerson, Replaces: old.ID}
	mustCreateEntry(t, f.s, r)
	mustCreateEntry(t, f.s, f.learning(f.onEpic(), "not the project's"))
	page, err := f.s.ListJournal("ACP", "", JournalDefaultLimit)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 1 || page.Total != 1 || page.Entries[0].Text != "Ports from 3012." || page.Entries[0].Author != "implementer" {
		t.Fatalf("journal = %+v, want only the replacement, by the agent's role", page)
	}
	stored, err := f.s.GetEntry(old.ID)
	if err != nil || stored.Type != models.EntryDecision || stored.Source != models.DecisionSourcePerson || stored.AuthorName != "Bilal" {
		t.Fatalf("appended journal entry stored as %+v, %v; want the person's decision", stored, err)
	}
}
