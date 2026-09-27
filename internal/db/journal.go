package db

import (
	"fmt"

	"github.com/tcarac/taskboard/internal/models"
)

// The project journal is the project's own entries (entries.go), read and
// written through the journal's older shape until its surfaces move to
// entries. Its entries moved into project entries with migration 019.
const (
	// JournalPreviewLimit is how many of a project's latest journal entries
	// a read of the project carries.
	JournalPreviewLimit = 5
	// JournalDefaultLimit and JournalMaxLimit bound a page of a journal read
	// on its own.
	JournalDefaultLimit = EntryDefaultLimit
	JournalMaxLimit     = EntryMaxLimit
	// JournalAuthorMaxLength caps an entry's author, in characters: it is a
	// name, not a message.
	JournalAuthorMaxLength = EntryAuthorNameMaxLength
)

// AppendJournalEntry adds a decision to the project's entries, stamped now,
// written by the person under the name the writer gives, and answers with it
// in the journal's shape. projectRef is a project id or prefix
// (case-insensitive). The author and text are trimmed and neither may be
// blank; an unknown project, or either of those, is an ErrInvalidInput and
// writes nothing.
func (s *Store) AppendJournalEntry(projectRef string, req models.AppendJournalEntryRequest) (*models.JournalEntry, error) {
	e, err := s.CreateEntry(models.CreateEntryRequest{
		EntryOwner: models.EntryOwner{ProjectID: projectRef},
		Type:       models.EntryDecision,
		Text:       req.Text,
		AuthorName: req.Author,
		Source:     models.DecisionSourcePerson,
	})
	if err != nil {
		return nil, err
	}
	j, err := s.journalEntries([]models.Entry{*e})
	if err != nil {
		return nil, err
	}
	return &j[0], nil
}

// ListJournal returns one page of a project's journal, which is the
// project's current entries of every type, newest first: up to limit entries
// (1 to JournalMaxLimit), starting with the newest when before is empty, or
// else with the newest entry older than the entry before names, which must
// be one of this project's. Entries written within the same instant come
// back in the reverse of the order they were written. An unknown project or
// before, or a limit out of range, is an ErrInvalidInput.
func (s *Store) ListJournal(projectRef, before string, limit int) (models.JournalPage, error) {
	page := models.JournalPage{Entries: []models.JournalEntry{}}
	if limit < 1 || limit > JournalMaxLimit {
		return page, invalidInput("limit must be between 1 and %d", JournalMaxLimit)
	}
	projectID, err := resolveProjectRef(s.db, projectRef)
	if err != nil {
		return page, err
	}
	entries, err := s.listEntries(models.EntryOwner{ProjectID: projectID}, models.EntryFilter{}, before, limit,
		"an entry in this project's journal")
	if err != nil {
		return page, err
	}
	if page.Entries, err = s.journalEntries(entries.Entries); err != nil {
		return page, err
	}
	page.Total, page.HasMore, page.NextBefore = entries.Total, entries.HasMore, entries.NextBefore
	return page, nil
}

// journalEntries puts project entries in the journal's shape. The author is
// the person's name, or for an entry an agent wrote, the agent's role.
func (s *Store) journalEntries(entries []models.Entry) ([]models.JournalEntry, error) {
	out := make([]models.JournalEntry, len(entries))
	roles := map[string]string{}
	for i, e := range entries {
		author := e.AuthorName
		if e.AgentID != "" {
			role, ok := roles[e.AgentID]
			if !ok {
				if err := s.db.QueryRow(`SELECT role FROM agents WHERE id = ?`, e.AgentID).Scan(&role); err != nil {
					return nil, fmt.Errorf("reading agent %q: %w", e.AgentID, err)
				}
				roles[e.AgentID] = role
			}
			author = role
		}
		out[i] = models.JournalEntry{ID: e.ID, ProjectID: e.ProjectID, Author: author, Text: e.Text, CreatedAt: e.CreatedAt}
	}
	return out, nil
}
