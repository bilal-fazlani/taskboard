package server

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// activityLines renders an activity page as "KEY from>to note" lines.
func activityLines(page map[string]any) string {
	var lines []string
	for _, raw := range page["entries"].([]any) {
		e := raw.(map[string]any)
		lines = append(lines, strings.TrimSpace(fmt.Sprintf("%s %s>%s %s", e["ticketKey"], e["fromStatus"], e["toStatus"], e["note"])))
	}
	return strings.Join(lines, ", ")
}

// The activity route answers with the project's status changes newest
// first, each with its ticket's key, title and epic; ?epic= narrows it, and
// pages follow on through nextBefore.
func TestProjectActivityEndpoint(t *testing.T) {
	r := serve(t)
	project, _ := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/projects", `{"name":"Billing","prefix":"BILL"}`)
	id := project["id"].(string)
	if _, status := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/epics", `{"projectId":"BILL","name":"Invoices"}`); status != http.StatusCreated {
		t.Fatalf("create epic: status %d", status)
	}
	create := func(body string) string {
		t.Helper()
		tk, status := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/tickets", body)
		if status != http.StatusCreated {
			t.Fatalf("create ticket: status %d, %#v", status, tk)
		}
		return tk["id"].(string)
	}
	move := func(ticket, status, note string) {
		t.Helper()
		if got, code := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/tickets/"+ticket+"/move",
			fmt.Sprintf(`{"status":%q,"note":%q}`, status, note)); code != http.StatusOK {
			t.Fatalf("move: status %d, %#v", code, got)
		}
	}
	invoice := create(fmt.Sprintf(`{"projectId":%q,"title":"Send invoices","epic":"Invoices"}`, id))
	loose := create(fmt.Sprintf(`{"projectId":%q,"title":"Loose end"}`, id))
	move(invoice, "in_progress", "")
	move(loose, "in_progress", "picked up")
	move(invoice, "done", "Landed in 39a07fd")

	page, status := doRequest[map[string]any](t, http.MethodGet, r.url+"/api/projects/bill/activity", "")
	want := "BILL-1 in_progress>done Landed in 39a07fd, BILL-2 todo>in_progress picked up, BILL-1 todo>in_progress, BILL-2 >todo, BILL-1 >todo"
	if status != http.StatusOK || activityLines(page) != want {
		t.Fatalf("activity: status %d\n got %s\nwant %s", status, activityLines(page), want)
	}
	if page["hasMore"] != false {
		t.Fatalf("activity hasMore = %v", page["hasMore"])
	}
	first := page["entries"].([]any)[0].(map[string]any)
	epic, _ := first["epic"].(map[string]any)
	if first["ticketTitle"] != "Send invoices" || first["ticketId"] != invoice || epic["name"] != "Invoices" || first["createdAt"] == nil {
		t.Fatalf("first entry = %#v", first)
	}
	if _, ok := page["entries"].([]any)[1].(map[string]any)["epic"]; ok {
		t.Fatalf("an entry without an epic carries one: %#v", page["entries"].([]any)[1])
	}

	byEpic, _ := doRequest[map[string]any](t, http.MethodGet, r.url+"/api/projects/"+id+"/activity?epic=invoices", "")
	if got := activityLines(byEpic); got != "BILL-1 in_progress>done Landed in 39a07fd, BILL-1 todo>in_progress, BILL-1 >todo" {
		t.Fatalf("?epic=invoices = %s", got)
	}
	noEpic, _ := doRequest[map[string]any](t, http.MethodGet, r.url+"/api/projects/"+id+"/activity?epic=none", "")
	if got := activityLines(noEpic); got != "BILL-2 todo>in_progress picked up, BILL-2 >todo" {
		t.Fatalf("?epic=none = %s", got)
	}
	both, _ := doRequest[map[string]any](t, http.MethodGet, r.url+"/api/projects/"+id+"/activity?epic=none&epic=Invoices", "")
	if got := activityLines(both); got != want {
		t.Fatalf("?epic=none&epic=Invoices = %s", got)
	}

	firstPage, _ := doRequest[map[string]any](t, http.MethodGet, r.url+"/api/projects/"+id+"/activity?limit=3", "")
	if firstPage["hasMore"] != true || firstPage["nextBefore"] == nil {
		t.Fatalf("first page paging = %#v", firstPage)
	}
	rest, _ := doRequest[map[string]any](t, http.MethodGet,
		r.url+"/api/projects/"+id+"/activity?limit=3&before="+firstPage["nextBefore"].(string), "")
	if got := activityLines(firstPage) + ", " + activityLines(rest); got != want || rest["hasMore"] != false {
		t.Fatalf("two pages = %s (hasMore %v)", got, rest["hasMore"])
	}
}

func TestProjectActivityEndpointErrors(t *testing.T) {
	r := serve(t)
	created, _ := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/projects", `{"name":"Billing","prefix":"BILL"}`)
	activityURL := r.url + "/api/projects/" + created["id"].(string) + "/activity"
	cases := []struct {
		name, url string
		want      int
	}{
		{"unknown project", r.url + "/api/projects/NOPE/activity", http.StatusNotFound},
		{"limit not a number", activityURL + "?limit=ten", http.StatusBadRequest},
		{"limit zero", activityURL + "?limit=0", http.StatusBadRequest},
		{"limit too large", activityURL + "?limit=201", http.StatusBadRequest},
		{"unknown before", activityURL + "?before=nope", http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			body, status := doRequest[map[string]any](t, http.MethodGet, c.url, "")
			if status != c.want {
				t.Fatalf("status %d, want %d (%#v)", status, c.want, body)
			}
		})
	}
}
