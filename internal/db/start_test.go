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

// startSizeCeiling guards the start's structure, not what a real start
// costs. The fixture has every part (three subtasks, two dependencies, four
// entries and a hand-off, an open note at each level, an epic with a
// decision and a spec, the project's newest ten entries), each with one
// sentence of text: 5.5 KB when this was written, with room for timestamps,
// which vary in length. Owner fields, history or any other bulk creeping
// back in breaks it. A real start is dominated by its text instead: on an
// ACP-shaped ticket (3.9 KB of agent instructions, a 2 KB description, ten
// project entries of about 400 bytes) it measured about 19 KB, some 5k
// tokens, in review. Raise the ceiling only for a part an agent needs.
const startSizeCeiling = 6000

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
