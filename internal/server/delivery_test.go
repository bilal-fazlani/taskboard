package server

import (
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/tcarac/taskboard/internal/models"
)

// PUT /api/tickets/{id} sets the delivery fields, several commits across two
// repos included, and GET reads them back; a PUT without delivery, or with
// only one of its fields, leaves the rest alone.
func TestUpdateTicketSetsDelivery(t *testing.T) {
	r := serve(t)
	project := doJSON[models.Project](t, http.MethodPost, r.url+"/api/projects", map[string]string{
		"name": "Billing", "prefix": "BILL",
	})
	ticket := doJSON[models.Ticket](t, http.MethodPost, r.url+"/api/tickets", map[string]any{
		"projectId": project.ID, "title": "Invoice export", "repos": []string{"acme/api", "acme/web"},
	})
	if ticket.Delivery != nil {
		t.Fatalf("new ticket delivery = %+v, want none", ticket.Delivery)
	}
	url := r.url + "/api/tickets/" + ticket.ID

	updated := rawUpdate(t, url, `{"delivery":{
		"branch":"bill-1-invoice","worktree":"/w/bill-1","prUrl":"https://github.com/acme/api/pull/7",
		"landedCommits":[
			{"sha":"6BAFA19","repo":"acme/api"},
			{"sha":"a198cc5","repo":"acme/web"},
			{"sha":"77aa0bc","repo":"acme/api"}
		]}}`)
	want := &models.Delivery{
		Branch: "bill-1-invoice", Worktree: "/w/bill-1", PRURL: "https://github.com/acme/api/pull/7",
		LandedCommits: []models.LandedCommit{
			{SHA: "6bafa19", Repo: "acme/api"},
			{SHA: "a198cc5", Repo: "acme/web"},
			{SHA: "77aa0bc", Repo: "acme/api"},
		},
	}
	if !reflect.DeepEqual(updated.Delivery, want) {
		t.Fatalf("PUT answered delivery %+v, want %+v", updated.Delivery, want)
	}

	rawUpdate(t, url, `{"title":"Invoice export v2"}`)
	rawUpdate(t, url, `{"delivery":{"branch":""}}`)
	want.Branch = ""
	if got := doJSON[models.Ticket](t, http.MethodGet, url, nil); !reflect.DeepEqual(got.Delivery, want) {
		t.Fatalf("GET delivery = %+v, want %+v", got.Delivery, want)
	}

	// A bad sha is a 400 that applies nothing.
	body, status := errorBody(t, http.MethodPut, url, `{"title":"Changed","delivery":{"landedCommits":[{"sha":"xyz","repo":"acme/api"}]}}`)
	if status != http.StatusBadRequest || !strings.Contains(body.Error, "not a commit sha") {
		t.Fatalf("PUT with a bad sha = %d %q, want a 400 about the sha", status, body.Error)
	}
	if got := doJSON[models.Ticket](t, http.MethodGet, url, nil); got.Title != "Invoice export v2" || !reflect.DeepEqual(got.Delivery, want) {
		t.Fatalf("after a rejected PUT: %q %+v, want nothing applied", got.Title, got.Delivery)
	}
}
