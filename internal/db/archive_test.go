package db

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/tcarac/taskboard/internal/imagedoc/imagedoctest"
	"github.com/tcarac/taskboard/internal/models"
)

// archiveFixture is two projects, GONE and KEEP, with something of every kind
// in GONE and links both ways between them. Tests delete GONE.
type archiveFixture struct {
	gone, keep *models.Project
	epic       *models.Epic
	// g1 is in progress, in the epic, labelled, with a subtask, delivery,
	// documents and an image; it depends on k2. g2 is todo, surfaced from
	// k2. g3 landed just now.
	g1, g2, g3 *models.Ticket
	// k1 depends on g2 and was surfaced from g1; k2 is what g1 depends on
	// and g2 was surfaced from.
	k1, k2              *models.Ticket
	subtask             *models.Subtask
	doc, epicDoc, image *models.Document
	label               string
}

const archiveSHA = "fe11a5cafe0000000000000000000000000000aa"

func seedArchive(t *testing.T, s *Store) archiveFixture {
	t.Helper()
	var f archiveFixture
	f.gone = seedProject(t, s, "Gone", "GONE")
	f.keep = seedProject(t, s, "Kept", "KEEP")
	f.epic = seedEpic(t, s, "GONE", "Plans")
	f.label = "shared"

	var err error
	f.k2 = seedTicket(t, s, f.keep.ID, "Kept dependency")
	f.g1, err = s.CreateTicket(models.CreateTicketRequest{
		ProjectID: "GONE", Title: "Gone in progress", Status: models.StatusInProgress,
		Epic: strPtr("Plans"), Labels: []string{f.label}, Repos: []string{"acme/app"},
		DependsOn: []models.DependencyInput{{Ticket: "KEEP-1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	f.g2, err = s.CreateTicket(models.CreateTicketRequest{ProjectID: "GONE", Title: "Gone todo", SurfacedFrom: strPtr("KEEP-1")})
	if err != nil {
		t.Fatal(err)
	}
	f.g3 = seedTicket(t, s, f.gone.ID, "Gone landed")
	moveTo(t, s, f.g3.ID, models.StatusDone)
	f.k1, err = s.CreateTicket(models.CreateTicketRequest{
		ProjectID: "KEEP", Title: "Kept waiting", Labels: []string{f.label},
		DependsOn: []models.DependencyInput{{Ticket: "GONE-2"}}, SurfacedFrom: strPtr("GONE-1"),
	})
	if err != nil {
		t.Fatal(err)
	}

	if f.subtask, err = s.AddSubtask(f.g1.ID, models.CreateSubtaskRequest{Title: "Step"}); err != nil {
		t.Fatal(err)
	}
	setDelivery(t, s, f.g1.ID, models.DeliveryUpdate{
		Branch:        strPtr("gone-branch"),
		LandedCommits: commitsPtr(models.LandedCommit{SHA: archiveSHA, Repo: "acme/app"}),
	})
	f.image = seedImage(t, s, DocumentOwner{TicketID: f.g1.ID}, "Shot", models.DocumentFormatPNG, imagedoctest.PNG(4, 4))
	if f.doc, err = s.CreateDocument(models.CreateDocumentRequest{
		TicketID: f.g1.ID, Name: "Page", Format: models.DocumentFormatHTML,
		Content: `<p>the needle is here</p><img src="Shot.png">`,
	}); err != nil {
		t.Fatal(err)
	}
	if img, err := s.ReferencedImage(f.doc.ID, "Shot.png"); err != nil || img == nil {
		t.Fatalf("before delete: ReferencedImage = %v, %v; want the image", img, err)
	}
	if f.epicDoc, err = s.CreateDocument(models.CreateDocumentRequest{EpicID: f.epic.ID, Name: "Decisions", Content: "needle"}); err != nil {
		t.Fatal(err)
	}
	appendEntry(t, s, "GONE", "Bilal", "gone entry")
	return f
}

// archiveTables are every table a project's things live in.
var archiveTables = []string{
	"projects", "tickets", "epics", "documents", "document_images", "document_search", "subtasks",
	"labels", "ticket_labels", "ticket_repos", "ticket_dependencies", "ticket_surfaced_from",
	"ticket_status_changes", "ticket_delivery", "ticket_landed_commits", "entries",
}

// rowCounts counts the rows of every table in archiveTables.
func rowCounts(t *testing.T, s *Store) map[string]int {
	t.Helper()
	counts := map[string]int{}
	for _, table := range archiveTables {
		counts[table] = countRows(t, s, "SELECT COUNT(*) FROM "+table)
	}
	return counts
}

func wantSameRows(t *testing.T, what string, before, after map[string]int) {
	t.Helper()
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("%s changed the rows: before %v, after %v", what, before, after)
	}
}

func wantNotFound(t *testing.T, what string, err error) {
	t.Helper()
	var invalid *ErrInvalidInput
	if !errors.As(err, &invalid) {
		t.Fatalf("%s: err = %v, want a not-found ErrInvalidInput", what, err)
	}
}

func deleteGone(t *testing.T, s *Store, f archiveFixture) {
	t.Helper()
	if err := s.DeleteProject(f.gone.ID); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}
}

func ticketIDs(tickets []models.Ticket) map[string]bool {
	ids := map[string]bool{}
	for _, tk := range tickets {
		ids[tk.ID] = true
	}
	return ids
}

func TestDeleteProjectArchivesAndRemovesNothing(t *testing.T) {
	s := newTestStore(t)
	f := seedArchive(t, s)
	before := rowCounts(t, s)

	deleteGone(t, s, f)

	wantSameRows(t, "DeleteProject", before, rowCounts(t, s))
	var status string
	if err := s.db.QueryRow("SELECT status FROM projects WHERE id = ?", f.gone.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != ProjectArchived {
		t.Fatalf("status after delete = %q, want %q", status, ProjectArchived)
	}

	assertInvalidInput(t, s.DeleteProject(f.gone.ID), `project not found: "`+f.gone.ID+`"`)
	assertInvalidInput(t, s.DeleteProject("01ARZ3NDEKTSV4RRFFQ69G5FAV"), `project not found: "01ARZ3NDEKTSV4RRFFQ69G5FAV"`)
	if p, err := s.GetProject(f.keep.ID); err != nil || p == nil {
		t.Fatalf("the other project after delete: %+v, %v", p, err)
	}
}

func TestDeletedProjectIsGoneFromEveryRead(t *testing.T) {
	s := newTestStore(t)
	f := seedArchive(t, s)
	gone := []string{f.g1.ID, f.g2.ID, f.g3.ID}

	// Before the delete every read below sees GONE, so each check after it
	// is known to test something.
	if all, err := s.ListTickets(models.TicketFilter{}); err != nil || !ticketIDs(all)[f.g1.ID] {
		t.Fatalf("before delete: ListTickets = %v, %v; want it to include GONE-1", all, err)
	}
	if now, err := s.Now("", time.Now()); err != nil || len(now.InProgress) != 1 || len(now.Landed) != 1 {
		t.Fatalf("before delete: Now = %+v, %v; want GONE-1 in progress and GONE-3 landed", now, err)
	}

	deleteGone(t, s, f)

	// Projects.
	projects, err := s.ListProjects()
	if err != nil || len(projects) != 1 || projects[0].ID != f.keep.ID {
		t.Fatalf("ListProjects = %+v, %v; want KEEP only", projects, err)
	}
	if p, err := s.GetProject(f.gone.ID); err != nil || p != nil {
		t.Fatalf("GetProject = %+v, %v; want nil, nil", p, err)
	}
	for _, ref := range []string{f.gone.ID, "GONE", "gone"} {
		_, err := s.ResolveProjectRef(ref)
		wantNotFound(t, "ResolveProjectRef "+ref, err)
	}

	// Tickets: list, page, board, ready, get, resolve, history.
	for _, filter := range []models.TicketFilter{{}, {ProjectID: "GONE"}, {Epic: "Plans"}, {Label: f.label}, {Repo: "acme/app"}} {
		tickets, err := s.ListTickets(filter)
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range gone {
			if ticketIDs(tickets)[id] {
				t.Fatalf("ListTickets(%+v) still has a ticket of GONE", filter)
			}
		}
	}
	if page, total, err := s.ListTicketsPage(models.TicketFilter{}, 50, 0); err != nil || total != 2 || len(page) != 2 {
		t.Fatalf("ListTicketsPage = %d tickets, total %d, %v; want KEEP's 2", len(page), total, err)
	}
	for _, ref := range []string{"", "GONE"} {
		board, err := s.GetBoard(ref)
		if err != nil {
			t.Fatal(err)
		}
		for _, col := range board.Columns {
			for _, id := range gone {
				if ticketIDs(col.Tickets)[id] {
					t.Fatalf("GetBoard(%q) column %s still has a ticket of GONE", ref, col.Status)
				}
			}
		}
	}
	for _, id := range gone {
		if tk, err := s.GetTicket(id); err != nil || tk != nil {
			t.Fatalf("GetTicket = %+v, %v; want nil, nil", tk, err)
		}
		if ok, err := s.TicketExists(id); err != nil || ok {
			t.Fatalf("TicketExists = %v, %v; want false", ok, err)
		}
		if h, err := s.ListStatusChanges(id); err != nil || len(h) != 0 {
			t.Fatalf("ListStatusChanges = %v, %v; want none", h, err)
		}
		_, err := s.ResolveTicketID(id)
		wantNotFound(t, "ResolveTicketID by id", err)
	}
	_, err = s.ResolveTicketID("GONE-1")
	wantNotFound(t, "ResolveTicketID GONE-1", err)

	// Search and find by commit.
	if ids, err := s.SearchDocumentTickets("needle", ""); err != nil || len(ids) != 0 {
		t.Fatalf("SearchDocumentTickets = %v, %v; want none", ids, err)
	}
	if found, err := s.FindTicketsByCommit(archiveSHA, ""); err != nil || len(found) != 0 {
		t.Fatalf("FindTicketsByCommit = %v, %v; want none", found, err)
	}

	// Epics.
	if e, err := s.GetEpic(f.epic.ID); err != nil || e != nil {
		t.Fatalf("GetEpic = %+v, %v; want nil, nil", e, err)
	}
	_, err = s.ListEpics("GONE")
	wantNotFound(t, "ListEpics", err)
	_, err = s.NoEpicProgress("GONE")
	wantNotFound(t, "NoEpicProgress", err)
	_, err = s.ResolveEpicRef("GONE", "Plans")
	wantNotFound(t, "ResolveEpicRef", err)

	// Documents.
	for _, d := range []*models.Document{f.doc, f.epicDoc, f.image} {
		if got, err := s.GetDocument(d.ID); err != nil || got != nil {
			t.Fatalf("GetDocument %s = %+v, %v; want nil, nil", d.Name, got, err)
		}
	}
	_, err = s.ListDocuments(DocumentOwner{TicketID: f.g1.ID})
	wantNotFound(t, "ListDocuments of a ticket", err)
	_, err = s.ListDocuments(DocumentOwner{EpicID: f.epic.ID})
	wantNotFound(t, "ListDocuments of an epic", err)
	if img, err := s.GetDocumentImage(f.image.ID); err != nil || img != nil {
		t.Fatalf("GetDocumentImage = %+v, %v; want nil, nil", img, err)
	}
	if img, err := s.GetDocumentThumbnail(f.image.ID); err != nil || img != nil {
		t.Fatalf("GetDocumentThumbnail = %+v, %v; want nil, nil", img, err)
	}
	if _, found, err := s.ImageUsage(f.image.ID); err != nil || found {
		t.Fatalf("ImageUsage found = %v, %v; want not found", found, err)
	}
	if img, err := s.ReferencedImage(f.doc.ID, "Shot.png"); err != nil || img != nil {
		t.Fatalf("ReferencedImage = %+v, %v; want nil, nil", img, err)
	}

	// Subtasks, journal, Now, activity, labels.
	if st, err := s.GetSubtask(f.subtask.ID); err != nil || st != nil {
		t.Fatalf("GetSubtask = %+v, %v; want nil, nil", st, err)
	}
	_, err = s.ListJournal("GONE", "", JournalDefaultLimit)
	wantNotFound(t, "ListJournal", err)
	_, err = s.ListActivity("GONE", nil, "", ActivityDefaultLimit)
	wantNotFound(t, "ListActivity", err)
	now, err := s.Now("", time.Now())
	if err != nil || len(now.InProgress)+len(now.InReview)+len(now.Landed) != 0 {
		t.Fatalf("Now = %+v, %v; want nothing moving", now, err)
	}
	if now, err := s.Now("GONE", time.Now()); err != nil || len(now.InProgress)+len(now.Landed) != 0 {
		t.Fatalf("Now(GONE) = %+v, %v; want nothing", now, err)
	}
	labels, err := s.ListLabels()
	if err != nil || len(labels) != 1 || labels[0].TicketCount != 1 {
		t.Fatalf("ListLabels = %+v, %v; want %q on KEEP's 1 ticket", labels, err, f.label)
	}
	updated, err := s.UpdateLabel(labels[0].ID, models.UpdateLabelRequest{Color: strPtr("#000000")})
	if err != nil || updated.TicketCount != 1 {
		t.Fatalf("UpdateLabel count = %+v, %v; want 1", updated, err)
	}
}

func TestLinksIntoDeletedProjectAreLeftOut(t *testing.T) {
	s := newTestStore(t)
	f := seedArchive(t, s)

	ready, err := s.ListTickets(models.TicketFilter{Ready: true})
	if err != nil {
		t.Fatal(err)
	}
	if ticketIDs(ready)[f.k1.ID] {
		t.Fatal("before delete: KEEP-2 is ready, want it held back by GONE-2")
	}

	deleteGone(t, s, f)

	k1, err := s.GetTicket(f.k1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(k1.DependsOn) != 0 || k1.SurfacedFrom != nil {
		t.Fatalf("KEEP-2 dependsOn = %v, surfacedFrom = %v; want neither", k1.DependsOn, k1.SurfacedFrom)
	}
	k2, err := s.GetTicket(f.k2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(k2.Blocks) != 0 || len(k2.Surfaced) != 0 {
		t.Fatalf("KEEP-1 blocks = %v, surfaced = %v; want neither", k2.Blocks, k2.Surfaced)
	}
	list, err := s.ListTickets(models.TicketFilter{ProjectID: "KEEP"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tk := range list {
		if len(tk.DependsOn) != 0 || tk.SurfacedFrom != nil {
			t.Fatalf("listed %s dependsOn = %v, surfacedFrom = %v; want neither", tk.DisplayKey(), tk.DependsOn, tk.SurfacedFrom)
		}
	}

	ready, err = s.ListTickets(models.TicketFilter{Ready: true})
	if err != nil {
		t.Fatal(err)
	}
	if !ticketIDs(ready)[f.k1.ID] {
		t.Fatal("after delete: KEEP-2 is not ready, want GONE-2 no longer to count")
	}

	// The link rows stay; replacing KEEP-2's dependencies keeps the one it
	// cannot see.
	if _, err := s.UpdateTicket(f.k1.ID, models.UpdateTicketRequest{DependsOn: []models.DependencyInput{{Ticket: "KEEP-1"}}}); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, s, "SELECT COUNT(*) FROM ticket_dependencies WHERE ticket_id = ?", f.k1.ID); n != 2 {
		t.Fatalf("KEEP-2 dependency rows = %d, want 2 (KEEP-1 and the hidden GONE-2)", n)
	}
	k1, err = s.GetTicket(f.k1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(k1.DependsOn) != 1 || k1.DependsOn[0].ID != f.k2.ID {
		t.Fatalf("KEEP-2 dependsOn = %v, want KEEP-1 only", k1.DependsOn)
	}
}

func TestWritesToDeletedProjectAreNotFound(t *testing.T) {
	s := newTestStore(t)
	f := seedArchive(t, s)
	deleteGone(t, s, f)
	before := rowCounts(t, s)

	if p, err := s.UpdateProject(f.gone.ID, models.UpdateProjectRequest{Name: strPtr("Back")}); err != nil || p != nil {
		t.Fatalf("UpdateProject = %+v, %v; want nil, nil", p, err)
	}
	_, err := s.CreateTicket(models.CreateTicketRequest{ProjectID: "GONE", Title: "New"})
	wantNotFound(t, "CreateTicket in GONE", err)
	_, err = s.CreateTicket(models.CreateTicketRequest{ProjectID: "KEEP", Title: "New", DependsOn: []models.DependencyInput{{Ticket: "GONE-2"}}})
	wantNotFound(t, "CreateTicket depending on GONE-2", err)
	_, err = s.CreateTicket(models.CreateTicketRequest{ProjectID: "KEEP", Title: "New", SurfacedFrom: strPtr(f.g1.ID)})
	wantNotFound(t, "CreateTicket surfaced from GONE-1", err)
	_, err = s.UpdateTicket(f.k2.ID, models.UpdateTicketRequest{Epic: strPtr(f.epic.ID)})
	wantNotFound(t, "UpdateTicket into GONE's epic", err)
	if tk, err := s.UpdateTicket(f.g1.ID, models.UpdateTicketRequest{Title: strPtr("Changed")}); err != nil || tk != nil {
		t.Fatalf("UpdateTicket = %+v, %v; want nil, nil", tk, err)
	}
	if tk, err := s.MoveTicket(f.g1.ID, models.MoveTicketRequest{Status: models.StatusDone}); err != nil || tk != nil {
		t.Fatalf("MoveTicket = %+v, %v; want nil, nil", tk, err)
	}
	wantNotFound(t, "DeleteTicket", s.DeleteTicket(f.g1.ID))

	_, err = s.AddSubtask(f.g1.ID, models.CreateSubtaskRequest{Title: "More"})
	wantNotFound(t, "AddSubtask", err)
	_, err = s.ToggleSubtask(f.subtask.ID)
	wantNotFound(t, "ToggleSubtask", err)
	_, err = s.SetSubtaskState(f.subtask.ID, true)
	wantNotFound(t, "SetSubtaskState", err)
	wantNotFound(t, "DeleteSubtask", s.DeleteSubtask(f.subtask.ID))

	_, err = s.CreateEpic(models.CreateEpicRequest{ProjectID: "GONE", Name: "More"})
	wantNotFound(t, "CreateEpic", err)
	if e, err := s.UpdateEpic(f.epic.ID, models.UpdateEpicRequest{Name: strPtr("Changed")}); err != nil || e != nil {
		t.Fatalf("UpdateEpic = %+v, %v; want nil, nil", e, err)
	}
	_, err = s.DeleteEpic(f.epic.ID)
	wantNotFound(t, "DeleteEpic", err)

	_, err = s.CreateDocument(models.CreateDocumentRequest{TicketID: f.g1.ID, Name: "More", Content: "x"})
	wantNotFound(t, "CreateDocument on a ticket", err)
	_, err = s.CreateDocument(models.CreateDocumentRequest{EpicID: f.epic.ID, Name: "More", Content: "x"})
	wantNotFound(t, "CreateDocument on an epic", err)
	_, err = s.CreateImageDocument(models.CreateImageRequest{TicketID: f.g1.ID, Name: "More", Format: models.DocumentFormatPNG, Data: imagedoctest.PNG(4, 4)})
	wantNotFound(t, "CreateImageDocument", err)
	if d, err := s.UpdateDocument(f.doc.ID, models.UpdateDocumentRequest{Content: strPtr("changed")}); err != nil || d != nil {
		t.Fatalf("UpdateDocument content = %+v, %v; want nil, nil", d, err)
	}
	if d, err := s.UpdateDocument(f.image.ID, models.UpdateDocumentRequest{Name: strPtr("Renamed")}); err != nil || d != nil {
		t.Fatalf("UpdateDocument rename = %+v, %v; want nil, nil", d, err)
	}
	if d, err := s.ReplaceDocumentImage(f.image.ID, imagedoctest.PNG(6, 6), nil); err != nil || d != nil {
		t.Fatalf("ReplaceDocumentImage = %+v, %v; want nil, nil", d, err)
	}
	wantNotFound(t, "DeleteDocument", s.DeleteDocument(f.doc.ID))
	_, err = s.DeleteDocumentReportingUse(f.image.ID)
	wantNotFound(t, "DeleteDocumentReportingUse", err)

	_, err = s.AppendJournalEntry("GONE", models.AppendJournalEntryRequest{Author: "Bilal", Text: "more"})
	wantNotFound(t, "AppendJournalEntry", err)

	wantSameRows(t, "the refused writes", before, rowCounts(t, s))
	var title, docContent string
	var completed bool
	if err := s.db.QueryRow("SELECT t.title, st.completed FROM tickets t JOIN subtasks st ON st.ticket_id = t.id WHERE t.id = ?", f.g1.ID).
		Scan(&title, &completed); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow("SELECT content FROM documents WHERE id = ?", f.doc.ID).Scan(&docContent); err != nil {
		t.Fatal(err)
	}
	if title != f.g1.Title || completed || docContent != f.doc.Content {
		t.Fatalf("GONE-1 after the refused writes: title %q, subtask completed %v, document %q; want them unchanged", title, completed, docContent)
	}
}

func TestUpdateProjectRefusesStatusOtherThanActive(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	const msg = `a project's status can only be "active"; to archive a project, delete it`

	for _, status := range []string{ProjectArchived, "", "paused"} {
		_, err := s.UpdateProject(p.ID, models.UpdateProjectRequest{Status: strPtr(status), Name: strPtr("Renamed")})
		var invalid *ErrInvalidInput
		if !errors.As(err, &invalid) || !strings.Contains(invalid.Msg, msg) {
			t.Fatalf("UpdateProject status %q: err = %v, want an ErrInvalidInput pointing at delete", status, err)
		}
	}
	got, err := s.GetProject(p.ID)
	if err != nil || got.Status != ProjectActive || got.Name != "Billing" {
		t.Fatalf("project after refused updates = %+v, %v; want it unchanged", got, err)
	}
	if got, err := s.UpdateProject(p.ID, models.UpdateProjectRequest{Status: strPtr(ProjectActive)}); err != nil || got.Status != ProjectActive {
		t.Fatalf("UpdateProject status active = %+v, %v", got, err)
	}
}

// An update that read the project before a delete landed must not write it
// back to active: the write matches only a live project.
func TestStaleProjectUpdateCannotUnarchive(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Gone", "GONE")
	stale, err := s.GetProject(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteProject(p.ID); err != nil {
		t.Fatal(err)
	}

	stale.Name = "Back"
	written, err := writeProject(s.db, stale)
	if err != nil || written {
		t.Fatalf("writeProject with a pre-delete read = %v, %v; want false, nil", written, err)
	}
	var name, status string
	if err := s.db.QueryRow("SELECT name, status FROM projects WHERE id = ?", p.ID).Scan(&name, &status); err != nil {
		t.Fatal(err)
	}
	if status != ProjectArchived || name != "Gone" {
		t.Fatalf("project after a stale update: name %q, status %q; want it untouched and archived", name, status)
	}
	if got, err := s.UpdateProject(p.ID, models.UpdateProjectRequest{Name: strPtr("Back"), Status: strPtr(ProjectActive)}); err != nil || got != nil {
		t.Fatalf("UpdateProject after delete = %+v, %v; want nil, nil", got, err)
	}
	if err := s.db.QueryRow("SELECT status FROM projects WHERE id = ?", p.ID).Scan(&status); err != nil || status != ProjectArchived {
		t.Fatalf("status after UpdateProject = %q, %v; want archived", status, err)
	}
}

func TestDeletedProjectKeepsItsPrefix(t *testing.T) {
	s := newTestStore(t)
	gone := seedProject(t, s, "Gone", "GONE")
	other := seedProject(t, s, "Other", "OTH")
	if err := s.DeleteProject(gone.ID); err != nil {
		t.Fatal(err)
	}

	_, err := s.CreateProject(models.CreateProjectRequest{Name: "Again", Prefix: "GONE"})
	assertInvalidInput(t, err, `project prefix "GONE" is already used by a deleted project`)
	_, err = s.CreateProject(models.CreateProjectRequest{Name: "Again", Prefix: "gone"})
	assertInvalidInput(t, err, `project prefix "gone" is already used by a deleted project as "GONE"; prefixes must differ by more than letter case`)
	_, err = s.UpdateProject(other.ID, models.UpdateProjectRequest{Prefix: strPtr("GONE")})
	assertInvalidInput(t, err, `project prefix "GONE" is already used by a deleted project`)
}
