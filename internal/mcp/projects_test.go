package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

// projectJSON calls a project tool the way a client does and decodes the
// JSON text it gets back.
func projectJSON(t *testing.T, s *MCPServer, tool string, args map[string]any, into any) {
	t.Helper()
	text, isError := callToolText(t, s, tool, args)
	if isError {
		t.Fatalf("%s: %s", tool, text)
	}
	if err := json.Unmarshal([]byte(text), into); err != nil {
		t.Fatalf("%s returned %q: %v", tool, text, err)
	}
}

func TestGetProjectReturnsAgentInstructionsAndListProjectsDoesNot(t *testing.T) {
	s := newTestServer(t)
	var created map[string]any
	projectJSON(t, s, "create_project", map[string]any{
		"name": "Billing", "prefix": "BILL", "description": "What billing is.",
		"agentInstructions": "Run the tests before landing.",
	}, &created)
	projectJSON(t, s, "create_project", map[string]any{"name": "Support", "prefix": "SUP"}, &map[string]any{})

	var got map[string]any
	projectJSON(t, s, "get_project", map[string]any{"id": "BILL"}, &got)
	if got["agentInstructions"] != "Run the tests before landing." {
		t.Fatalf("get_project agentInstructions = %#v", got["agentInstructions"])
	}
	if got["description"] != "What billing is." {
		t.Fatalf("get_project description = %#v", got["description"])
	}

	// A project without instructions still says so, with an empty string.
	var none map[string]any
	projectJSON(t, s, "get_project", map[string]any{"id": "SUP"}, &none)
	if v, ok := none["agentInstructions"]; !ok || v != "" {
		t.Fatalf("get_project without instructions: agentInstructions = %#v (present %v), want \"\"", v, ok)
	}

	text, isError := callToolText(t, s, "list_projects", map[string]any{})
	if isError {
		t.Fatalf("list_projects: %s", text)
	}
	var list []map[string]any
	if err := json.Unmarshal([]byte(text), &list); err != nil {
		t.Fatalf("list_projects returned %q: %v", text, err)
	}
	if len(list) != 2 {
		t.Fatalf("list_projects returned %d projects, want 2", len(list))
	}
	for _, p := range list {
		for _, key := range []string{"agentInstructions", "hasAgentInstructions"} {
			if _, ok := p[key]; ok {
				t.Fatalf("list_projects carries %s for %v", key, p["prefix"])
			}
		}
	}
	if strings.Contains(text, "Run the tests") {
		t.Fatalf("list_projects output contains the instructions text: %s", text)
	}
}

func TestCreateAndUpdateProjectAcceptAgentInstructions(t *testing.T) {
	s := newTestServer(t)
	var created map[string]any
	projectJSON(t, s, "create_project", map[string]any{
		"name": "Billing", "prefix": "BILL", "agentInstructions": "First rules.", "full": true,
	}, &created)
	if created["agentInstructions"] != "First rules." {
		t.Fatalf("create_project agentInstructions = %#v", created["agentInstructions"])
	}

	var updated map[string]any
	projectJSON(t, s, "update_project", map[string]any{"id": "BILL", "agentInstructions": "Second rules.", "full": true}, &updated)
	if updated["agentInstructions"] != "Second rules." {
		t.Fatalf("update_project agentInstructions = %#v", updated["agentInstructions"])
	}

	// An update that leaves them out keeps them.
	projectJSON(t, s, "update_project", map[string]any{"id": "BILL", "name": "Billing 2", "description": "New"}, &updated)
	var got map[string]any
	projectJSON(t, s, "get_project", map[string]any{"id": "BILL"}, &got)
	if got["agentInstructions"] != "Second rules." || got["name"] != "Billing 2" {
		t.Fatalf("after an update without instructions: %#v", got)
	}

	// An empty string clears them.
	projectJSON(t, s, "update_project", map[string]any{"id": "BILL", "agentInstructions": ""}, &updated)
	projectJSON(t, s, "get_project", map[string]any{"id": "BILL"}, &got)
	if got["agentInstructions"] != "" {
		t.Fatalf("cleared agentInstructions = %#v", got["agentInstructions"])
	}
}

func TestProjectToolDescriptionsExplainAgentInstructions(t *testing.T) {
	s := newTestServer(t)
	defs := map[string]toolDef{}
	for _, def := range s.toolDefinitions() {
		defs[def.Name] = def
	}

	get := strings.ToLower(defs["get_project"].Description)
	for _, want := range []string{"agent instructions", "read", "follow", "before working"} {
		if !strings.Contains(get, want) {
			t.Fatalf("get_project description = %q, want it to mention %q", defs["get_project"].Description, want)
		}
	}

	for _, tool := range []string{"create_project", "update_project"} {
		props := defs[tool].InputSchema.Properties
		desc := props["description"].Description
		if !strings.Contains(desc, "What the project is") || !strings.Contains(desc, "Not how to work on it") {
			t.Fatalf("%s description field help = %q, want it to say what the project is, not how to work on it", tool, desc)
		}
		if _, ok := props["agentInstructions"]; !ok {
			t.Fatalf("%s has no agentInstructions field", tool)
		}
	}
	if _, ok := defs["list_projects"].InputSchema.Properties["agentInstructions"]; ok {
		t.Fatal("list_projects takes agentInstructions")
	}
}
