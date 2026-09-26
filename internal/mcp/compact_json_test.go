package mcp

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/tcarac/taskboard/internal/imagedoc/imagedoctest"
	"github.com/tcarac/taskboard/internal/weburl"
)

// assertCompactJSON fails if text is not valid JSON, or is not written in
// its compact form (no indentation, no newlines): MarshalIndent output
// always contains "\n" between fields once there is more than one, which
// json.Marshal never emits.
func assertCompactJSON(t *testing.T, label, text string) {
	t.Helper()
	if !json.Valid([]byte(text)) {
		t.Fatalf("%s is not valid JSON: %q", label, text)
	}
	if strings.Contains(text, "\n") || strings.Contains(text, "  ") {
		t.Fatalf("%s is not compact JSON: %q", label, text)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(text)); err != nil {
		t.Fatalf("%s: compacting for comparison: %v", label, err)
	}
	if compact.String() != text {
		t.Fatalf("%s is not already compact:\ngot:  %q\nwant: %q", label, text, compact.String())
	}
}

// TestToolAnswersAreCompactJSON pins handleToolCall's text content to
// compact JSON (no MarshalIndent) across a spread of tools: a list, a single
// object, and a plain confirmation. Tokens are spent on every answer an
// agent reads, so the indentation and newlines json.MarshalIndent added were
// pure waste.
func TestToolAnswersAreCompactJSON(t *testing.T) {
	s := newTestServer(t)
	tk := seedMCPTicket(t, s)

	text, isError := callToolText(t, s, "list_projects", map[string]any{})
	if isError {
		t.Fatalf("list_projects errored: %s", text)
	}
	assertCompactJSON(t, "list_projects", text)

	text, isError = callToolText(t, s, "get_ticket", map[string]any{"id": tk.ID})
	if isError {
		t.Fatalf("get_ticket errored: %s", text)
	}
	assertCompactJSON(t, "get_ticket", text)
	// Sanity: it really is the ticket, not an empty or truncated body.
	var got map[string]any
	if err := json.Unmarshal([]byte(text), &got); err != nil || got["id"] != tk.ID {
		t.Fatalf("get_ticket text = %q, want it to decode to ticket %s", text, tk.ID)
	}

	text, isError = callToolText(t, s, "move_ticket", map[string]any{"id": tk.ID, "status": "in_progress"})
	if isError {
		t.Fatalf("move_ticket errored: %s", text)
	}
	assertCompactJSON(t, "move_ticket", text)
}

// TestImageDocumentDetailsAreCompactJSON pins the text part of get_document's
// image answer (built in documents.go, marshalled separately from
// handleToolCall) to compact JSON too.
func TestImageDocumentDetailsAreCompactJSON(t *testing.T) {
	t.Setenv(weburl.BaseEnv, "http://board.test")
	s := newTestServer(t)
	tk := seedMCPTicket(t, s)

	if _, err := s.callTool("create_document", mustJSON(t, map[string]any{
		"full": true, "ticket": tk.ID, "name": "Shot", "format": "png",
		"data": base64.StdEncoding.EncodeToString(imagedoctest.PNG(20, 20)),
	})); err != nil {
		t.Fatalf("create_document: %v", err)
	}

	resp := s.handleRequest(jsonrpcRequest{JSONRPC: "2.0", ID: 1, Method: "tools/call",
		Params: mustJSON(t, map[string]any{"name": "get_document", "arguments": map[string]any{"id": "Shot.png", "ticket": tk.ID}})})
	raw, _ := json.Marshal(resp)
	var out struct {
		Result struct {
			Content []struct{ Text string } `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || len(out.Result.Content) == 0 {
		t.Fatalf("get_document: %s", raw)
	}
	var meta map[string]any
	if err := json.Unmarshal([]byte(out.Result.Content[0].Text), &meta); err != nil || meta["name"] != "Shot" {
		t.Fatalf("get_document details = %q, want name Shot", out.Result.Content[0].Text)
	}
	assertCompactJSON(t, "get_document image details", out.Result.Content[0].Text)
}
