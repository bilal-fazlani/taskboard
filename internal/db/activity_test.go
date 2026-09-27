package db

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/tcarac/taskboard/internal/models"
)

// activityFixture is a project with three tickets, one in each of two epics
// and one in none, plus a second project whose changes must never show.
type activityFixture struct {
	s       *Store
	project *models.Project
	graph   *models.Epic
	views   *models.Epic
}

// insertChange writes one status change stamped at a given time, so a test
// controls the order independently of the order rows are written in.
func insertChange(t *testing.T, s *Store, ticketID, from, to, note string, at time.Time) {
	t.Helper()
	if _, err := s.db.Exec(
		`INSERT INTO ticket_status_changes (id, ticket_id, from_status, to_status, note, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		newID(), ticketID, from, to, note, stamp(at),
	); err != nil {
		t.Fatal(err)
	}
}

func seedActivity(t *testing.T) activityFixture {
	t.Helper()
	s := newTestStore(t)
	p := seedProject(t, s, "Agent Control Plane", "ACP")
	graph := seedEpic(t, s, "ACP", "Graph")
	views := seedEpic(t, s, "ACP", "Views")
	a := seedTicketInEpic(t, s, p.ID, "Graph", "Layout") // ACP-1
	b := seedTicketInEpic(t, s, p.ID, "Views", "Table")  // ACP-2
	c := seedTicket(t, s, p.ID, "Loose end")             // ACP-3
	other := seedProject(t, s, "Other", "OTH")
	x := seedTicket(t, s, other.ID, "Elsewhere") // OTH-1

	// Every change below is later than the three creations. They are
	// written out of time order on purpose.
	base := time.Now().Add(time.Hour)
	insertChange(t, s, a.ID, "in_progress", "agent_review", "a2", base.Add(4*time.Minute))
	insertChange(t, s, a.ID, "todo", "in_progress", "a1", base.Add(1*time.Minute))
	insertChange(t, s, b.ID, "todo", "in_progress", "b1", base.Add(3*time.Minute))
	insertChange(t, s, c.ID, "todo", "in_progress", "c1", base.Add(2*time.Minute))
	insertChange(t, s, x.ID, "todo", "in_progress", "x1", base.Add(5*time.Minute))
	// Two changes within the same instant: the one written later is newer.
	tie := base.Add(6 * time.Minute)
	insertChange(t, s, b.ID, "in_progress", "done", "b2", tie)
	insertChange(t, s, c.ID, "in_progress", "done", "c2", tie)
	return activityFixture{s: s, project: p, graph: graph, views: views}
}

// activityLines renders a page as "KEY note", or "KEY created" for a
// ticket's creation, so a whole order compares in one line.
func activityLines(page models.ActivityPage) string {
	lines := make([]string, len(page.Entries))
	for i, e := range page.Entries {
		what := e.Note
		if e.FromStatus == "" {
			what = "created"
		}
		lines[i] = e.TicketKey + " " + what
	}
	return strings.Join(lines, ", ")
}

func listActivity(t *testing.T, s *Store, project string, epics []string, before string, limit int) models.ActivityPage {
	t.Helper()
	page, err := s.ListActivity(project, epics, before, limit)
	if err != nil {
		t.Fatalf("ListActivity(%q, %v, %q, %d): %v", project, epics, before, limit, err)
	}
	return page
}

// The feed holds the project's changes and creations, newest first by when
// they happened rather than when they were written, ties broken by write
// order, and nothing from another project. Each entry carries its ticket's
// key, title and epic.
func TestListActivityNewestFirst(t *testing.T) {
	f := seedActivity(t)
	page := listActivity(t, f.s, "acp", nil, "", ActivityDefaultLimit)

	want := "ACP-3 c2, ACP-2 b2, ACP-1 a2, ACP-2 b1, ACP-3 c1, ACP-1 a1, ACP-3 created, ACP-2 created, ACP-1 created"
	if got := activityLines(page); got != want {
		t.Fatalf("activity = %s\nwant       %s", got, want)
	}
	if page.HasMore || page.NextBefore != "" {
		t.Fatalf("a page holding everything says hasMore %v nextBefore %q", page.HasMore, page.NextBefore)
	}

	first := page.Entries[0]
	if first.TicketTitle != "Loose end" || first.Epic != nil || first.FromStatus != "in_progress" || first.ToStatus != "done" {
		t.Fatalf("first entry = %+v", first)
	}
	a2 := page.Entries[2]
	if a2.TicketTitle != "Layout" || a2.Epic == nil || a2.Epic.ID != f.graph.ID || a2.Epic.Name != "Graph" {
		t.Fatalf("ACP-1's entry = %+v, want its title and epic", a2)
	}
}

// Pages follow on from each other through nextBefore without gaps or
// repeats, across the tie too.
func TestListActivityPages(t *testing.T) {
	f := seedActivity(t)
	var seen []string
	before := ""
	for i := 0; ; i++ {
		page := listActivity(t, f.s, f.project.ID, nil, before, 2)
		if len(page.Entries) > 2 {
			t.Fatalf("page %d has %d entries, want at most 2", i, len(page.Entries))
		}
		seen = append(seen, activityLines(page))
		if !page.HasMore {
			if page.NextBefore != "" {
				t.Fatalf("last page carries nextBefore %q", page.NextBefore)
			}
			break
		}
		if page.NextBefore == "" {
			t.Fatalf("page %d has more but no nextBefore", i)
		}
		before = page.NextBefore
	}
	want := "ACP-3 c2, ACP-2 b2 | ACP-1 a2, ACP-2 b1 | ACP-3 c1, ACP-1 a1 | ACP-3 created, ACP-2 created | ACP-1 created"
	if got := strings.Join(seen, " | "); got != want {
		t.Fatalf("pages = %s\nwant    %s", got, want)
	}
}

// The epic filter takes names in any case, ids and "none", several at once
// matching any of them, and pages within the filter. A name that matches no
// epic matches nothing.
func TestListActivityEpicFilter(t *testing.T) {
	f := seedActivity(t)
	cases := []struct {
		name  string
		epics []string
		want  string
	}{
		{"by name", []string{"graph"}, "ACP-1 a2, ACP-1 a1, ACP-1 created"},
		{"by id", []string{f.views.ID}, "ACP-2 b2, ACP-2 b1, ACP-2 created"},
		{"no epic", []string{"NONE"}, "ACP-3 c2, ACP-3 c1, ACP-3 created"},
		{"several", []string{"Graph", "none"}, "ACP-3 c2, ACP-1 a2, ACP-3 c1, ACP-1 a1, ACP-3 created, ACP-1 created"},
		{"unknown and known", []string{"Nope", "Views"}, "ACP-2 b2, ACP-2 b1, ACP-2 created"},
		{"unknown only", []string{"Nope"}, ""},
		{"blank is no filter", []string{" "}, "ACP-3 c2, ACP-2 b2, ACP-1 a2, ACP-2 b1, ACP-3 c1, ACP-1 a1, ACP-3 created, ACP-2 created, ACP-1 created"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := activityLines(listActivity(t, f.s, "ACP", c.epics, "", ActivityDefaultLimit)); got != c.want {
				t.Fatalf("activity = %s\nwant       %s", got, c.want)
			}
		})
	}

	// Another project's epic of the same name is not this project's.
	seedEpic(t, f.s, "OTH", "Elsewhere")
	if got := activityLines(listActivity(t, f.s, "ACP", []string{"Elsewhere"}, "", ActivityDefaultLimit)); got != "" {
		t.Fatalf("another project's epic matched %s", got)
	}

	first := listActivity(t, f.s, "ACP", []string{"none", "graph"}, "", 4)
	if !first.HasMore {
		t.Fatalf("filtered first page says no more: %s", activityLines(first))
	}
	rest := listActivity(t, f.s, "ACP", []string{"none", "graph"}, first.NextBefore, 4)
	if got := activityLines(rest); got != "ACP-3 created, ACP-1 created" || rest.HasMore {
		t.Fatalf("filtered second page = %s (hasMore %v)", got, rest.HasMore)
	}
}

// A project with no tickets has an empty feed; a deleted ticket's changes
// leave it with the ticket.
func TestListActivityEmptyAndDeleted(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Quiet", "QT")
	page := listActivity(t, s, "QT", nil, "", ActivityDefaultLimit)
	if page.Entries == nil || len(page.Entries) != 0 || page.HasMore {
		t.Fatalf("empty project's feed = %+v, want an empty list", page)
	}

	tk := seedTicket(t, s, p.ID, "Short-lived")
	if _, err := s.MoveTicket(tk.ID, models.MoveTicketRequest{Status: models.StatusInProgress, Note: "started"}); err != nil {
		t.Fatal(err)
	}
	if got := activityLines(listActivity(t, s, "QT", nil, "", ActivityDefaultLimit)); got != "QT-1 started, QT-1 created" {
		t.Fatalf("feed = %s", got)
	}
	if err := s.DeleteTicket(tk.ID); err != nil {
		t.Fatal(err)
	}
	if got := activityLines(listActivity(t, s, "QT", nil, "", ActivityDefaultLimit)); got != "" {
		t.Fatalf("feed after delete = %s, want empty", got)
	}
}

// A walk through the feed carries on when the ticket whose change the cursor
// was taken from is deleted in between: the next page starts right after
// where the last one ended, with nothing skipped and nothing repeated. The
// same holds for an epic-filtered walk.
func TestListActivityPagesPastADeletedCursor(t *testing.T) {
	for _, c := range []struct {
		name  string
		epics []string
		first string
		rest  []string
	}{
		{
			name:  "whole feed",
			first: "ACP-3 c2, ACP-2 b2",
			rest:  []string{"ACP-1 a2, ACP-3 c1", "ACP-1 a1, ACP-3 created", "ACP-1 created"},
		},
		{
			name:  "filtered by epic",
			epics: []string{"Views", "none"},
			first: "ACP-3 c2, ACP-2 b2",
			rest:  []string{"ACP-3 c1, ACP-3 created"},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := seedActivity(t)
			page := listActivity(t, f.s, "ACP", c.epics, "", 2)
			if got := activityLines(page); got != c.first || !page.HasMore {
				t.Fatalf("first page = %s (hasMore %v), want %s", got, page.HasMore, c.first)
			}
			// ACP-2 owns the page's oldest entry, which the cursor came from.
			if err := f.s.DeleteTicket(page.Entries[1].TicketID); err != nil {
				t.Fatal(err)
			}
			var rest []string
			for before := page.NextBefore; before != ""; before = page.NextBefore {
				page = listActivity(t, f.s, "ACP", c.epics, before, 2)
				rest = append(rest, activityLines(page))
			}
			if got, want := strings.Join(rest, " | "), strings.Join(c.rest, " | "); got != want {
				t.Fatalf("pages after the delete = %s\nwant                    %s", got, want)
			}
		})
	}
}

// An unknown project, a before that is no nextBefore, or a limit out of
// range is the caller's mistake.
func TestListActivityRejectsBadInput(t *testing.T) {
	f := seedActivity(t)
	aChangeID := listActivity(t, f.s, "ACP", nil, "", 1).Entries[0].ID
	cases := []struct {
		name, project, before string
		limit                 int
		want                  string
	}{
		{"unknown project", "NOPE", "", 10, "project not found"},
		{"zero limit", "ACP", "", 0, fmt.Sprintf("between 1 and %d", ActivityMaxLimit)},
		{"limit too big", "ACP", "", ActivityMaxLimit + 1, fmt.Sprintf("between 1 and %d", ActivityMaxLimit)},
		{"garbage before", "ACP", "not a cursor!", 10, "is not a nextBefore from this feed"},
		{"a change id", "ACP", aChangeID, 10, "is not a nextBefore from this feed"},
		{"no rowid", "ACP", base64.RawURLEncoding.EncodeToString([]byte("2026-09-27T10:00:00.000000000Z|x")), 10, "is not a nextBefore from this feed"},
		{"no time", "ACP", base64.RawURLEncoding.EncodeToString([]byte("|12")), 10, "is not a nextBefore from this feed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := f.s.ListActivity(c.project, nil, c.before, c.limit)
			var invalid *ErrInvalidInput
			if !errors.As(err, &invalid) || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error = %v, want an ErrInvalidInput containing %q", err, c.want)
			}
		})
	}
}
