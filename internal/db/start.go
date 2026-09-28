package db

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/tcarac/taskboard/internal/models"
)

// StartProjectEntries is how many of the project's newest current entries a
// start carries; the rest are read a page at a time from its nextBefore.
// Every start pays for them, so the page is short.
const StartProjectEntries = 5

// StartTicket is an agent beginning work on a ticket (an id or display key):
// it claims the ticket for the agent under ClaimTicket's rules, and answers
// with everything needed to begin (models.Start). A ticket another session's
// live agent holds is refused with ClaimTicket's ErrTicketHeld, and nothing
// is read or written; a stale holder's ticket is taken over, and the start
// says from whom. Unfinished dependencies never block it: the start names
// them.
//
// The payload is read after the claim commits, not inside its transaction.
// That is safe because everything that depends on who held the ticket (the
// refusal, the takeover and the hand-off it reports) is decided inside the
// claim's transaction and handed back in its Claim. The rest is context
// that any write can change the moment after a start answers anyway: a write
// landing between the commit and the read only makes the payload newer than
// the claim, never older, and the ticket cannot be taken from the agent the
// claim has just touched, since it is live. Reading inside the claim would
// hold the write lock (every transaction here is BEGIN IMMEDIATE) across a
// dozen reads through helpers that all read through the store's pool.
//
// The project's agent instructions come once per agent (per agent, not per
// session: an orchestrator and the implementers it launches share one), and
// again when they change; giveAgentInstructions records each giving, after
// every read has succeeded.
func (s *Store) StartTicket(ticketRef, agentID string) (*models.Start, error) {
	claim, err := s.ClaimTicket(ticketRef, agentID)
	if err != nil {
		return nil, err
	}
	t := claim.Ticket
	if t == nil {
		return nil, fmt.Errorf("reading the claimed ticket %q: it is gone", ticketRef)
	}
	t.Agent = nil
	if t.AnsweredRequests, err = s.AnsweredRequests(t.ID); err != nil {
		return nil, err
	}
	start := &models.Start{Ticket: models.NewStartTicket(t), TakenFrom: claim.TakenFrom, Stopped: claim.Stopped, HandOff: claim.HandOff}

	if start.TakenFrom == nil {
		if start.HandOff, err = latestHandOff(s.db, t.ID); err != nil {
			return nil, err
		}
	}
	if start.HandOff != nil {
		start.HandOff.EntryOwner = models.EntryOwner{}
	}
	var notes models.StartNotes

	ticket := models.EntryOwner{TicketID: t.ID}
	entries, err := s.allEntries(ticket)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if start.HandOff == nil || e.ID != start.HandOff.ID {
			start.Entries = append(start.Entries, e)
		}
	}
	if notes.Ticket, err = s.openNotes(ticket); err != nil {
		return nil, err
	}

	if t.Epic != nil {
		if start.Epic, notes.Epic, err = s.startEpic(t.Epic.ID); err != nil {
			return nil, err
		}
	}

	p, err := s.GetProject(t.ProjectID)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, fmt.Errorf("reading ticket %s's project: it is gone", t.DisplayKey())
	}
	start.Project = models.StartProject{Name: p.Name, Description: p.Description}
	project := models.EntryOwner{ProjectID: p.ID}
	page, err := s.listEntries(project, models.EntryFilter{ExcludeOpenNotes: true}, "", StartProjectEntries, "")
	if err != nil {
		return nil, err
	}
	if page.Total > 0 {
		page = bareEntryPage(page)
		start.Project.Entries = &page
	}
	if notes.Project, err = s.openNotes(project); err != nil {
		return nil, err
	}

	if !notes.Empty() {
		start.Notes = &notes
	}
	for _, d := range start.Ticket.DependsOn {
		if d.Status != models.StatusDone {
			start.UnfinishedDependencies = append(start.UnfinishedDependencies, d.Key)
		}
	}
	if err := s.nameEntryAuthors(start); err != nil {
		return nil, err
	}

	// Last, once every read has succeeded, so the instructions are recorded
	// as given only in a start that answers.
	if p.AgentInstructions != nil && *p.AgentInstructions != "" {
		given, err := s.giveAgentInstructions(strings.TrimSpace(agentID), p.ID, *p.AgentInstructions)
		if err != nil {
			return nil, err
		}
		if given {
			start.Project.AgentInstructions = *p.AgentInstructions
		} else {
			start.Project.AgentInstructionsLeftOut = fmt.Sprintf("Given to you on an earlier start; read them again with "+
				"get_project %s (GET /api/projects/%s).", p.Prefix, p.ID)
		}
	}
	return start, nil
}

// giveAgentInstructions records that the agent is given the project's agent
// instructions, and reports whether a start should carry them: true when the
// agent has not been given them before, or was given other text (they have
// changed since); false when it has these already. One upsert both decides
// and records, so two starts of one agent at once carry them once.
func (s *Store) giveAgentInstructions(agentID, projectID, instructions string) (bool, error) {
	sum := sha256.Sum256([]byte(instructions))
	res, err := s.db.Exec(`INSERT INTO agent_instructions_given (agent_id, project_id, instructions_hash, given_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (agent_id, project_id) DO UPDATE SET instructions_hash = excluded.instructions_hash, given_at = excluded.given_at
		WHERE instructions_hash <> excluded.instructions_hash`,
		agentID, projectID, hex.EncodeToString(sum[:]), stamp(time.Now()))
	if err != nil {
		return false, fmt.Errorf("recording the agent instructions given: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("recording the agent instructions given: %w", err)
	}
	return n > 0, nil
}

// nameEntryAuthors names, on every entry in the start that an agent wrote,
// that agent's role and model.
func (s *Store) nameEntryAuthors(start *models.Start) error {
	var all []*models.Entry
	add := func(entries []models.Entry) {
		for i := range entries {
			all = append(all, &entries[i])
		}
	}
	if start.HandOff != nil {
		all = append(all, start.HandOff)
	}
	add(start.Entries)
	if start.Epic != nil {
		add(start.Epic.Entries)
	}
	if start.Project.Entries != nil {
		add(start.Project.Entries.Entries)
	}
	if start.Notes != nil {
		add(start.Notes.Ticket)
		add(start.Notes.Epic)
		add(start.Notes.Project)
	}
	authors := make([]models.Entry, 0, len(all))
	for _, e := range all {
		if e.AgentID != "" {
			authors = append(authors, models.Entry{AgentID: e.AgentID})
		}
	}
	agents, err := s.EntryAgents(authors)
	if err != nil {
		return err
	}
	for _, e := range all {
		if a, ok := agents[e.AgentID]; ok {
			e.AgentRole, e.AgentModel = a.Role, a.Model
		}
	}
	return nil
}

// startEpic reads the epic's part of a start, nil when it has nothing to
// say beyond the name the ticket carries, and its open notes.
func (s *Store) startEpic(id string) (*models.StartEpic, []models.Entry, error) {
	e, err := scanEpic(s.db.QueryRow(epicSelect+" WHERE id = ?", id))
	if err != nil {
		return nil, nil, fmt.Errorf("reading epic %q: %w", id, err)
	}
	epic := models.StartEpic{Description: e.Description}
	docs, err := loadOwnerDocuments(s.db, DocumentOwner{EpicID: id})
	if err != nil {
		return nil, nil, err
	}
	for _, d := range docs {
		epic.Documents = append(epic.Documents, models.DocumentDisplayName(d.Name, d.Format))
	}
	owner := models.EntryOwner{EpicID: id}
	if epic.Entries, err = s.allEntries(owner); err != nil {
		return nil, nil, err
	}
	notes, err := s.openNotes(owner)
	if err != nil {
		return nil, nil, err
	}
	if epic.Empty() {
		return nil, notes, nil
	}
	return &epic, notes, nil
}

// allEntries reads every current entry on a resolved owner, newest first,
// open notes aside, each without its owner.
func (s *Store) allEntries(owner models.EntryOwner) ([]models.Entry, error) {
	var out []models.Entry
	before := ""
	for {
		page, err := s.listEntries(owner, models.EntryFilter{ExcludeOpenNotes: true}, before, EntryMaxLimit, "")
		if err != nil {
			return nil, err
		}
		out = append(out, bareEntryPage(page).Entries...)
		if !page.HasMore {
			return out, nil
		}
		before = page.NextBefore
	}
}

// openNotes reads the open notes (models.Entry.Open) on a resolved owner,
// newest first, each without its owner.
func (s *Store) openNotes(owner models.EntryOwner) ([]models.Entry, error) {
	col, id := ownerColumn(owner)
	rows, err := s.db.Query(entrySelect+` WHERE e.`+col+` = ? AND `+openNote+
		` ORDER BY e.created_at DESC, e.rowid DESC`, id)
	if err != nil {
		return nil, fmt.Errorf("reading open notes: %w", err)
	}
	defer rows.Close()
	var out []models.Entry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		e.EntryOwner = models.EntryOwner{}
		out = append(out, e)
	}
	return out, rows.Err()
}

// bareEntryPage leaves out each entry's owner: the part of the answer it
// sits in names it.
func bareEntryPage(page models.EntryPage) models.EntryPage {
	for i := range page.Entries {
		page.Entries[i].EntryOwner = models.EntryOwner{}
	}
	return page
}
