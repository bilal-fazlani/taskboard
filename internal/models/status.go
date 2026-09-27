package models

import "slices"

// Ticket status values. Every layer reads the status set from here: Statuses
// is what the board, the tickets table and filters, the CLI flag help and the
// MCP tool schemas offer, and KnownStatuses adds the statuses no surface
// offers yet. Adding a status is a data change here, not a hunt through
// every layer.
const (
	StatusTodo       = "todo"
	StatusInProgress = "in_progress"
	// StatusNeedsUserInput is a ticket waiting on the person, whatever the
	// type of user input it waits on (UserInputTypes). There is no separate
	// status per type.
	StatusNeedsUserInput = "needs_user_input"
	// StatusAgentReview is a ticket the implementer agent has handed to the
	// review agent. It sits between in_progress and done because the work is
	// written but not accepted, and it tells the two directions of that
	// bounce apart, which both used to look like in_progress.
	StatusAgentReview = "agent_review"
	StatusDone        = "done"
)

// Statuses is the ordered list of statuses the surfaces offer (the board's
// columns, filters, the CLI and the MCP enums) and the store accepts in a
// write or a filter, in board column order.
var Statuses = []string{StatusTodo, StatusInProgress, StatusAgentReview, StatusDone}

// KnownStatuses is every status a ticket can hold: Statuses with
// StatusNeedsUserInput after in_progress. No surface offers it yet; each
// surface moves to offering it on its own.
var KnownStatuses = slices.Insert(slices.Clone(Statuses),
	slices.Index(Statuses, StatusInProgress)+1, StatusNeedsUserInput)

// IsKnownStatus reports whether status is one of KnownStatuses.
func IsKnownStatus(status string) bool {
	return slices.Contains(KnownStatuses, status)
}
