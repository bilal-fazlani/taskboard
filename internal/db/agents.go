package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/tcarac/taskboard/internal/models"
)

// agentColumns reads an agent aliased a, with the last seen of its session:
// the latest last seen among the session's agents. Stale is not a column:
// the store sets it on every read (markStale).
const agentColumns = `a.id, a.session_id, a.role, a.model, a.provider, a.created_at, a.last_seen_at,
	(SELECT MAX(o.last_seen_at) FROM agents o WHERE o.session_id = a.session_id)`

// agentSelect reads agents, aliased a.
const agentSelect = `SELECT ` + agentColumns + ` FROM agents a`

// scanAgent reads agentColumns, after any columns in before.
func scanAgent(row interface{ Scan(...any) error }, before ...any) (models.Agent, error) {
	var a models.Agent
	// MAX() loses the column's type, so the driver hands back the stored
	// text rather than a time.
	var sessionLastSeen string
	dest := append(before, &a.ID, &a.SessionID, &a.Role, &a.Model, &a.Provider, &a.CreatedAt, &a.LastSeenAt, &sessionLastSeen)
	if err := row.Scan(dest...); err != nil {
		return a, err
	}
	t, err := time.Parse(sortableTimeFormat, sessionLastSeen)
	if err != nil {
		return a, fmt.Errorf("reading the last seen of agent %s's session: %w", a.ID, err)
	}
	a.SessionLastSeenAt = t
	return a, nil
}

// AgentSettings reads the install's agent timings, the stale threshold and
// the lease, from the database, where every process reads them.
func (s *Store) AgentSettings() (models.AgentSettings, error) {
	return agentSettings(s.db)
}

func agentSettings(q dbtx) (models.AgentSettings, error) {
	var staleAfter, lease int64
	err := q.QueryRow(`SELECT stale_after_seconds, lease_seconds FROM agent_settings WHERE id = 1`).Scan(&staleAfter, &lease)
	if err == sql.ErrNoRows {
		return models.DefaultAgentSettings(), nil
	}
	if err != nil {
		return models.AgentSettings{}, fmt.Errorf("reading the agent settings: %w", err)
	}
	return models.AgentSettings{StaleAfter: time.Duration(staleAfter) * time.Second, Lease: time.Duration(lease) * time.Second}, nil
}

// UpdateAgentSettings changes the timings req sets, for the whole install,
// and answers with both as they now are. Each must be a positive whole
// number of seconds; anything else is an ErrInvalidInput and changes
// nothing.
func (s *Store) UpdateAgentSettings(req models.UpdateAgentSettingsRequest) (models.AgentSettings, error) {
	for _, setting := range []struct {
		name  string
		value *time.Duration
	}{{"stale-after", req.StaleAfter}, {"lease", req.Lease}} {
		if setting.value == nil {
			continue
		}
		if d := *setting.value; d < time.Second || d%time.Second != 0 {
			return models.AgentSettings{}, invalidInput("%s is %s: it must be a positive whole number of seconds, such as 45m or 2h",
				setting.name, d)
		}
	}
	tx, err := s.db.Begin()
	if err != nil {
		return models.AgentSettings{}, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	settings, err := agentSettings(tx)
	if err != nil {
		return settings, err
	}
	if req.StaleAfter != nil {
		settings.StaleAfter = *req.StaleAfter
	}
	if req.Lease != nil {
		settings.Lease = *req.Lease
	}
	if settings.Lease < settings.StaleAfter {
		return models.AgentSettings{}, invalidInput("lease (%s) is shorter than stale-after (%s): "+
			"a ticket would be given back before its agent counts as stale; keep the lease at least as long",
			models.FormatDuration(settings.Lease), models.FormatDuration(settings.StaleAfter))
	}
	if _, err := tx.Exec(`INSERT INTO agent_settings (id, stale_after_seconds, lease_seconds) VALUES (1, ?, ?)
		ON CONFLICT (id) DO UPDATE SET stale_after_seconds = excluded.stale_after_seconds, lease_seconds = excluded.lease_seconds`,
		int64(settings.StaleAfter/time.Second), int64(settings.Lease/time.Second)); err != nil {
		return settings, fmt.Errorf("writing the agent settings: %w", err)
	}
	return settings, tx.Commit()
}

// markStale is the one place staleness is decided. A session owns the
// tickets its agents hold, so an agent is stale once its whole session has
// gone unseen, no agent of it seen, for longer than the install's stale
// threshold: an orchestrator waiting on the person keeps the ticket its
// quiet implementer holds alive. ClaimTicket's takeover rule and every read
// of an agent (its Stale field) both ask here, with the settings every
// process reads from the database, so what the board shows and what a claim
// does always agree.
func markStale(settings models.AgentSettings, a *models.Agent, now time.Time) {
	a.Stale = now.Sub(a.SessionLastSeenAt) > settings.StaleAfter
}

// IdentifyAgent finds the session req names by its vendor and the vendor's
// session ID, creating it when there is none, and creates a new agent in it
// with req's role, model and provider, seen now. It answers with the agent.
// A session found keeps the machine, resume command and web link it was
// created with. Anything missing or unknown is an ErrInvalidInput and
// writes nothing.
func (s *Store) IdentifyAgent(req models.IdentifyAgentRequest) (*models.Agent, error) {
	vendor := strings.TrimSpace(req.Vendor)
	vendorSessionID := strings.TrimSpace(req.VendorSessionID)
	role := strings.TrimSpace(req.Role)
	model := strings.TrimSpace(req.Model)
	provider := strings.TrimSpace(req.Provider)
	switch {
	case vendor == "":
		return nil, invalidInput("vendor is required: the tool the session runs in, such as claude_code or codex")
	case vendorSessionID == "":
		return nil, invalidInput("vendorSessionId is required: the session's ID in its tool")
	case role == "":
		return nil, invalidInput("role is required: what the agent does in its session, such as orchestrator, implementer or reviewer")
	case model == "":
		return nil, invalidInput("model is required: the model the agent runs on")
	case !models.ValidProvider(provider):
		return nil, invalidInput("provider %q is not a provider: use one of %s", provider, strings.Join(models.Providers, ", "))
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	now := time.Now().UTC()
	var sessionID string
	err = tx.QueryRow(`SELECT id FROM sessions WHERE vendor = ? AND vendor_session_id = ?`, vendor, vendorSessionID).Scan(&sessionID)
	if err == sql.ErrNoRows {
		sessionID = newID()
		_, err = tx.Exec(`INSERT INTO sessions (id, vendor, vendor_session_id, machine, resume_command, web_url, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, sessionID, vendor, vendorSessionID, strings.TrimSpace(req.Machine),
			strings.TrimSpace(req.ResumeCommand), strings.TrimSpace(req.WebURL), stamp(now))
	}
	if err != nil {
		return nil, fmt.Errorf("finding or creating the session: %w", err)
	}

	id := newID()
	if _, err := tx.Exec(`INSERT INTO agents (id, session_id, role, model, provider, created_at, last_seen_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, id, sessionID, role, model, provider, stamp(now), stamp(now)); err != nil {
		return nil, fmt.Errorf("creating the agent: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetAgent(id)
}

// GetAgent returns one agent, with Stale as of now, or (nil, nil) for an
// unknown one.
func (s *Store) GetAgent(id string) (*models.Agent, error) {
	a, err := scanAgent(s.db.QueryRow(agentSelect+` WHERE a.id = ?`, strings.TrimSpace(id)))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	settings, err := s.AgentSettings()
	if err != nil {
		return nil, err
	}
	markStale(settings, &a, time.Now())
	return &a, nil
}

// TouchAgent records that the agent was seen now. Every write an agent
// makes touches it the same way, inside its own transaction. An unknown
// agent is an ErrInvalidInput.
func (s *Store) TouchAgent(id string) error {
	return touchAgent(s.db, strings.TrimSpace(id), time.Now().UTC())
}

// touchAgent sets the agent's last seen to now, reporting an unknown or
// missing agent as the caller's mistake.
func touchAgent(q dbtx, id string, now time.Time) error {
	if id == "" {
		return invalidInput("agentId is required: the agent making the call")
	}
	res, err := q.Exec(`UPDATE agents SET last_seen_at = ? WHERE id = ?`, stamp(now), id)
	if err != nil {
		return fmt.Errorf("touching agent %q: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return invalidInput("agentId %q is not an agent: identify first", id)
	}
	return nil
}

// ErrTicketHeld is a claim refused because a live agent holds the ticket. It
// names that agent and when it was last seen. It is also an
// ErrInvalidInput, so a surface that knows no better reports it as the
// caller's mistake.
type ErrTicketHeld struct {
	Ticket string
	Holder models.Agent
	msg    string
}

func (e *ErrTicketHeld) Error() string { return e.msg }

// Unwrap makes errors.As find an ErrInvalidInput with the same message.
func (e *ErrTicketHeld) Unwrap() error { return &ErrInvalidInput{Msg: e.msg} }

// ClaimTicket gives the ticket (an id or display key) to the agent and moves
// it to in_progress. A ticket waiting on the person (needs_user_input) or
// in review (agent_review) keeps its status: its open request still waits
// for an answer, and leaving review takes an agent's note. It touches the
// agent. It never looks at the ticket's dependencies.
//
// A session owns the tickets its agents hold, since a resumed chat keeps its
// session: an agent in the holder's session takes the ticket as its own,
// with no refusal and no takeover. A ticket an agent of another session
// holds is refused with an ErrTicketHeld while that agent is live, and
// taken over once it is stale (markStale: no agent of its session seen
// within the stale threshold). A takeover writes a row of status
// history, even when the status does not change, whose note names both
// agents and the ticket's latest hand-off; the answer carries the agent it
// was taken from and that hand-off.
func (s *Store) ClaimTicket(ticketID, agentID string) (*models.Claim, error) {
	agentID = strings.TrimSpace(agentID)
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	id, err := resolveTicketArg(tx, ticketID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if err := touchAgent(tx, agentID, now); err != nil {
		return nil, err
	}
	claimer, err := scanAgent(tx.QueryRow(agentSelect+` WHERE a.id = ?`, agentID))
	if err != nil {
		return nil, fmt.Errorf("reading agent %q: %w", agentID, err)
	}
	status, holder, err := ticketHolding(tx, id)
	if err != nil {
		return nil, err
	}

	claim := &models.Claim{}
	note := ""
	if holder != "" && holder != agentID {
		prev, err := scanAgent(tx.QueryRow(agentSelect+` WHERE a.id = ?`, holder))
		if err != nil {
			return nil, fmt.Errorf("reading the holding agent %q: %w", holder, err)
		}
		if prev.SessionID != claimer.SessionID {
			settings, err := agentSettings(tx)
			if err != nil {
				return nil, err
			}
			markStale(settings, &prev, now)
			key, err := ticketKey(tx, id)
			if err != nil {
				return nil, err
			}
			if !prev.Stale {
				return nil, &ErrTicketHeld{Ticket: key, Holder: prev, msg: fmt.Sprintf(
					"ticket %s is held by agent %s (%s, %s), whose session was last seen %s (%s ago); "+
						"it can be taken over only once no agent of that session has been seen for more than %s",
					key, prev.ID, prev.Role, prev.Model, prev.SessionLastSeenAt.UTC().Format(time.RFC3339),
					models.FormatDuration(now.Sub(prev.SessionLastSeenAt)), models.FormatDuration(settings.StaleAfter))}
			}
			claim.TakenFrom = &prev
			if claim.HandOff, err = latestHandOff(tx, id); err != nil {
				return nil, err
			}
			note = fmt.Sprintf("Taken over by agent %s (%s, %s) from agent %s (%s, %s), whose session was last seen %s, stale after %s.",
				claimer.ID, claimer.Role, claimer.Model, prev.ID, prev.Role, prev.Model,
				prev.SessionLastSeenAt.UTC().Format(time.RFC3339), models.FormatDuration(settings.StaleAfter))
			if claim.HandOff != nil {
				note += " Latest hand-off: entry " + claim.HandOff.ID + "."
			} else {
				note += " The ticket has no hand-off."
			}
		}
	}

	if _, err := tx.Exec(`UPDATE tickets SET agent_id = ?, updated_at = ? WHERE id = ?`, agentID, stamp(now), id); err != nil {
		return nil, fmt.Errorf("claiming ticket: %w", err)
	}
	to := models.StatusInProgress
	if status == models.StatusNeedsUserInput || status == models.StatusAgentReview {
		to = status
	}
	if to != status || note != "" {
		if err := setStatus(tx, id, status, to, note, now); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing claim: %w", err)
	}
	if claim.Ticket, err = s.GetTicket(id); err != nil {
		return nil, err
	}
	return claim, nil
}

// ReleaseTicket is the agent that holds the ticket (an id or display key),
// or another agent of its session, letting it go, leaving the record its
// outcome needs: giving it back writes the hand-off and returns the ticket
// to todo, and finishing writes the proof and moves it to done. The record
// is an entry on the ticket by the releasing agent (CreateEntry), written in
// the same transaction as the move, and the agent is cleared. It touches
// the releasing agent.
//
// A release without its record is refused, naming what is missing, and so
// is one by an agent outside the holder's session, or one while the ticket
// waits on the person. Nothing is written when it is refused.
func (s *Store) ReleaseTicket(ticketID string, req models.ReleaseTicketRequest) (*models.Ticket, error) {
	agentID := strings.TrimSpace(req.AgentID)
	outcome := strings.TrimSpace(req.Outcome)
	handOff := strings.TrimSpace(req.HandOff)
	proof := strings.TrimSpace(req.Proof)
	var entryType, text, to string
	switch outcome {
	case models.ReleaseGiveBack:
		if handOff == "" {
			return nil, invalidInput("giving a ticket back needs its hand-off: where the work stopped and the next step")
		}
		if proof != "" {
			return nil, invalidInput("proof is for finishing a ticket; giving it back takes a hand-off only")
		}
		entryType, text, to = models.EntryHandOff, handOff, models.StatusTodo
	case models.ReleaseFinish:
		if proof == "" {
			return nil, invalidInput("finishing a ticket needs its proof: what was verified, how, and the result")
		}
		if handOff != "" {
			return nil, invalidInput("a hand-off is for giving a ticket back; finishing it takes its proof only")
		}
		entryType, text, to = models.EntryProof, proof, models.StatusDone
	default:
		return nil, invalidInput("outcome %q is not a way to release a ticket: use %s (with a hand-off) or %s (with proof)",
			outcome, models.ReleaseGiveBack, models.ReleaseFinish)
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
	now := time.Now().UTC()
	if err := touchAgent(tx, agentID, now); err != nil {
		return nil, err
	}
	status, holder, err := ticketHolding(tx, id)
	if err != nil {
		return nil, err
	}
	key, err := ticketKey(tx, id)
	if err != nil {
		return nil, err
	}
	if holder == "" {
		return nil, invalidInput("ticket %s is not held by any agent: only the agent holding a ticket, or its session, releases it", key)
	}
	if holder != agentID {
		same, err := sameSession(tx, holder, agentID)
		if err != nil {
			return nil, err
		}
		if !same {
			return nil, invalidInput("ticket %s is held by agent %s of another session, not %s: "+
				"only the agent holding a ticket, or its session, releases it", key, holder, agentID)
		}
	}
	if open, err := openRequestOn(tx, id); err != nil {
		return nil, err
	} else if open != nil {
		return nil, invalidInput("ticket %s waits on the person's answer to request %s (%s): it can be released once that is answered",
			key, open.ID, open.Type)
	}

	entryID, err := createEntry(tx, models.CreateEntryRequest{
		EntryOwner: models.EntryOwner{TicketID: id}, Type: entryType, Text: text, AgentID: agentID,
	}, now)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`UPDATE tickets SET agent_id = NULL, updated_at = ? WHERE id = ?`, stamp(now), id); err != nil {
		return nil, fmt.Errorf("releasing ticket: %w", err)
	}
	note := fmt.Sprintf("Given back by agent %s. Hand-off: entry %s.", agentID, entryID)
	if outcome == models.ReleaseFinish {
		note = fmt.Sprintf("Finished by agent %s. Proof: entry %s.", agentID, entryID)
	}
	if err := setStatus(tx, id, status, to, note, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing release: %w", err)
	}
	return s.GetTicket(id)
}

// sameSession reports whether two agents are workers in the same session.
func sameSession(q dbtx, a, b string) (bool, error) {
	var n int
	if err := q.QueryRow(`SELECT COUNT(*) FROM agents x JOIN agents y ON y.session_id = x.session_id
		WHERE x.id = ? AND y.id = ?`, a, b).Scan(&n); err != nil {
		return false, fmt.Errorf("comparing the sessions of agents %q and %q: %w", a, b, err)
	}
	return n > 0, nil
}

// resolveTicketArg resolves a ticket id or display key given to one of the
// agent protocol's writes.
func resolveTicketArg(q dbtx, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", invalidInput("ticket id is required")
	}
	return lookupTicketRef(q, ref)
}

// ticketHolding reads a ticket's status and the agent holding it, "" when
// none, inside q's transaction.
func ticketHolding(q dbtx, id string) (status, agentID string, err error) {
	var holder sql.NullString
	if err := q.QueryRow(`SELECT status, agent_id FROM `+liveTickets+` WHERE id = ?`, id).Scan(&status, &holder); err != nil {
		return "", "", fmt.Errorf("reading ticket %q: %w", id, err)
	}
	return status, holder.String, nil
}

// ticketKey is a ticket's display key, for messages and notes.
func ticketKey(q dbtx, id string) (string, error) {
	var key string
	err := q.QueryRow(`SELECT COALESCE(p.prefix, '') || '-' || t.number FROM `+liveTickets+` t
		LEFT JOIN `+liveProjects+` p ON p.id = t.project_id WHERE t.id = ?`, id).Scan(&key)
	if err != nil {
		return "", fmt.Errorf("reading ticket %q's key: %w", id, err)
	}
	return key, nil
}

// latestHandOff is the ticket's newest current hand-off, whichever agent
// wrote it, or nil when it has none: where the work stood.
func latestHandOff(q dbtx, ticketID string) (*models.Entry, error) {
	e, err := scanEntry(q.QueryRow(entrySelect+` WHERE e.ticket_id = ? AND e.type = ? AND r.id IS NULL
		ORDER BY e.created_at DESC, e.rowid DESC LIMIT 1`, ticketID, models.EntryHandOff))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading the latest hand-off: %w", err)
	}
	return &e, nil
}

// setStatus moves a ticket, inside q's transaction, from status from to the
// end of status to's column, and writes the row of status history with note.
// A ticket already in to keeps its place and still gets the row: that is
// how a takeover, which changes no status, is recorded.
func setStatus(q dbtx, id, from, to, note string, now time.Time) error {
	if from != to {
		var position float64
		if err := q.QueryRow("SELECT COALESCE(MAX(position), 0) + 1000 FROM "+liveTickets+" WHERE status = ?", to).
			Scan(&position); err != nil {
			return fmt.Errorf("finding the end of the column: %w", err)
		}
		if _, err := q.Exec(`UPDATE tickets SET status = ?, position = ?, updated_at = ? WHERE id = ?`,
			to, position, stamp(now), id); err != nil {
			return fmt.Errorf("moving ticket: %w", err)
		}
	}
	return recordStatusChange(q, id, from, to, note, now)
}

// attachHolding fills Agent, with Stale as of now, and OpenRequest for a
// page of tickets, with one query each. index maps a ticket id to its place
// in tickets; placeholders and ids are the IN list for the same page.
func (s *Store) attachHolding(tickets []models.Ticket, index map[string]int, placeholders string, ids []any) error {
	settings, err := s.AgentSettings()
	if err != nil {
		return err
	}
	now := time.Now()
	rows, err := s.db.Query(`SELECT t.id, `+agentColumns+`
		FROM `+liveTickets+` t JOIN agents a ON a.id = t.agent_id
		WHERE t.id IN (`+placeholders+`)`, ids...)
	if err != nil {
		return fmt.Errorf("loading holding agents: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var ticketID string
		a, err := scanAgent(rows, &ticketID)
		if err != nil {
			return err
		}
		markStale(settings, &a, now)
		if i, ok := index[ticketID]; ok {
			tickets[i].Agent = &a
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()

	reqRows, err := s.db.Query(requestSelect+` WHERE r.answered_at IS NULL AND r.ticket_id IN (`+placeholders+`)`, ids...)
	if err != nil {
		return fmt.Errorf("loading open requests: %w", err)
	}
	defer reqRows.Close()
	for reqRows.Next() {
		r, err := scanRequest(reqRows)
		if err != nil {
			return err
		}
		if i, ok := index[r.TicketID]; ok {
			tickets[i].OpenRequest = &r
		}
	}
	return reqRows.Err()
}
