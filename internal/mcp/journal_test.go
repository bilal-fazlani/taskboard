package mcp

import (
	"fmt"
	"strings"
	"testing"
)

// Agents append entries with append_project_journal, get_project carries the
// latest five, and list_project_journal pages on from its nextBefore.
func TestProjectJournalTools(t *testing.T) {
	s := newTestServer(t)
	projectJSON(t, s, "create_project", map[string]any{"name": "Billing", "prefix": "BILL"}, &map[string]any{})

	for i := 0; i < 7; i++ {
		var entry map[string]any
		projectJSON(t, s, "append_project_journal", map[string]any{
			"projectId": "bill", "author": "orchestrator", "text": fmt.Sprintf("entry %d", i),
		}, &entry)
		if entry["author"] != "orchestrator" || entry["text"] != fmt.Sprintf("entry %d", i) || entry["id"] == nil || entry["createdAt"] == nil {
			t.Fatalf("append_project_journal answered %#v", entry)
		}
	}

	var project map[string]any
	projectJSON(t, s, "get_project", map[string]any{"id": "BILL"}, &project)
	journal := project["journal"].(map[string]any)
	if texts := pageTexts(journal); fmt.Sprint(texts) != "[entry 6 entry 5 entry 4 entry 3 entry 2]" {
		t.Fatalf("get_project journal = %v", texts)
	}
	if journal["total"] != float64(7) || journal["hasMore"] != true {
		t.Fatalf("get_project journal paging = %#v", journal)
	}

	var page map[string]any
	projectJSON(t, s, "list_project_journal", map[string]any{
		"projectId": "BILL", "before": journal["nextBefore"], "limit": 1,
	}, &page)
	if fmt.Sprint(pageTexts(page)) != "[entry 1]" || page["hasMore"] != true {
		t.Fatalf("list_project_journal page = %#v", page)
	}
	projectJSON(t, s, "list_project_journal", map[string]any{"projectId": "BILL", "before": page["nextBefore"]}, &page)
	if fmt.Sprint(pageTexts(page)) != "[entry 0]" || page["hasMore"] != false {
		t.Fatalf("list_project_journal last page = %#v", page)
	}
	projectJSON(t, s, "list_project_journal", map[string]any{"projectId": "BILL"}, &page)
	if len(pageTexts(page)) != 7 {
		t.Fatalf("list_project_journal default page = %v, want all 7", pageTexts(page))
	}

	// list_projects never carries a journal.
	text, _ := callToolText(t, s, "list_projects", map[string]any{})
	if strings.Contains(text, "journal") {
		t.Fatalf("list_projects carries a journal: %s", text)
	}
}

func TestProjectJournalToolErrors(t *testing.T) {
	s := newTestServer(t)
	projectJSON(t, s, "create_project", map[string]any{"name": "Billing", "prefix": "BILL"}, &map[string]any{})

	cases := []struct {
		tool string
		args map[string]any
		want string
	}{
		{"append_project_journal", map[string]any{"author": "a", "text": "t"}, "projectId is required"},
		{"append_project_journal", map[string]any{"projectId": "NOPE", "author": "a", "text": "t"}, "project not found"},
		{"append_project_journal", map[string]any{"projectId": "BILL", "text": "t"}, "author is required"},
		{"append_project_journal", map[string]any{"projectId": "BILL", "author": "a", "text": " "}, "text is required"},
		{"list_project_journal", map[string]any{}, "projectId is required"},
		{"list_project_journal", map[string]any{"projectId": "BILL", "limit": 0}, "limit must be between 1 and 100"},
		{"list_project_journal", map[string]any{"projectId": "BILL", "before": "nope"}, "is not an entry"},
	}
	for _, c := range cases {
		text, isError := callToolText(t, s, c.tool, c.args)
		if !isError || !strings.Contains(text, c.want) {
			t.Errorf("%s %v: %q (error %v), want an error containing %q", c.tool, c.args, text, isError, c.want)
		}
	}
}

func pageTexts(page map[string]any) []string {
	var texts []string
	for _, e := range page["entries"].([]any) {
		texts = append(texts, e.(map[string]any)["text"].(string))
	}
	return texts
}
