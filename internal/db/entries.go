package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/tcarac/taskboard/internal/models"
)

const (
	// EntryDefaultLimit and EntryMaxLimit bound a page of a read of one
	// owner's entries.
	EntryDefaultLimit = 20
	EntryMaxLimit     = 100
	// EntryAuthorNameMaxLength caps the person's name on an entry, in
	// characters: it is a name, not a message.
	EntryAuthorNameMaxLength = 100
)

// entrySelect reads an entry with the id of the entry that replaced it, if
// any: replaced_by is empty for a current entry. Both sides go through
// liveEntries; an entry and its replacement share an owner, so they are live
// together.
const entrySelect = `SELECT e.id, COALESCE(e.project_id, ''), COALESCE(e.epic_id, ''), COALESCE(e.ticket_id, ''),
		e.type, e.text, COALESCE(e.agent_id, ''), e.author_name, e.source, COALESCE(e.replaces_id, ''),
		COALESCE(r.id, ''), COALESCE(e.about_id, ''), COALESCE(e.handled_by, ''), e.handled_at,
		e.verdict, e.findings, e.report_document, e.created_at
	FROM ` + liveEntries + ` e
	LEFT JOIN ` + liveEntries + ` r ON r.replaces_id = e.id`

func scanEntry(row interface{ Scan(...any) error }) (models.Entry, error) {
	var e models.Entry
	var findings sql.NullString
	err := row.Scan(&e.ID, &e.ProjectID, &e.EpicID, &e.TicketID, &e.Type, &e.Text, &e.AgentID, &e.AuthorName,
		&e.Source, &e.Replaces, &e.ReplacedBy, &e.About, &e.HandledBy, &e.HandledAt,
		&e.Verdict, &findings, &e.ReportDocument, &e.CreatedAt)
	if err != nil {
		return e, err
	}
	if findings.Valid {
		if err := json.Unmarshal([]byte(findings.String), &e.Findings); err != nil {
			return e, fmt.Errorf("reading entry %s's findings: %w", e.ID, err)
		}
	}
	return e, nil
}

// ownerKind names what an owner is, for messages and for its column.
func ownerKind(o models.EntryOwner) string {
	switch {
	case o.TicketID != "":
		return "ticket"
	case o.EpicID != "":
		return "epic"
	default:
		return "project"
	}
}

// ownerColumn is the entries column holding o's id.
func ownerColumn(o models.EntryOwner) (string, string) {
	switch {
	case o.TicketID != "":
		return "ticket_id", o.TicketID
	case o.EpicID != "":
		return "epic_id", o.EpicID
	default:
		return "project_id", o.ProjectID
	}
}

// resolveEntryOwner checks that o names exactly one project, epic or ticket
// and resolves it to that thing's id: a project by id or prefix, a ticket by
// id or display key, an epic by id. A deleted project's things are not found.
func resolveEntryOwner(q dbtx, o models.EntryOwner) (models.EntryOwner, error) {
	o.ProjectID = strings.TrimSpace(o.ProjectID)
	o.EpicID = strings.TrimSpace(o.EpicID)
	o.TicketID = strings.TrimSpace(o.TicketID)
	set := 0
	for _, id := range []string{o.ProjectID, o.EpicID, o.TicketID} {
		if id != "" {
			set++
		}
	}
	if set != 1 {
		return o, invalidInput("an entry sits on exactly one project, epic or ticket: give one of projectId, epicId and ticketId")
	}
	var err error
	switch {
	case o.ProjectID != "":
		o.ProjectID, err = resolveProjectRef(q, o.ProjectID)
	case o.TicketID != "":
		o.TicketID, err = lookupTicketRef(q, o.TicketID)
	default:
		var id string
		err = q.QueryRow(`SELECT id FROM `+liveEpics+` WHERE id = ?`, o.EpicID).Scan(&id)
		if err == sql.ErrNoRows {
			err = invalidInput("epic not found: %q", o.EpicID)
		}
	}
	return o, err
}

// entryOnOwner reads the entry id if it sits on owner, or reports what the
// caller got wrong, naming the argument (what) that gave the id.
func entryOnOwner(q dbtx, owner models.EntryOwner, id, what string) (models.Entry, error) {
	e, err := scanEntry(q.QueryRow(entrySelect+` WHERE e.id = ?`, id))
	if err == sql.ErrNoRows {
		return e, invalidInput("%s %q is not an entry", what, id)
	}
	if err != nil {
		return e, err
	}
	if e.EntryOwner != owner {
		return e, invalidInput("%s %q is an entry on another %s: it must be on this %s", what, id,
			ownerKind(e.EntryOwner), ownerKind(owner))
	}
	return e, nil
}

// agentExists reports an unknown agent as the caller's mistake, naming the
// argument (what) that gave it.
func agentExists(q dbtx, id, what string) error {
	var n int
	if err := q.QueryRow(`SELECT COUNT(*) FROM agents WHERE id = ?`, id).Scan(&n); err != nil {
		return fmt.Errorf("looking up agent %q: %w", id, err)
	}
	if n == 0 {
		return invalidInput("%s %q is not an agent", what, id)
	}
	return nil
}

// CreateEntry writes a new entry, stamped now, on the one project, epic or
// ticket req names, and answers with it as read back. The type must be one
// of models.EntryTypes, and a ticket-only type goes on a ticket only. The
// author is the agent (AgentID) or the person by name (AuthorName), never
// both. Each type-specific field is for its own type:
//
//   - a decision has a Source; one from an agent may be the person's (a
//     correction), but one the person writes is the person's.
//   - a note is the person's own, and may point (About) at an entry on the
//     same owner, which is how the person challenges it.
//   - a review has a Verdict, its Findings counted by severity, and a text
//     of one line, its summary; it may name the ticket's document that holds
//     the full report (ReportDocument), stored under that document's name.
//
// Replaces names an earlier entry of the same type on the same owner that
// this one replaces; each entry is replaced at most once. Anything else
// wrong is an ErrInvalidInput and writes nothing. An agent's entry touches
// the agent.
func (s *Store) CreateEntry(req models.CreateEntryRequest) (*models.Entry, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	id, err := createEntry(tx, req, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetEntry(id)
}

// createEntry is CreateEntry inside q's transaction, stamped now, so a write
// such as ReleaseTicket can leave its entry in the same transaction as its
// other changes. It answers with the new entry's id.
func createEntry(q dbtx, req models.CreateEntryRequest, now time.Time) (string, error) {
	e := models.Entry{
		Type:       strings.TrimSpace(req.Type),
		Text:       strings.TrimSpace(req.Text),
		AgentID:    strings.TrimSpace(req.AgentID),
		AuthorName: strings.TrimSpace(req.AuthorName),
		Source:     strings.TrimSpace(req.Source),
		Replaces:   strings.TrimSpace(req.Replaces),
		About:      strings.TrimSpace(req.About),
		Verdict:    strings.TrimSpace(req.Verdict),
		CreatedAt:  now,
	}
	reportDocument := strings.TrimSpace(req.ReportDocument)
	if !models.ValidEntryType(e.Type) {
		return "", invalidInput("type %q is not an entry type: use one of %s", e.Type, strings.Join(models.EntryTypes, ", "))
	}
	if e.Text == "" {
		return "", invalidInput("text is required")
	}
	if (e.AgentID == "") == (e.AuthorName == "") {
		return "", invalidInput("author is required: the agent that writes the entry (agentId) or the person's name (authorName), not both")
	}
	if n := utf8.RuneCountInString(e.AuthorName); n > EntryAuthorNameMaxLength {
		return "", invalidInput("author is %d characters long; at most %d are allowed", n, EntryAuthorNameMaxLength)
	}
	if err := checkEntryTypeFields(&e, req.Findings, reportDocument); err != nil {
		return "", err
	}

	var err error
	if e.EntryOwner, err = resolveEntryOwner(q, req.EntryOwner); err != nil {
		return "", err
	}
	if models.TicketOnlyEntryType(e.Type) && e.TicketID == "" {
		return "", invalidInput("a %s entry goes on a ticket only, not on a %s", e.Type, ownerKind(e.EntryOwner))
	}
	if e.AgentID != "" {
		if err := agentExists(q, e.AgentID, "agentId"); err != nil {
			return "", err
		}
	}
	if e.About != "" {
		if _, err := entryOnOwner(q, e.EntryOwner, e.About, "about"); err != nil {
			return "", err
		}
	}
	if e.Replaces != "" {
		old, err := entryOnOwner(q, e.EntryOwner, e.Replaces, "replaces")
		if err != nil {
			return "", err
		}
		if old.Type != e.Type {
			return "", invalidInput("replaces %q is a %s entry: an entry replaces one of its own type (%s)", e.Replaces, old.Type, e.Type)
		}
		if old.ReplacedBy != "" {
			return "", invalidInput("entry %q is already replaced by %q: replace that one instead", e.Replaces, old.ReplacedBy)
		}
	}
	if reportDocument != "" {
		if e.ReportDocument, err = ticketDocumentName(q, e.TicketID, reportDocument); err != nil {
			return "", err
		}
	}

	e.ID = newID()
	var findings any
	if e.Findings != nil {
		raw, err := json.Marshal(e.Findings)
		if err != nil {
			return "", err
		}
		findings = string(raw)
	}
	if _, err := q.Exec(`INSERT INTO entries (id, project_id, epic_id, ticket_id, type, text, agent_id, author_name,
			source, replaces_id, about_id, verdict, findings, report_document, created_at)
		VALUES (?, NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''), ?, ?, NULLIF(?, ''), ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, ?, ?, ?)`,
		e.ID, e.ProjectID, e.EpicID, e.TicketID, e.Type, e.Text, e.AgentID, e.AuthorName,
		e.Source, e.Replaces, e.About, e.Verdict, findings, e.ReportDocument, stamp(e.CreatedAt),
	); err != nil {
		return "", fmt.Errorf("writing entry: %w", err)
	}
	if e.AgentID != "" {
		if err := touchAgent(q, e.AgentID, now); err != nil {
			return "", err
		}
	}
	return e.ID, nil
}

// checkEntryTypeFields checks the fields that belong to one type against
// e's type, and sets e.Findings from findings for a review, with every
// severity counted.
func checkEntryTypeFields(e *models.Entry, findings map[string]int, reportDocument string) error {
	if e.Type == models.EntryDecision {
		if !models.ValidDecisionSource(e.Source) {
			return invalidInput("a decision's source is required: %s (the person's, for a correction, even when an agent writes it)",
				strings.Join(models.DecisionSources, " or "))
		}
		if e.Source == models.DecisionSourceAgent && e.AgentID == "" {
			return invalidInput("a decision the person writes has the person as its source, not %s", models.DecisionSourceAgent)
		}
	} else if e.Source != "" {
		return invalidInput("source is for a decision only, not a %s", e.Type)
	}

	if e.Type == models.EntryNote {
		if e.AgentID != "" {
			return invalidInput("a note is the person's: give authorName, not agentId")
		}
	} else if e.About != "" {
		return invalidInput("about is for a note only, not a %s", e.Type)
	}

	if e.Type != models.EntryReview {
		if e.Verdict != "" || findings != nil || reportDocument != "" {
			return invalidInput("verdict, findings and reportDocument are for a review only, not a %s", e.Type)
		}
		return nil
	}
	if !models.ValidReviewVerdict(e.Verdict) {
		return invalidInput("a review's verdict is required: %s", strings.Join(models.ReviewVerdicts, " or "))
	}
	if strings.ContainsAny(e.Text, "\r\n") {
		return invalidInput("a review's text is its one-line summary; the full report goes in its Review document")
	}
	e.Findings = make(map[string]int, len(models.ReviewSeverities))
	for _, sev := range models.ReviewSeverities {
		e.Findings[sev] = 0
	}
	for sev, n := range findings {
		if !models.ValidReviewSeverity(sev) {
			return invalidInput("findings are counted by severity: %q is not one of %s", sev, strings.Join(models.ReviewSeverities, ", "))
		}
		if n < 0 {
			return invalidInput("findings %s is %d: a count is never negative", sev, n)
		}
		e.Findings[sev] = n
	}
	return nil
}

// ticketDocumentName finds one of the ticket's documents by name, ignoring
// case, with or without its format's extension, and returns its name.
func ticketDocumentName(q dbtx, ticketID, ref string) (string, error) {
	rows, err := q.Query(`SELECT name, format FROM `+liveDocuments+` WHERE ticket_id = ?`, ticketID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var name, format string
		if err := rows.Scan(&name, &format); err != nil {
			return "", err
		}
		if strings.EqualFold(name, ref) || strings.EqualFold(models.DocumentDisplayName(name, format), ref) {
			return name, nil
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return "", invalidInput(`reportDocument: this ticket has no document called "%s"`, ref)
}

// GetEntry returns one entry, replaced or not, or (nil, nil) for an unknown
// one or one in a deleted project.
func (s *Store) GetEntry(id string) (*models.Entry, error) {
	e, err := scanEntry(s.db.QueryRow(entrySelect+` WHERE e.id = ?`, strings.TrimSpace(id)))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// MarkNoteHandled records that the agent handled the note: the note is no
// longer open. It answers with the note. Only an open note can be handled
// (models.Entry.Open), and only once; an unknown note or agent, or a note
// already handled or replaced by a revised one, is an ErrInvalidInput and
// changes nothing. It touches the agent.
func (s *Store) MarkNoteHandled(noteID, agentID string) (*models.Entry, error) {
	noteID, agentID = strings.TrimSpace(noteID), strings.TrimSpace(agentID)
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	if agentID == "" {
		return nil, invalidInput("agentId is required: the agent that handled the note")
	}
	if err := agentExists(tx, agentID, "agentId"); err != nil {
		return nil, err
	}
	note, err := scanEntry(tx.QueryRow(entrySelect+` WHERE e.id = ?`, noteID))
	if err == sql.ErrNoRows {
		return nil, invalidInput("entry not found: %q", noteID)
	}
	if err != nil {
		return nil, err
	}
	if note.Type != models.EntryNote {
		return nil, invalidInput("entry %q is a %s: only a note is handled", noteID, note.Type)
	}
	if note.HandledAt != nil {
		return nil, invalidInput("note %q is already handled, by agent %q", noteID, note.HandledBy)
	}
	if note.ReplacedBy != "" {
		return nil, invalidInput("note %q is replaced by %q, so it is not open: handle the note that replaced it", noteID, note.ReplacedBy)
	}
	now := time.Now().UTC()
	if _, err := tx.Exec(`UPDATE entries SET handled_by = ?, handled_at = ? WHERE id = ? AND handled_at IS NULL`,
		agentID, stamp(now), noteID); err != nil {
		return nil, fmt.Errorf("marking note handled: %w", err)
	}
	if err := touchAgent(tx, agentID, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetEntry(noteID)
}

// openNote is models.Entry.Open in SQL, over an entry e joined to the entry r
// that replaced it the way entrySelect joins them: a note that is neither
// handled nor replaced. TestOpenNoteCountAgreesWithEntryOpen holds the two to
// the same rule.
const openNote = `e.type = '` + models.EntryNote + `' AND e.handled_at IS NULL AND r.id IS NULL`

// CountOpenNotes counts the open notes (models.Entry.Open) on one project,
// epic or ticket, given by id. An unknown owner, or one in a deleted project,
// has none.
func (s *Store) CountOpenNotes(owner models.EntryOwner) (int, error) {
	col, id := ownerColumn(owner)
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM `+liveEntries+` e LEFT JOIN `+liveEntries+` r ON r.replaces_id = e.id
		WHERE e.`+col+` = ? AND `+openNote, id).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("counting open notes: %w", err)
	}
	return n, nil
}

// TicketEntries reads what a read of the ticket carries about entries
// (models.TicketEntries): its newest EntryDefaultLimit current entries (no
// page when it has none), its open notes counted, and the open notes on its
// epic, when it has one, and its project.
func (s *Store) TicketEntries(t *models.Ticket) (models.TicketEntries, error) {
	var out models.TicketEntries
	ticket := models.EntryOwner{TicketID: t.ID}
	page, err := s.listEntries(ticket, models.EntryFilter{}, "", EntryDefaultLimit, "")
	if err != nil {
		return out, err
	}
	if page.Total > 0 {
		out.Entries = &page
	}
	if out.OpenNotes, err = s.CountOpenNotes(ticket); err != nil {
		return out, err
	}
	if t.Epic != nil {
		if out.EpicOpenNotes, err = s.CountOpenNotes(models.EntryOwner{EpicID: t.Epic.ID}); err != nil {
			return out, err
		}
	}
	out.ProjectOpenNotes, err = s.CountOpenNotes(models.EntryOwner{ProjectID: t.ProjectID})
	return out, err
}

// ListEntries returns one page of one owner's entries, newest first: up to
// limit entries (1 to EntryMaxLimit), starting with the newest when before
// is empty, or else with the newest entry older than the entry before
// names, which must be on the same owner. Replaced entries are left out
// unless filter asks for them. Entries written within the same instant come
// back in the reverse of the order they were written. An unknown owner, type
// or before, or a limit out of range, is an ErrInvalidInput.
func (s *Store) ListEntries(owner models.EntryOwner, filter models.EntryFilter, before string, limit int) (models.EntryPage, error) {
	owner, err := resolveEntryOwner(s.db, owner)
	if err != nil {
		return models.EntryPage{Entries: []models.Entry{}}, err
	}
	return s.listEntries(owner, filter, before, limit, "an entry on this "+ownerKind(owner))
}

// listEntries is ListEntries on a resolved owner. beforeIs completes the
// message for a before that is not one of the owner's entries.
func (s *Store) listEntries(owner models.EntryOwner, filter models.EntryFilter, before string, limit int, beforeIs string) (models.EntryPage, error) {
	page := models.EntryPage{Entries: []models.Entry{}}
	if limit < 1 || limit > EntryMaxLimit {
		return page, invalidInput("limit must be between 1 and %d", EntryMaxLimit)
	}
	col, ownerID := ownerColumn(owner)
	where := ` WHERE e.` + col + ` = ?`
	args := []any{ownerID}
	if !filter.IncludeReplaced {
		where += ` AND r.id IS NULL`
	}
	if filter.ExcludeOpenNotes {
		where += ` AND NOT (` + openNote + `)`
	}
	if len(filter.Types) > 0 {
		for _, t := range filter.Types {
			if !models.ValidEntryType(t) {
				return page, invalidInput("type %q is not an entry type: use one of %s", t, strings.Join(models.EntryTypes, ", "))
			}
		}
		types := slices.Compact(slices.Sorted(slices.Values(filter.Types)))
		where += ` AND e.type IN (?` + strings.Repeat(`, ?`, len(types)-1) + `)`
		for _, t := range types {
			args = append(args, t)
		}
	}

	if err := s.db.QueryRow(`SELECT COUNT(*) FROM `+liveEntries+` e LEFT JOIN `+liveEntries+` r ON r.replaces_id = e.id`+where,
		args...).Scan(&page.Total); err != nil {
		return page, fmt.Errorf("counting entries: %w", err)
	}

	// rowid follows commit order (writes are serialised), so it breaks ties
	// between entries stamped with the same instant.
	query := entrySelect + where
	if before = strings.TrimSpace(before); before != "" {
		var found int
		err := s.db.QueryRow(`SELECT COUNT(*) FROM `+liveEntries+` e WHERE e.id = ? AND e.`+col+` = ?`,
			before, ownerID).Scan(&found)
		if err != nil {
			return page, fmt.Errorf("reading entry %q: %w", before, err)
		}
		if found == 0 {
			return page, invalidInput("before %q is not %s", before, beforeIs)
		}
		// Compared in SQL, on the stored text: read into Go, the driver
		// turns created_at into a time.Time, which formats differently.
		query += ` AND (e.created_at, e.rowid) < (SELECT created_at, rowid FROM ` + liveEntries + ` b WHERE b.id = ?)`
		args = append(args, before)
	}
	// One more than asked for says whether another page follows.
	query += ` ORDER BY e.created_at DESC, e.rowid DESC LIMIT ?`
	args = append(args, limit+1)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
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
	return page, nil
}
