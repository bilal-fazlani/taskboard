package mcp

import (
	"strings"
	"testing"

	"github.com/tcarac/taskboard/internal/models"
)

func TestProjectToolsRejectPrefixTakenIgnoringCase(t *testing.T) {
	s := newTestServer(t)
	if _, err := s.store.CreateProject(models.CreateProjectRequest{Name: "Glow", Prefix: "GLOW"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.CreateProject(models.CreateProjectRequest{Name: "Billing", Prefix: "BILL"}); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{"create_project", map[string]any{"name": "Glowworm", "prefix": "glow"}},
		{"update_project", map[string]any{"id": "BILL", "prefix": "Glow"}},
	} {
		text, isError := callToolText(t, s, tc.tool, tc.args)
		if !isError {
			t.Fatalf("%s %v: not flagged as an error: %s", tc.tool, tc.args, text)
		}
		if !strings.Contains(text, `already used by project "Glow"`) || !strings.Contains(text, "letter case") {
			t.Fatalf("%s %v: error %q should name the project and the case rule", tc.tool, tc.args, text)
		}
	}

	projects, err := s.store.ListProjects("")
	if err != nil {
		t.Fatal(err)
	}
	var prefixes []string
	for _, p := range projects {
		prefixes = append(prefixes, p.Prefix)
	}
	if len(prefixes) != 2 || !strings.Contains(strings.Join(prefixes, ","), "BILL") {
		t.Fatalf("projects after the rejections = %v, want GLOW and BILL unchanged", prefixes)
	}
}
