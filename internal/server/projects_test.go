package server

import (
	"net/http"
	"testing"
)

func TestProjectEndpointsCarryAgentInstructions(t *testing.T) {
	r := serve(t)

	created, status := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/projects",
		`{"name":"Billing","prefix":"BILL","description":"What billing is.","agentInstructions":"Run the tests."}`)
	if status != http.StatusCreated {
		t.Fatalf("create status = %d", status)
	}
	if created["agentInstructions"] != "Run the tests." || created["description"] != "What billing is." {
		t.Fatalf("created = %#v", created)
	}
	id := created["id"].(string)

	got, _ := doRequest[map[string]any](t, http.MethodGet, r.url+"/api/projects/"+id, "")
	if got["agentInstructions"] != "Run the tests." {
		t.Fatalf("get agentInstructions = %#v", got["agentInstructions"])
	}

	updated, status := doRequest[map[string]any](t, http.MethodPut, r.url+"/api/projects/"+id, `{"agentInstructions":"Review first."}`)
	if status != http.StatusOK || updated["agentInstructions"] != "Review first." {
		t.Fatalf("update: status %d, %#v", status, updated)
	}

	// An update that leaves them out keeps them.
	updated, _ = doRequest[map[string]any](t, http.MethodPut, r.url+"/api/projects/"+id, `{"name":"Billing 2"}`)
	if updated["agentInstructions"] != "Review first." || updated["name"] != "Billing 2" {
		t.Fatalf("update without instructions = %#v", updated)
	}
	got, _ = doRequest[map[string]any](t, http.MethodGet, r.url+"/api/projects/"+id, "")
	if got["agentInstructions"] != "Review first." {
		t.Fatalf("read back after an update without instructions = %#v", got["agentInstructions"])
	}

	// An empty string clears them, and a project without any says "".
	updated, _ = doRequest[map[string]any](t, http.MethodPut, r.url+"/api/projects/"+id, `{"agentInstructions":""}`)
	if v, ok := updated["agentInstructions"]; !ok || v != "" {
		t.Fatalf("cleared agentInstructions = %#v (present %v)", v, ok)
	}
}

func TestProjectListSaysWhichHaveAgentInstructionsWithoutText(t *testing.T) {
	r := serve(t)
	for _, body := range []string{
		`{"name":"Billing","prefix":"BILL","agentInstructions":"Run the tests."}`,
		`{"name":"Support","prefix":"SUP"}`,
	} {
		if _, status := doRequest[map[string]any](t, http.MethodPost, r.url+"/api/projects", body); status != http.StatusCreated {
			t.Fatalf("create %s: status %d", body, status)
		}
	}

	list, status := doRequest[[]map[string]any](t, http.MethodGet, r.url+"/api/projects", "")
	if status != http.StatusOK || len(list) != 2 {
		t.Fatalf("list: status %d, %d projects", status, len(list))
	}
	has := map[string]any{}
	for _, p := range list {
		if _, ok := p["agentInstructions"]; ok {
			t.Fatalf("list carries %v's agent instructions", p["prefix"])
		}
		has[p["prefix"].(string)] = p["hasAgentInstructions"]
	}
	if has["BILL"] != true || has["SUP"] != false {
		t.Fatalf("hasAgentInstructions = %#v, want BILL true and SUP false", has)
	}

	// The list is still an empty array, not null, with no projects.
	empty := serve(t)
	none, _ := doRequest[[]map[string]any](t, http.MethodGet, empty.url+"/api/projects", "")
	if none == nil {
		t.Fatal("empty list decoded as null")
	}
}
