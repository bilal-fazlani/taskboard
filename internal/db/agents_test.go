package db

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tcarac/taskboard/internal/models"
)

// identify makes a new agent in the session vendorSessionID of claude_code.
func identify(t *testing.T, s *Store, vendorSessionID, role string) *models.Agent {
	t.Helper()
	a, err := s.IdentifyAgent(models.IdentifyAgentRequest{
		Vendor: "claude_code", VendorSessionID: vendorSessionID, Machine: "bilal-mbp",
		ResumeCommand: "claude --resume " + vendorSessionID, Role: role, Model: "claude-opus-5-5",
		Provider: models.ProviderAnthropic,
	})
	if err != nil {
		t.Fatalf("identifying %s in %s: %v", role, vendorSessionID, err)
	}
	return a
}

// setLastSeen moves an agent's last seen back to ago before now.
func setLastSeen(t *testing.T, s *Store, agentID string, ago time.Duration) {
	t.Helper()
	execOrFail(t, s.db, `UPDATE agents SET last_seen_at = ? WHERE id = ?`, stamp(time.Now().Add(-ago)), agentID)
}

func lastSeen(t *testing.T, s *Store, agentID string) time.Time {
	t.Helper()
	a, err := s.GetAgent(agentID)
	if err != nil || a == nil {
		t.Fatalf("reading agent %s: %v, %v", agentID, a, err)
	}
	return a.LastSeenAt
}

// Identify finds the session by vendor and session ID, or creates it with
// its link, and always creates a new agent in it, seen now.
func TestIdentifyAgentFindsOrCreatesTheSessionAndCreatesAnAgent(t *testing.T) {
	s := newTestStore(t)
	before := time.Now().Add(-time.Second)
	orchestrator := identify(t, s, "3da2c294", "orchestrator")
	if orchestrator.ID == "" || orchestrator.SessionID == "" || orchestrator.Role != "orchestrator" ||
		orchestrator.Model != "claude-opus-5-5" || orchestrator.Provider != models.ProviderAnthropic {
		t.Fatalf("identified agent = %+v", orchestrator)
	}
	if orchestrator.LastSeenAt.Before(before) || orchestrator.Stale {
		t.Fatalf("a new agent was last seen %v (stale %v), want now and live", orchestrator.LastSeenAt, orchestrator.Stale)
	}
	var vendor, machine, resume string
	if err := s.db.QueryRow(`SELECT vendor, machine, resume_command FROM sessions WHERE id = ?`, orchestrator.SessionID).
		Scan(&vendor, &machine, &resume); err != nil {
		t.Fatal(err)
	}
	if vendor != "claude_code" || machine != "bilal-mbp" || resume != "claude --resume 3da2c294" {
		t.Fatalf("session = %s, %s, %s", vendor, machine, resume)
	}

	// The same session again: a second agent in it, and the session keeps
	// the link it was created with.
	implementer, err := s.IdentifyAgent(models.IdentifyAgentRequest{
		Vendor: "claude_code", VendorSessionID: "3da2c294", Machine: "other-machine",
		Role: "implementer", Model: "claude-sonnet", Provider: models.ProviderAnthropic,
	})
	if err != nil {
		t.Fatal(err)
	}
	if implementer.SessionID != orchestrator.SessionID || implementer.ID == orchestrator.ID {
		t.Fatalf("second agent %+v, want a new agent in session %s", implementer, orchestrator.SessionID)
	}
	if err := s.db.QueryRow(`SELECT machine FROM sessions WHERE id = ?`, orchestrator.SessionID).Scan(&machine); err != nil || machine != "bilal-mbp" {
		t.Fatalf("session machine = %q (%v), want bilal-mbp kept", machine, err)
	}

	// Another session ID is another session.
	other := identify(t, s, "b10d8a02", "orchestrator")
	if other.SessionID == orchestrator.SessionID {
		t.Fatal("a different vendor session ID found the same session")
	}
	var sessions int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&sessions); err != nil || sessions != 2 {
		t.Fatalf("sessions = %d (%v), want 2", sessions, err)
	}
}

func TestIdentifyAgentRefusesWhatIsMissingOrUnknown(t *testing.T) {
	s := newTestStore(t)
	ok := models.IdentifyAgentRequest{Vendor: "codex", VendorSessionID: "x1", Role: "implementer", Model: "gpt", Provider: models.ProviderOpenAI}
	for _, tc := range []struct {
		name string
		edit func(*models.IdentifyAgentRequest)
		want string
	}{
		{"no vendor", func(r *models.IdentifyAgentRequest) { r.Vendor = " " }, "vendor is required"},
		{"no session", func(r *models.IdentifyAgentRequest) { r.VendorSessionID = "" }, "vendorSessionId is required"},
		{"no role", func(r *models.IdentifyAgentRequest) { r.Role = "" }, "role is required"},
		{"no model", func(r *models.IdentifyAgentRequest) { r.Model = "" }, "model is required"},
		{"unknown provider", func(r *models.IdentifyAgentRequest) { r.Provider = "acme" }, `provider "acme" is not a provider`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := ok
			tc.edit(&req)
			_, err := s.IdentifyAgent(req)
			wantInvalidContaining(t, err, tc.want)
		})
	}
	var n int
	if err := s.db.QueryRow(`SELECT (SELECT COUNT(*) FROM sessions) + (SELECT COUNT(*) FROM agents)`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("refused identifies wrote %d rows (%v)", n, err)
	}
}

// TouchAgent moves last seen to now; every write an agent makes touches it
// the same way. An unknown agent is refused.
func TestTouchAgentUpdatesLastSeen(t *testing.T) {
	s := newTestStore(t)
	a := identify(t, s, "3da2c294", "implementer")
	setLastSeen(t, s, a.ID, time.Hour)
	if got, _ := s.GetAgent(a.ID); !got.Stale {
		t.Fatal("an agent unseen for an hour is not stale")
	}
	if err := s.TouchAgent(a.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetAgent(a.ID)
	if time.Since(got.LastSeenAt) > time.Minute || got.Stale {
		t.Fatalf("after a touch the agent was last seen %v (stale %v), want now", got.LastSeenAt, got.Stale)
	}
	wantInvalidContaining(t, s.TouchAgent("no-such-agent"), `"no-such-agent" is not an agent`)
	wantInvalidContaining(t, s.TouchAgent(""), "agentId is required")
}

// An agent's entry, and a note it handles, touch it.
func TestAgentEntriesTouchTheAgent(t *testing.T) {
	f := newEntriesFixture(t)
	setLastSeen(t, f.s, f.agent, time.Hour)
	mustCreateEntry(t, f.s, models.CreateEntryRequest{EntryOwner: f.onTicket(), Type: models.EntryLearning, Text: "WAL needs two stores", AgentID: f.agent})
	if time.Since(lastSeen(t, f.s, f.agent)) > time.Minute {
		t.Fatal("writing an entry did not touch the agent")
	}

	note := mustCreateEntry(t, f.s, models.CreateEntryRequest{EntryOwner: f.onTicket(), Type: models.EntryNote, Text: "Keep it small", AuthorName: "Bilal"})
	setLastSeen(t, f.s, f.agent, time.Hour)
	if _, err := f.s.MarkNoteHandled(note.ID, f.agent); err != nil {
		t.Fatal(err)
	}
	if time.Since(lastSeen(t, f.s, f.agent)) > time.Minute {
		t.Fatal("handling a note did not touch the agent")
	}
}

// The store decides staleness from last seen and the install's stale
// threshold, stored in the database: 30m by default. A change made through
// one store is what another store on the same file uses.
func TestStoreDecidesStalenessFromLastSeenAndTheSetting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.db")
	open := func() *Store {
		database, err := OpenAt(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { database.Close() })
		return NewStore(database)
	}
	s, other := open(), open()
	if got, err := s.AgentSettings(); err != nil || got.StaleAfter != 30*time.Minute || got.Lease != 2*time.Hour {
		t.Fatalf("default settings = %+v, %v; want 30m and 2h", got, err)
	}
	a := identify(t, s, "3da2c294", "implementer")
	for _, tc := range []struct {
		ago   time.Duration
		stale bool
	}{{29 * time.Minute, false}, {31 * time.Minute, true}} {
		setLastSeen(t, s, a.ID, tc.ago)
		if got, _ := s.GetAgent(a.ID); got.Stale != tc.stale {
			t.Errorf("unseen for %v: stale = %v, want %v", tc.ago, got.Stale, tc.stale)
		}
	}

	five := 5 * time.Minute
	got, err := other.UpdateAgentSettings(models.UpdateAgentSettingsRequest{StaleAfter: &five})
	if err != nil || got.StaleAfter != five || got.Lease != 2*time.Hour {
		t.Fatalf("UpdateAgentSettings = %+v, %v; want 5m and the lease kept", got, err)
	}
	setLastSeen(t, s, a.ID, 6*time.Minute)
	if got, _ := s.GetAgent(a.ID); !got.Stale {
		t.Error("unseen for 6m after the threshold was set to 5m by another store: not stale")
	}

	for _, bad := range []time.Duration{0, -time.Minute, 1500 * time.Millisecond} {
		_, err := s.UpdateAgentSettings(models.UpdateAgentSettingsRequest{Lease: &bad})
		wantInvalidContaining(t, err, "positive whole number of seconds")
	}
	// A lease shorter than the stale threshold would give a ticket back
	// before its agent counts as stale.
	short, long := 4*time.Minute, 3*time.Hour
	_, err = s.UpdateAgentSettings(models.UpdateAgentSettingsRequest{Lease: &short})
	wantInvalidContaining(t, err, "lease (4m) is shorter than stale-after (5m)")
	_, err = s.UpdateAgentSettings(models.UpdateAgentSettingsRequest{StaleAfter: &long})
	wantInvalidContaining(t, err, "lease (2h) is shorter than stale-after (3h)")
	if got, _ := s.AgentSettings(); got.StaleAfter != five || got.Lease != 2*time.Hour {
		t.Fatalf("refused updates changed the settings: %+v", got)
	}
}

// ListAgents reads every agent in one query and every currently held ticket
// in a second, grouping held tickets by agent in Go rather than querying per
// agent (Review 1 on ACP-11). This checks the grouping itself: an agent
// holding several tickets gets them in order, an agent holding none gets a
// non-nil empty slice, and each agent carries its own session.
func TestListAgentsGroupsHeldTicketsByAgent(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Agent Control Plane", "ACP")
	busy := identify(t, s, "busy", "implementer")
	idle := identify(t, s, "idle", "implementer")

	first := seedTicket(t, s, p.ID, "First")
	second := seedTicket(t, s, p.ID, "Second")
	mustClaim(t, s, first.ID, busy.ID)
	mustClaim(t, s, second.ID, busy.ID)

	items, err := s.ListAgents()
	if err != nil {
		t.Fatal(err)
	}
	byID := make(map[string]models.AgentListItem, len(items))
	for _, item := range items {
		byID[item.Agent.ID] = item
	}

	busyItem, ok := byID[busy.ID]
	if !ok {
		t.Fatalf("busy agent missing from %+v", items)
	}
	if len(busyItem.HeldTickets) != 2 || busyItem.HeldTickets[0].Key != "ACP-1" || busyItem.HeldTickets[1].Key != "ACP-2" {
		t.Fatalf("busy agent's held tickets = %+v, want ACP-1 then ACP-2", busyItem.HeldTickets)
	}
	if busyItem.Session.Vendor != "claude_code" || busyItem.Session.ID != busy.SessionID {
		t.Fatalf("busy agent's session = %+v", busyItem.Session)
	}

	idleItem, ok := byID[idle.ID]
	if !ok {
		t.Fatalf("idle agent missing from %+v", items)
	}
	if idleItem.HeldTickets == nil || len(idleItem.HeldTickets) != 0 {
		t.Fatalf("idle agent's held tickets = %#v, want a non-nil empty slice", idleItem.HeldTickets)
	}
}

func wantHistoryNote(t *testing.T, s *Store, ticketID, from, to, contains string) {
	t.Helper()
	changes, err := s.ListStatusChanges(ticketID)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) == 0 {
		t.Fatal("no status history")
	}
	c := changes[0]
	if c.FromStatus != from || c.ToStatus != to || !strings.Contains(c.Note, contains) {
		t.Fatalf("latest status change = %s -> %s %q, want %s -> %s with a note containing %q",
			c.FromStatus, c.ToStatus, c.Note, from, to, contains)
	}
}
