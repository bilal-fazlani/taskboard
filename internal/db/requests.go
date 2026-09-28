package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/tcarac/taskboard/internal/models"
)

// DefaultAwaitPoll is how often AwaitAnswer reads its request again. The
// answer may come from another process (the web UI's server, the CLI), so
// polling the database is the only way to see it.
const DefaultAwaitPoll = 500 * time.Millisecond

// nullIfEmpty is s as a NULL write when it is empty, or itself otherwise:
// for an optional column, such as a request's note, where "" and "never
// given" mean the same thing.
func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// requestSelect reads a request for user input. A request on a ticket in a
// deleted project is not read.
const requestSelect = `SELECT r.id, r.ticket_id, r.agent_id, r.type, r.prompt, r.choices,
		r.answer, r.answered_by, r.note, r.created_at, r.answered_at
	FROM ticket_requests r`

// liveRequest keeps requestSelect to requests on live tickets.
const liveRequest = ` r.ticket_id IN (` + liveTicketIDs + `)`

func scanRequest(row interface{ Scan(...any) error }) (models.TicketRequest, error) {
	var r models.TicketRequest
	var choices string
	var answer, answeredBy, note sql.NullString
	if err := row.Scan(&r.ID, &r.TicketID, &r.AgentID, &r.Type, &r.Prompt, &choices,
		&answer, &answeredBy, &note, &r.CreatedAt, &r.AnsweredAt); err != nil {
		return r, err
	}
	if err := json.Unmarshal([]byte(choices), &r.Choices); err != nil {
		return r, fmt.Errorf("reading request %s's choices: %w", r.ID, err)
	}
	r.Answer, r.AnsweredBy, r.Note = answer.String, answeredBy.String, note.String
	return r, nil
}

// GetRequest returns one request for user input, answered or not, or
// (nil, nil) for an unknown one or one on a ticket in a deleted project.
func (s *Store) GetRequest(id string) (*models.TicketRequest, error) {
	return getRequest(s.db, strings.TrimSpace(id))
}

func getRequest(q dbtx, id string) (*models.TicketRequest, error) {
	r, err := scanRequest(q.QueryRow(requestSelect+` WHERE r.id = ? AND`+liveRequest, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// openRequestOn is the ticket's unanswered request, or nil when it has none.
func openRequestOn(q dbtx, ticketID string) (*models.TicketRequest, error) {
	r, err := scanRequest(q.QueryRow(requestSelect+` WHERE r.ticket_id = ? AND r.answered_at IS NULL`, ticketID))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading the open request: %w", err)
	}
	return &r, nil
}

// ListRequests returns a ticket's requests for user input, newest first: its
// whole history, answered and unanswered alike. ticketID is a resolved
// ticket id, not a display key.
func (s *Store) ListRequests(ticketID string) ([]models.TicketRequest, error) {
	rows, err := s.db.Query(requestSelect+` WHERE r.ticket_id = ? AND`+liveRequest+`
		ORDER BY r.created_at DESC, r.rowid DESC`, ticketID)
	if err != nil {
		return nil, fmt.Errorf("listing requests: %w", err)
	}
	defer rows.Close()
	out := []models.TicketRequest{}
	for rows.Next() {
		req, err := scanRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, req)
	}
	return out, rows.Err()
}

// CreateRequest records the agent asking the person for user input on a
// ticket (an id or display key) and moves the ticket to needs_user_input,
// whatever the type. It answers with the new request's id. The type must be
// one of models.UserInputTypes, the prompt is required, and choices, when
// given, may not be blank. A ticket has one open request at a time, so a
// second one while the first is unanswered is refused. The ticket must be
// held by an agent, though not necessarily the one asking: an orchestrator
// may ask for approval of the work its implementer holds. It touches the
// agent. Anything wrong is an ErrInvalidInput and writes nothing; an agent
// whose session's work on the ticket the person stopped gets an ErrStopped.
func (s *Store) CreateRequest(req models.CreateUserInputRequest) (string, error) {
	typ := strings.TrimSpace(req.Type)
	prompt := strings.TrimSpace(req.Prompt)
	agentID := strings.TrimSpace(req.AgentID)
	if !models.ValidUserInputType(typ) {
		return "", invalidInput("type %q is not a type of user input: use one of %s", typ, strings.Join(models.UserInputTypes, ", "))
	}
	if prompt == "" {
		return "", invalidInput("prompt is required: what the person is asked")
	}
	choices := make([]string, 0, len(req.Choices))
	for _, c := range req.Choices {
		c = strings.TrimSpace(c)
		if c == "" {
			return "", invalidInput("a choice is blank: each choice is an answer the person can give")
		}
		choices = append(choices, c)
	}
	rawChoices, err := json.Marshal(choices)
	if err != nil {
		return "", err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return "", fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	ticketID, err := resolveTicketArg(tx, req.TicketID)
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	if err := touchAgent(tx, agentID, now); err != nil {
		return "", err
	}
	if err := checkNotStopped(tx, ticketID, agentID); err != nil {
		return "", err
	}
	if open, err := openRequestOn(tx, ticketID); err != nil {
		return "", err
	} else if open != nil {
		key, err := ticketKey(tx, ticketID)
		if err != nil {
			return "", err
		}
		return "", invalidInput("ticket %s already waits on request %s (%s), not yet answered: a ticket has one open request at a time",
			key, open.ID, open.Type)
	}
	status, holder, err := ticketHolding(tx, ticketID)
	if err != nil {
		return "", err
	}
	if holder == "" {
		key, err := ticketKey(tx, ticketID)
		if err != nil {
			return "", err
		}
		return "", invalidInput("ticket %s is not held by any agent: a request goes on a ticket an agent is working, so start it first", key)
	}

	id := newID()
	if _, err := tx.Exec(`INSERT INTO ticket_requests (id, ticket_id, agent_id, type, prompt, choices, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, id, ticketID, agentID, typ, prompt, string(rawChoices), stamp(now)); err != nil {
		return "", fmt.Errorf("writing request: %w", err)
	}
	note := fmt.Sprintf("Agent %s asks for %s: request %s.", agentID, typ, id)
	if err := setStatus(tx, ticketID, status, models.StatusNeedsUserInput, note, now); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("committing request: %w", err)
	}
	return id, nil
}

// AnswerRequest stores the person's answer to a request, who gave it and an
// optional note, and moves the request's ticket from needs_user_input back
// to in_progress; a ticket someone has since moved elsewhere stays where it
// is. A question's answer may be any non-empty text; when it matches one of
// the request's choices, matched ignoring case, it is stored as the choice
// is written. An approval's answer is always models.ApprovalApproved or
// models.ApprovalDeclined, matched ignoring case and stored as those are
// written, whatever choices the request offers. It answers with the
// answered request. An unknown request, one already answered, a blank
// answer or answerer, or an approval answered with anything else, is an
// ErrInvalidInput and writes nothing.
func (s *Store) AnswerRequest(requestID, answer, answeredBy, note string) (*models.TicketRequest, error) {
	requestID = strings.TrimSpace(requestID)
	answer = strings.TrimSpace(answer)
	answeredBy = strings.TrimSpace(answeredBy)
	note = strings.TrimSpace(note)
	if answer == "" {
		return nil, invalidInput("answer is required")
	}
	if answeredBy == "" {
		return nil, invalidInput("answeredBy is required: who answered")
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	r, err := getRequest(tx, requestID)
	if err != nil {
		return nil, err
	}
	if r == nil {
		return nil, invalidInput("request not found: %q", requestID)
	}
	if r.Answered() {
		return nil, invalidInput("request %s is already answered, by %s", r.ID, r.AnsweredBy)
	}
	switch r.Type {
	case models.UserInputApproval:
		switch {
		case strings.EqualFold(answer, models.ApprovalApproved):
			answer = models.ApprovalApproved
		case strings.EqualFold(answer, models.ApprovalDeclined):
			answer = models.ApprovalDeclined
		default:
			return nil, invalidInput("answer %q is not %s or %s: an approval's answer is always one of the two",
				answer, models.ApprovalApproved, models.ApprovalDeclined)
		}
	default:
		for _, c := range r.Choices {
			if strings.EqualFold(c, answer) {
				answer = c
				break
			}
		}
	}

	now := time.Now().UTC()
	if _, err := tx.Exec(`UPDATE ticket_requests SET answer = ?, answered_by = ?, note = ?, answered_at = ? WHERE id = ? AND answered_at IS NULL`,
		answer, answeredBy, nullIfEmpty(note), stamp(now), r.ID); err != nil {
		return nil, fmt.Errorf("answering request: %w", err)
	}
	status, _, err := ticketHolding(tx, r.TicketID)
	if err != nil {
		return nil, err
	}
	if status == models.StatusNeedsUserInput {
		note := fmt.Sprintf("Answered by %s: request %s.", answeredBy, r.ID)
		if err := setStatus(tx, r.TicketID, status, models.StatusInProgress, note, now); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing answer: %w", err)
	}
	return s.GetRequest(r.ID)
}

// AwaitAnswer waits until the request is answered, the timeout passes or ctx
// is done, reading the request again every poll interval, since the answer
// may be written by another process. It waits on this request only, never
// on whatever else is open on its ticket, so an agent acts only on the
// answer it asked for. It answers with the request as last read: answered,
// or not when the timeout passed (with a nil error) or ctx ended (with
// ctx's error). An unknown request is an ErrInvalidInput.
//
// Waiting keeps the agent that asked alive: every poll touches it
// (touchWaiting), so a wait on the person never makes it stale and open to
// a takeover.
func (s *Store) AwaitAnswer(ctx context.Context, requestID string, timeout time.Duration) (*models.TicketRequest, error) {
	requestID = strings.TrimSpace(requestID)
	deadline := time.Now().Add(timeout)
	for {
		r, err := s.GetRequest(requestID)
		if err != nil {
			return nil, err
		}
		if r == nil {
			return nil, invalidInput("request not found: %q", requestID)
		}
		if err := s.touchWaiting(r.AgentID); err != nil {
			return nil, err
		}
		left := time.Until(deadline)
		if r.Answered() || left <= 0 {
			return r, nil
		}
		wait := min(s.awaitPoll, left)
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return r, ctx.Err()
		case <-timer.C:
		}
	}
}

// touchWaiting records that an agent waiting on its request is alive. It
// writes only when the agent's last seen is older than a quarter of the
// stale threshold, and at most a minute old: that keeps the agent far from
// stale, while a poll every half second does not write, and so refresh
// every open board, every half second.
func (s *Store) touchWaiting(agentID string) error {
	settings, err := s.AgentSettings()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	fresh := min(time.Minute, settings.StaleAfter/4)
	if _, err := s.db.Exec(`UPDATE agents SET last_seen_at = ? WHERE id = ? AND last_seen_at < ?`,
		stamp(now), agentID, stamp(now.Add(-fresh))); err != nil {
		return fmt.Errorf("touching agent %q: %w", agentID, err)
	}
	return nil
}
