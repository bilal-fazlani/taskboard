package mcp

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tcarac/taskboard/internal/db"
	"github.com/tcarac/taskboard/internal/imagedoc/imagedoctest"
	"github.com/tcarac/taskboard/internal/models"
)

// entriesServer is an MCP server over a throwaway database holding a project
// (ACP) with an epic (Write-back) and a ticket (ACP-1) in it, and an agent in
// a session to write entries. Nothing creates agents over MCP yet, so the
// agent is inserted directly.
type entriesServer struct {
	s       *MCPServer
	project *models.Project
	epic    *models.Epic
	ticket  *models.Ticket
	agent   string
}

func newEntriesServer(t *testing.T) entriesServer {
	t.Helper()
	database, err := db.OpenAt(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("opening test database: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	for _, q := range []string{
		`INSERT INTO sessions (id, vendor, vendor_session_id, machine, resume_command, created_at)
			VALUES ('s1', 'claude_code', '3da2c294', 'mac', 'claude --resume 3da2c294', CURRENT_TIMESTAMP)`,
		`INSERT INTO agents (id, session_id, role, model, provider, created_at, last_seen_at)
			VALUES ('a1', 's1', 'implementer', 'opus', 'anthropic', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
	} {
		if _, err := database.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	f := entriesServer{s: NewServer(db.NewStore(database)), agent: "a1"}
	if f.project, err = f.s.store.CreateProject(models.CreateProjectRequest{Name: "Agent Control Plane", Prefix: "ACP"}); err != nil {
		t.Fatal(err)
	}
	if f.epic, err = f.s.store.CreateEpic(models.CreateEpicRequest{ProjectID: f.project.ID, Name: "Write-back"}); err != nil {
		t.Fatal(err)
	}
	if f.ticket, err = f.s.store.CreateTicket(models.CreateTicketRequest{ProjectID: f.project.ID, Title: "Entries", Epic: &f.epic.ID}); err != nil {
		t.Fatal(err)
	}
	return f
}

// note leaves one of the person's notes on owner.
func (f entriesServer) note(t *testing.T, owner models.EntryOwner, text string) *models.Entry {
	t.Helper()
	e, err := f.s.store.CreateEntry(models.CreateEntryRequest{EntryOwner: owner, Type: models.EntryNote, Text: text, AuthorName: "Bilal"})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// callError runs a tool the way an MCP client does and returns the error it
// answers with, failing the test when the call succeeds.
func callError(t *testing.T, s *MCPServer, tool string, args map[string]any) string {
	t.Helper()
	text, isError := callToolText(t, s, tool, args)
	if !isError {
		t.Fatalf("%s(%v) succeeded with %s, want an error", tool, args, text)
	}
	return text
}

// listEntries reads one owner's entries through list_entries.
func listEntries(t *testing.T, s *MCPServer, args map[string]any) models.EntryPage {
	t.Helper()
	text, isError := callToolText(t, s, "list_entries", args)
	if isError {
		t.Fatalf("list_entries(%v): %s", args, text)
	}
	var page models.EntryPage
	if err := json.Unmarshal([]byte(text), &page); err != nil {
		t.Fatalf("list_entries returned %q: %v", text, err)
	}
	return page
}

// An agent writes an entry on a ticket, an epic (by id, or by name with its
// project) and a project, and reads it back there; the answer is a short
// confirmation.
func TestWriteEntryAtEveryLevel(t *testing.T) {
	f := newEntriesServer(t)
	for _, owner := range []map[string]any{
		{"ticket": "acp-1"},
		{"epic": f.epic.ID},
		{"epic": "write-back", "project": "ACP"},
		{"project": "acp"},
	} {
		args := map[string]any{"type": "decision", "source": "agent", "text": "Findings as JSON.", "agentId": f.agent}
		for k, v := range owner {
			args[k] = v
		}
		got := callJSON(t, f.s, "write_entry", args)
		wantKeys(t, "write_entry", got, "id", "type", "created")
		if got["type"] != "decision" || got["created"] != true {
			t.Fatalf("write_entry answer = %v", got)
		}
		page := listEntries(t, f.s, owner)
		if len(page.Entries) == 0 || page.Entries[0].ID != got["id"] || page.Entries[0].AgentID != f.agent ||
			page.Entries[0].Source != models.DecisionSourceAgent {
			t.Fatalf("entries on %v = %+v, want the new decision first", owner, page.Entries)
		}
	}

	proof := callJSON(t, f.s, "write_entry", map[string]any{"ticket": "ACP-1", "type": "proof",
		"text": "go test ./... passed.", "agentId": f.agent})
	if proof["type"] != "proof" {
		t.Fatalf("proof answer = %v", proof)
	}
}

// write_entry refuses what an agent may not write, and what names no one
// owner, saying why.
func TestWriteEntryRefusals(t *testing.T) {
	f := newEntriesServer(t)
	for _, c := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"ticket": "ACP-1", "type": "note", "text": "t", "agentId": f.agent}, "a note is the person's"},
		{map[string]any{"ticket": "ACP-1", "type": "learning", "text": "t"}, "agentId is required"},
		{map[string]any{"ticket": "ACP-1", "type": "learning", "text": "t", "agentId": "ghost"}, `agentId "ghost" is not an agent`},
		{map[string]any{"type": "learning", "text": "t", "agentId": f.agent}, "pass one of ticket, epic"},
		{map[string]any{"ticket": "ACP-1", "project": "ACP", "type": "learning", "text": "t", "agentId": f.agent}, "pass one of ticket, epic"},
		{map[string]any{"ticket": "ACP-1", "epic": f.epic.ID, "type": "learning", "text": "t", "agentId": f.agent}, "pass one of ticket, epic"},
		{map[string]any{"epic": f.epic.ID, "type": "hand_off", "text": "t", "agentId": f.agent}, "goes on a ticket only"},
		{map[string]any{"project": "ACP", "type": "decision", "text": "t", "agentId": f.agent}, "a decision's source is required"},
		{map[string]any{"ticket": "ACP-9", "type": "learning", "text": "t", "agentId": f.agent}, "ACP-9"},
	} {
		if got := callError(t, f.s, "write_entry", c.args); !strings.Contains(got, c.want) {
			t.Errorf("write_entry(%v) = %s, want it to say %q", c.args, got, c.want)
		}
	}
	if page := listEntries(t, f.s, map[string]any{"ticket": "ACP-1"}); page.Total != 0 {
		t.Fatalf("entries after refused writes = %+v, want none", page.Entries)
	}
}

// An entry replaced by a later one is left out of reads unless asked for,
// and then names its replacement.
func TestWriteEntryReplaces(t *testing.T) {
	f := newEntriesServer(t)
	old := callJSON(t, f.s, "write_entry", map[string]any{"epic": f.epic.ID, "type": "learning",
		"text": "Port 3011 is free.", "agentId": f.agent})
	current := callJSON(t, f.s, "write_entry", map[string]any{"epic": f.epic.ID, "type": "learning",
		"text": "Port 3011 is taken by the dev server; use 3012.", "agentId": f.agent, "replaces": old["id"]})

	page := listEntries(t, f.s, map[string]any{"epic": f.epic.ID})
	if len(page.Entries) != 1 || page.Entries[0].ID != current["id"] || page.Entries[0].Replaces != old["id"] {
		t.Fatalf("current entries = %+v, want only the replacement", page.Entries)
	}
	all := listEntries(t, f.s, map[string]any{"epic": f.epic.ID, "includeReplaced": true})
	if len(all.Entries) != 2 || all.Entries[1].ID != old["id"] || all.Entries[1].ReplacedBy != current["id"] {
		t.Fatalf("all entries = %+v, want the replaced one too, naming its replacement", all.Entries)
	}
	if got := callError(t, f.s, "write_entry", map[string]any{"epic": f.epic.ID, "type": "learning",
		"text": "Again.", "agentId": f.agent, "replaces": old["id"]}); !strings.Contains(got, "already replaced") {
		t.Fatalf("replacing a replaced entry = %s", got)
	}
}

// list_entries reads newest first, a page at a time, filtered by type, and
// leaves each entry's owner out since the caller named it.
func TestListEntriesPagesNewestFirst(t *testing.T) {
	f := newEntriesServer(t)
	var written []string
	for _, text := range []string{"One.", "Two.", "Three."} {
		written = append(written, callJSON(t, f.s, "write_entry", map[string]any{"project": "ACP", "type": "learning",
			"text": text, "agentId": f.agent})["id"].(string))
	}
	f.note(t, models.EntryOwner{ProjectID: f.project.ID}, "Keep it lean.")

	first := listEntries(t, f.s, map[string]any{"project": "ACP", "types": []string{"learning"}, "limit": 2})
	if len(first.Entries) != 2 || first.Entries[0].ID != written[2] || first.Entries[1].ID != written[1] ||
		!first.HasMore || first.NextBefore != written[1] || first.Total != 3 {
		t.Fatalf("first page = %+v", first)
	}
	second := listEntries(t, f.s, map[string]any{"project": "ACP", "types": []string{"learning"}, "limit": 2, "before": first.NextBefore})
	if len(second.Entries) != 1 || second.Entries[0].ID != written[0] || second.HasMore {
		t.Fatalf("second page = %+v", second)
	}
	text, _ := callToolText(t, f.s, "list_entries", map[string]any{"project": "ACP"})
	if strings.Contains(text, `"projectId"`) {
		t.Fatalf("list_entries answer carries the owner: %s", text)
	}
	if got := callError(t, f.s, "list_entries", map[string]any{"project": "ACP", "types": []string{"gossip"}}); !strings.Contains(got, "not an entry type") {
		t.Fatalf("unknown type = %s", got)
	}
}

// get_ticket carries the ticket's current entries, its open notes counted,
// and its epic's and project's open notes counted but not read.
func TestGetTicketCarriesEntriesAndOpenNoteCounts(t *testing.T) {
	f := newEntriesServer(t)
	// With nothing to carry, get_ticket carries none of it.
	fresh := callJSON(t, f.s, "get_ticket", map[string]any{"id": "ACP-1"})
	for _, key := range []string{"entries", "openNotes", "epicOpenNotes", "projectOpenNotes"} {
		if v, ok := fresh[key]; ok {
			t.Fatalf("get_ticket on a ticket with no entries carries %s: %v", key, v)
		}
	}
	learning := callJSON(t, f.s, "write_entry", map[string]any{"ticket": "ACP-1", "type": "learning",
		"text": "The shell opens with foreign keys off.", "agentId": f.agent})
	note := f.note(t, models.EntryOwner{TicketID: f.ticket.ID}, "Check the migration on a copy.")
	f.note(t, models.EntryOwner{EpicID: f.epic.ID}, "Epic note.")
	f.note(t, models.EntryOwner{ProjectID: f.project.ID}, "Project note.")
	f.note(t, models.EntryOwner{ProjectID: f.project.ID}, "Another project note.")

	text, isError := callToolText(t, f.s, "get_ticket", map[string]any{"id": "ACP-1"})
	if isError {
		t.Fatal(text)
	}
	var got struct {
		Key              string           `json:"id"`
		Entries          models.EntryPage `json:"entries"`
		OpenNotes        int              `json:"openNotes"`
		EpicOpenNotes    *int             `json:"epicOpenNotes"`
		ProjectOpenNotes *int             `json:"projectOpenNotes"`
	}
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatal(err)
	}
	if got.Key != f.ticket.ID || got.Entries.Total != 2 || got.Entries.Entries[0].ID != note.ID ||
		got.Entries.Entries[1].ID != learning["id"] {
		t.Fatalf("get_ticket entries = %+v", got.Entries)
	}
	if got.OpenNotes != 1 || got.EpicOpenNotes == nil || *got.EpicOpenNotes != 1 ||
		got.ProjectOpenNotes == nil || *got.ProjectOpenNotes != 2 {
		t.Fatalf("get_ticket open notes: ticket %d, epic %v, project %v; want 1, 1, 2", got.OpenNotes, got.EpicOpenNotes, got.ProjectOpenNotes)
	}
	var raw struct {
		Entries json.RawMessage `json:"entries"`
	}
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "Epic note.") || strings.Contains(text, "Project note.") || strings.Contains(string(raw.Entries), `"ticketId"`) {
		t.Fatalf("get_ticket carries more than the ticket's own entries, or their owner: %s", text)
	}
}

// handle_note marks a note handled, by the agent, once; the answer is short.
func TestHandleNote(t *testing.T) {
	f := newEntriesServer(t)
	note := f.note(t, models.EntryOwner{TicketID: f.ticket.ID}, "Use port 3012.")
	got := callJSON(t, f.s, "handle_note", map[string]any{"id": note.ID, "agentId": f.agent})
	wantKeys(t, "handle_note", got, "id", "type", "handled")
	if stored, _ := f.s.store.GetEntry(note.ID); stored.Open() || stored.HandledBy != f.agent {
		t.Fatalf("note after handle_note = %+v", stored)
	}
	if text := callError(t, f.s, "handle_note", map[string]any{"id": note.ID, "agentId": f.agent}); !strings.Contains(text, "already handled") {
		t.Fatalf("handling twice = %s", text)
	}
	if text := callError(t, f.s, "handle_note", map[string]any{"agentId": f.agent}); !strings.Contains(text, "id is required") {
		t.Fatalf("handling no note = %s", text)
	}
}

// callAllText runs a tool the way an MCP client does and returns every text
// content item of its answer, failing the test on a tool error.
func callAllText(t *testing.T, s *MCPServer, name string, args map[string]any) []string {
	t.Helper()
	resp := s.handleRequest(jsonrpcRequest{JSONRPC: "2.0", ID: 1, Method: "tools/call",
		Params: mustJSON(t, map[string]any{"name": name, "arguments": args})})
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(mustJSON(t, resp.Result), &result); err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, c := range result.Content {
		if c.Type == "text" {
			texts = append(texts, c.Text)
		}
	}
	if result.IsError {
		t.Fatalf("%s(%v): %v", name, args, texts)
	}
	return texts
}

// openNotesIn reads the openNotes flag out of an answer's text content: a
// field of its JSON object, or an object of its own after it.
func openNotesIn(t *testing.T, texts []string) int {
	t.Helper()
	for _, text := range texts {
		var obj map[string]json.RawMessage
		if json.Unmarshal([]byte(text), &obj) != nil {
			continue
		}
		if raw, ok := obj["openNotes"]; ok {
			var n int
			if err := json.Unmarshal(raw, &n); err != nil {
				t.Fatalf("openNotes = %s: %v", raw, err)
			}
			return n
		}
	}
	return 0
}

// Every call on a ticket that has an open note says so, whatever the call,
// so an agent holding the ticket learns of the note on its next call; a
// call on a ticket with none, or on no ticket, says nothing.
func TestEveryTicketCallFlagsOpenNotes(t *testing.T) {
	f := newEntriesServer(t)
	st := f.s.store
	sub1, _ := st.AddSubtask(f.ticket.ID, models.CreateSubtaskRequest{Title: "One"})
	sub2, _ := st.AddSubtask(f.ticket.ID, models.CreateSubtaskRequest{Title: "Two"})
	plan, _ := st.CreateDocument(models.CreateDocumentRequest{TicketID: f.ticket.ID, Name: "Plan", Content: "x"})
	scratch, _ := st.CreateDocument(models.CreateDocumentRequest{TicketID: f.ticket.ID, Name: "Scratch", Content: "x"})
	shot, err := st.CreateImageDocument(models.CreateImageRequest{TicketID: f.ticket.ID, Name: "Shot", Format: "png", Data: imagedoctest.PNG(4, 4)})
	if err != nil {
		t.Fatal(err)
	}
	f.note(t, models.EntryOwner{TicketID: f.ticket.ID}, "Mind the port.")
	other := f.note(t, models.EntryOwner{TicketID: f.ticket.ID}, "And the other one.")
	quiet, _ := st.CreateTicket(models.CreateTicketRequest{ProjectID: f.project.ID, Title: "No notes"})
	f.note(t, models.EntryOwner{ProjectID: f.project.ID}, "A project note flags no ticket.")

	calls := []struct {
		tool string
		args map[string]any
	}{
		{"get_ticket", map[string]any{"id": "ACP-1"}},
		{"update_ticket", map[string]any{"key": "ACP-1", "priority": "high"}},
		{"move_ticket", map[string]any{"id": f.ticket.ID, "status": "in_progress"}},
		{"create_subtask", map[string]any{"ticketId": "ACP-1", "title": "Three"}},
		{"batch_create_subtasks", map[string]any{"ticketId": "ACP-1", "subtasks": []map[string]any{{"title": "Four"}}}},
		{"toggle_subtask", map[string]any{"id": sub1.ID, "completed": true}},
		{"delete_subtask", map[string]any{"id": sub2.ID}},
		{"list_documents", map[string]any{"ticket": "ACP-1"}},
		{"get_document", map[string]any{"id": plan.ID}},
		{"get_document", map[string]any{"id": "Shot", "ticket": "ACP-1"}},
		{"create_document", map[string]any{"ticket": "ACP-1", "name": "Notes", "content": "x"}},
		{"update_document", map[string]any{"id": plan.ID, "content": "y"}},
		{"delete_document", map[string]any{"id": scratch.ID}},
		{"write_entry", map[string]any{"ticket": "ACP-1", "type": "learning", "text": "t", "agentId": f.agent}},
		{"list_entries", map[string]any{"ticket": "ACP-1"}},
		{"handle_note", map[string]any{"id": other.ID, "agentId": f.agent}},
	}
	_ = shot
	covered := map[string]bool{}
	for _, c := range calls {
		covered[c.tool] = true
		want := 2
		if c.tool == "handle_note" {
			want = 1 // counted after the call: the other note is still open
		}
		if got := openNotesIn(t, callAllText(t, f.s, c.tool, c.args)); got != want {
			t.Errorf("%s(%v) says openNotes %d, want %d", c.tool, c.args, got, want)
		}
	}
	// The agent tools on a ticket, in the order an agent calls them; one note
	// is still open.
	if _, err := st.ClaimTicket(f.ticket.ID, f.agent); err != nil {
		t.Fatal(err)
	}
	flags := func(tool string, args map[string]any) []string {
		covered[tool] = true
		texts := callAllText(t, f.s, tool, args)
		if got := openNotesIn(t, texts); got != 1 {
			t.Errorf("%s(%v) says openNotes %d, want 1", tool, args, got)
		}
		return texts
	}
	var asked struct {
		ID string `json:"id"`
	}
	texts := flags("request_user_input", map[string]any{"ticket": "ACP-1", "agentId": f.agent, "type": "question", "prompt": "Port?"})
	if err := json.Unmarshal([]byte(texts[0]), &asked); err != nil {
		t.Fatal(err)
	}
	flags("await_answer", map[string]any{"request": asked.ID, "agentId": f.agent, "timeoutSeconds": 0})
	if _, err := st.AnswerRequest(asked.ID, "3014", "Bilal"); err != nil {
		t.Fatal(err)
	}
	flags("release_ticket", map[string]any{"ticket": "ACP-1", "agentId": f.agent, "outcome": "give_back", "handOff": "Stopped at the tests."})
	// Every registered tool is either checked by a call above or is on this
	// list of tools that are not about one existing ticket, so a new tool
	// fails here until it is classified: one that takes a ticket, however it
	// names it, needs a call above (and a case in ticketOfCall).
	notOnOneTicket := map[string]bool{
		"list_projects": true, "get_project": true, "create_project": true, "update_project": true, "delete_project": true,
		"append_project_journal": true, "list_project_journal": true,
		"list_epics": true, "create_epic": true, "update_epic": true, "delete_epic": true,
		"list_labels": true, "create_label": true, "update_label": true, "delete_label": true,
		"list_tickets": true, "find_tickets_by_commit": true, "get_board": true, "get_now": true,
		// A new ticket has no notes yet, and a deleted one is gone.
		"create_ticket": true, "delete_ticket": true,
		// Identifying names a session and an agent, not a ticket.
		"identify_agent": true,
	}
	for _, def := range f.s.toolDefinitions() {
		switch {
		case covered[def.Name] && notOnOneTicket[def.Name]:
			t.Errorf("tool %s is both checked above and listed as not on one ticket", def.Name)
		case !covered[def.Name] && !notOnOneTicket[def.Name]:
			t.Errorf("tool %s is not classified: add a call above that checks it flags open notes, "+
				"or list it as not on one ticket", def.Name)
		}
	}

	for _, c := range []struct {
		tool string
		args map[string]any
	}{
		{"get_ticket", map[string]any{"id": quiet.ID}},
		{"update_ticket", map[string]any{"id": quiet.ID, "priority": "low"}},
		{"get_project", map[string]any{"id": "ACP"}},
		{"list_entries", map[string]any{"project": "ACP"}},
		{"list_tickets", map[string]any{"projectId": "ACP", "summary": true}},
	} {
		texts := callAllText(t, f.s, c.tool, c.args)
		if strings.Contains(strings.Join(texts, ""), `"openNotes"`) {
			t.Errorf("%s(%v) carries openNotes: %v", c.tool, c.args, texts)
		}
	}
}

// The flag joins an object answer as a field and follows any other answer
// as content of its own; with no open notes the answer is untouched.
func TestWithOpenNotes(t *testing.T) {
	for _, c := range []struct {
		data string
		n    int
		want []string
	}{
		{`{"id":"x"}`, 0, []string{`{"id":"x"}`}},
		{`{"id":"x"}`, 2, []string{`{"id":"x","openNotes":2}`}},
		{`{}`, 1, []string{`{"openNotes":1}`}},
		{`[{"id":"x"}]`, 1, []string{`[{"id":"x"}]`, `{"openNotes":1}`}},
	} {
		got := withOpenNotes([]byte(c.data), c.n)
		var texts []string
		for _, g := range got {
			texts = append(texts, g.Text)
		}
		if strings.Join(texts, "|") != strings.Join(c.want, "|") {
			t.Errorf("withOpenNotes(%s, %d) = %v, want %v", c.data, c.n, texts, c.want)
		}
	}
}

// get_ticket carries only the newest entries, so an open note can be missing
// from them: its description says so, and it and handle_note send an agent
// to list_entries for the notes.
func TestEntryCapAndNoteReadingAreDescribed(t *testing.T) {
	descriptions := map[string]string{}
	for _, def := range newTestServer(t).toolDefinitions() {
		descriptions[def.Name] = def.Description
	}
	for tool, want := range map[string][]string{
		"get_ticket":  {"20 newest current entries", "before: nextBefore", `list_entries, types ["note"]`, "no epic"},
		"handle_note": {`list_entries, types ["note"]`},
	} {
		for _, w := range want {
			if !strings.Contains(descriptions[tool], w) {
				t.Errorf("%s's description does not say %q:\n%s", tool, w, descriptions[tool])
			}
		}
	}
}
