package db

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/tcarac/taskboard/internal/models"
)

// This file holds the typed links between tickets (migration 015): the kind
// of each dependency, and the "surfaced from" link to the ticket during whose
// work a ticket was found.

// dependency is one resolved dependsOn entry: the ticket depended on, its
// kind and its note.
type dependency struct {
	id   string
	kind string
	note string
}

// resolveDependencies checks and resolves a request's dependsOn entries,
// the whole list the ticket will have. Every entry names a ticket (an id or
// display key); its kind, trimmed and in any case, is needs_work when left
// out, and anything but DependencyKinds is an ErrInvalidInput; its note is
// trimmed. The same ticket listed twice with the same kind and note counts
// once, and with a different kind or note is an ErrInvalidInput, as is the
// ticket itself. selfID is the ticket declaring the dependencies.
func resolveDependencies(q dbtx, selfID string, entries []models.DependencyInput) ([]dependency, error) {
	deps := make([]dependency, 0, len(entries))
	at := make(map[string]int, len(entries))

	for _, entry := range entries {
		ref := strings.TrimSpace(entry.Ticket)
		if ref == "" {
			return nil, invalidInput("a dependsOn entry names no ticket: each entry is an object %s", models.DependencyShape)
		}
		kind := models.NormalizeDependencyKind(entry.Kind)
		if kind == "" {
			kind = models.DependencyNeedsWork
		}
		if !models.ValidDependencyKind(kind) {
			return nil, invalidInput("dependency kind %q for %q is invalid: must be one of %s",
				entry.Kind, ref, strings.Join(models.DependencyKinds, ", "))
		}
		note := strings.TrimSpace(entry.Note)

		id, err := lookupTicketRef(q, ref)
		if err != nil {
			return nil, err
		}
		if id == selfID {
			return nil, invalidInput("ticket %q cannot depend on itself", ref)
		}
		if i, seen := at[id]; seen {
			if deps[i].kind != kind || deps[i].note != note {
				return nil, invalidInput("ticket %q is listed twice in dependsOn with a different kind or note", ref)
			}
			continue
		}
		at[id] = len(deps)
		deps = append(deps, dependency{id: id, kind: kind, note: note})
	}
	return deps, nil
}

// replaceDependencies makes deps the ticket's whole set of dependencies,
// kinds and notes included.
func replaceDependencies(tx dbtx, ticketID string, deps []dependency) error {
	if _, err := tx.Exec("DELETE FROM ticket_dependencies WHERE ticket_id = ?", ticketID); err != nil {
		return fmt.Errorf("clearing dependencies: %w", err)
	}
	for _, d := range deps {
		if _, err := tx.Exec(
			"INSERT INTO ticket_dependencies (ticket_id, blocked_by_id, kind, note) VALUES (?, ?, ?, ?)",
			ticketID, d.id, d.kind, d.note,
		); err != nil {
			return fmt.Errorf("attaching dependency: %w", err)
		}
	}
	return nil
}

// resolveSurfacedFrom resolves a surfacedFrom value for ticket selfID: the
// id of the ticket it names, or nil for "" and "none" (any case), which
// remove the link. Only spaces is an ErrInvalidInput, as is naming the ticket
// itself or a ticket surfaced from it, directly or further down, which would
// make the links a loop.
func resolveSurfacedFrom(q dbtx, selfID, raw string) (*string, error) {
	if raw == "" {
		return nil, nil
	}
	ref := strings.TrimSpace(raw)
	if ref == "" {
		return nil, invalidInput("surfacedFrom is only spaces: pass a ticket id or key, or \"\" or \"none\" to remove the link")
	}
	if strings.EqualFold(ref, models.NoSurfacedFrom) {
		return nil, nil
	}
	id, err := lookupTicketRef(q, ref)
	if err != nil {
		return nil, err
	}
	if id == selfID {
		return nil, invalidInput("ticket %q cannot be surfaced from itself", ref)
	}

	// Walk up from the named ticket. Every ticket has at most one link, so
	// this is a chain; the visited set only guards against a loop already in
	// the data.
	visited := map[string]bool{id: true}
	for at := id; ; {
		var next string
		err := q.QueryRow("SELECT source_id FROM ticket_surfaced_from WHERE ticket_id = ?", at).Scan(&next)
		if err == sql.ErrNoRows {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading surfaced-from links: %w", err)
		}
		if next == selfID {
			return nil, invalidInput("ticket %q cannot be surfaced from %q: %q was itself surfaced from it", selfID, ref, ref)
		}
		if visited[next] {
			break
		}
		visited[next] = true
		at = next
	}
	return &id, nil
}

// setSurfacedFrom replaces the ticket's surfaced-from link with sourceID, or
// removes it when sourceID is nil.
func setSurfacedFrom(tx dbtx, ticketID string, sourceID *string) error {
	if _, err := tx.Exec("DELETE FROM ticket_surfaced_from WHERE ticket_id = ?", ticketID); err != nil {
		return fmt.Errorf("clearing surfaced-from link: %w", err)
	}
	if sourceID == nil {
		return nil
	}
	if _, err := tx.Exec(
		"INSERT INTO ticket_surfaced_from (ticket_id, source_id) VALUES (?, ?)", ticketID, *sourceID,
	); err != nil {
		return fmt.Errorf("setting surfaced-from link: %w", err)
	}
	return nil
}

// dependencyRefSelect is ticketRefSelect with the dependency's kind and
// note; the caller joins ticket_dependencies as d.
const dependencyRefSelect = `SELECT t.id,
	COALESCE(p.prefix, '') || '-' || t.number AS key,
	t.title, t.status, d.kind, d.note
	FROM tickets t LEFT JOIN projects p ON t.project_id = p.id`

func scanDependencyRefs(rows *sql.Rows) ([]models.TicketRef, error) {
	defer rows.Close()
	var refs []models.TicketRef
	for rows.Next() {
		var r models.TicketRef
		if err := rows.Scan(&r.ID, &r.Key, &r.Title, &r.Status, &r.Kind, &r.Note); err != nil {
			return nil, err
		}
		refs = append(refs, r)
	}
	return refs, rows.Err()
}

// getTicketSurfacedFrom returns the ticket this one was surfaced from, or nil.
func (s *Store) getTicketSurfacedFrom(ticketID string) (*models.TicketRef, error) {
	rows, err := s.db.Query(ticketRefSelect+
		` JOIN ticket_surfaced_from sf ON sf.source_id = t.id
		WHERE sf.ticket_id = ?`, ticketID)
	if err != nil {
		return nil, err
	}
	refs, err := scanTicketRefs(rows)
	if err != nil || len(refs) == 0 {
		return nil, err
	}
	return &refs[0], nil
}

// getTicketSurfaced returns the tickets surfaced from this one.
func (s *Store) getTicketSurfaced(ticketID string) ([]models.TicketRef, error) {
	rows, err := s.db.Query(ticketRefSelect+
		` JOIN ticket_surfaced_from sf ON sf.ticket_id = t.id
		WHERE sf.source_id = ? ORDER BY t.number`, ticketID)
	if err != nil {
		return nil, err
	}
	return scanTicketRefs(rows)
}

// attachSurfacedFrom fills SurfacedFrom for a page of tickets in one query,
// the way attachListDetails fills the other relations.
func (s *Store) attachSurfacedFrom(tickets []models.Ticket, index map[string]int, placeholders string, ids []any) error {
	rows, err := s.db.Query(
		`SELECT sf.ticket_id, t.id,
		COALESCE(p.prefix, '') || '-' || t.number, t.title, t.status
		FROM ticket_surfaced_from sf
		JOIN tickets t ON t.id = sf.source_id
		LEFT JOIN projects p ON p.id = t.project_id
		WHERE sf.ticket_id IN (`+placeholders+`)`, ids...)
	if err != nil {
		return fmt.Errorf("loading surfaced-from links: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var ticketID string
		var r models.TicketRef
		if err := rows.Scan(&ticketID, &r.ID, &r.Key, &r.Title, &r.Status); err != nil {
			return err
		}
		if i, ok := index[ticketID]; ok {
			tickets[i].SurfacedFrom = &r
		}
	}
	return rows.Err()
}
