package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/tcarac/taskboard/internal/models"
)

// WriteOption adjusts how UpdateTicket and MoveTicket write a ticket. The
// store applies the rules; which callers ask for them is up to the callers.
type WriteOption func(*writeOptions)

type writeOptions struct {
	requireNoteLeavingReview bool
}

// RequireNoteLeavingReview makes a status change that leaves agent_review
// for any other status fail with an ErrInvalidInput unless it carries a note
// that is not blank. The check runs inside the write's transaction, against
// the status the ticket has at that moment.
func RequireNoteLeavingReview() WriteOption {
	return func(o *writeOptions) { o.requireNoteLeavingReview = true }
}

func collectWriteOptions(opts []WriteOption) writeOptions {
	var o writeOptions
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// NoteRequiredLeavingReviewMsg is the error a status change out of
// agent_review gets without a note when RequireNoteLeavingReview is set. The
// MCP tools set it, and for an agent this text is the documentation, so it
// says what the note is for.
const NoteRequiredLeavingReviewMsg = "a note is required when moving a ticket out of agent_review: " +
	"pass note saying why it is leaving review, either that it was approved and landed, " +
	"or the review findings it is being sent back to fix"

// checkStatusChange applies the write options' rules to a change from one
// status to another.
func (o writeOptions) checkStatusChange(from, to, note string) error {
	if o.requireNoteLeavingReview &&
		from == models.StatusAgentReview && to != models.StatusAgentReview &&
		strings.TrimSpace(note) == "" {
		return invalidInput("%s", NoteRequiredLeavingReviewMsg)
	}
	return nil
}

// validStatus reports whether status is one of models.Statuses, the single
// source of truth for the status set. It gates writes (CreateTicket,
// UpdateTicket, MoveTicket) and a list's status filter (splitStatusFilter): a
// row already holding some other value, from before this check existed, must
// still be readable, so no path that reads a ticket's own stored status
// calls it.
func validStatus(status string) bool {
	for _, s := range models.Statuses {
		if status == s {
			return true
		}
	}
	return false
}

// invalidStatus is the error a write gets for a status outside
// models.Statuses, naming the valid values so the caller — HTTP, MCP or the
// CLI — knows what to send instead.
func invalidStatus(status string) error {
	return invalidInput("invalid status %q: must be one of %s", status, strings.Join(models.Statuses, ", "))
}

// currentStatus reads a ticket's status inside q's transaction, so a rule
// checked against it and the write that follows see the same value. found is
// false when there is no such ticket.
func currentStatus(q dbtx, ticketID string) (status string, found bool, err error) {
	err = q.QueryRow("SELECT status FROM tickets WHERE id = ?", ticketID).Scan(&status)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("reading ticket status: %w", err)
	}
	return status, true, nil
}

// recordStatusChange writes one row of a ticket's status history. from is ""
// for the row written when the ticket is created. It is stored in UTC so the
// text SQLite keeps sorts in time order whatever the local offset was.
func recordStatusChange(q dbtx, ticketID, from, to, note string, at time.Time) error {
	if _, err := q.Exec(
		`INSERT INTO ticket_status_changes (id, ticket_id, from_status, to_status, note, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		newID(), ticketID, from, to, strings.TrimSpace(note), stamp(at),
	); err != nil {
		return fmt.Errorf("recording status change: %w", err)
	}
	return nil
}

// ListStatusChanges returns a ticket's status history, newest first. A ticket
// with no history, or no ticket at all, gives an empty list.
func (s *Store) ListStatusChanges(ticketID string) ([]models.StatusChange, error) {
	// rowid breaks ties between changes written within the same instant, in
	// the order they were written.
	rows, err := s.db.Query(
		`SELECT id, ticket_id, from_status, to_status, note, created_at
		FROM ticket_status_changes WHERE ticket_id = ?
		ORDER BY created_at DESC, rowid DESC`, ticketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	changes := []models.StatusChange{}
	for rows.Next() {
		var c models.StatusChange
		if err := rows.Scan(&c.ID, &c.TicketID, &c.FromStatus, &c.ToStatus, &c.Note, &c.CreatedAt); err != nil {
			return nil, err
		}
		changes = append(changes, c)
	}
	return changes, rows.Err()
}

// reviewRoundsQuery counts entries into agent_review. The list form groups it
// by ticket, so a page of tickets costs one query rather than one per ticket.
const reviewRoundsQuery = `SELECT ticket_id, COUNT(*) FROM ticket_status_changes
	WHERE to_status = '` + models.StatusAgentReview + `'`

func (s *Store) getTicketReviewRounds(ticketID string) (int, error) {
	var id string
	var n int
	err := s.db.QueryRow(reviewRoundsQuery+` AND ticket_id = ? GROUP BY ticket_id`, ticketID).Scan(&id, &n)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return n, err
}

// attachReviewRounds fills ReviewRounds for a page of tickets. index maps a
// ticket id to its place in tickets; placeholders and ids are the IN list
// attachListDetails already built for the same page.
func (s *Store) attachReviewRounds(tickets []models.Ticket, index map[string]int, placeholders string, ids []any) error {
	rows, err := s.db.Query(reviewRoundsQuery+` AND ticket_id IN (`+placeholders+`) GROUP BY ticket_id`, ids...)
	if err != nil {
		return fmt.Errorf("loading review rounds: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var ticketID string
		var n int
		if err := rows.Scan(&ticketID, &n); err != nil {
			return err
		}
		if i, ok := index[ticketID]; ok {
			tickets[i].ReviewRounds = n
		}
	}
	return rows.Err()
}
