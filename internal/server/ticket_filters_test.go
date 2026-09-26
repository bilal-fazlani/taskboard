package server

import (
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/tcarac/taskboard/internal/models"
)

// listTitles GETs /api/tickets with the raw query and returns the titles,
// sorted.
func listTitles(t *testing.T, r *running, query string) []string {
	t.Helper()
	tickets := doJSON[[]models.Ticket](t, http.MethodGet, r.url+"/api/tickets"+query, nil)
	titles := make([]string, len(tickets))
	for i, tk := range tickets {
		titles[i] = tk.Title
	}
	sort.Strings(titles)
	return titles
}

func assertListed(t *testing.T, r *running, query string, want ...string) {
	t.Helper()
	got := listTitles(t, r, query)
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("GET /api/tickets%s = %v, want %v", query, got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("GET /api/tickets%s = %v, want %v", query, got, want)
		}
	}
}

// GET /api/tickets takes several statuses as repeated params, ready=true
// and excludeLabel, alone and with the existing filters, and a single
// ?status= still works.
func TestListTicketsHTTPFilters(t *testing.T) {
	r := serve(t)
	store := r.srv.store
	p, err := store.CreateProject(models.CreateProjectRequest{Name: "Billing", Prefix: "BILL"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.CreateProject(models.CreateProjectRequest{Name: "Search", Prefix: "SRCH"})
	if err != nil {
		t.Fatal(err)
	}
	create := func(projectID, title, status string, labels []string, dependsOn ...string) *models.Ticket {
		t.Helper()
		tk, err := store.CreateTicket(models.CreateTicketRequest{
			ProjectID: projectID, Title: title, Status: status, Priority: "high", Labels: labels, DependsOn: models.DependOn(dependsOn...),
		})
		if err != nil {
			t.Fatal(err)
		}
		return tk
	}
	finished := create(p.ID, "finished", models.StatusDone, nil)
	doing := create(p.ID, "doing", models.StatusInProgress, nil)
	create(p.ID, "reviewing", models.StatusAgentReview, nil)
	create(p.ID, "startable", models.StatusTodo, []string{"api"}, finished.ID)
	create(p.ID, "waits on doing", models.StatusTodo, nil, doing.ID)
	create(p.ID, "held", models.StatusTodo, []string{"hold"})
	create(other.ID, "elsewhere", models.StatusTodo, nil)

	assertListed(t, r, "?status=done", "finished")
	assertListed(t, r, "?status=in_progress&status=agent_review", "doing", "reviewing")
	assertListed(t, r, "?projectId=BILL&status=todo&status=in_progress&status=agent_review",
		"doing", "reviewing", "startable", "waits on doing", "held")
	assertListed(t, r, "?ready=true", "startable", "held", "elsewhere")
	assertListed(t, r, "?ready=true&excludeLabel=HOLD", "startable", "elsewhere")
	assertListed(t, r, "?projectId=bill&ready=true&excludeLabel=hold&label=api&priority=high", "startable")
	assertListed(t, r, "?ready=true&status=in_progress")
	assertListed(t, r, "?ready=false&status=todo&excludeLabel=hold", "startable", "waits on doing", "elsewhere")
	assertListed(t, r, "?excludeLabel=hold&epic=none&projectId=BILL",
		"finished", "doing", "reviewing", "startable", "waits on doing")
	// A single comma-separated value splits the same as repeated params,
	// matching the CLI's --status.
	assertListed(t, r, "?status=in_progress,agent_review", "doing", "reviewing")

	if _, status := errorBody(t, http.MethodGet, r.url+"/api/tickets?ready=maybe", ""); status != http.StatusBadRequest {
		t.Fatalf("?ready=maybe status = %d, want 400", status)
	}

	// A mistyped status is a 400 naming the allowed values, not a silent
	// empty list.
	body, status := errorBody(t, http.MethodGet, r.url+"/api/tickets?status=in-progress", "")
	if status != http.StatusBadRequest {
		t.Fatalf("?status=in-progress status = %d, want 400", status)
	}
	if !strings.Contains(body.Error, `"in-progress"`) {
		t.Fatalf("?status=in-progress error = %q, want it to name %q", body.Error, "in-progress")
	}
	for _, st := range models.Statuses {
		if !strings.Contains(body.Error, st) {
			t.Fatalf("?status=in-progress error = %q, want it to name %q", body.Error, st)
		}
	}

	// A mistyped status paired with an unknown project is still the error,
	// not the unknown project's own "matches nothing".
	body, status = errorBody(t, http.MethodGet, r.url+"/api/tickets?projectId=NOPE&status=in-progress", "")
	if status != http.StatusBadRequest {
		t.Fatalf("?projectId=NOPE&status=in-progress status = %d, want 400", status)
	}
	if !strings.Contains(body.Error, `"in-progress"`) {
		t.Fatalf("?projectId=NOPE&status=in-progress error = %q, want it to name %q", body.Error, "in-progress")
	}
}
