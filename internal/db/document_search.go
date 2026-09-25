package db

import (
	"database/sql"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/tcarac/taskboard/internal/doctext"
	"github.com/tcarac/taskboard/internal/models"
)

// SearchDocumentTickets returns the ids of the tickets that have a document
// whose display name ("Plan.md") or readable text contains q anywhere, even
// inside a word, ignoring case. It reads the text kept in document_search,
// never the content. Only tickets' own documents count, not epics'. With
// projectRef (id or prefix) it looks in that project only. q is trimmed; an
// empty q, or a project that names nothing, matches no tickets. Case is
// folded here with foldCase rather than in SQL, whose lower() and
// LIKE fold ASCII only. The result is sorted and never nil.
func (s *Store) SearchDocumentTickets(q, projectRef string) ([]string, error) {
	ids := []string{}
	needle := foldCase(strings.TrimSpace(q))
	if needle == "" {
		return ids, nil
	}
	query := `SELECT d.ticket_id, d.name, d.format, COALESCE(ds.text, '') FROM documents d
		JOIN tickets t ON t.id = d.ticket_id
		LEFT JOIN document_search ds ON ds.document_id = d.id`
	var args []any
	if strings.TrimSpace(projectRef) != "" {
		projectID, ok, err := resolveProjectFilterID(s.db, projectRef)
		if err != nil {
			return nil, err
		}
		if !ok {
			return ids, nil
		}
		query += " WHERE t.project_id = ?"
		args = append(args, projectID)
	}
	query += " ORDER BY d.ticket_id"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("searching documents: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var ticketID, name, format, text string
		if err := rows.Scan(&ticketID, &name, &format, &text); err != nil {
			return nil, err
		}
		if len(ids) > 0 && ids[len(ids)-1] == ticketID {
			continue
		}
		if strings.Contains(foldCase(models.DocumentDisplayName(name, format)), needle) ||
			strings.Contains(foldCase(text), needle) {
			ids = append(ids, ticketID)
		}
	}
	return ids, rows.Err()
}

// foldCase applies Unicode simple case folding: every rune becomes the
// smallest rune of its unicode.SimpleFold cycle, so two strings fold alike
// exactly when strings.EqualFold holds rune by rune. Unlike strings.ToLower
// this puts "Σ", "σ" and the final "ς" together, as the browser's
// toLowerCase-based ticket search does for a word ending in sigma.
func foldCase(s string) string {
	return strings.Map(foldRune, s)
}

func foldRune(r rune) rune {
	if r < utf8.RuneSelf {
		// ASCII fast path. The smallest rune of an ASCII letter's cycle is its
		// upper case ("k", "K" and the Kelvin sign fold to "K").
		if 'a' <= r && r <= 'z' {
			return r - ('a' - 'A')
		}
		return r
	}
	least := r
	for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
		if f < least {
			least = f
		}
	}
	return least
}

// saveDocumentText records a document's readable text as of revision, the
// revision its content has once the caller's write lands.
func saveDocumentText(q dbtx, id string, revision int, text string) error {
	_, err := q.Exec(`INSERT INTO document_search (document_id, revision, text) VALUES (?, ?, ?)
		ON CONFLICT(document_id) DO UPDATE SET revision = excluded.revision, text = excluded.text`,
		id, revision, text)
	if err != nil {
		return fmt.Errorf("saving document text: %w", err)
	}
	return nil
}

// fillDocumentSearch works out the readable text of every document that has
// none, or whose text is from an older revision: documents written before
// the text was kept, or by an older build since. OpenAt runs it, so they
// become searchable the first time a build that keeps the text opens the
// database; after that it finds nothing to do. Each document is parsed
// outside any transaction, and its text is stored only if the document is
// still at the revision that was read, so a save that lands meanwhile, and
// writes its own text, is never overwritten with an older one.
func fillDocumentSearch(database *sql.DB) error {
	rows, err := database.Query(`SELECT d.id FROM documents d
		LEFT JOIN document_search s ON s.document_id = d.id
		WHERE s.revision IS NOT d.revision`)
	if err != nil {
		return fmt.Errorf("finding documents without text: %w", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, id := range ids {
		var format, content string
		var revision int
		err := database.QueryRow("SELECT format, content, revision FROM documents WHERE id = ?", id).
			Scan(&format, &content, &revision)
		if err == sql.ErrNoRows {
			continue // deleted meanwhile
		}
		if err != nil {
			return fmt.Errorf("reading document %s: %w", id, err)
		}
		text := doctext.ReadableText(format, content)
		if _, err := database.Exec(`INSERT INTO document_search (document_id, revision, text)
			SELECT id, revision, ? FROM documents WHERE id = ? AND revision = ?
			ON CONFLICT(document_id) DO UPDATE SET revision = excluded.revision, text = excluded.text`,
			text, id, revision); err != nil {
			return fmt.Errorf("saving document text: %w", err)
		}
	}
	return nil
}
