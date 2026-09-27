package db

import (
	"testing"

	"github.com/tcarac/taskboard/internal/models"
)

// EntryAgents reads each agent an entry names, as author or as the agent that
// handled a note, once, with its role, model, provider and session.
func TestEntryAgentsReadAuthorsAndHandlersWithTheirSessions(t *testing.T) {
	f := newEntriesFixture(t)
	if err := insertSession(f.s, "s2", "codex", "c0d3x"); err != nil {
		t.Fatal(err)
	}
	if err := insertAgent(f.s, "a2", "s2", "reviewer", models.ProviderOpenAI); err != nil {
		t.Fatal(err)
	}
	learning := mustCreateEntry(t, f.s, f.learning(f.onTicket(), "A second connection needs a commit."))
	again := mustCreateEntry(t, f.s, f.learning(f.onTicket(), "Tests use a temp dir."))
	note := mustCreateEntry(t, f.s, models.CreateEntryRequest{EntryOwner: f.onTicket(), Type: models.EntryNote,
		Text: "Aim for 100 ms.", AuthorName: "Bilal"})
	handled, err := f.s.MarkNoteHandled(note.ID, "a2")
	if err != nil {
		t.Fatal(err)
	}

	agents, err := f.s.EntryAgents([]models.Entry{*learning, *again, *handled})
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 2 {
		t.Fatalf("agents = %#v, want a1 and a2", agents)
	}
	a1 := agents["a1"]
	if a1.Role != "implementer" || a1.Model != "claude-opus-5-5" || a1.Provider != models.ProviderAnthropic ||
		a1.Session.ID != "s1" || a1.Session.Machine != "bilal-mbp" || a1.Session.ResumeCommand != "claude --resume 3da2c294" {
		t.Errorf("a1 = %#v", a1)
	}
	if a2 := agents["a2"]; a2.Role != "reviewer" || a2.Provider != models.ProviderOpenAI || a2.Session.Vendor != "codex" {
		t.Errorf("a2 = %#v", a2)
	}

	// Staleness and the session's last seen agree with GetAgent: a2's session
	// has gone unseen for longer than the stale threshold, a1's has not.
	setLastSeen(t, f.s, "a2", 2*models.DefaultStaleAfter)
	agents, err = f.s.EntryAgents([]models.Entry{*learning, *handled})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a1", "a2"} {
		want, err := f.s.GetAgent(id)
		if err != nil {
			t.Fatal(err)
		}
		got := agents[id]
		if got.Stale != want.Stale || !got.SessionLastSeenAt.Equal(want.SessionLastSeenAt) || got.SessionLastSeenAt.IsZero() {
			t.Errorf("%s: stale %v, session last seen %v; GetAgent says %v, %v",
				id, got.Stale, got.SessionLastSeenAt, want.Stale, want.SessionLastSeenAt)
		}
	}
	if !agents["a2"].Stale || agents["a1"].Stale {
		t.Errorf("stale: a1 %v, a2 %v; want a2 only", agents["a1"].Stale, agents["a2"].Stale)
	}

	none, err := f.s.EntryAgents([]models.Entry{*note})
	if err != nil || len(none) != 0 {
		t.Fatalf("a note the person wrote names no agent: %#v, %v", none, err)
	}
}
