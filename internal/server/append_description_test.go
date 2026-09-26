package server

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/tcarac/taskboard/internal/models"
)

// PUT /api/tickets/{id} with appendDescription adds a paragraph and leaves the
// existing text, and every field it does not name, as they were.
func TestUpdateTicketAppendsToDescription(t *testing.T) {
	r := serve(t)
	project := doJSON[models.Project](t, http.MethodPost, r.url+"/api/projects", map[string]string{
		"name": "Billing", "prefix": "BILL",
	})
	ticket := doJSON[models.Ticket](t, http.MethodPost, r.url+"/api/tickets", map[string]any{
		"projectId": project.ID, "title": "Invoice export", "description": "The first draft\n", "priority": "high",
	})
	url := r.url + "/api/tickets/" + ticket.ID

	updated := rawUpdate(t, url, `{"appendDescription":"Landed in: abc123"}`)
	want := "The first draft\n\nLanded in: abc123"
	if updated.Description != want || updated.Title != "Invoice export" || updated.Priority != "high" {
		t.Fatalf("ticket = %q/%q/%q, want the description %q and the rest unchanged",
			updated.Title, updated.Description, updated.Priority, want)
	}
	stored := doJSON[models.Ticket](t, http.MethodGet, url, nil)
	if stored.Description != want {
		t.Fatalf("stored description = %q, want %q", stored.Description, want)
	}

	// Asking for both a replacement and an append, or appending nothing, is
	// the caller's mistake: a 400 that applies nothing.
	for _, body := range []string{
		`{"description":"New","appendDescription":"More"}`,
		`{"appendDescription":"","title":"Changed"}`,
	} {
		errBody, status := errorBody(t, http.MethodPut, url, body)
		if status != http.StatusBadRequest || !strings.Contains(errBody.Error, "appendDescription") {
			t.Fatalf("PUT %s = %d %q, want a 400 naming appendDescription", body, status, errBody.Error)
		}
	}
	if again := doJSON[models.Ticket](t, http.MethodGet, url, nil); again.Description != want || again.Title != "Invoice export" {
		t.Fatalf("ticket after rejected appends = %q/%q, want nothing applied", again.Title, again.Description)
	}
}

// An append is a ticket change like any other, so open boards hear about it.
func TestEventsFireOnAppend(t *testing.T) {
	r := serve(t)
	project := doJSON[models.Project](t, http.MethodPost, r.url+"/api/projects", map[string]string{
		"name": "Billing", "prefix": "BILL",
	})
	ticket := doJSON[models.Ticket](t, http.MethodPost, r.url+"/api/tickets", map[string]any{
		"projectId": project.ID, "title": "Invoice export",
	})

	stream := openStream(t, context.Background(), r.url)
	// Let the signals for the setup writes arrive and go, so the one below
	// can only come from the append.
	for quiet := false; !quiet; {
		select {
		case <-stream.messages:
		case <-time.After(100 * time.Millisecond):
			quiet = true
		}
	}

	rawUpdate(t, r.url+"/api/tickets/"+ticket.ID, `{"appendDescription":"Worktree: /w"}`)
	stream.expect(t, "changed {}")
}
