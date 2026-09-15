package models

// Ticket status values, in board column order. This is the single source of
// truth for the status set: the board, the tickets table and filters, the
// CLI flag help, and the MCP tool schemas all read from it, so adding a
// status (M3 adds needs_input and needs_review) is a data change here, not a
// hunt through every layer.
const (
	StatusTodo       = "todo"
	StatusInProgress = "in_progress"
	StatusDone       = "done"
)

// Statuses is the ordered list of valid ticket statuses, in board column
// order.
var Statuses = []string{StatusTodo, StatusInProgress, StatusDone}
