package db

import (
	"reflect"
	"testing"

	"github.com/tcarac/taskboard/internal/models"
)

// CountOpenNotes and models.Entry.Open are one rule written twice, in SQL
// and in Go: over notes in every state (open, handled, replaced, a replaced
// note's open revision, a handled revision) and entries that are not notes,
// on every kind of owner, the count is the number of entries Open() says
// are open.
func TestOpenNoteCountAgreesWithEntryOpen(t *testing.T) {
	f := newEntriesFixture(t)
	for kind, owner := range f.owners() {
		t.Run(kind, func(t *testing.T) {
			note := func(text, replaces string) *models.Entry {
				return mustCreateEntry(t, f.s, models.CreateEntryRequest{EntryOwner: owner, Type: models.EntryNote,
					Text: text, AuthorName: "Bilal", Replaces: replaces})
			}
			count := func() int {
				t.Helper()
				n, err := f.s.CountOpenNotes(owner)
				if err != nil {
					t.Fatal(err)
				}
				all, err := f.s.ListEntries(owner, models.EntryFilter{IncludeReplaced: true}, "", EntryMaxLimit)
				if err != nil {
					t.Fatal(err)
				}
				open := 0
				for _, e := range all.Entries {
					if e.Open() {
						open++
					}
				}
				if n != open {
					t.Fatalf("CountOpenNotes = %d, but Open() holds for %d of %v", n, open, all.Entries)
				}
				return n
			}

			if got := count(); got != 0 {
				t.Fatalf("open notes on a fresh %s = %d, want 0", kind, got)
			}
			mustCreateEntry(t, f.s, f.learning(owner, "Not a note."))
			open := note("Open.", "")
			handled := note("Handled.", "")
			if _, err := f.s.MarkNoteHandled(handled.ID, f.agent); err != nil {
				t.Fatal(err)
			}
			replaced := note("Replaced.", "")
			revision := note("The revision, open.", replaced.ID)
			if got := count(); got != 2 {
				t.Fatalf("open notes = %d, want 2 (%s and %s)", got, open.ID, revision.ID)
			}
			if _, err := f.s.MarkNoteHandled(revision.ID, f.agent); err != nil {
				t.Fatal(err)
			}
			if got := count(); got != 1 {
				t.Fatalf("open notes after handling the revision = %d, want 1", got)
			}
		})
	}
}

// A read of a ticket carries its current entries, its open notes counted,
// and its epic's and project's open notes counted but not read; the epic's
// count stays zero for a ticket with no epic, and a ticket with nothing
// carries nothing.
func TestTicketEntriesCarryCurrentEntriesAndOpenNoteCounts(t *testing.T) {
	f := newEntriesFixture(t)
	if got, err := f.s.TicketEntries(f.ticket); err != nil || !reflect.DeepEqual(got, models.TicketEntries{}) {
		t.Fatalf("a fresh ticket's entries = %+v, %v; want nothing", got, err)
	}
	personNote := func(owner models.EntryOwner, text string) *models.Entry {
		return mustCreateEntry(t, f.s, models.CreateEntryRequest{EntryOwner: owner, Type: models.EntryNote, Text: text, AuthorName: "Bilal"})
	}
	old := mustCreateEntry(t, f.s, f.learning(f.onTicket(), "Old."))
	current := mustCreateEntry(t, f.s, models.CreateEntryRequest{EntryOwner: f.onTicket(), Type: models.EntryLearning,
		Text: "New.", AgentID: f.agent, Replaces: old.ID})
	ticketNote := personNote(f.onTicket(), "Mind the port.")
	personNote(f.onEpic(), "Epic note.")
	personNote(f.onProject(), "Project note one.")
	personNote(f.onProject(), "Project note two.")
	mustCreateEntry(t, f.s, f.learning(f.onEpic(), "Not on the ticket."))

	// Without an epic first: the epic's note is not counted.
	got, err := f.s.TicketEntries(f.ticket)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{ticketNote.ID, current.ID}; got.Entries == nil || len(got.Entries.Entries) != 2 ||
		got.Entries.Entries[0].ID != want[0] || got.Entries.Entries[1].ID != want[1] || got.Entries.Total != 2 {
		t.Fatalf("ticket entries = %+v, want %v", got.Entries, want)
	}
	if got.OpenNotes != 1 || got.EpicOpenNotes != 0 || got.ProjectOpenNotes != 2 {
		t.Fatalf("counts = ticket %d, epic %d, project %d; want 1, 0, 2", got.OpenNotes, got.EpicOpenNotes, got.ProjectOpenNotes)
	}

	epicID := f.epic.ID
	inEpic, err := f.s.UpdateTicket(f.ticket.ID, models.UpdateTicketRequest{Epic: &epicID})
	if err != nil {
		t.Fatal(err)
	}
	got, err = f.s.TicketEntries(inEpic)
	if err != nil {
		t.Fatal(err)
	}
	if got.EpicOpenNotes != 1 {
		t.Fatalf("epic open notes = %d, want 1", got.EpicOpenNotes)
	}
}

// DocumentTicketID names a ticket document's ticket, and nothing for an
// epic's document or an unknown one.
func TestDocumentTicketID(t *testing.T) {
	f := newEntriesFixture(t)
	onTicket, err := f.s.CreateDocument(models.CreateDocumentRequest{TicketID: f.ticket.ID, Name: "Plan", Content: "x"})
	if err != nil {
		t.Fatal(err)
	}
	onEpic, err := f.s.CreateDocument(models.CreateDocumentRequest{EpicID: f.epic.ID, Name: "Spec", Content: "x"})
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{onTicket.ID: f.ticket.ID, onEpic.ID: "", "nope": ""} {
		if got, err := f.s.DocumentTicketID(id); err != nil || got != want {
			t.Fatalf("DocumentTicketID(%s) = %q, %v; want %q", id, got, err, want)
		}
	}
}
