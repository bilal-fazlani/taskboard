package server

import (
	"fmt"
	"net/http"
	"testing"
)

// Entries are appended with POST and read back with GET, newest first, a
// page at a time through nextBefore; a read of the project carries its
// latest entries.
func TestProjectJournalEndpoints(t *testing.T) {
	r := serve(t)
	created, status := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/projects", `{"name":"Billing","prefix":"BILL"}`)
	if status != http.StatusCreated {
		t.Fatalf("create project: status %d", status)
	}
	id := created["id"].(string)

	// A fresh project's read carries an empty journal.
	got, _ := doRequest[map[string]any](t, http.MethodGet, r.url+"/api/projects/"+id, "")
	journal, ok := got["journal"].(map[string]any)
	if !ok || journal["total"] != float64(0) || len(journal["entries"].([]any)) != 0 {
		t.Fatalf("fresh project's journal = %#v, want an empty page", got["journal"])
	}

	for i := 0; i < 7; i++ {
		entry, status := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/projects/"+id+"/journal",
			fmt.Sprintf(`{"author":"Bilal","text":"entry %d"}`, i))
		if status != http.StatusCreated {
			t.Fatalf("append %d: status %d, %#v", i, status, entry)
		}
		if entry["author"] != "Bilal" || entry["text"] != fmt.Sprintf("entry %d", i) || entry["projectId"] != id || entry["createdAt"] == nil {
			t.Fatalf("appended entry = %#v", entry)
		}
	}

	// The project read carries the latest five, and where to read on from.
	got, _ = doRequest[map[string]any](t, http.MethodGet, r.url+"/api/projects/"+id, "")
	journal = got["journal"].(map[string]any)
	if texts := pageTexts(journal); fmt.Sprint(texts) != "[entry 6 entry 5 entry 4 entry 3 entry 2]" {
		t.Fatalf("project's journal = %v", texts)
	}
	if journal["total"] != float64(7) || journal["hasMore"] != true || journal["nextBefore"] == nil {
		t.Fatalf("project's journal paging = %#v", journal)
	}

	// The journal route pages the rest, by project prefix too.
	page, status := doRequest[map[string]any](t, http.MethodGet,
		r.url+"/api/projects/bill/journal?limit=1&before="+journal["nextBefore"].(string), "")
	if status != http.StatusOK || fmt.Sprint(pageTexts(page)) != "[entry 1]" || page["hasMore"] != true {
		t.Fatalf("page after the preview: status %d, %#v", status, page)
	}
	page, _ = doRequest[map[string]any](t, http.MethodGet,
		r.url+"/api/projects/"+id+"/journal?before="+page["nextBefore"].(string), "")
	if fmt.Sprint(pageTexts(page)) != "[entry 0]" || page["hasMore"] != false {
		t.Fatalf("last page = %#v", page)
	}
	if _, ok := page["nextBefore"]; ok {
		t.Fatalf("last page carries nextBefore: %#v", page)
	}
	page, _ = doRequest[map[string]any](t, http.MethodGet, r.url+"/api/projects/"+id+"/journal", "")
	if len(page["entries"].([]any)) != 7 {
		t.Fatalf("default page = %v, want all 7", pageTexts(page))
	}
}

func TestProjectJournalEndpointErrors(t *testing.T) {
	r := serve(t)
	created, _ := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/projects", `{"name":"Billing","prefix":"BILL"}`)
	journalURL := r.url + "/api/projects/" + created["id"].(string) + "/journal"

	cases := []struct {
		name, method, url, body string
		want                    int
	}{
		{"append to unknown project", http.MethodPost, r.url + "/api/projects/NOPE/journal", `{"author":"a","text":"t"}`, http.StatusNotFound},
		{"read unknown project", http.MethodGet, r.url + "/api/projects/NOPE/journal", "", http.StatusNotFound},
		{"blank author", http.MethodPost, journalURL, `{"author":" ","text":"t"}`, http.StatusBadRequest},
		{"missing text", http.MethodPost, journalURL, `{"author":"a"}`, http.StatusBadRequest},
		{"invalid JSON", http.MethodPost, journalURL, `{`, http.StatusBadRequest},
		{"limit not a number", http.MethodGet, journalURL + "?limit=ten", "", http.StatusBadRequest},
		{"limit zero", http.MethodGet, journalURL + "?limit=0", "", http.StatusBadRequest},
		{"limit too large", http.MethodGet, journalURL + "?limit=101", "", http.StatusBadRequest},
		{"unknown before", http.MethodGet, journalURL + "?before=nope", "", http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			body, status := doRequest[map[string]any](t, c.method, c.url, c.body)
			if status != c.want {
				t.Fatalf("status %d, want %d (%#v)", status, c.want, body)
			}
		})
	}
}

func pageTexts(page map[string]any) []string {
	var texts []string
	for _, e := range page["entries"].([]any) {
		texts = append(texts, e.(map[string]any)["text"].(string))
	}
	return texts
}
