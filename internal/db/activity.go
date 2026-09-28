package db

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"github.com/tcarac/taskboard/internal/models"
)

// ActivityDefaultLimit and ActivityMaxLimit bound a page of a project's
// activity.
const (
	ActivityDefaultLimit = 50
	ActivityMaxLimit     = 200
)

// ListActivity returns one page of a project's activity: the status changes
// of its tickets, ticket creation included, newest first. It pages like
// ListEntries: up to limit entries (1 to ActivityMaxLimit), starting with the
// newest when before is empty, or else with the newest change older than the
// position before names, a previous page's NextBefore. Unlike the entries',
// that position is not a row id: it stays valid after the change it came from
// is deleted with its ticket, so a walk through the feed never breaks.
//
// epics narrows it to the tickets in any of the given epics, each an epic
// name (case-insensitive) or id, or models.NoEpic for tickets without one;
// none given means every ticket. As with the ticket list's epic filter, a
// value that names no epic of the project matches nothing rather than
// failing the read. An unknown project or before, or a limit out of range,
// is an ErrInvalidInput.
func (s *Store) ListActivity(projectRef string, epics []string, before string, limit int) (models.ActivityPage, error) {
	page := models.ActivityPage{Entries: []models.ActivityEntry{}}
	if limit < 1 || limit > ActivityMaxLimit {
		return page, invalidInput("limit must be between 1 and %d", ActivityMaxLimit)
	}
	projectID, err := resolveProjectRef(s.db, projectRef)
	if err != nil {
		return page, err
	}

	// created_at is read twice: as a time for the entry, and as the stored
	// text (an expression, which the driver leaves as text) for the cursor.
	query := `SELECT c.id, c.ticket_id, c.from_status, c.to_status, c.note, c.created_at,
			p.prefix, t.number, t.title, e.id, e.name, c.rowid, c.created_at || ''
		FROM ticket_status_changes c
		JOIN ` + liveTickets + ` t ON t.id = c.ticket_id
		JOIN ` + liveProjects + ` p ON p.id = t.project_id
		LEFT JOIN ` + liveEpics + ` e ON e.id = t.epic_id
		WHERE t.project_id = ?`
	args := []any{projectID}

	var cursorClause string
	var cursorArgs []any
	if before = strings.TrimSpace(before); before != "" {
		at, rowid, ok := decodeActivityCursor(before)
		if !ok {
			return page, invalidInput("before %q is not a nextBefore from this feed", before)
		}
		// Compared in SQL on the stored text, as ListEntries does, with rowid
		// breaking ties between changes written within the same instant. The
		// position needs no row to exist, so it outlives the change it came
		// from when that change's ticket is deleted.
		cursorClause = ` AND (c.created_at, c.rowid) < (?, ?)`
		cursorArgs = []any{at, rowid}
	}

	epicClause, epicArgs, matchesAny, err := activityEpicFilter(s, projectID, epics)
	if err != nil {
		return page, err
	}
	if !matchesAny {
		return page, nil
	}
	query += epicClause + cursorClause
	args = append(append(args, epicArgs...), cursorArgs...)
	// One more than asked for says whether another page follows.
	query += ` ORDER BY c.created_at DESC, c.rowid DESC LIMIT ?`
	args = append(args, limit+1)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	var cursors []string
	for rows.Next() {
		var a models.ActivityEntry
		var prefix string
		var number int
		var epicID, epicName *string
		var rowid int64
		var storedAt string
		if err := rows.Scan(&a.ID, &a.TicketID, &a.FromStatus, &a.ToStatus, &a.Note, &a.CreatedAt,
			&prefix, &number, &a.TicketTitle, &epicID, &epicName, &rowid, &storedAt); err != nil {
			return page, err
		}
		cursors = append(cursors, encodeActivityCursor(storedAt, rowid))
		a.TicketKey = fmt.Sprintf("%s-%d", prefix, number)
		if epicID != nil && epicName != nil {
			a.Epic = &models.EpicRef{ID: *epicID, Name: *epicName}
		}
		page.Entries = append(page.Entries, a)
	}
	if err := rows.Err(); err != nil {
		return page, err
	}
	if len(page.Entries) > limit {
		page.Entries = page.Entries[:limit]
		page.HasMore = true
		page.NextBefore = cursors[limit-1]
	}
	return page, nil
}

// encodeActivityCursor is a feed position as nextBefore carries it: a
// change's stored created_at text and its rowid, opaque to the caller.
func encodeActivityCursor(storedAt string, rowid int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(storedAt + "|" + strconv.FormatInt(rowid, 10)))
}

// decodeActivityCursor reads a position encodeActivityCursor wrote. ok is
// false for anything else.
func decodeActivityCursor(cursor string) (storedAt string, rowid int64, ok bool) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return "", 0, false
	}
	at, id, found := strings.Cut(string(raw), "|")
	if !found || at == "" {
		return "", 0, false
	}
	rowid, err = strconv.ParseInt(id, 10, 64)
	if err != nil || rowid < 1 {
		return "", 0, false
	}
	return at, rowid, true
}

// activityEpicFilter builds ListActivity's epic condition. matchesAny is
// false when epics were given but none of them names anything, so no ticket
// can match.
func activityEpicFilter(s *Store, projectID string, epics []string) (clause string, args []any, matchesAny bool, err error) {
	noEpic := false
	var ids []string
	given := false
	for _, ref := range epics {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		given = true
		if strings.EqualFold(ref, models.NoEpic) {
			noEpic = true
			continue
		}
		found, err := findEpicIDs(s.db, projectID, ref)
		if err != nil {
			return "", nil, false, err
		}
		ids = append(ids, found...)
	}
	if !given {
		return "", nil, true, nil
	}
	var terms []string
	if noEpic {
		terms = append(terms, "t.epic_id IS NULL")
	}
	if len(ids) > 0 {
		terms = append(terms, "t.epic_id IN ("+strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")+")")
		for _, id := range ids {
			args = append(args, id)
		}
	}
	if len(terms) == 0 {
		return "", nil, false, nil
	}
	return " AND (" + strings.Join(terms, " OR ") + ")", args, true, nil
}
