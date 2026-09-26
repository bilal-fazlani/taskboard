package db

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/tcarac/taskboard/internal/models"
)

const (
	// JournalPreviewLimit is how many of a project's latest journal entries
	// a read of the project carries.
	JournalPreviewLimit = 5
	// JournalDefaultLimit and JournalMaxLimit bound a page of a journal read
	// on its own.
	JournalDefaultLimit = 20
	JournalMaxLimit     = 100
	// JournalAuthorMaxLength caps an entry's author, in characters: it is a
	// name, not a message.
	JournalAuthorMaxLength = 100
)

// AppendJournalEntry adds an entry to a project's journal, stamped now.
// projectRef is a project id or prefix (case-insensitive). The author and
// text are trimmed and neither may be blank; an unknown project, or either
// of those, is an ErrInvalidInput and writes nothing.
func (s *Store) AppendJournalEntry(projectRef string, req models.AppendJournalEntryRequest) (*models.JournalEntry, error) {
	author := strings.TrimSpace(req.Author)
	text := strings.TrimSpace(req.Text)
	if author == "" {
		return nil, invalidInput("author is required: the name of whoever writes the entry")
	}
	if n := utf8.RuneCountInString(author); n > JournalAuthorMaxLength {
		return nil, invalidInput("author is %d characters long; at most %d are allowed", n, JournalAuthorMaxLength)
	}
	if text == "" {
		return nil, invalidInput("text is required")
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	projectID, err := resolveProjectRef(tx, projectRef)
	if err != nil {
		return nil, err
	}
	e := models.JournalEntry{
		ID:        newID(),
		ProjectID: projectID,
		Author:    author,
		Text:      text,
		CreatedAt: time.Now().UTC(),
	}
	if _, err := tx.Exec(
		`INSERT INTO project_journal_entries (id, project_id, author, text, created_at) VALUES (?, ?, ?, ?, ?)`,
		e.ID, e.ProjectID, e.Author, e.Text, stamp(e.CreatedAt),
	); err != nil {
		return nil, fmt.Errorf("appending journal entry: %w", err)
	}
	return &e, tx.Commit()
}

// ListJournal returns one page of a project's journal, newest first: up to
// limit entries (1 to JournalMaxLimit), starting with the newest when before
// is empty, or else with the newest entry older than the entry before names,
// which must be one of this project's. Entries written within the same
// instant come back in the reverse of the order they were written. An
// unknown project or before, or a limit out of range, is an ErrInvalidInput.
func (s *Store) ListJournal(projectRef, before string, limit int) (models.JournalPage, error) {
	page := models.JournalPage{Entries: []models.JournalEntry{}}
	if limit < 1 || limit > JournalMaxLimit {
		return page, invalidInput("limit must be between 1 and %d", JournalMaxLimit)
	}
	projectID, err := resolveProjectRef(s.db, projectRef)
	if err != nil {
		return page, err
	}

	// rowid follows commit order (writes are serialised), so it breaks ties
	// between entries stamped with the same instant.
	query := `SELECT id, project_id, author, text, created_at FROM project_journal_entries WHERE project_id = ?`
	args := []any{projectID}
	if before = strings.TrimSpace(before); before != "" {
		var found int
		err := s.db.QueryRow(
			`SELECT COUNT(*) FROM project_journal_entries WHERE id = ? AND project_id = ?`,
			before, projectID,
		).Scan(&found)
		if err != nil {
			return page, fmt.Errorf("reading journal entry %q: %w", before, err)
		}
		if found == 0 {
			return page, invalidInput("before %q is not an entry in this project's journal", before)
		}
		// Compared in SQL, on the stored text: read into Go, the driver
		// turns created_at into a time.Time, which formats differently.
		query += ` AND (created_at, rowid) < (SELECT created_at, rowid FROM project_journal_entries WHERE id = ?)`
		args = append(args, before)
	}
	// One more than asked for says whether another page follows.
	query += ` ORDER BY created_at DESC, rowid DESC LIMIT ?`
	args = append(args, limit+1)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var e models.JournalEntry
		if err := rows.Scan(&e.ID, &e.ProjectID, &e.Author, &e.Text, &e.CreatedAt); err != nil {
			return page, err
		}
		page.Entries = append(page.Entries, e)
	}
	if err := rows.Err(); err != nil {
		return page, err
	}
	if len(page.Entries) > limit {
		page.Entries = page.Entries[:limit]
		page.HasMore = true
		page.NextBefore = page.Entries[limit-1].ID
	}

	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM project_journal_entries WHERE project_id = ?`, projectID,
	).Scan(&page.Total); err != nil {
		return page, fmt.Errorf("counting journal entries: %w", err)
	}
	return page, nil
}
