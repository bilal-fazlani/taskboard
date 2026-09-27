package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// A prefix another project already uses, in any letter case, is the caller's
// mistake: a 400 that says which project has it, not a 500.
func TestProjectPrefixTakenIgnoringCaseIs400(t *testing.T) {
	url, _ := newHistoryServer(t)

	code, body := send(t, http.MethodPost, url+"/api/projects", `{"name":"Glow","prefix":"GLOW"}`)
	if code != http.StatusCreated {
		t.Fatalf("create GLOW: %d %s", code, body)
	}
	code, body = send(t, http.MethodPost, url+"/api/projects", `{"name":"Billing","prefix":"BILL"}`)
	if code != http.StatusCreated {
		t.Fatalf("create BILL: %d %s", code, body)
	}
	var bill struct{ ID string }
	if err := json.Unmarshal(body, &bill); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		what, method, path, body string
	}{
		{"create glow", http.MethodPost, "/api/projects", `{"name":"Glowworm","prefix":"glow"}`},
		{"create GLOW again", http.MethodPost, "/api/projects", `{"name":"Glow again","prefix":"GLOW"}`},
		{"rename BILL to Glow", http.MethodPut, "/api/projects/" + bill.ID, `{"prefix":"Glow"}`},
	} {
		code, body := send(t, tc.method, url+tc.path, tc.body)
		if code != http.StatusBadRequest {
			t.Fatalf("%s: status %d (%s), want 400", tc.what, code, body)
		}
		var e struct{ Error string }
		if err := json.Unmarshal(body, &e); err != nil {
			t.Fatalf("%s: %v in %s", tc.what, err, body)
		}
		if !strings.Contains(e.Error, `already used by project "Glow"`) {
			t.Fatalf("%s: error %q should name the project that has the prefix", tc.what, e.Error)
		}
	}
}

// The reserved prefix "all", in any case, is a 400 that says why.
func TestProjectPrefixAllIs400(t *testing.T) {
	url, _ := newHistoryServer(t)

	code, body := send(t, http.MethodPost, url+"/api/projects", `{"name":"Billing","prefix":"BILL"}`)
	if code != http.StatusCreated {
		t.Fatalf("create BILL: %d %s", code, body)
	}
	var bill struct{ ID string }
	if err := json.Unmarshal(body, &bill); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		what, method, path, body string
	}{
		{"create ALL", http.MethodPost, "/api/projects", `{"name":"Everything","prefix":"ALL"}`},
		{"create all", http.MethodPost, "/api/projects", `{"name":"Everything","prefix":"all"}`},
		{"rename BILL to All", http.MethodPut, "/api/projects/" + bill.ID, `{"prefix":"All"}`},
	} {
		code, body := send(t, tc.method, url+tc.path, tc.body)
		if code != http.StatusBadRequest {
			t.Fatalf("%s: status %d (%s), want 400", tc.what, code, body)
		}
		var e struct{ Error string }
		if err := json.Unmarshal(body, &e); err != nil {
			t.Fatalf("%s: %v in %s", tc.what, err, body)
		}
		if !strings.Contains(e.Error, "is reserved") || !strings.Contains(e.Error, "every project") {
			t.Fatalf("%s: error %q should say the prefix is reserved and why", tc.what, e.Error)
		}
	}
}
