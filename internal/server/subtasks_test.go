package server

import (
	"net/http"
	"testing"
)

// DELETE /api/subtasks/{id} takes a raw id with no key resolution, like
// DELETE /api/tickets/{id} and DELETE /api/labels/{id}. Before this ticket,
// store.DeleteSubtask never checked RowsAffected, so a well-formed but
// unknown id here answered 204 as if a subtask had actually been deleted
// (ACP-121; ACP-63 fixed every other Delete* store method and HTTP route but
// left this one as a dependent follow-up).
func TestDeleteSubtaskNotFound(t *testing.T) {
	r := serve(t)

	body, status := errorBody(t, http.MethodDelete, r.url+"/api/subtasks/01ARZ3NDEKTSV4RRFFQ69G5FAV", "")
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", status)
	}
	if body.Error == "" {
		t.Fatal("want an error message")
	}
}
