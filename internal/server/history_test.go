package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/tcarac/taskboard/internal/db"
	"github.com/tcarac/taskboard/internal/models"
)

// newHistoryServer serves the API over a throwaway database, without the
// change watcher, which these tests have no use for.
func newHistoryServer(t *testing.T) (string, *db.Store) {
	t.Helper()
	database, err := db.OpenAt(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	store := db.NewStore(database)
	ts := httptest.NewServer(New(store, nil))
	t.Cleanup(ts.Close)
	return ts.URL, store
}

func send(t *testing.T, method, url, body string) (int, []byte) {
	t.Helper()
	var payload io.Reader
	if body != "" {
		payload = bytes.NewReader([]byte(body))
	}
	req, err := http.NewRequest(method, url, payload)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, data
}

func seedReviewTicket(t *testing.T, store *db.Store) *models.Ticket {
	t.Helper()
	p, err := store.CreateProject(models.CreateProjectRequest{Name: "Agent Control Plane", Prefix: "ACP"})
	if err != nil {
		t.Fatal(err)
	}
	tk, err := store.CreateTicket(models.CreateTicketRequest{ProjectID: p.ID, Title: "Reviewed", Status: models.StatusAgentReview})
	if err != nil {
		t.Fatal(err)
	}
	return tk
}

func history(t *testing.T, base, id string) []models.StatusChange {
	t.Helper()
	code, body := send(t, http.MethodGet, base+"/api/tickets/"+id+"/history", "")
	if code != http.StatusOK {
		t.Fatalf("GET history: %d %s", code, body)
	}
	var changes []models.StatusChange
	if err := json.Unmarshal(body, &changes); err != nil {
		t.Fatalf("decoding history %s: %v", body, err)
	}
	return changes
}

// Over HTTP a note is accepted on every status change and required on none,
// agent_review included: only the MCP tools require one.
func TestHTTPMoveAndUpdateTakeAnOptionalNote(t *testing.T) {
	base, store := newHistoryServer(t)
	tk := seedReviewTicket(t, store)

	code, body := send(t, http.MethodPost, base+"/api/tickets/"+tk.ID+"/move", `{"status":"in_progress"}`)
	if code != http.StatusOK {
		t.Fatalf("move out of agent_review without a note: %d %s", code, body)
	}
	code, body = send(t, http.MethodPut, base+"/api/tickets/"+tk.ID, `{"status":"agent_review","note":"ready again"}`)
	if code != http.StatusOK {
		t.Fatalf("update with a note: %d %s", code, body)
	}
	code, body = send(t, http.MethodPut, base+"/api/tickets/"+tk.ID, `{"status":"done"}`)
	if code != http.StatusOK {
		t.Fatalf("update out of agent_review without a note: %d %s", code, body)
	}
	code, body = send(t, http.MethodPost, base+"/api/tickets/"+tk.ID+"/move", `{"status":"in_progress","note":"reopened"}`)
	if code != http.StatusOK {
		t.Fatalf("move with a note: %d %s", code, body)
	}
	var moved models.Ticket
	if err := json.Unmarshal(body, &moved); err != nil {
		t.Fatal(err)
	}
	if moved.ReviewRounds != 2 {
		t.Fatalf("reviewRounds = %d, want 2", moved.ReviewRounds)
	}

	changes := history(t, base, tk.ID)
	want := []struct{ from, to, note string }{
		{"done", "in_progress", "reopened"},
		{"agent_review", "done", ""},
		{"in_progress", "agent_review", "ready again"},
		{"agent_review", "in_progress", ""},
		{"", "agent_review", ""},
	}
	if len(changes) != len(want) {
		t.Fatalf("history has %d entries, want %d: %+v", len(changes), len(want), changes)
	}
	for i, w := range want {
		c := changes[i]
		if c.FromStatus != w.from || c.ToStatus != w.to || c.Note != w.note {
			t.Fatalf("history[%d] = %s>%s %q, want %s>%s %q", i, c.FromStatus, c.ToStatus, c.Note, w.from, w.to, w.note)
		}
	}
}

func TestHTTPHistoryShape(t *testing.T) {
	base, store := newHistoryServer(t)
	tk := seedReviewTicket(t, store)

	code, body := send(t, http.MethodGet, base+"/api/tickets/"+tk.ID+"/history", "")
	if code != http.StatusOK {
		t.Fatalf("GET history: %d %s", code, body)
	}
	var raw []map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}
	if len(raw) != 1 {
		t.Fatalf("history = %s, want the birth row only", body)
	}
	for _, key := range []string{"id", "ticketId", "fromStatus", "toStatus", "note", "createdAt"} {
		if _, ok := raw[0][key]; !ok {
			t.Fatalf("history entry %s has no %q", body, key)
		}
	}
}

func TestHTTPHistoryUnknownTicketIs404(t *testing.T) {
	base, _ := newHistoryServer(t)
	code, body := send(t, http.MethodGet, base+"/api/tickets/01ARZ3NDEKTSV4RRFFQ69G5FAV/history", "")
	if code != http.StatusNotFound {
		t.Fatalf("GET history of an unknown ticket: %d %s, want 404", code, body)
	}
}

func TestHTTPTicketJSONCarriesReviewRounds(t *testing.T) {
	base, store := newHistoryServer(t)
	tk := seedReviewTicket(t, store)

	for _, url := range []string{base + "/api/tickets/" + tk.ID, base + "/api/tickets"} {
		code, body := send(t, http.MethodGet, url, "")
		if code != http.StatusOK {
			t.Fatalf("GET %s: %d %s", url, code, body)
		}
		if !bytes.Contains(body, []byte(`"reviewRounds":1`)) {
			t.Fatalf("GET %s = %s, want reviewRounds 1", url, body)
		}
	}
}

// The list the Dependencies page fetches carries doneAt on a done ticket, the
// time of its move to done, and leaves the key out on one that is not done.
func TestHTTPTicketListCarriesDoneAt(t *testing.T) {
	base, store := newHistoryServer(t)
	open := seedReviewTicket(t, store)
	done, err := store.CreateTicket(models.CreateTicketRequest{ProjectID: open.ProjectID, Title: "Finished"})
	if err != nil {
		t.Fatal(err)
	}
	code, body := send(t, http.MethodPost, base+"/api/tickets/"+done.ID+"/move", `{"status":"done"}`)
	if code != http.StatusOK {
		t.Fatalf("move to done: %d %s", code, body)
	}
	moved := history(t, base, done.ID)[0]

	code, body = send(t, http.MethodGet, base+"/api/tickets", "")
	if code != http.StatusOK {
		t.Fatalf("GET /api/tickets: %d %s", code, body)
	}
	var raw []map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}
	for _, tk := range raw {
		at, has := tk["doneAt"]
		switch tk["id"] {
		case open.ID:
			if has {
				t.Fatalf("an open ticket carries doneAt %v", at)
			}
		case done.ID:
			if !has {
				t.Fatalf("the done ticket has no doneAt: %v", tk)
			}
			var want models.Ticket
			if err := json.Unmarshal([]byte(`{"doneAt":"`+at.(string)+`"}`), &want); err != nil {
				t.Fatal(err)
			}
			if want.DoneAt == nil || !want.DoneAt.Equal(moved.CreatedAt) {
				t.Fatalf("doneAt = %v, want the move's %v", at, moved.CreatedAt)
			}
		}
	}
}
