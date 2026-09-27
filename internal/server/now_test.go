package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/tcarac/taskboard/internal/models"
)

// GET /api/now answers the Now page in one request, across projects unless
// projectId narrows it.
func TestHTTPNow(t *testing.T) {
	base, store := newHistoryServer(t)
	acp, err := store.CreateProject(models.CreateProjectRequest{Name: "Control plane", Prefix: "ACP"})
	if err != nil {
		t.Fatal(err)
	}
	ldr, err := store.CreateProject(models.CreateProjectRequest{Name: "Ledger", Prefix: "LDR"})
	if err != nil {
		t.Fatal(err)
	}
	create := func(projectID, title, status string) *models.Ticket {
		tk, err := store.CreateTicket(models.CreateTicketRequest{ProjectID: projectID, Title: title, Status: status})
		if err != nil {
			t.Fatal(err)
		}
		return tk
	}
	working := create(acp.ID, "Working", models.StatusInProgress)
	st, err := store.AddSubtask(working.ID, models.CreateSubtaskRequest{Title: "one"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetSubtaskState(st.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddSubtask(working.ID, models.CreateSubtaskRequest{Title: "two"}); err != nil {
		t.Fatal(err)
	}
	reviewed := create(ldr.ID, "Reviewed", models.StatusAgentReview)
	if _, err := store.CreateDocument(models.CreateDocumentRequest{TicketID: reviewed.ID, Name: "Review 1", Content: "VERDICT: APPROVE\n"}); err != nil {
		t.Fatal(err)
	}
	shipped := create(acp.ID, "Shipped", models.StatusDone)
	if _, err := store.UpdateTicket(shipped.ID, models.UpdateTicketRequest{Delivery: &models.DeliveryUpdate{
		LandedCommits: &[]models.LandedCommit{{SHA: "39a07fd", Repo: "acme/app"}},
	}}); err != nil {
		t.Fatal(err)
	}
	create(acp.ID, "Waiting", models.StatusTodo)

	get := func(query string) models.Now {
		t.Helper()
		code, body := send(t, http.MethodGet, base+"/api/now"+query, "")
		if code != http.StatusOK {
			t.Fatalf("GET /api/now%s: %d %s", query, code, body)
		}
		var n models.Now
		if err := json.Unmarshal(body, &n); err != nil {
			t.Fatalf("decoding %s: %v", body, err)
		}
		return n
	}

	n := get("")
	if len(n.InProgress) != 1 || n.InProgress[0].Key != "ACP-1" || n.InProgress[0].SubtasksDone != 1 || n.InProgress[0].SubtasksTotal != 2 {
		t.Errorf("in progress = %+v, want ACP-1 at 1/2", n.InProgress)
	}
	if len(n.InReview) != 1 || n.InReview[0].Key != "LDR-1" || n.InReview[0].Review != models.NowReviewApproved || n.InReview[0].ReviewRounds != 1 {
		t.Errorf("in review = %+v, want LDR-1 approved in round 1", n.InReview)
	}
	if len(n.Landed) != 1 || n.Landed[0].Key != "ACP-2" || len(n.Landed[0].Commits) != 1 || n.Landed[0].Commits[0].SHA != "39a07fd" {
		t.Errorf("landed = %+v, want ACP-2 with 39a07fd", n.Landed)
	}

	n = get("?projectId=ldr")
	if len(n.InProgress) != 0 || len(n.InReview) != 1 || len(n.Landed) != 0 {
		t.Errorf("LDR only = %+v", n)
	}

	code, body := send(t, http.MethodGet, base+"/api/now?projectId=NOPE", "")
	if code != http.StatusOK || string(body) != "{\"inProgress\":[],\"waiting\":[],\"inReview\":[],\"landed\":[]}\n" {
		t.Errorf("unknown project: %d %s", code, body)
	}
}
