package mcp

import (
	"encoding/json"
	"fmt"
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

func TestListProjectsShortensLongDescriptions(t *testing.T) {
	s := newTestServer(t)

	firstParagraph := strings.Repeat("word ", 100) // 500 chars, well past the preview cap
	longDescription := firstParagraph + "\n\nSecond paragraph with more detail that only get_project should return."
	var created map[string]any
	projectJSON(t, s, "create_project", map[string]any{
		"name": "Billing", "prefix": "BILL", "description": longDescription,
	}, &created)

	shortParagraph := "Support tickets and refunds."
	projectJSON(t, s, "create_project", map[string]any{
		"name": "Support", "prefix": "SUP", "description": shortParagraph,
	}, &map[string]any{})

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

	var bill, sup map[string]any
	for _, p := range list {
		switch p["prefix"] {
		case "BILL":
			bill = p
		case "SUP":
			sup = p
		}
	}
	if bill == nil || sup == nil {
		t.Fatalf("list_projects missing a project: %s", text)
	}

	billDesc, _ := bill["description"].(string)
	if len(billDesc) > projectDescriptionPreviewLimit {
		t.Fatalf("list_projects description is %d chars, want at most %d: %q", len(billDesc), projectDescriptionPreviewLimit, billDesc)
	}
	if strings.Contains(billDesc, "Second paragraph") {
		t.Fatalf("list_projects description leaks the second paragraph: %q", billDesc)
	}
	if bill["descriptionTruncated"] != true {
		t.Fatalf("list_projects descriptionTruncated = %#v, want true for a long description", bill["descriptionTruncated"])
	}

	if sup["description"] != shortParagraph {
		t.Fatalf("list_projects description = %#v, want the untouched short description", sup["description"])
	}
	if sup["descriptionTruncated"] != false {
		t.Fatalf("list_projects descriptionTruncated = %#v, want false for a short description", sup["descriptionTruncated"])
	}

	// get_project still returns the whole thing, second paragraph included.
	var got map[string]any
	projectJSON(t, s, "get_project", map[string]any{"id": "BILL"}, &got)
	if got["description"] != longDescription {
		t.Fatalf("get_project description = %#v, want the full text", got["description"])
	}
	if _, ok := got["descriptionTruncated"]; ok {
		t.Fatalf("get_project carries descriptionTruncated: %#v", got["descriptionTruncated"])
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

func TestListProjectsToolDescriptionExplainsShortDescription(t *testing.T) {
	s := newTestServer(t)
	defs := map[string]toolDef{}
	for _, def := range s.toolDefinitions() {
		defs[def.Name] = def
	}

	list := defs["list_projects"].Description
	wants := []string{"first paragraph", fmt.Sprintf("%d characters", projectDescriptionPreviewLimit), "descriptionTruncated", "get_project"}
	for _, want := range wants {
		if !strings.Contains(list, want) {
			t.Fatalf("list_projects description = %q, want it to mention %q", list, want)
		}
	}

	get := defs["get_project"].Description
	if !strings.Contains(get, "full description") {
		t.Fatalf("get_project description = %q, want it to mention the full description", get)
	}
}

func TestShortProjectDescription(t *testing.T) {
	tests := []struct {
		name          string
		description   string
		wantShort     string
		wantTruncated bool
	}{
		{
			name:          "empty description",
			description:   "",
			wantShort:     "",
			wantTruncated: false,
		},
		{
			name:          "short single paragraph is untouched",
			description:   "Short and sweet.",
			wantShort:     "Short and sweet.",
			wantTruncated: false,
		},
		{
			name:          "CRLF paragraph break",
			description:   "First paragraph.\r\n\r\nSecond paragraph.",
			wantShort:     "First paragraph.",
			wantTruncated: true,
		},
		{
			name:          "whitespace-only line is a paragraph break",
			description:   "First paragraph.\n \nSecond paragraph.",
			wantShort:     "First paragraph.",
			wantTruncated: true,
		},
		{
			name:          "leading blank lines still give a non-empty preview",
			description:   "\n\nOnly paragraph, nothing else.",
			wantShort:     "Only paragraph, nothing else.",
			wantTruncated: false,
		},
		{
			name:          "trailing blank lines are not a truncation",
			description:   "Only paragraph.\n\n\n",
			wantShort:     "Only paragraph.",
			wantTruncated: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			short, truncated := shortProjectDescription(tt.description)
			if short != tt.wantShort || truncated != tt.wantTruncated {
				t.Fatalf("shortProjectDescription(%q) = (%q, %v), want (%q, %v)",
					tt.description, short, truncated, tt.wantShort, tt.wantTruncated)
			}
		})
	}

	t.Run("one long paragraph is cut only by the cap", func(t *testing.T) {
		long := strings.Repeat("a", projectDescriptionPreviewLimit+50) // one line, no paragraph break
		short, truncated := shortProjectDescription(long)
		if !truncated {
			t.Fatal("descriptionTruncated = false, want true")
		}
		if got := len([]rune(short)); got != projectDescriptionPreviewLimit {
			t.Fatalf("short is %d runes, want exactly %d", got, projectDescriptionPreviewLimit)
		}
		if short != long[:projectDescriptionPreviewLimit] {
			t.Fatalf("short = %q, want the input's first %d characters", short, projectDescriptionPreviewLimit)
		}
	})

	t.Run("multi-byte runes are capped by rune count, not bytes", func(t *testing.T) {
		// Each "日" is 3 bytes, so a byte-based cap would cut this well
		// before a rune-based one does.
		long := strings.Repeat("日", projectDescriptionPreviewLimit+50)
		short, truncated := shortProjectDescription(long)
		if !truncated {
			t.Fatal("descriptionTruncated = false, want true")
		}
		runes := []rune(short)
		if len(runes) != projectDescriptionPreviewLimit {
			t.Fatalf("short is %d runes, want exactly %d", len(runes), projectDescriptionPreviewLimit)
		}
	})

	t.Run("the preview is always a prefix of the normalised original", func(t *testing.T) {
		inputs := []string{
			"First paragraph.\r\n\r\nSecond paragraph.",
			"First paragraph.\n \nSecond paragraph.",
			"\n\nLeading blank lines then text.",
			"Trailing blank lines.\n\n\n",
			strings.Repeat("word ", 200),
			strings.Repeat("日", 400),
			"",
			"Plain short text.",
		}
		for _, in := range inputs {
			short, _ := shortProjectDescription(in)
			normalised := strings.TrimSpace(strings.ReplaceAll(in, "\r\n", "\n"))
			if !strings.HasPrefix(normalised, short) {
				t.Fatalf("shortProjectDescription(%q) = %q, not a prefix of the normalised original %q", in, short, normalised)
			}
		}
	})
}
