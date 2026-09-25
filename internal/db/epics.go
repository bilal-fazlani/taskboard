package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/tcarac/taskboard/internal/models"
)

// The texts of the epic rules. They are the whole error message, so the HTTP
// API (400), the MCP tools and the CLI all report exactly the same words.
const (
	msgEpicNameRequired = "Enter a name"
	msgEpicNameReserved = `"none" is reserved for tickets without an epic.`
	msgEpicRefRequired  = "Enter an epic name or id."
)

func msgEpicNameTaken(existing string) string {
	return fmt.Sprintf(`This project already has an epic called "%s".`, existing)
}

type epicRef struct {
	id   string
	name string
}

// loadProjectEpics reads the id and name of every epic in a project. Names are
// folded in Go with strings.EqualFold, as label names are, because SQLite's
// NOCASE and LOWER() fold ASCII only.
func loadProjectEpics(q dbtx, projectID string) ([]epicRef, error) {
	rows, err := q.Query("SELECT id, name FROM epics WHERE project_id = ?", projectID)
	if err != nil {
		return nil, fmt.Errorf("loading epics: %w", err)
	}
	defer rows.Close()

	var refs []epicRef
	for rows.Next() {
		var r epicRef
		if err := rows.Scan(&r.id, &r.name); err != nil {
			return nil, fmt.Errorf("scanning epic: %w", err)
		}
		refs = append(refs, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("loading epics: %w", err)
	}
	return refs, nil
}

// checkEpicName applies the naming rules to a new or changed name and returns
// it trimmed, which is the form that is stored. exceptID is the epic being
// renamed, so it may keep its own name in different capitals; it is "" on
// create.
func checkEpicName(q dbtx, projectID, name, exceptID string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", invalidInput(msgEpicNameRequired)
	}
	if strings.EqualFold(trimmed, models.NoEpic) {
		return "", invalidInput(msgEpicNameReserved)
	}
	refs, err := loadProjectEpics(q, projectID)
	if err != nil {
		return "", err
	}
	for _, r := range refs {
		if r.id != exceptID && strings.EqualFold(r.name, trimmed) {
			return "", invalidInput("%s", msgEpicNameTaken(r.name))
		}
	}
	return trimmed, nil
}

// resolveEpicInProject maps an epic id or name (case-insensitive) to the id of
// an epic in projectID. An id that names an epic in another project gets its
// own message, since the caller clearly meant that epic.
func resolveEpicInProject(q dbtx, projectID, ref string) (string, error) {
	trimmed := strings.TrimSpace(ref)
	refs, err := loadProjectEpics(q, projectID)
	if err != nil {
		return "", err
	}
	for _, r := range refs {
		if r.id == trimmed {
			return r.id, nil
		}
	}
	for _, r := range refs {
		if strings.EqualFold(r.name, trimmed) {
			return r.id, nil
		}
	}

	var otherName string
	err = q.QueryRow("SELECT name FROM epics WHERE id = ?", trimmed).Scan(&otherName)
	if err == nil {
		return "", invalidInput(`Epic "%s" belongs to another project.`, otherName)
	}
	if err != sql.ErrNoRows {
		return "", fmt.Errorf("looking up epic %q: %w", trimmed, err)
	}
	return "", invalidInput(`This project has no epic called "%s".`, trimmed)
}

// ResolveEpicRef resolves an epic id or name (case-insensitive) within a
// project given by id or prefix. It is what CLI and MCP entry points call to
// accept an epic name wherever they take an epic, before calling an id-based
// method such as UpdateEpic or DeleteEpic.
func (s *Store) ResolveEpicRef(projectRef, ref string) (string, error) {
	projectID, err := resolveProjectRef(s.db, projectRef)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(ref) == "" {
		return "", invalidInput(msgEpicRefRequired)
	}
	return resolveEpicInProject(s.db, projectID, ref)
}

const epicSelect = `SELECT id, project_id, name, COALESCE(description, ''), created_at, updated_at FROM epics`

func scanEpic(row interface{ Scan(...any) error }) (models.Epic, error) {
	var e models.Epic
	err := row.Scan(&e.ID, &e.ProjectID, &e.Name, &e.Description, &e.CreatedAt, &e.UpdatedAt)
	return e, err
}

// loadProgress counts the tickets matching where by epic and status in one
// grouped query, without loading the tickets. The result is keyed by epic id,
// with "" for tickets that have no epic; a key is present only when it has
// tickets.
//
// The latest activity is read with SQLite's bare-column rule: in a query with
// a single max() aggregate, a plain column takes its value from the row that
// holds the maximum. Selecting t.updated_at that way, rather than using the
// max() value itself, keeps the column's DATETIME type, so the driver parses
// it back into a time.Time. The max() within one group compares the stored
// text; the groups of an epic are then compared as times here.
func loadProgress(q dbtx, where string, args ...any) (map[string]*models.EpicProgress, error) {
	rows, err := q.Query(`SELECT COALESCE(t.epic_id, ''), t.status, COUNT(*), MAX(t.updated_at), t.updated_at
		FROM tickets t WHERE `+where+` GROUP BY t.epic_id, t.status`, args...)
	if err != nil {
		return nil, fmt.Errorf("counting epic tickets: %w", err)
	}
	defer rows.Close()

	progress := map[string]*models.EpicProgress{}
	for rows.Next() {
		var epicID, status string
		var count int
		// Only there to make t.updated_at the latest row's; the value
		// itself is discarded.
		var maxText any
		var latest time.Time
		if err := rows.Scan(&epicID, &status, &count, &maxText, &latest); err != nil {
			return nil, fmt.Errorf("scanning epic counts: %w", err)
		}
		p, ok := progress[epicID]
		if !ok {
			fresh := models.NewEpicProgress()
			p = &fresh
			progress[epicID] = p
		}
		p.Counts[status] += count
		p.Total += count
		if p.LastActivityAt == nil || latest.After(*p.LastActivityAt) {
			p.LastActivityAt = &latest
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("counting epic tickets: %w", err)
	}
	for _, p := range progress {
		p.Complete = p.Total > 0 && p.Counts[models.StatusDone] == p.Total
	}
	return progress, nil
}

// progressOf returns the entry for key, or the progress of no tickets.
func progressOf(progress map[string]*models.EpicProgress, key string) models.EpicProgress {
	if p, ok := progress[key]; ok {
		return *p
	}
	return models.NewEpicProgress()
}

// NoEpicProgress summarises a project's tickets (project id or prefix) that
// have no epic, with the same counts an epic carries. An unknown project is
// an ErrInvalidInput.
func (s *Store) NoEpicProgress(projectRef string) (*models.EpicProgress, error) {
	projectID, err := resolveProjectRef(s.db, projectRef)
	if err != nil {
		return nil, err
	}
	progress, err := loadProgress(s.db, "t.project_id = ? AND t.epic_id IS NULL", projectID)
	if err != nil {
		return nil, err
	}
	p := progressOf(progress, "")
	return &p, nil
}

// GetEpic returns (nil, nil) for an unknown id, like GetTicket, so the HTTP
// layer can answer 404.
func (s *Store) GetEpic(id string) (*models.Epic, error) {
	e, err := scanEpic(s.db.QueryRow(epicSelect+" WHERE id = ?", id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	progress, err := loadProgress(s.db, "t.epic_id = ?", e.ID)
	if err != nil {
		return nil, err
	}
	e.EpicProgress = progressOf(progress, e.ID)
	if e.Documents, err = loadOwnerDocuments(s.db, DocumentOwner{EpicID: e.ID}); err != nil {
		return nil, err
	}
	e.DocumentCount = len(e.Documents)
	return &e, nil
}

// ListEpics returns a project's epics (project id or prefix) by name, each
// with its progress. An unknown project is an ErrInvalidInput. The result is
// never nil.
func (s *Store) ListEpics(projectRef string) ([]models.Epic, error) {
	projectID, err := resolveProjectRef(s.db, projectRef)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(epicSelect+" WHERE project_id = ? ORDER BY name COLLATE NOCASE, id", projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	epics := []models.Epic{}
	for rows.Next() {
		e, err := scanEpic(rows)
		if err != nil {
			return nil, err
		}
		epics = append(epics, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()

	// One grouped query for every epic in the project at once.
	progress, err := loadProgress(s.db, "t.project_id = ?", projectID)
	if err != nil {
		return nil, err
	}
	for i := range epics {
		epics[i].EpicProgress = progressOf(progress, epics[i].ID)
	}
	counts, err := loadEpicDocumentCounts(s.db, projectID)
	if err != nil {
		return nil, err
	}
	for i := range epics {
		epics[i].DocumentCount = counts[epics[i].ID]
	}
	return epics, nil
}

// loadEpicDocumentCounts counts the documents of every epic in a project in
// one grouped query, keyed by epic id; an epic with none has no key.
func loadEpicDocumentCounts(q dbtx, projectID string) (map[string]int, error) {
	rows, err := q.Query(`SELECT d.epic_id, COUNT(*) FROM documents d
		JOIN epics e ON e.id = d.epic_id WHERE e.project_id = ? GROUP BY d.epic_id`, projectID)
	if err != nil {
		return nil, fmt.Errorf("counting epic documents: %w", err)
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, fmt.Errorf("scanning epic document count: %w", err)
		}
		counts[id] = n
	}
	return counts, rows.Err()
}

// CreateEpic checks the name and inserts in one transaction, so two writers
// cannot both pass the duplicate check.
func (s *Store) CreateEpic(req models.CreateEpicRequest) (*models.Epic, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	projectID, err := resolveProjectRef(tx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	name, err := checkEpicName(tx, projectID, req.Name, "")
	if err != nil {
		return nil, err
	}

	now := time.Now()
	id := newID()
	if _, err := tx.Exec(
		"INSERT INTO epics (id, project_id, name, description, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
		id, projectID, name, req.Description, now, now,
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing epic: %w", err)
	}
	return s.GetEpic(id)
}

// UpdateEpic returns (nil, nil) for an unknown id, like UpdateTicket.
func (s *Store) UpdateEpic(id string, req models.UpdateEpicRequest) (*models.Epic, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	e, err := scanEpic(tx.QueryRow(epicSelect+" WHERE id = ?", id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if req.Name != nil {
		if e.Name, err = checkEpicName(tx, e.ProjectID, *req.Name, e.ID); err != nil {
			return nil, err
		}
	}
	if req.Description != nil {
		e.Description = *req.Description
	}
	if _, err := tx.Exec(
		"UPDATE epics SET name = ?, description = ?, updated_at = ? WHERE id = ?",
		e.Name, e.Description, time.Now(), e.ID,
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing epic: %w", err)
	}
	return s.GetEpic(id)
}

// DeleteEpic removes an epic. Its tickets stay and lose their epic (the
// foreign key sets it to NULL); its own documents are deleted with it (the
// foreign key cascades), its tickets' documents are not. It reports how many tickets that cleared,
// read in the same transaction as the delete, like DeleteLabel.
func (s *Store) DeleteEpic(id string) (int, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	var count int
	if err := tx.QueryRow("SELECT COUNT(*) FROM tickets WHERE epic_id = ?", id).Scan(&count); err != nil {
		return 0, fmt.Errorf("counting tickets for epic: %w", err)
	}
	if _, err := tx.Exec("DELETE FROM epics WHERE id = ?", id); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("committing epic deletion: %w", err)
	}
	return count, nil
}

// resolveTicketEpic turns a ticket's requested epic into the epic_id to store,
// called only once the caller's pointer is known to be non-nil (nil means
// "leave it", handled by CreateTicket and UpdateTicket). "" and "none" (any
// case, surrounding spaces ignored) clear it, a nil result, so "none" means no
// epic here as it does in the filter. A value of only spaces is rejected, like
// a due date of only spaces, rather than read as a clear. Anything else must
// be the id or name of an epic in the ticket's project, or the request is
// rejected with an ErrInvalidInput.
func resolveTicketEpic(q dbtx, projectID, raw string) (*string, error) {
	if raw == "" {
		return nil, nil
	}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, invalidInput(msgEpicRefRequired)
	}
	if strings.EqualFold(trimmed, models.NoEpic) {
		return nil, nil
	}
	id, err := resolveEpicInProject(q, projectID, trimmed)
	if err != nil {
		return nil, err
	}
	return &id, nil
}

// findEpicIDs returns the ids of the epics a ticket filter value names: the
// epic with that id, or every epic with that name (case-insensitive) in
// projectID, or in any project when projectID is "". It never errors on no
// match; an empty result means the filter matches no tickets.
func findEpicIDs(q dbtx, projectID, ref string) ([]string, error) {
	query := "SELECT id, name FROM epics"
	var args []any
	if projectID != "" {
		query += " WHERE project_id = ?"
		args = append(args, projectID)
	}
	rows, err := q.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("loading epics: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var r epicRef
		if err := rows.Scan(&r.id, &r.name); err != nil {
			return nil, fmt.Errorf("scanning epic: %w", err)
		}
		if r.id == ref || strings.EqualFold(r.name, ref) {
			ids = append(ids, r.id)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("loading epics: %w", err)
	}
	return ids, nil
}
