package models

// Ticket status values, in board column order. This is the single source of
// truth for the status set: the board, the tickets table and filters, the
// CLI flag help, and the MCP tool schemas all read from it, so adding a
// status (M4 adds needs_input and needs_approval) is a data change here, not
// a hunt through every layer.
const (
	StatusTodo       = "todo"
	StatusInProgress = "in_progress"
	// StatusAgentReview is a ticket the implementer agent has handed to the
	// review agent. It sits between in_progress and done because the work is
	// written but not accepted, and it tells the two directions of that
	// bounce apart, which both used to look like in_progress.
	StatusAgentReview = "agent_review"
	StatusDone        = "done"
)

// Statuses is the ordered list of valid ticket statuses, in board column
// order.
var Statuses = []string{StatusTodo, StatusInProgress, StatusAgentReview, StatusDone}
