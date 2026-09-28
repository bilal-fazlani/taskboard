package db

import (
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/tcarac/taskboard/internal/models"
)

// startFixture is a ticket as an agent meets it: a project with agent
// instructions and entries, an epic with a spec and entries, and a ticket in
// the epic with subtasks, dependencies (one done, one not), entries of
// every kind and open notes at every level.
type startFixture struct {
	s                   *Store
	project             *models.Project
	epic                *models.Epic
	ticket, dep         *models.Ticket
	first, second       *models.Agent
	olderHandOff        *models.Entry
	handOff             *models.Entry
	ticketNote, handled *models.Entry
	epicNote, projNote  *models.Entry
	projectEntries      []*models.Entry
}

func newStartFixture(t *testing.T) startFixture {
	t.Helper()
	s := newTestStore(t)
	f := startFixture{s: s}
	var err error
	f.project, err = s.CreateProject(models.CreateProjectRequest{Name: "Agent Control Plane", Prefix: "ACP",
		Description:       "Turn Taskboard into the control plane for a fleet of AI agents.",
		AgentInstructions: "Worktrees under taskboard-worktrees/<key-slug>, one branch per ticket; never push."})
	if err != nil {
		t.Fatal(err)
	}
	f.epic, err = s.CreateEpic(models.CreateEpicRequest{ProjectID: f.project.ID, Name: "Write-back",
		Description: "Sessions leave a trace: agent chats are a log, the board is the project's state."})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateDocument(models.CreateDocumentRequest{EpicID: f.epic.ID, Name: "Spec", Content: "# Spec"}); err != nil {
		t.Fatal(err)
	}
	done, err := s.CreateTicket(models.CreateTicketRequest{ProjectID: f.project.ID, Title: "Entries in the store", Status: models.StatusDone})
	if err != nil {
		t.Fatal(err)
	}
	f.dep = seedTicket(t, s, f.project.ID, "Agent identify")
	epic := "Write-back"
	f.ticket, err = s.CreateTicket(models.CreateTicketRequest{ProjectID: f.project.ID, Title: "One-call start", Epic: &epic,
		Description: "Starting a ticket claims it and returns everything needed to begin.",
		DependsOn:   []models.DependencyInput{{Ticket: done.DisplayKey()}, {Ticket: f.dep.DisplayKey()}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"Store: the start payload", "MCP start_ticket", "CLI and HTTP"} {
		if _, err := s.AddSubtask(f.ticket.ID, models.CreateSubtaskRequest{Title: title}); err != nil {
			t.Fatal(err)
		}
	}
	f.first = identify(t, s, "3da2c294", "implementer")
	f.second = identify(t, s, "b10d8a02", "implementer")

	onTicket := models.EntryOwner{TicketID: f.ticket.ID}
	onEpic := models.EntryOwner{EpicID: f.epic.ID}
	onProject := models.EntryOwner{ProjectID: f.project.ID}
	byAgent := func(owner models.EntryOwner, typ, text string) *models.Entry {
		req := models.CreateEntryRequest{EntryOwner: owner, Type: typ, Text: text, AgentID: f.first.ID}
		if typ == models.EntryDecision {
			req.Source = models.DecisionSourceAgent
		}
		return mustCreateEntry(t, s, req)
	}
	note := func(owner models.EntryOwner, text string) *models.Entry {
		return mustCreateEntry(t, s, models.CreateEntryRequest{EntryOwner: owner, Type: models.EntryNote, Text: text, AuthorName: "Bilal"})
	}
	for i := 0; i < StartProjectEntries+2; i++ {
		f.projectEntries = append(f.projectEntries, byAgent(onProject, models.EntryDecision,
			"The board is the project's state; chats are a log."))
	}
	f.projNote = note(onProject, "Keep every read lean.")
	byAgent(onEpic, models.EntryDecision, "The start returns the ticket's and epic's entries in full.")
	f.epicNote = note(onEpic, "Mind the token budget.")
	byAgent(onTicket, models.EntryDecision, "The payload is read after the claim commits.")
	byAgent(onTicket, models.EntryLearning, "listEntries reads through the pool, not a transaction.")
	f.olderHandOff = byAgent(onTicket, models.EntryHandOff, "Stopped at the store; MCP is next.")
	f.handOff = byAgent(onTicket, models.EntryHandOff, "Store and MCP done; CLI and HTTP are next.")
	f.handled = note(onTicket, "Use port 3021.")
	if _, err := s.MarkNoteHandled(f.handled.ID, f.first.ID); err != nil {
		t.Fatal(err)
	}
	f.ticketNote = note(onTicket, "Say what a start costs.")
	return f
}

func mustStart(t *testing.T, s *Store, ticketRef, agentID string) *models.Start {
	t.Helper()
	start, err := s.StartTicket(ticketRef, agentID)
	if err != nil {
		t.Fatalf("starting %s as %s: %v", ticketRef, agentID, err)
	}
	return start
}

func entryIDs(entries []models.Entry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.ID
	}
	return out
}

// jsonKeys is the top-level keys of v's JSON, sorted.
func jsonKeys(t *testing.T, v any) []string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Starting a ticket claims it, the way ClaimTicket does, and carries each
// part of the payload: the ticket without its holder, the latest hand-off
// apart from the ticket's other current entries, the epic and the project
// with their context, every open note at every level, and the unfinished
// dependencies, with no entry naming its owner.
func TestStartTicketClaimsAndCarriesEverythingToBegin(t *testing.T) {
	f := newStartFixture(t)
	start := mustStart(t, f.s, "acp-3", f.first.ID)

	got, _ := f.s.GetTicket(f.ticket.ID)
	if got.Agent == nil || got.Agent.ID != f.first.ID || got.Status != models.StatusInProgress {
		t.Fatalf("after the start: agent %+v, status %s; want in_progress held by %s", got.Agent, got.Status, f.first.ID)
	}
	wantHistoryNote(t, f.s, f.ticket.ID, models.StatusTodo, models.StatusInProgress, "")
	if start.Ticket.ID != f.ticket.ID || start.Ticket.Status != models.StatusInProgress ||
		len(start.Ticket.Subtasks) != 3 || start.Ticket.Description == "" {
		t.Fatalf("start's ticket = %+v", start.Ticket)
	}
	if start.Ticket.Agent != nil {
		t.Errorf("the ticket names its holder, the caller: %+v", start.Ticket.Agent)
	}
	if start.TakenFrom != nil {
		t.Errorf("a free ticket was taken from %+v", start.TakenFrom)
	}

	if start.HandOff == nil || start.HandOff.ID != f.handOff.ID {
		t.Fatalf("handOff = %+v, want the latest, %s", start.HandOff, f.handOff.ID)
	}
	// Every current entry but the lifted hand-off and the open note, newest
	// first; the handled note and the older hand-off stay.
	wantTicket := []string{f.handled.ID, f.olderHandOff.ID}
	ticketEntries := entryIDs(start.Entries)
	if len(ticketEntries) != 4 || !reflect.DeepEqual(ticketEntries[:2], wantTicket) {
		t.Fatalf("ticket entries = %v, want 4 starting %v", ticketEntries, wantTicket)
	}
	if slices.Contains(ticketEntries, f.handOff.ID) || slices.Contains(ticketEntries, f.ticketNote.ID) {
		t.Errorf("ticket entries %v repeat the hand-off or the open note", ticketEntries)
	}

	if start.Notes == nil ||
		!reflect.DeepEqual(entryIDs(start.Notes.Ticket), []string{f.ticketNote.ID}) ||
		!reflect.DeepEqual(entryIDs(start.Notes.Epic), []string{f.epicNote.ID}) ||
		!reflect.DeepEqual(entryIDs(start.Notes.Project), []string{f.projNote.ID}) {
		t.Fatalf("notes = %+v, want one open note at each level", start.Notes)
	}

	if start.Epic == nil || start.Epic.Description == "" || !reflect.DeepEqual(start.Epic.Documents, []string{"Spec.md"}) ||
		len(start.Epic.Entries) != 1 || start.Epic.Entries[0].Type != models.EntryDecision {
		t.Fatalf("epic = %+v, want its description, Spec.md and its one decision", start.Epic)
	}

	p := start.Project
	if p.Name != "Agent Control Plane" || p.Description == "" || !strings.Contains(p.AgentInstructions, "never push") {
		t.Fatalf("project = %+v, want its name, description and agent instructions", p)
	}
	if p.Entries == nil || len(p.Entries.Entries) != StartProjectEntries || p.Entries.Total != StartProjectEntries+2 ||
		!p.Entries.HasMore || p.Entries.NextBefore != p.Entries.Entries[StartProjectEntries-1].ID {
		t.Fatalf("project entries = %d of %d, hasMore %v, nextBefore %q; want the newest %d of %d and a cursor",
			len(p.Entries.Entries), p.Entries.Total, p.Entries.HasMore, p.Entries.NextBefore, StartProjectEntries, StartProjectEntries+2)
	}
	if p.Entries.Entries[0].ID != f.projectEntries[len(f.projectEntries)-1].ID {
		t.Errorf("project entries start with %s, want the newest", p.Entries.Entries[0].ID)
	}
	// The cursor fetches the rest through ListEntries.
	rest, err := f.s.ListEntries(models.EntryOwner{ProjectID: "ACP"}, models.EntryFilter{}, p.Entries.NextBefore, EntryMaxLimit)
	if err != nil || len(rest.Entries) != 2 {
		t.Fatalf("the rest of the project's entries = %d, %v; want 2", len(rest.Entries), err)
	}

	for part, entries := range map[string][]models.Entry{
		"handOff": {*start.HandOff}, "entries": start.Entries, "epic": start.Epic.Entries, "project": p.Entries.Entries,
		"ticket notes": start.Notes.Ticket, "epic notes": start.Notes.Epic, "project notes": start.Notes.Project,
	} {
		for _, e := range entries {
			if e.EntryOwner != (models.EntryOwner{}) {
				t.Errorf("%s entry %s names its owner %+v", part, e.ID, e.EntryOwner)
			}
		}
	}

	if !reflect.DeepEqual(start.UnfinishedDependencies, []string{f.dep.DisplayKey()}) {
		t.Errorf("unfinishedDependencies = %v, want only %s", start.UnfinishedDependencies, f.dep.DisplayKey())
	}
}

// A ticket another session's live agent holds is refused, naming the holder
// and its session's last seen, and nothing changes; a stale holder's ticket
// is taken over, and the start says from whom and where the work stood.
func TestStartTicketRefusesALiveHolderAndTakesOverAStaleOne(t *testing.T) {
	f := newStartFixture(t)
	mustStart(t, f.s, f.ticket.ID, f.first.ID)
	setLastSeen(t, f.s, f.first.ID, 29*time.Minute)
	n := historyLen(t, f.s, f.ticket.ID)

	_, err := f.s.StartTicket(f.ticket.ID, f.second.ID)
	var held *ErrTicketHeld
	if !errors.As(err, &held) || held.Holder.ID != f.first.ID {
		t.Fatalf("start of a live agent's ticket: %v, want ErrTicketHeld naming %s", err, f.first.ID)
	}
	if !strings.Contains(err.Error(), "held by agent "+f.first.ID) ||
		!strings.Contains(err.Error(), "last seen "+held.Holder.SessionLastSeenAt.UTC().Format(time.RFC3339)) {
		t.Fatalf("refusal %q does not name the holder and its last seen", err)
	}
	if got, _ := f.s.GetTicket(f.ticket.ID); got.Agent.ID != f.first.ID || historyLen(t, f.s, f.ticket.ID) != n {
		t.Fatal("a refused start changed the ticket")
	}

	setLastSeen(t, f.s, f.first.ID, 31*time.Minute)
	start := mustStart(t, f.s, f.ticket.ID, f.second.ID)
	if start.TakenFrom == nil || start.TakenFrom.ID != f.first.ID || !start.TakenFrom.Stale {
		t.Fatalf("takenFrom = %+v, want the stale %s", start.TakenFrom, f.first.ID)
	}
	if start.HandOff == nil || start.HandOff.ID != f.handOff.ID || start.HandOff.TicketID != "" {
		t.Fatalf("handOff = %+v, want %s without its owner", start.HandOff, f.handOff.ID)
	}
	if slices.Contains(entryIDs(start.Entries), f.handOff.ID) {
		t.Error("a takeover's hand-off is repeated in the ticket's entries")
	}
	wantHistoryNote(t, f.s, f.ticket.ID, models.StatusInProgress, models.StatusInProgress, "from agent "+f.first.ID)
	if got, _ := f.s.GetTicket(f.ticket.ID); got.Agent.ID != f.second.ID {
		t.Fatalf("after the takeover the ticket is held by %s", got.Agent.ID)
	}
}

// A ticket with nothing around it starts with only its ticket and its
// project's name: every empty part is left out.
func TestStartTicketLeavesOutEmptyParts(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Bare", "BARE")
	tk := seedTicket(t, s, p.ID, "Nothing around it")
	agent := identify(t, s, "3da2c294", "implementer")
	start := mustStart(t, s, tk.ID, agent.ID)
	if keys := jsonKeys(t, start); !reflect.DeepEqual(keys, []string{"project", "ticket"}) {
		t.Fatalf("a bare start has %v, want only project and ticket", keys)
	}
	if keys := jsonKeys(t, start.Project); !reflect.DeepEqual(keys, []string{"name"}) {
		t.Fatalf("a bare project has %v, want only its name", keys)
	}

	// An epic with nothing to say beyond its name is left out too.
	seedEpic(t, s, p.ID, "Empty")
	epic := "Empty"
	inEpic, err := s.CreateTicket(models.CreateTicketRequest{ProjectID: p.ID, Title: "In an empty epic", Epic: &epic})
	if err != nil {
		t.Fatal(err)
	}
	if start := mustStart(t, s, inEpic.ID, agent.ID); start.Epic != nil || start.Ticket.Epic == nil {
		t.Fatalf("an empty epic: start.epic %+v, ticket.epic %+v; want only the ticket's", start.Epic, start.Ticket.Epic)
	}
}

// The start's ticket is lean: a subtask carries its id (toggle_subtask
// takes it), title and whether it is done, but not its ticket's id or its
// position; a label its name and color; a linked ticket its key, title,
// status, and a dependency's kind and note, but no id. The rest of the
// ticket is as it stands.
func TestStartTicketIsLean(t *testing.T) {
	f := newStartFixture(t)
	from := f.dep.DisplayKey()
	if _, err := f.s.UpdateTicket(f.ticket.ID, models.UpdateTicketRequest{Labels: []string{"api"}, SurfacedFrom: &from,
		DependsOn: []models.DependencyInput{{Ticket: f.dep.DisplayKey(), Kind: models.DependencyConflictOnly, Note: "start.go"}}}); err != nil {
		t.Fatal(err)
	}
	start := mustStart(t, f.s, f.ticket.ID, f.first.ID)
	raw, err := json.Marshal(start.Ticket)
	if err != nil {
		t.Fatal(err)
	}
	var ticket struct {
		ID, Title, Description, Status string
		Subtasks, DependsOn, Labels    []map[string]any
		SurfacedFrom                   map[string]any
	}
	if err := json.Unmarshal(raw, &ticket); err != nil {
		t.Fatal(err)
	}
	if ticket.ID != f.ticket.ID || ticket.Title != "One-call start" || ticket.Description == "" || ticket.Status != models.StatusInProgress {
		t.Fatalf("the lean ticket lost its own fields: %s", raw)
	}
	keysOf := func(m map[string]any) []string {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return keys
	}
	for part, c := range map[string]struct {
		got  []map[string]any
		want []string
	}{
		"subtask":      {ticket.Subtasks, []string{"completed", "id", "title"}},
		"dependsOn":    {ticket.DependsOn, []string{"key", "kind", "note", "status", "title"}},
		"label":        {ticket.Labels, []string{"color", "name"}},
		"surfacedFrom": {[]map[string]any{ticket.SurfacedFrom}, []string{"key", "status", "title"}},
	} {
		if len(c.got) == 0 {
			t.Fatalf("the start's ticket has no %s: %s", part, raw)
		}
		for _, item := range c.got {
			if keys := keysOf(item); !reflect.DeepEqual(keys, c.want) {
				t.Errorf("a %s carries %v, want %v", part, keys, c.want)
			}
		}
	}
	if len(ticket.Subtasks) != 3 || ticket.Subtasks[0]["title"] != "Store: the start payload" {
		t.Errorf("subtasks = %v, want the three in order", ticket.Subtasks)
	}
	if ticket.DependsOn[0]["key"] != f.dep.DisplayKey() || ticket.DependsOn[0]["kind"] != models.DependencyConflictOnly {
		t.Errorf("dependsOn = %v, want %s, conflict_only", ticket.DependsOn, f.dep.DisplayKey())
	}
	if !reflect.DeepEqual(start.UnfinishedDependencies, []string{f.dep.DisplayKey()}) {
		t.Errorf("unfinishedDependencies = %v, want %s", start.UnfinishedDependencies, f.dep.DisplayKey())
	}

	// The ticket that blocks shows the one it blocks the same way.
	blocker := mustStart(t, f.s, f.dep.ID, f.second.ID)
	if raw, _ := json.Marshal(blocker.Ticket.Blocks); len(blocker.Ticket.Blocks) != 1 || strings.Contains(string(raw), `"id"`) {
		t.Errorf("blocks = %s, want one link without an id", raw)
	}
}

// The project's agent instructions come on each agent's first start in the
// project, not on each session's: an agent that has them gets, in their
// place, where to read them again, while another agent of the same session
// (an implementer its orchestrator launched) still gets them on its own
// first start. Changed instructions come again, and another project's come
// on the agent's first start there.
func TestStartTicketGivesAgentInstructionsOncePerAgent(t *testing.T) {
	f := newStartFixture(t)
	instructions := "Worktrees under taskboard-worktrees/<key-slug>, one branch per ticket; never push."
	wantGiven := func(start *models.Start, who string) {
		t.Helper()
		if start.Project.AgentInstructions != instructions || start.Project.AgentInstructionsLeftOut != "" {
			t.Fatalf("%s: agentInstructions %q, leftOut %q; want the instructions", who,
				start.Project.AgentInstructions, start.Project.AgentInstructionsLeftOut)
		}
	}
	wantLeftOut := func(start *models.Start, who string) {
		t.Helper()
		p := start.Project
		if p.AgentInstructions != "" || !strings.Contains(p.AgentInstructionsLeftOut, "earlier start") ||
			!strings.Contains(p.AgentInstructionsLeftOut, "get_project ACP") ||
			!strings.Contains(p.AgentInstructionsLeftOut, "/api/projects/"+f.project.ID) {
			t.Fatalf("%s: agentInstructions %q, leftOut %q; want them left out, saying where to read them",
				who, p.AgentInstructions, p.AgentInstructionsLeftOut)
		}
	}

	orchestrator := identify(t, f.s, "5e55104a", "orchestrator")
	wantGiven(mustStart(t, f.s, f.dep.ID, orchestrator.ID), "the orchestrator's first start")
	wantLeftOut(mustStart(t, f.s, f.ticket.ID, orchestrator.ID), "the orchestrator's second start")

	// An implementer of the same session has not had them.
	implementer := identify(t, f.s, "5e55104a", "implementer")
	wantGiven(mustStart(t, f.s, f.ticket.ID, implementer.ID), "a second agent of the session")
	wantLeftOut(mustStart(t, f.s, f.ticket.ID, implementer.ID), "the implementer's second start")

	// Changed instructions come again, once.
	instructions = "Worktrees under taskboard-worktrees/<key-slug>; never push; land with --ff-only."
	if _, err := f.s.UpdateProject(f.project.ID, models.UpdateProjectRequest{AgentInstructions: &instructions}); err != nil {
		t.Fatal(err)
	}
	wantGiven(mustStart(t, f.s, f.ticket.ID, implementer.ID), "the start after they changed")
	wantLeftOut(mustStart(t, f.s, f.ticket.ID, implementer.ID), "the next start")

	// Another project's instructions are its own.
	other, err := f.s.CreateProject(models.CreateProjectRequest{Name: "Other", Prefix: "OTH", AgentInstructions: "Run make check."})
	if err != nil {
		t.Fatal(err)
	}
	there := seedTicket(t, f.s, other.ID, "Elsewhere")
	if p := mustStart(t, f.s, there.ID, implementer.ID).Project; p.AgentInstructions != "Run make check." || p.AgentInstructionsLeftOut != "" {
		t.Fatalf("the first start in another project: agentInstructions %q, leftOut %q", p.AgentInstructions, p.AgentInstructionsLeftOut)
	}
}

// Every entry an agent wrote names that agent's role and model inline; an
// entry the person wrote names the person, as ever.
func TestStartTicketNamesEachEntrysAgentRoleAndModel(t *testing.T) {
	f := newStartFixture(t)
	start := mustStart(t, f.s, f.ticket.ID, f.second.ID)
	for part, entries := range map[string][]models.Entry{
		"handOff": {*start.HandOff}, "entries": start.Entries, "epic": start.Epic.Entries, "project": start.Project.Entries.Entries,
		"ticket notes": start.Notes.Ticket, "project notes": start.Notes.Project,
	} {
		for _, e := range entries {
			switch {
			case e.AgentID != "" && (e.AgentRole != "implementer" || e.AgentModel != "claude-opus-5-5"):
				t.Errorf("%s entry %s by %s names role %q, model %q", part, e.ID, e.AgentID, e.AgentRole, e.AgentModel)
			case e.AgentID == "" && (e.AgentRole != "" || e.AgentModel != "" || e.AuthorName != "Bilal"):
				t.Errorf("%s entry %s by the person: %+v", part, e.ID, e)
			}
		}
	}
	raw, err := json.Marshal(start.HandOff)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"agentRole":"implementer","agentModel":"claude-opus-5-5"`) {
		t.Errorf("the hand-off's JSON does not name its author inline: %s", raw)
	}

	// Other reads of entries leave them out.
	page, err := f.s.ListEntries(models.EntryOwner{TicketID: f.ticket.ID}, models.EntryFilter{}, "", EntryMaxLimit)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range page.Entries {
		if e.AgentRole != "" || e.AgentModel != "" {
			t.Errorf("list_entries names entry %s's agent inline: %+v", e.ID, e)
		}
	}
}

// startSizeCeiling guards the start's structure, not what a real start
// costs. The fixture has every part (three subtasks, two dependencies, four
// entries and a hand-off, an open note at each level, an epic with a
// decision and a spec, the project's newest five entries), each with one
// sentence of text: 4.7 KB since the lean start, with room for timestamps,
// which vary in length. It was 5.5 KB under a 6,000-byte ceiling before
// (ten project entries, ids and positions on the ticket's lists); the
// ceiling came down with it so that the room it guards stays the same.
// Owner fields, ids, history or any other bulk creeping back in breaks it.
// A real start is dominated by its text instead:
// TestStartTicketRealisticSize measures one. Raise the ceiling only for a
// part an agent needs.
const startSizeCeiling = 5300

// The start carries no bulk beyond its parts: the fixture's start, as
// compact JSON the way MCP sends it, stays under the ceiling.
func TestStartTicketSize(t *testing.T) {
	f := newStartFixture(t)
	start := mustStart(t, f.s, f.ticket.ID, f.first.ID)
	raw, err := json.Marshal(start)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("the seeded start is %d bytes", len(raw))
	if len(raw) > startSizeCeiling {
		t.Fatalf("the seeded start is %d bytes, over its ceiling of %d:\n%s", len(raw), startSizeCeiling, raw)
	}
}

// start_ticket carries the ticket's answered requests, newest first, each
// with its type, prompt, answer, note, who answered and when; a ticket with
// none carries no answeredRequests at all.
func TestStartTicketCarriesAnsweredRequestsNewestFirst(t *testing.T) {
	f := newClaimFixture(t)
	mustClaim(t, f.s, f.ticket.ID, f.first.ID)

	if start := mustStart(t, f.s, f.ticket.ID, f.first.ID); start.Ticket.AnsweredRequests != nil {
		t.Fatalf("answeredRequests on a ticket with none = %+v, want it left out", start.Ticket.AnsweredRequests)
	}

	first := mustCreateRequest(t, f.s, models.CreateUserInputRequest{TicketID: f.ticket.ID, AgentID: f.first.ID,
		Type: models.UserInputQuestion, Prompt: "Which port?", Choices: []string{"3011", "3014"}})
	if _, err := f.s.AnswerRequest(first, "3011", "Bilal", ""); err != nil {
		t.Fatal(err)
	}
	second := mustCreateRequest(t, f.s, models.CreateUserInputRequest{TicketID: f.ticket.ID, AgentID: f.first.ID,
		Type: models.UserInputApproval, Prompt: "Land it?"})
	if _, err := f.s.AnswerRequest(second, "approved", "Bilal", "Ship it once CI is green."); err != nil {
		t.Fatal(err)
	}

	got := mustStart(t, f.s, f.ticket.ID, f.first.ID).Ticket.AnsweredRequests
	if len(got) != 2 {
		t.Fatalf("answeredRequests = %+v, want 2", got)
	}
	// Newest first: the approval, answered second, comes before the question.
	if got[0].Type != models.UserInputApproval || got[0].Prompt != "Land it?" || got[0].Answer != models.ApprovalApproved ||
		got[0].Note != "Ship it once CI is green." || got[0].AnsweredBy != "Bilal" || got[0].AnsweredAt.IsZero() || got[0].Stopped {
		t.Fatalf("newest answeredRequests entry = %+v", got[0])
	}
	if got[1].Type != models.UserInputQuestion || got[1].Prompt != "Which port?" || got[1].Answer != "3011" ||
		got[1].Note != "" || got[1].AnsweredBy != "Bilal" || got[1].AnsweredAt.IsZero() || got[1].Stopped {
		t.Fatalf("oldest answeredRequests entry = %+v", got[1])
	}
	if got[0].AnsweredAt.Before(got[1].AnsweredAt) {
		t.Errorf("answeredRequests not newest first: %+v", got)
	}
}

// A request closed by Stop work shows as stopped, not as an answer: no
// answer or note, and answeredBy is who stopped the work.
func TestStartTicketRequestClosedByStopShowsAsStopped(t *testing.T) {
	f := newClaimFixture(t)
	mustClaim(t, f.s, f.ticket.ID, f.first.ID)
	mustCreateRequest(t, f.s, models.CreateUserInputRequest{TicketID: f.ticket.ID, AgentID: f.first.ID,
		Type: models.UserInputApproval, Prompt: "Land it?"})

	if _, err := f.s.StopWork(f.ticket.ID, "Bilal"); err != nil {
		t.Fatal(err)
	}

	got := mustStart(t, f.s, f.ticket.ID, f.second.ID).Ticket.AnsweredRequests
	if len(got) != 1 {
		t.Fatalf("answeredRequests after a stop = %+v, want 1", got)
	}
	r := got[0]
	if !r.Stopped || r.Answer != "" || r.Note != "" || r.AnsweredBy != "Bilal" || r.AnsweredAt.IsZero() ||
		r.Type != models.UserInputApproval || r.Prompt != "Land it?" {
		t.Fatalf("stopped request's answeredRequests entry = %+v, want stopped with no answer", r)
	}
}

// An answer the person gives after the asking session has gone stale still
// reaches the next agent: once a new session's start takes the ticket over,
// its answeredRequests carries it.
func TestStartTicketCarriesAnAnswerGivenAfterTheAskingSessionWentStale(t *testing.T) {
	f := newClaimFixture(t)
	mustStart(t, f.s, f.ticket.ID, f.first.ID)
	id := mustCreateRequest(t, f.s, models.CreateUserInputRequest{TicketID: f.ticket.ID, AgentID: f.first.ID,
		Type: models.UserInputQuestion, Prompt: "Which port?"})

	// The asking session goes stale before the person answers.
	setLastSeen(t, f.s, f.first.ID, 31*time.Minute)
	if _, err := f.s.AnswerRequest(id, "3011", "Bilal", "Free on this machine."); err != nil {
		t.Fatal(err)
	}

	// A second session's start takes the stale ticket over, and sees the
	// answer given after the first session went quiet.
	start := mustStart(t, f.s, f.ticket.ID, f.second.ID)
	if start.TakenFrom == nil || start.TakenFrom.ID != f.first.ID {
		t.Fatalf("start.takenFrom = %+v, want the stale %s", start.TakenFrom, f.first.ID)
	}
	got := start.Ticket.AnsweredRequests
	if len(got) != 1 || got[0].Answer != "3011" || got[0].Note != "Free on this machine." || got[0].AnsweredBy != "Bilal" || got[0].Stopped {
		t.Fatalf("answeredRequests after the takeover = %+v, want the answer given while stale", got)
	}
}
