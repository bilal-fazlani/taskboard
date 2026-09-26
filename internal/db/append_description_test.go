package db

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/tcarac/taskboard/internal/models"
)

// Appending keeps every byte already there and adds the text as a new
// paragraph, or as the whole description when there was none.
func TestUpdateTicketAppendsToDescription(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")

	cases := []struct {
		name, existing, text, want string
	}{
		{"empty description", "", "Worktree: /tmp/w", "Worktree: /tmp/w"},
		{"no trailing newline", "The plan.", "Review: APPROVE", "The plan.\n\nReview: APPROVE"},
		{"one trailing newline", "The plan.\n", "Review: APPROVE", "The plan.\n\nReview: APPROVE"},
		{"already a blank line", "The plan.\n\n", "Review: APPROVE", "The plan.\n\nReview: APPROVE"},
		{"text kept as given", "  Indented **markdown**  ", "- a\n- b\n", "  Indented **markdown**  \n\n- a\n- b\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tk, err := s.CreateTicket(models.CreateTicketRequest{ProjectID: p.ID, Title: c.name, Description: c.existing})
			if err != nil {
				t.Fatal(err)
			}
			got, err := s.UpdateTicket(tk.ID, models.UpdateTicketRequest{AppendDescription: strPtr(c.text)})
			if err != nil {
				t.Fatal(err)
			}
			if got.Description != c.want {
				t.Fatalf("description = %q, want %q", got.Description, c.want)
			}
			stored, _ := s.GetTicket(tk.ID)
			if stored.Description != c.want {
				t.Fatalf("stored description = %q, want %q", stored.Description, c.want)
			}
		})
	}
}

// An append rides along with the rest of an update, like any other field.
func TestUpdateTicketAppendsAlongsideOtherFields(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	tk, err := s.CreateTicket(models.CreateTicketRequest{ProjectID: p.ID, Title: "Invoice", Description: "Draft"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.UpdateTicket(tk.ID, models.UpdateTicketRequest{
		AppendDescription: strPtr("Landed"), Status: strPtr("done"), Priority: strPtr("low"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Description != "Draft\n\nLanded" || got.Status != "done" || got.Priority != "low" {
		t.Fatalf("ticket = %q/%q/%q", got.Description, got.Status, got.Priority)
	}
}

// Replace and append at once has no single sensible meaning, and an empty
// append is almost certainly a mistake; both are refused and apply nothing.
func TestUpdateTicketRejectsBadAppends(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	tk, err := s.CreateTicket(models.CreateTicketRequest{ProjectID: p.ID, Title: "Invoice", Description: "Draft"})
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		req  models.UpdateTicketRequest
		msg  string
	}{
		{"both", models.UpdateTicketRequest{Description: strPtr("New"), AppendDescription: strPtr("More"), Title: strPtr("Changed")},
			"pass description or appendDescription, not both"},
		{"empty", models.UpdateTicketRequest{AppendDescription: strPtr(""), Title: strPtr("Changed")},
			"appendDescription is empty: pass the text to add"},
		{"blank", models.UpdateTicketRequest{AppendDescription: strPtr(" \n\t"), Title: strPtr("Changed")},
			"appendDescription is empty: pass the text to add"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := s.UpdateTicket(tk.ID, c.req)
			var invalid *ErrInvalidInput
			if !errors.As(err, &invalid) || err.Error() != c.msg {
				t.Fatalf("err = %v, want ErrInvalidInput %q", err, c.msg)
			}
			stored, _ := s.GetTicket(tk.ID)
			if stored.Description != "Draft" || stored.Title != "Invoice" {
				t.Fatalf("ticket = %q/%q, want nothing applied", stored.Title, stored.Description)
			}
		})
	}
}

// Appends at the same moment, from separate connections, must all land: the
// description is read and written inside one write transaction, so none of
// them writes back a description that is missing another's text.
func TestConcurrentAppendsAllSurvive(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Billing", "BILL")
	tk, err := s.CreateTicket(models.CreateTicketRequest{ProjectID: p.ID, Title: "Invoice", Description: "Start"})
	if err != nil {
		t.Fatal(err)
	}

	const writers = 12
	start := make(chan struct{})
	errs := make(chan error, writers)
	var wg sync.WaitGroup
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := s.UpdateTicket(tk.ID, models.UpdateTicketRequest{AppendDescription: strPtr(fmt.Sprintf("line %02d", i))})
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	stored, err := s.GetTicket(tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	paragraphs := strings.Split(stored.Description, "\n\n")
	if paragraphs[0] != "Start" || len(paragraphs) != writers+1 {
		t.Fatalf("description = %q, want Start and %d appended paragraphs", stored.Description, writers)
	}
	got := append([]string(nil), paragraphs[1:]...)
	sort.Strings(got)
	for i, line := range got {
		if want := fmt.Sprintf("line %02d", i); line != want {
			t.Fatalf("appended paragraphs = %v, missing %q", got, want)
		}
	}
}
