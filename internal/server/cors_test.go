package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// Two separate defenses protect the API from another origin: no CORS
// middleware, so a browser refuses to let a page on another origin read a
// cross-origin GET response (including from /api/events); and
// rejectCrossOriginWrites, which refuses a cross-origin
// POST/PUT/PATCH/DELETE outright with 403, because a "simple" request (for
// example a POST with Content-Type text/plain) triggers no CORS preflight
// and so is never touched by the first defense. See setupRoutes for the
// full rationale.

// TestCORSCrossOriginReadGetsNoAllowOriginHeader checks that a GET request
// carrying a foreign Origin header still succeeds at the transport level
// (the server itself does not block it) but comes back with no
// Access-Control-Allow-Origin header, which is what makes a browser refuse
// to let the requesting page's JavaScript read the response.
func TestCORSCrossOriginReadGetsNoAllowOriginHeader(t *testing.T) {
	r := serve(t)

	req, err := http.NewRequest(http.MethodGet, r.url+"/api/tickets", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "https://evil.example")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want empty", got)
	}
}

// TestCORSCrossOriginWritePreflightRefused checks that a browser's CORS
// preflight (an OPTIONS request with Access-Control-Request-Method) for a
// write, from a foreign Origin, gets no Access-Control-Allow-Origin or
// Access-Control-Allow-Methods header, and no success status. Without an
// allow header a browser never sends the real write request at all; there is
// no CORS middleware left to answer the preflight, so chi's router answers
// with its default 405 for a method (OPTIONS) no route registers.
func TestCORSCrossOriginWritePreflightRefused(t *testing.T) {
	r := serve(t)

	req, err := http.NewRequest(http.MethodOptions, r.url+"/api/tickets", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "content-type")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusMethodNotAllowed)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want empty", got)
	}
	if got := resp.Header.Get("Access-Control-Allow-Methods"); got != "" {
		t.Fatalf("Access-Control-Allow-Methods = %q, want empty", got)
	}
}

// TestCORSCrossOriginSimpleWriteRefused checks the case that removing the
// CORS middleware alone does not cover: a browser "simple request", which
// skips the preflight entirely. A POST with Content-Type text/plain (or
// application/x-www-form-urlencoded, or multipart/form-data) is simple, so a
// cross-origin page can send it with no preflight and no CORS headers in
// play at all, and the handler decodes the body as JSON regardless of the
// Content-Type header the browser actually sent. rejectCrossOriginWrites is
// the only thing standing between that request and the store, so this test
// asserts the request is refused with 403 and never reaches the store.
func TestCORSCrossOriginSimpleWriteRefused(t *testing.T) {
	r := serve(t)

	req, err := http.NewRequest(http.MethodPost, r.url+"/api/projects",
		strings.NewReader(`{"name":"Evil","prefix":"EVL"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("Origin", "https://evil.example")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}

	list, err := http.Get(r.url + "/api/projects")
	if err != nil {
		t.Fatal(err)
	}
	defer list.Body.Close()
	if list.StatusCode != http.StatusOK {
		t.Fatalf("listing projects: status %d", list.StatusCode)
	}
	var projects []struct{ Name string }
	if err := json.NewDecoder(list.Body).Decode(&projects); err != nil {
		t.Fatal(err)
	}
	if len(projects) != 0 {
		t.Fatalf("projects = %v, want none created", projects)
	}
}

// TestCORSSimpleWriteWithNoOriginSucceeds checks that the same "simple"
// request as above, but without an Origin header the way curl, a script, or
// the CLI/MCP server would send it, still succeeds: rejectCrossOriginWrites
// must not refuse a request just for lacking Origin, since only a browser
// adds that header for a cross-origin request in the first place.
func TestCORSSimpleWriteWithNoOriginSucceeds(t *testing.T) {
	r := serve(t)

	resp, err := http.Post(r.url+"/api/projects", "text/plain",
		strings.NewReader(`{"name":"NoOrigin","prefix":"NOG"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}
}

// TestCORSSimpleWriteWithSameOriginSucceeds checks that the same "simple"
// request, sent with an Origin header matching the server's own origin (the
// way the embedded UI or the Vite dev proxy would present it), still
// succeeds.
func TestCORSSimpleWriteWithSameOriginSucceeds(t *testing.T) {
	r := serve(t)

	req, err := http.NewRequest(http.MethodPost, r.url+"/api/projects",
		strings.NewReader(`{"name":"SameOrigin","prefix":"SOG"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("Origin", r.url)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}
}

// TestCORSSameOriginRequestStillWorks checks that a normal same-origin style
// request (no Origin header at all, the way a browser navigation or a
// non-browser client such as the CLI, MCP server or curl would send it)
// still gets a full, ordinary response. CORS headers are irrelevant here:
// the browser only enforces CORS for cross-origin requests, and this request
// carries no Origin because it isn't one.
func TestCORSSameOriginRequestStillWorks(t *testing.T) {
	r := serve(t)

	resp, err := http.Get(r.url + "/api/tickets")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
}
