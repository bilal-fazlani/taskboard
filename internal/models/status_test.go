package models

import "testing"

// The order of Statuses is the board's column order, and every other layer
// (board, table, filters, CLI help, MCP enums) reads it, so the order is part
// of the contract rather than an implementation detail.
func TestStatusesAreInBoardColumnOrder(t *testing.T) {
	want := []string{"todo", "in_progress", "agent_review", "done"}
	if len(Statuses) != len(want) {
		t.Fatalf("Statuses = %v, want %v", Statuses, want)
	}
	for i, status := range want {
		if Statuses[i] != status {
			t.Errorf("Statuses[%d] = %q, want %q", i, Statuses[i], status)
		}
	}
}

func TestStatusConstantsMatchTheirWireValues(t *testing.T) {
	for _, tc := range []struct{ got, want string }{
		{StatusTodo, "todo"},
		{StatusInProgress, "in_progress"},
		{StatusAgentReview, "agent_review"},
		{StatusDone, "done"},
	} {
		if tc.got != tc.want {
			t.Errorf("status constant = %q, want %q", tc.got, tc.want)
		}
	}
}
