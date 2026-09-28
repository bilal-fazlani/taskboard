package db

import (
	"fmt"

	"github.com/tcarac/taskboard/internal/models"
)

// StartProjectEntries is how many of the project's newest current entries a
// start carries; the rest are read a page at a time from its nextBefore.
const StartProjectEntries = 10

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
	start := &models.Start{Ticket: t, TakenFrom: claim.TakenFrom, HandOff: claim.HandOff}

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
	if p.AgentInstructions != nil {
		start.Project.AgentInstructions = *p.AgentInstructions
	}
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
	for _, d := range t.DependsOn {
		if d.Status != models.StatusDone {
			start.UnfinishedDependencies = append(start.UnfinishedDependencies, d.Key)
		}
	}
	return start, nil
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
