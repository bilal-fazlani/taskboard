package db

import (
	"database/sql"
	"fmt"

	"github.com/tcarac/taskboard/internal/doctext"
)

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
