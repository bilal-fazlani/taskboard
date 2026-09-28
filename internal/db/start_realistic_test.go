package db

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/tcarac/taskboard/internal/models"
)

// prose is n bytes of plain sentences, the length of real text without its
// content.
func prose(n int) string {
	const sentence = "The start carries what an agent needs to begin, and nothing twice. "
	return strings.Repeat(sentence, n/len(sentence)+1)[:n]
}

// realisticStart is a ticket shaped like this project's own: 3.9 KB of agent
// instructions, a 2 KB description, ten project entries of about 400 bytes,
// and a ticket part like the ticket that made the start lean (six subtasks,
// two dependencies, a surfaced-from link, a label, an answered request, a
// ticket decision and learning, a delivery), each text the length of its real counterpart. It
// answers the store, the ticket, and two agents of one session.
func realisticStart(t *testing.T) (*Store, *models.Ticket, *models.Agent, *models.Agent) {
	t.Helper()
	s := newTestStore(t)
	p, err := s.CreateProject(models.CreateProjectRequest{Name: "Taskboard: agent control plane", Prefix: "ACP",
		Description:       prose(900),
		AgentInstructions: prose(3900)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateEpic(models.CreateEpicRequest{ProjectID: p.ID, Name: "Token economy",
		Description: prose(160)}); err != nil {
		t.Fatal(err)
	}
	source, err := s.CreateTicket(models.CreateTicketRequest{ProjectID: p.ID, Status: models.StatusDone,
		Title: "One-call start: claim a ticket and get everything needed to begin"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.CreateTicket(models.CreateTicketRequest{ProjectID: p.ID, Status: models.StatusDone,
		Title: "Decide whether starting a done ticket is refused"})
	if err != nil {
		t.Fatal(err)
	}
	epic, from := "Token economy", source.DisplayKey()
	tk, err := s.CreateTicket(models.CreateTicketRequest{ProjectID: p.ID, Epic: &epic, SurfacedFrom: &from,
		Title:       "A lean start: trim the ticket part and the project's share of every start",
		Description: prose(2000), Priority: "medium", Labels: []string{"api"}, Repos: []string{"bilal-fazlani/taskboard"},
		DependsOn: []models.DependencyInput{{Ticket: source.DisplayKey()},
			{Ticket: other.DisplayKey(), Kind: models.DependencyConflictOnly, Note: "internal/db/agents.go start/claim path and the start_ticket MCP tool"}}})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		if _, err := s.AddSubtask(tk.ID, models.CreateSubtaskRequest{Title: prose(110)}); err != nil {
			t.Fatal(err)
		}
	}
	branch, worktree := "acp-217-lean-start", "/Users/bilal/Projects/taskboard-worktrees/acp-217-lean-start"
	if _, err := s.UpdateTicket(tk.ID, models.UpdateTicketRequest{Delivery: &models.DeliveryUpdate{Branch: &branch, Worktree: &worktree}}); err != nil {
		t.Fatal(err)
	}

	orchestrator := identify(t, s, "b8e2e2c4", "orchestrator")
	for i := 0; i < 10; i++ {
		mustCreateEntry(t, s, models.CreateEntryRequest{EntryOwner: models.EntryOwner{ProjectID: p.ID},
			Type: models.EntryLearning, Text: prose(400), AgentID: orchestrator.ID})
	}
	mustCreateEntry(t, s, models.CreateEntryRequest{EntryOwner: models.EntryOwner{TicketID: tk.ID},
		Type: models.EntryDecision, Source: models.DecisionSourcePerson, Text: prose(420), AgentID: orchestrator.ID})
	mustCreateEntry(t, s, models.CreateEntryRequest{EntryOwner: models.EntryOwner{TicketID: tk.ID},
		Type: models.EntryLearning, Text: prose(330), AgentID: orchestrator.ID})
	mustStart(t, s, tk.ID, orchestrator.ID)
	q := mustCreateRequest(t, s, models.CreateUserInputRequest{TicketID: tk.ID, AgentID: orchestrator.ID,
		Type: models.UserInputQuestion, Prompt: prose(620)})
	if _, err := s.AnswerRequest(q, "All four, (c) per agent", "bilal", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReleaseTicket(tk.ID, models.ReleaseTicketRequest{AgentID: orchestrator.ID,
		Outcome: models.ReleaseGiveBack, HandOff: "Answered; ready to dispatch."}); err != nil {
		t.Fatal(err)
	}
	return s, tk, identify(t, s, "b8e2e2c4", "implementer"), orchestrator
}

// startSizes is a start's compact JSON size, in all and by part: each
// top-level key, and the project's by its own keys.
func startSizes(t *testing.T, start *models.Start) (int, map[string]int) {
	t.Helper()
	raw, err := json.Marshal(start)
	if err != nil {
		t.Fatal(err)
	}
	parts := map[string]int{}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatal(err)
	}
	for k, v := range top {
		parts[k] = len(v)
	}
	var project map[string]json.RawMessage
	if err := json.Unmarshal(top["project"], &project); err != nil {
		t.Fatal(err)
	}
	for k, v := range project {
		parts["project."+k] = len(v)
	}
	return len(raw), parts
}

func logSizes(t *testing.T, name string, total int, parts map[string]int) {
	t.Helper()
	keys := make([]string, 0, len(parts))
	for k := range parts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "\n  %s: %d", k, parts[k])
	}
	t.Logf("%s: %d bytes%s", name, total, b.String())
}

// The realistic start's ceilings. Before the lean start (ids and positions
// on the ticket's lists, the project's ten newest entries, the agent
// instructions on every start, entries naming only an agent id) this
// fixture's start was 17,236 bytes, every start alike: 5,396 for the
// ticket, 3,902 for the agent instructions and 5,490 for the project's
// entries. Lean, an agent's first start was 14,563 bytes and its every
// later start 10,788, the instructions left out for a 120-byte pointer.
// Each ceiling leaves room for timestamps, which vary in length.
const (
	realisticFirstStartCeiling = 15000
	realisticLaterStartCeiling = 11200
)

// What a realistic start costs, by part: an implementer's first start of an
// ACP-shaped ticket, and the same agent starting it again. Run with -v to
// see the parts.
func TestStartTicketRealisticSize(t *testing.T) {
	s, tk, implementer, _ := realisticStart(t)
	first, parts := startSizes(t, mustStart(t, s, tk.ID, implementer.ID))
	logSizes(t, "first start", first, parts)
	again, againParts := startSizes(t, mustStart(t, s, tk.ID, implementer.ID))
	logSizes(t, "the same agent's next start", again, againParts)
	if first > realisticFirstStartCeiling {
		t.Errorf("an agent's first start is %d bytes, over %d", first, realisticFirstStartCeiling)
	}
	if again > realisticLaterStartCeiling {
		t.Errorf("an agent's later start is %d bytes, over %d", again, realisticLaterStartCeiling)
	}
	if parts["project.agentInstructions"] == 0 || againParts["project.agentInstructions"] != 0 {
		t.Errorf("agent instructions: %d bytes on the first start, %d on the next; want them on the first only",
			parts["project.agentInstructions"], againParts["project.agentInstructions"])
	}
}
