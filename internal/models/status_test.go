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
		{StatusNeedsUserInput, "needs_user_input"},
		{StatusAgentReview, "agent_review"},
		{StatusDone, "done"},
	} {
		if tc.got != tc.want {
			t.Errorf("status constant = %q, want %q", tc.got, tc.want)
		}
	}
}

// needs_user_input is a status a ticket can hold, but no surface offers it
// yet: Statuses, which every surface reads, leaves it out.
func TestNeedsUserInputIsKnownButNotOffered(t *testing.T) {
	want := []string{"todo", "in_progress", "needs_user_input", "agent_review", "done"}
	if len(KnownStatuses) != len(want) {
		t.Fatalf("KnownStatuses = %v, want %v", KnownStatuses, want)
	}
	for i, status := range want {
		if KnownStatuses[i] != status {
			t.Errorf("KnownStatuses[%d] = %q, want %q", i, KnownStatuses[i], status)
		}
		if !IsKnownStatus(status) {
			t.Errorf("IsKnownStatus(%q) = false, want true", status)
		}
	}
	for _, status := range []string{"", "needs_approval", "needs_input", "Todo"} {
		if IsKnownStatus(status) {
			t.Errorf("IsKnownStatus(%q) = true, want false", status)
		}
	}
	for _, status := range Statuses {
		if status == StatusNeedsUserInput {
			t.Fatalf("Statuses offers %q; no surface offers it yet", status)
		}
		if !IsKnownStatus(status) {
			t.Errorf("offered status %q is not known", status)
		}
	}
}
