package server

import (
	"net/http"
	"testing"
)

// DELETE /api/labels/{id} takes a raw id with no key resolution, unlike the
// CLI (which resolves it first via ResolveLabelRef and errors on an unknown
// one) and MCP (same, via resolveLabelRefOrError). Before this ticket, a
// well-formed but unknown id here answered 204 as if a label had actually
// been deleted.
func TestDeleteLabelNotFound(t *testing.T) {
	r := serve(t)

	body, status := errorBody(t, http.MethodDelete, r.url+"/api/labels/01ARZ3NDEKTSV4RRFFQ69G5FAV", "")
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", status)
	}
	if body.Error == "" {
		t.Fatal("want an error message")
	}
}
