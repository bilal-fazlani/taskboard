package mcp

import (
	"reflect"
	"strings"
	"testing"

	"github.com/tcarac/taskboard/internal/models"
	"github.com/tcarac/taskboard/internal/weburl"
)

// refKinds maps each dependsOn/blocks entry of a JSON ticket to
// "kind|note".
func refKinds(t *testing.T, refs any) map[string]string {
	t.Helper()
	out := map[string]string{}
	list, _ := refs.([]any)
	for _, r := range list {
		m := r.(map[string]any)
		note, _ := m["note"].(string)
		kind, _ := m["kind"].(string)
		out[m["key"].(string)] = kind + "|" + note
	}
	return out
}

func TestTicketToolsSetAndReadDependencyKindsAndNotes(t *testing.T) {
	s := newTestServer(t)
	if _, err := s.store.CreateProject(models.CreateProjectRequest{Name: "Agent control plane", Prefix: "ACP"}); err != nil {
		t.Fatal(err)
	}
	callJSON(t, s, "create_ticket", map[string]any{"projectId": "ACP", "title": "Delivery fields"}) // ACP-1
	callJSON(t, s, "create_ticket", map[string]any{"projectId": "ACP", "title": "Project journal"}) // ACP-2
	created := callJSON(t, s, "create_ticket", map[string]any{                                      // ACP-3
		"projectId": "ACP", "title": "Typed links", "full": true,
		"dependsOn": []map[string]string{
			{"ticket": "ACP-1", "kind": "conflict_only", "note": "internal/models/models.go"},
			{"ticket": "acp-2"},
		},
	})
	want := map[string]string{"ACP-1": "conflict_only|internal/models/models.go", "ACP-2": "needs_work|"}
	if got := refKinds(t, created["dependsOn"]); !reflect.DeepEqual(got, want) {
		t.Fatalf("create_ticket dependsOn = %v, want %v", got, want)
	}

	// get_ticket reads the kind and note on both ends.
	got := callJSON(t, s, "get_ticket", map[string]any{"id": "ACP-3"})
	if kinds := refKinds(t, got["dependsOn"]); !reflect.DeepEqual(kinds, want) {
		t.Fatalf("get_ticket dependsOn = %v, want %v", kinds, want)
	}
	blocker := callJSON(t, s, "get_ticket", map[string]any{"id": "ACP-1"})
	if kinds := refKinds(t, blocker["blocks"]); !reflect.DeepEqual(kinds, map[string]string{"ACP-3": want["ACP-1"]}) {
		t.Fatalf("get_ticket blocks = %v, want ACP-3 conflict only with its note", kinds)
	}

	// Changing only a note is a dependsOn change; resending the same list is none.
	same := []map[string]string{{"ticket": "ACP-1", "kind": "conflict_only", "note": "internal/models/models.go"}, {"ticket": "ACP-2"}}
	short := callJSON(t, s, "update_ticket", map[string]any{"id": "ACP-3", "dependsOn": same})
	wantChanged(t, "update_ticket", short)
	short = callJSON(t, s, "update_ticket", map[string]any{"id": "ACP-3", "dependsOn": []map[string]string{
		{"ticket": "ACP-1", "kind": "conflict_only", "note": "internal/mcp/mcp.go"}, {"ticket": "ACP-2"},
	}})
	wantChanged(t, "update_ticket", short, "dependsOn")

	// The summary names a conflict-only dependency's kind, leaves the default
	// out, and carries a note when there is one.
	out, err := s.callTool("list_tickets", mustJSON(t, map[string]any{"summary": true, "projectId": "ACP"}))
	if err != nil {
		t.Fatal(err)
	}
	for _, tk := range toJSONMap(t, out)["tickets"].([]any) {
		m := tk.(map[string]any)
		if m["key"] != "ACP-3" {
			continue
		}
		wantDeps := []any{
			map[string]any{"key": "ACP-1", "status": "todo", "kind": "conflict_only", "note": "internal/mcp/mcp.go"},
			map[string]any{"key": "ACP-2", "status": "todo"},
		}
		if !reflect.DeepEqual(m["dependsOn"], wantDeps) {
			t.Fatalf("summary dependsOn = %#v, want %#v", m["dependsOn"], wantDeps)
		}
	}

	// A plain string entry, the old form, is an error naming the object
	// shape; so is an unknown kind.
	for _, tc := range []struct {
		deps any
		want string
	}{
		{[]string{"ACP-1"}, "each dependsOn entry is an object"},
		{[]map[string]string{{"ticket": "ACP-1", "kind": "blocked"}}, "needs_work, conflict_only"},
	} {
		text, isError := callToolText(t, s, "update_ticket", map[string]any{"id": "ACP-3", "dependsOn": tc.deps})
		if !isError || !strings.Contains(text, tc.want) {
			t.Fatalf("update_ticket dependsOn %v = %q (error %v), want an error containing %q", tc.deps, text, isError, tc.want)
		}
	}
}

func TestTicketToolsSetAndReadSurfacedFrom(t *testing.T) {
	t.Setenv(weburl.BaseEnv, "http://board.test")
	s := newTestServer(t)
	if _, err := s.store.CreateProject(models.CreateProjectRequest{Name: "Agent control plane", Prefix: "ACP"}); err != nil {
		t.Fatal(err)
	}
	callJSON(t, s, "create_ticket", map[string]any{"projectId": "ACP", "title": "Hardening run"}) // ACP-1
	created := callJSON(t, s, "create_ticket", map[string]any{                                    // ACP-2
		"projectId": "ACP", "title": "Found in the run", "surfacedFrom": "ACP-1", "full": true,
	})
	if from, _ := created["surfacedFrom"].(map[string]any); from["key"] != "ACP-1" || from["title"] != "Hardening run" {
		t.Fatalf("create_ticket surfacedFrom = %v, want ACP-1", created["surfacedFrom"])
	}

	source := callJSON(t, s, "get_ticket", map[string]any{"id": "ACP-1"})
	surfaced, _ := source["surfaced"].([]any)
	if len(surfaced) != 1 || surfaced[0].(map[string]any)["key"] != "ACP-2" {
		t.Fatalf("get_ticket surfaced = %v, want ACP-2", source["surfaced"])
	}

	out, err := s.callTool("list_tickets", mustJSON(t, map[string]any{"summary": true, "projectId": "ACP"}))
	if err != nil {
		t.Fatal(err)
	}
	for _, tk := range toJSONMap(t, out)["tickets"].([]any) {
		m := tk.(map[string]any)
		want := map[string]any{"ACP-1": nil, "ACP-2": "ACP-1"}[m["key"].(string)]
		if m["surfacedFrom"] != want {
			t.Fatalf("summary %v surfacedFrom = %v, want %v", m["key"], m["surfacedFrom"], want)
		}
	}

	// update_ticket reports it among the changed fields, and "none" removes it.
	callJSON(t, s, "create_ticket", map[string]any{"projectId": "ACP", "title": "Second run"}) // ACP-3
	short := callJSON(t, s, "update_ticket", map[string]any{"id": "ACP-2", "surfacedFrom": "ACP-3"})
	wantChanged(t, "update_ticket", short, "surfacedFrom")
	short = callJSON(t, s, "update_ticket", map[string]any{"id": "ACP-2", "title": "Renamed"})
	wantChanged(t, "update_ticket", short, "title")
	full := callJSON(t, s, "update_ticket", map[string]any{"id": "ACP-2", "surfacedFrom": "none", "full": true})
	if _, has := full["surfacedFrom"]; has {
		t.Fatalf("surfacedFrom = %v after none, want it gone", full["surfacedFrom"])
	}

	text, isError := callToolText(t, s, "update_ticket", map[string]any{"id": "ACP-2", "surfacedFrom": "ACP-2"})
	if !isError || !strings.Contains(text, "itself") {
		t.Fatalf("update_ticket surfaced from itself = %q (error %v), want an error", text, isError)
	}
}

// Both tools document the link arguments, so an agent knows the kind suffix
// and how to clear surfacedFrom.
func TestTicketToolsDocumentTypedLinks(t *testing.T) {
	s := newTestServer(t)
	defs := map[string]toolDef{}
	for _, d := range s.toolDefinitions() {
		defs[d.Name] = d
	}
	for _, tool := range []string{"create_ticket", "update_ticket"} {
		props := defs[tool].InputSchema.Properties
		deps := props["dependsOn"]
		if !strings.Contains(deps.Description, "conflict_only") || deps.Items == nil || deps.Items.Type != "object" {
			t.Fatalf("%s dependsOn = %+v, want documented objects", tool, deps)
		}
		for _, field := range []string{"ticket", "kind", "note"} {
			if _, ok := deps.Items.Properties[field]; !ok {
				t.Fatalf("%s dependsOn items have no %s", tool, field)
			}
		}
		if !reflect.DeepEqual(deps.Items.Properties["kind"].Enum, models.DependencyKinds) || !reflect.DeepEqual(deps.Items.Required, []string{"ticket"}) {
			t.Fatalf("%s dependsOn items = %+v", tool, deps.Items)
		}
		if props["surfacedFrom"].Type != "string" || !strings.Contains(props["surfacedFrom"].Description, "none") {
			t.Fatalf("%s surfacedFrom = %+v, want a documented string", tool, props["surfacedFrom"])
		}
	}
	if !strings.Contains(defs["get_ticket"].Description, "surfacedFrom") {
		t.Fatal("get_ticket does not say it returns surfacedFrom")
	}
}
