package db

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/tcarac/taskboard/internal/models"
)

// nowAt is the instant the Now tests read the board at.
var nowAt = time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)

// seedStatusTicket creates a ticket in a status, as the status history
// records it: one row, to that status.
func seedStatusTicket(t *testing.T, s *Store, projectID, title, status string) *models.Ticket {
	t.Helper()
	tk, err := s.CreateTicket(models.CreateTicketRequest{ProjectID: projectID, Title: title, Status: status})
	if err != nil {
		t.Fatalf("seeding ticket %q: %v", title, err)
	}
	return tk
}

func moveThrough(t *testing.T, s *Store, id string, statuses ...string) {
	t.Helper()
	for _, st := range statuses {
		if _, err := s.MoveTicket(id, models.MoveTicketRequest{Status: st}); err != nil {
			t.Fatalf("moving %s to %s: %v", id, st, err)
		}
	}
}

// setChangeTimes backdates a ticket's status history: its rows, oldest
// first, get the times given, in order.
func setChangeTimes(t *testing.T, s *Store, ticketID string, times ...time.Time) {
	t.Helper()
	rows, err := s.db.Query(`SELECT rowid FROM ticket_status_changes WHERE ticket_id = ? ORDER BY rowid`, ticketID)
	if err != nil {
		t.Fatal(err)
	}
	var rowids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		rowids = append(rowids, id)
	}
	rows.Close()
	if len(rowids) != len(times) {
		t.Fatalf("ticket %s has %d status changes, got %d times", ticketID, len(rowids), len(times))
	}
	for i, id := range rowids {
		if _, err := s.db.Exec(`UPDATE ticket_status_changes SET created_at = ? WHERE rowid = ?`, stamp(times[i]), id); err != nil {
			t.Fatal(err)
		}
	}
}

// seedReview attaches a document to a ticket, saved at the time given.
func seedReview(t *testing.T, s *Store, ticketID, name, content string, at time.Time) {
	t.Helper()
	d, err := s.CreateDocument(models.CreateDocumentRequest{TicketID: ticketID, Name: name, Content: content})
	if err != nil {
		t.Fatalf("seeding document %q: %v", name, err)
	}
	if _, err := s.db.Exec(`UPDATE documents SET created_at = ?, updated_at = ? WHERE id = ?`, stamp(at), stamp(at), d.ID); err != nil {
		t.Fatal(err)
	}
}

func readNow(t *testing.T, s *Store, project string) *models.Now {
	t.Helper()
	n, err := s.Now(project, nowAt)
	if err != nil {
		t.Fatalf("Now(%q): %v", project, err)
	}
	return n
}

func nowKeys(tickets []models.NowTicket) []string {
	keys := []string{}
	for _, t := range tickets {
		keys = append(keys, t.Key)
	}
	return keys
}

func TestNowGroupsTicketsInProgressAndInReview(t *testing.T) {
	s := newTestStore(t)
	acp := seedProject(t, s, "Control plane", "ACP")
	ldr := seedProject(t, s, "Ledger", "LDR")

	// ACP-1: in progress for two hours, one of three subtasks done.
	a1 := seedStatusTicket(t, s, acp.ID, "Running", models.StatusInProgress)
	setChangeTimes(t, s, a1.ID, nowAt.Add(-2*time.Hour))
	for i, done := range []bool{true, false, false} {
		st, err := s.AddSubtask(a1.ID, models.CreateSubtaskRequest{Title: fmt.Sprintf("step %d", i)})
		if err != nil {
			t.Fatal(err)
		}
		if done {
			if _, err := s.SetSubtaskState(st.ID, true); err != nil {
				t.Fatal(err)
			}
		}
	}
	// ACP-2: back from its first review an hour ago. Its time runs from
	// that entry, not from its first start.
	a2 := seedTicket(t, s, acp.ID, "Bounced")
	moveThrough(t, s, a2.ID, models.StatusInProgress, models.StatusAgentReview, models.StatusInProgress)
	setChangeTimes(t, s, a2.ID, nowAt.Add(-5*time.Hour), nowAt.Add(-4*time.Hour), nowAt.Add(-3*time.Hour), nowAt.Add(-time.Hour))
	// Neither todo nor done tickets are shown.
	seedTicket(t, s, acp.ID, "Waiting")
	seedStatusTicket(t, s, acp.ID, "Finished long ago", models.StatusDone)
	// LDR-1: in review for half an hour.
	l1 := seedStatusTicket(t, s, ldr.ID, "Reviewing", models.StatusAgentReview)
	setChangeTimes(t, s, l1.ID, nowAt.Add(-30*time.Minute))

	n := readNow(t, s, "")
	if got := nowKeys(n.InProgress); strings.Join(got, ",") != "ACP-1,ACP-2" {
		t.Fatalf("in progress = %v, want ACP-1 then ACP-2 (longest running first)", got)
	}
	if got := nowKeys(n.InReview); strings.Join(got, ",") != "LDR-1" {
		t.Fatalf("in review = %v, want LDR-1", got)
	}
	first := n.InProgress[0]
	if first.SubtasksDone != 1 || first.SubtasksTotal != 3 {
		t.Errorf("ACP-1 subtasks = %d/%d, want 1/3", first.SubtasksDone, first.SubtasksTotal)
	}
	if !first.Since.Equal(nowAt.Add(-2*time.Hour)) || first.ReviewRounds != 0 || first.Review != "" {
		t.Errorf("ACP-1 = %+v, want since 2h ago, no review rounds and no review state", first)
	}
	if first.Title != "Running" || first.ProjectPrefix != "ACP" || first.Status != models.StatusInProgress || first.ID != a1.ID {
		t.Errorf("ACP-1 = %+v", first)
	}
	bounced := n.InProgress[1]
	if !bounced.Since.Equal(nowAt.Add(-time.Hour)) || bounced.ReviewRounds != 1 || bounced.SubtasksTotal != 0 {
		t.Errorf("ACP-2 = %+v, want since 1h ago (its latest entry into in_progress), one review round, no subtasks", bounced)
	}
	review := n.InReview[0]
	if !review.Since.Equal(nowAt.Add(-30*time.Minute)) || review.ReviewRounds != 1 || review.Review != models.NowReviewRunning {
		t.Errorf("LDR-1 = %+v, want since 30m ago, round 1, review running", review)
	}

	// A project narrows every group; an unknown one matches nothing.
	n = readNow(t, s, "acp")
	if len(n.InProgress) != 2 || len(n.InReview) != 0 {
		t.Errorf("ACP only: in progress %v, in review %v", nowKeys(n.InProgress), nowKeys(n.InReview))
	}
	n = readNow(t, s, "NOPE")
	if n.InProgress == nil || n.InReview == nil || n.Landed == nil || len(n.InProgress)+len(n.InReview)+len(n.Landed) != 0 {
		t.Errorf("unknown project = %+v, want empty (not nil) lists", n)
	}
}

func TestNowTellsARunningReviewFromADecidedOne(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Control plane", "ACP")
	entered := nowAt.Add(-time.Hour)
	review := func(title string) *models.Ticket {
		tk := seedStatusTicket(t, s, p.ID, title, models.StatusAgentReview)
		setChangeTimes(t, s, tk.ID, entered)
		return tk
	}

	review("No report yet")
	approved := review("Approved")
	seedReview(t, s, approved.ID, "Review 1", "VERDICT: APPROVE\n\nClean.", entered.Add(10*time.Minute))
	changes := review("Changes")
	seedReview(t, s, changes.ID, "Review 1", "  verdict: changes\n\n- **major** it breaks", entered.Add(10*time.Minute))
	// Its only review came before its latest entry into review: an earlier
	// round's, so this round's is still running.
	stale := seedStatusTicket(t, s, p.ID, "Earlier round", models.StatusInProgress)
	moveThrough(t, s, stale.ID, models.StatusAgentReview, models.StatusInProgress, models.StatusAgentReview)
	setChangeTimes(t, s, stale.ID, nowAt.Add(-4*time.Hour), nowAt.Add(-3*time.Hour), nowAt.Add(-2*time.Hour), entered)
	seedReview(t, s, stale.ID, "Review 1", "VERDICT: CHANGES", nowAt.Add(-150*time.Minute))
	// The highest round is the latest review, whenever the others were saved.
	rounds := review("Two rounds")
	seedReview(t, s, rounds.ID, "Review 2", "VERDICT: APPROVE", entered.Add(5*time.Minute))
	seedReview(t, s, rounds.ID, "Review 1", "VERDICT: CHANGES", entered.Add(20*time.Minute))
	// Other documents, and a review with no verdict, decide nothing.
	other := review("Other documents")
	seedReview(t, s, other.ID, "Review notes", "VERDICT: APPROVE", entered.Add(5*time.Minute))
	seedReview(t, s, other.ID, "Review 1", "Still reading.", entered.Add(5*time.Minute))
	// A ticket in progress has no review state, whatever its documents say.
	working := seedStatusTicket(t, s, p.ID, "Working", models.StatusInProgress)
	seedReview(t, s, working.ID, "Review 1", "VERDICT: APPROVE", nowAt)

	want := map[string]string{
		"No report yet":   models.NowReviewRunning,
		"Approved":        models.NowReviewApproved,
		"Changes":         models.NowReviewChanges,
		"Earlier round":   models.NowReviewRunning,
		"Two rounds":      models.NowReviewApproved,
		"Other documents": models.NowReviewRunning,
	}
	n := readNow(t, s, "")
	if len(n.InReview) != len(want) {
		t.Fatalf("in review = %v, want %d tickets", nowKeys(n.InReview), len(want))
	}
	for _, tk := range n.InReview {
		if tk.Review != want[tk.Title] {
			t.Errorf("%s review = %q, want %q", tk.Title, tk.Review, want[tk.Title])
		}
	}
	if len(n.InProgress) != 1 || n.InProgress[0].Review != "" {
		t.Errorf("in progress = %+v, want one ticket with no review state", n.InProgress)
	}
}

func TestNowLandedIsTheLastDayNewestFirstAtMostTen(t *testing.T) {
	s := newTestStore(t)
	acp := seedProject(t, s, "Control plane", "ACP")
	ldr := seedProject(t, s, "Ledger", "LDR")

	// ACP-1 … ACP-12 landed 1 … 12 hours ago.
	var landed []*models.Ticket
	for i := 1; i <= 12; i++ {
		tk := seedTicket(t, s, acp.ID, fmt.Sprintf("Landed %d", i))
		moveThrough(t, s, tk.ID, models.StatusDone)
		setChangeTimes(t, s, tk.ID, nowAt.Add(-48*time.Hour), nowAt.Add(-time.Duration(i)*time.Hour))
		landed = append(landed, tk)
	}
	if _, err := s.UpdateTicket(landed[0].ID, models.UpdateTicketRequest{Delivery: &models.DeliveryUpdate{
		LandedCommits: &[]models.LandedCommit{{SHA: "BBBBBBB", Repo: "acme/app"}, {SHA: "aaaaaaa", Repo: "acme/app"}},
	}}); err != nil {
		t.Fatal(err)
	}
	// Landed a day and an hour ago: too old.
	old := seedStatusTicket(t, s, acp.ID, "Too old", models.StatusDone)
	setChangeTimes(t, s, old.ID, nowAt.Add(-25*time.Hour))
	// Moved to done half an hour ago, then reopened: not landed.
	reopened := seedStatusTicket(t, s, acp.ID, "Reopened", models.StatusDone)
	moveThrough(t, s, reopened.ID, models.StatusInProgress)
	setChangeTimes(t, s, reopened.ID, nowAt.Add(-30*time.Minute), nowAt.Add(-20*time.Minute))
	// Done two days ago, reopened and done again ten minutes ago: its latest
	// move counts.
	again := seedStatusTicket(t, s, ldr.ID, "Done again", models.StatusDone)
	moveThrough(t, s, again.ID, models.StatusInProgress, models.StatusDone)
	setChangeTimes(t, s, again.ID, nowAt.Add(-50*time.Hour), nowAt.Add(-49*time.Hour), nowAt.Add(-10*time.Minute))

	n := readNow(t, s, "")
	var keys []string
	for _, l := range n.Landed {
		keys = append(keys, l.Key)
	}
	want := "LDR-1,ACP-1,ACP-2,ACP-3,ACP-4,ACP-5,ACP-6,ACP-7,ACP-8,ACP-9"
	if strings.Join(keys, ",") != want {
		t.Fatalf("landed = %v, want %s", keys, want)
	}
	if !n.Landed[0].DoneAt.Equal(nowAt.Add(-10 * time.Minute)) {
		t.Errorf("LDR-1 done at %v, want its latest move to done", n.Landed[0].DoneAt)
	}
	first := n.Landed[1]
	if len(first.Commits) != 2 || first.Commits[0].SHA != "bbbbbbb" || first.Commits[1].SHA != "aaaaaaa" || first.Commits[0].Repo != "acme/app" {
		t.Errorf("ACP-1 commits = %+v, want bbbbbbb then aaaaaaa in acme/app", first.Commits)
	}
	if first.Title != "Landed 1" || first.ProjectPrefix != "ACP" || first.ID != landed[0].ID {
		t.Errorf("ACP-1 = %+v", first)
	}
	if n.Landed[2].Commits == nil || len(n.Landed[2].Commits) != 0 {
		t.Errorf("ACP-2 commits = %#v, want an empty list", n.Landed[2].Commits)
	}
	if got := nowKeys(n.InProgress); strings.Join(got, ",") != "ACP-14" {
		t.Errorf("in progress = %v, want the reopened ticket", got)
	}

	n = readNow(t, s, "LDR")
	if len(n.Landed) != 1 || n.Landed[0].Key != "LDR-1" {
		t.Errorf("LDR only: landed = %+v", n.Landed)
	}

	// The empty lists go out as [], never null.
	s2 := newTestStore(t)
	data, err := json.Marshal(readNow(t, s2, ""))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"inProgress":[],"waiting":[],"inReview":[],"landed":[]}` {
		t.Errorf("empty board = %s", data)
	}
}

// Each ticket waiting on the person carries its open request's type, prompt
// and time, oldest wait first; no other ticket carries one, and its JSON
// leaves the field out.
func TestNowWaitingCarriesEachRequestOldestFirst(t *testing.T) {
	f := newClaimFixture(t)
	second := seedTicket(t, f.s, f.project.ID, "Reviews as entries")
	working := seedTicket(t, f.s, f.project.ID, "Still being written")
	for _, id := range []string{f.ticket.ID, second.ID, working.ID} {
		mustClaim(t, f.s, id, f.first.ID)
	}
	mustCreateRequest(t, f.s, models.CreateUserInputRequest{TicketID: f.ticket.ID, AgentID: f.first.ID,
		Type: models.UserInputApproval, Prompt: "Delete the Decisions document?\nNothing else goes."})
	mustCreateRequest(t, f.s, models.CreateUserInputRequest{TicketID: second.ID, AgentID: f.first.ID,
		Type: models.UserInputQuestion, Prompt: "Round only, or the finding too?", Choices: []string{"Round", "Both"}})
	// ACP-2 has waited since an hour ago, ACP-1 since ten minutes ago.
	setChangeTimes(t, f.s, f.ticket.ID, nowAt.Add(-3*time.Hour), nowAt.Add(-2*time.Hour), nowAt.Add(-10*time.Minute))
	setChangeTimes(t, f.s, second.ID, nowAt.Add(-3*time.Hour), nowAt.Add(-2*time.Hour), nowAt.Add(-time.Hour))

	n := readNow(t, f.s, "")
	if got := nowKeys(n.Waiting); strings.Join(got, ",") != "ACP-2,ACP-1" {
		t.Fatalf("waiting = %v, want ACP-2 then ACP-1 (oldest wait first)", got)
	}
	q, a := n.Waiting[0].Request, n.Waiting[1].Request
	if q == nil || q.Type != models.UserInputQuestion || q.Prompt != "Round only, or the finding too?" || q.CreatedAt.IsZero() {
		t.Errorf("ACP-2's request = %+v, want the question", q)
	}
	if a == nil || a.Type != models.UserInputApproval || a.Prompt != "Delete the Decisions document?\nNothing else goes." {
		t.Errorf("ACP-1's request = %+v, want the approval, its whole prompt", a)
	}
	if len(n.InProgress) != 1 || n.InProgress[0].Request != nil {
		t.Fatalf("in progress = %+v, want ACP-3 with no request", n.InProgress)
	}
	data, err := json.Marshal(n.InProgress[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"request"`) {
		t.Errorf("a ticket not waiting names a request: %s", data)
	}

	// Answered, the ticket leaves the waiting group, request and all.
	reqs, err := f.s.ListRequests(second.ID)
	if err != nil || len(reqs) != 1 {
		t.Fatalf("ListRequests: %+v, %v", reqs, err)
	}
	if _, err := f.s.AnswerRequest(reqs[0].ID, "Round", "Bilal", ""); err != nil {
		t.Fatal(err)
	}
	if got := nowKeys(readNow(t, f.s, "").Waiting); strings.Join(got, ",") != "ACP-1" {
		t.Errorf("waiting after ACP-2's answer = %v, want ACP-1", got)
	}
}

func TestReviewRoundAndVerdict(t *testing.T) {
	for name, want := range map[string]int{"Review 1": 1, "review 12": 12, "REVIEW 3": 3, "Review": 0, "Review 0": 0, "Review notes": 0, "Reviews 1": 0, "Review 1b": 0} {
		got, ok := reviewRound(name)
		if got != want || ok != (want > 0) {
			t.Errorf("reviewRound(%q) = %d, %v; want %d", name, got, ok, want)
		}
	}
	for head, want := range map[string]string{
		"VERDICT: APPROVE\n\n":       models.NowReviewApproved,
		"\n verdict: approve":        models.NowReviewApproved,
		"VERDICT: CHANGES":           models.NowReviewChanges,
		"Verdict - approve":          "",
		"# Review\nVERDICT: APPROVE": "",
	} {
		if got := reviewVerdict(head); got != want {
			t.Errorf("reviewVerdict(%q) = %q, want %q", head, got, want)
		}
	}
}
