package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/tcarac/taskboard/internal/models"
)

// StoppedAnswer is what a request left open when the person stopped work
// is closed with (models.StoppedAnswer): the agent waiting on it learns, on
// its next poll, that the ticket is no longer its own.
const StoppedAnswer = models.StoppedAnswer

// ErrStopped is an agent's write on a ticket refused because the person
// stopped the work its session held there (StopWork). It says so in the words
// "stopped by the person", and that the hand-off is the one write still
// accepted. It is also an ErrInvalidInput, so a surface that knows no better
// reports it as the caller's mistake; the HTTP layer recognizes it ahead of
// that and answers 409 instead.
type ErrStopped struct {
	Ticket string
	msg    string
}

func (e *ErrStopped) Error() string { return e.msg }

// Unwrap makes errors.As find an ErrInvalidInput with the same message.
func (e *ErrStopped) Unwrap() error { return &ErrInvalidInput{Msg: e.msg} }

// StopWork is the person, stoppedBy, stopping the work an agent does on the
// ticket (an id or display key), whether that agent is live or stale. The
// agent is cleared and the ticket returns to todo at once, with a row of
// status history naming who stopped it and the agent that held it. A request
// left open is closed with StoppedAnswer, so the ticket no longer waits on
// the person. The agent is not asked: agents pull, so its session learns on
// its next write on the ticket, which is refused with an ErrStopped, all but
// its hand-off (checkNotStopped). A ticket no agent holds is an
// ErrInvalidInput, and nothing is written.
func (s *Store) StopWork(ticketID, stoppedBy string) (*models.Ticket, error) {
	stoppedBy = strings.TrimSpace(stoppedBy)
	if stoppedBy == "" {
		return nil, invalidInput("stoppedBy is required: the person stopping the work")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	id, err := resolveTicketArg(tx, ticketID)
	if err != nil {
		return nil, err
	}
	status, holder, err := ticketHolding(tx, id)
	if err != nil {
		return nil, err
	}
	if holder == "" {
		key, err := ticketKey(tx, id)
		if err != nil {
			return nil, err
		}
		return nil, invalidInput("ticket %s is not held by any agent: there is no work on it to stop", key)
	}
	held, err := scanAgent(tx.QueryRow(agentSelect+` WHERE a.id = ?`, holder))
	if err != nil {
		return nil, fmt.Errorf("reading the holding agent %q: %w", holder, err)
	}

	now := time.Now().UTC()
	note := fmt.Sprintf("Stopped by the person, %s. Agent %s (%s, %s) held it.", stoppedBy, held.ID, held.Role, held.Model)
	if open, err := openRequestOn(tx, id); err != nil {
		return nil, err
	} else if open != nil {
		if _, err := tx.Exec(`UPDATE ticket_requests SET answer = ?, answered_by = ?, answered_at = ? WHERE id = ? AND answered_at IS NULL`,
			StoppedAnswer, stoppedBy, stamp(now), open.ID); err != nil {
			return nil, fmt.Errorf("closing the open request: %w", err)
		}
		note += fmt.Sprintf(" Its open request %s (%s) was closed unanswered.", open.ID, open.Type)
	}
	if _, err := tx.Exec(`INSERT INTO ticket_stops (ticket_id, agent_id, stopped_by, stopped_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (ticket_id, agent_id) DO UPDATE SET stopped_by = excluded.stopped_by, stopped_at = excluded.stopped_at`,
		id, holder, stoppedBy, stamp(now)); err != nil {
		return nil, fmt.Errorf("recording the stop: %w", err)
	}
	if _, err := tx.Exec(`UPDATE tickets SET agent_id = NULL, updated_at = ? WHERE id = ?`, stamp(now), id); err != nil {
		return nil, fmt.Errorf("stopping work: %w", err)
	}
	if err := setStatus(tx, id, status, models.StatusTodo, note, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing the stop: %w", err)
	}
	return s.GetTicket(id)
}

// checkNotStopped refuses agentID's write on the ticket with an ErrStopped
// when the person stopped work that an agent of agentID's session held
// there, and its session has not claimed the ticket again since. A session
// owns the tickets its agents hold, so a resumed chat, a new agent in the
// stopped one's session, learns of the stop too. The caller lets the
// hand-off through: it is the one write still accepted.
func checkNotStopped(q dbtx, ticketID, agentID string) error {
	var stoppedBy string
	var stoppedAt time.Time
	err := q.QueryRow(`SELECT st.stopped_by, st.stopped_at FROM ticket_stops st
		JOIN agents held ON held.id = st.agent_id
		JOIN agents caller ON caller.session_id = held.session_id
		WHERE st.ticket_id = ? AND caller.id = ?
		ORDER BY st.stopped_at DESC LIMIT 1`, ticketID, agentID).Scan(&stoppedBy, &stoppedAt)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading whether work on the ticket was stopped: %w", err)
	}
	key, err := ticketKey(q, ticketID)
	if err != nil {
		return err
	}
	return &ErrStopped{Ticket: key, msg: fmt.Sprintf(
		"work on ticket %s was stopped by the person (%s, at %s): your session no longer holds it, and no write of yours "+
			"on it is accepted but your hand-off. Give it back with outcome %s and a hand-off saying where the work "+
			"stood, then stop working on it",
		key, stoppedBy, stoppedAt.UTC().Format(time.RFC3339), models.ReleaseGiveBack)}
}

// forgetStops removes the stops recorded against any agent of sessionID on
// the ticket: that session has claimed it again, so the person's stop no
// longer stands between it and its writes. It answers with the latest stop
// it lifted, or nil when there was none.
func forgetStops(q dbtx, ticketID, sessionID string) (*models.Stop, error) {
	var stop models.Stop
	err := q.QueryRow(`SELECT stopped_by, stopped_at FROM ticket_stops
		WHERE ticket_id = ? AND agent_id IN (SELECT id FROM agents WHERE session_id = ?)
		ORDER BY stopped_at DESC LIMIT 1`, ticketID, sessionID).Scan(&stop.By, &stop.At)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading the ticket's stops: %w", err)
	}
	if _, err := q.Exec(`DELETE FROM ticket_stops WHERE ticket_id = ? AND agent_id IN (SELECT id FROM agents WHERE session_id = ?)`,
		ticketID, sessionID); err != nil {
		return nil, fmt.Errorf("clearing the ticket's stops: %w", err)
	}
	stop.At = stop.At.UTC()
	return &stop, nil
}
