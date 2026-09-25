package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/tcarac/taskboard/internal/doctext"
	"github.com/tcarac/taskboard/internal/imageref"
	"github.com/tcarac/taskboard/internal/models"
)

// An ownerText is one text that may use the owner's images: a ticket's
// description, or one of the owner's markdown or HTML documents.
type ownerText struct {
	place    models.ImagePlace
	format   string
	content  string
	revision int
}

// loadOwnerTexts reads the texts that can use an owner's images: a ticket's
// description first, then its or an epic's markdown and HTML documents in
// the order they were added. An epic's description does not show images,
// so it is not one of them.
func loadOwnerTexts(q dbtx, owner DocumentOwner) ([]ownerText, error) {
	var texts []ownerText
	if owner.TicketID != "" {
		var description string
		err := q.QueryRow("SELECT description FROM tickets WHERE id = ?", owner.TicketID).Scan(&description)
		if err != nil && err != sql.ErrNoRows {
			return nil, fmt.Errorf("reading description: %w", err)
		}
		if err == nil {
			texts = append(texts, ownerText{
				place:  models.ImagePlace{Kind: models.ImagePlaceDescription},
				format: models.DocumentFormatMarkdown, content: description,
			})
		}
	}
	cond, arg := owner.where()
	rows, err := q.Query(`SELECT id, name, format, content, revision FROM documents
		WHERE `+cond+` AND format IN ('markdown', 'html') ORDER BY created_at, id`, arg)
	if err != nil {
		return nil, fmt.Errorf("reading documents: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var t ownerText
		var name string
		if err := rows.Scan(&t.place.DocumentID, &name, &t.format, &t.content, &t.revision); err != nil {
			return nil, err
		}
		t.place.Kind = models.ImagePlaceDocument
		t.place.Name = models.DocumentDisplayName(name, t.format)
		texts = append(texts, t)
	}
	return texts, rows.Err()
}

// imageUsage lists the places in an owner's text that show the image: the
// ones where a reference names it (imageref.Uses). The result is never nil.
func imageUsage(q dbtx, owner DocumentOwner, img imageref.Image) ([]models.ImagePlace, error) {
	texts, err := loadOwnerTexts(q, owner)
	if err != nil {
		return nil, err
	}
	places := []models.ImagePlace{}
	for _, t := range texts {
		if imageref.Uses(t.format, t.content, img) > 0 {
			places = append(places, t.place)
		}
	}
	return places, nil
}

// ImageUsage lists where an image is used in its owner's text: the
// ticket's description and the owner's markdown and HTML documents whose
// references name it. It is worked out from the text each time. found is
// false for an unknown id; a document that is not an image is refused.
func (s *Store) ImageUsage(id string) (places []models.ImagePlace, found bool, err error) {
	meta, err := scanDocumentMeta(s.db.QueryRow("SELECT "+documentMetaColumns+" FROM documents WHERE id = ?", id))
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !models.IsImageFormat(meta.Format) {
		return nil, true, invalidInput(msgDocNotImage)
	}
	places, err = imageUsage(s.db, DocumentOwner{TicketID: meta.TicketID, EpicID: meta.EpicID},
		imageref.Image{Name: meta.Name, Format: meta.Format})
	return places, true, err
}

// DeleteDocumentReportingUse deletes a document for good, like
// DeleteDocument, and for an image also returns the places in its owner's
// text that used it and now show a missing image, worked out in the same
// transaction as the delete. It never refuses to delete an image in use.
// deleted is false, with no error, for an unknown id.
func (s *Store) DeleteDocumentReportingUse(id string) (deleted bool, usedIn []models.ImagePlace, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	meta, err := scanDocumentMeta(tx.QueryRow("SELECT "+documentMetaColumns+" FROM documents WHERE id = ?", id))
	if err == sql.ErrNoRows {
		return false, nil, nil
	}
	if err != nil {
		return false, nil, err
	}
	if models.IsImageFormat(meta.Format) {
		usedIn, err = imageUsage(tx, DocumentOwner{TicketID: meta.TicketID, EpicID: meta.EpicID},
			imageref.Image{Name: meta.Name, Format: meta.Format})
		if err != nil {
			return false, nil, err
		}
	}
	if _, err := tx.Exec("DELETE FROM documents WHERE id = ?", id); err != nil {
		return false, nil, err
	}
	if err := tx.Commit(); err != nil {
		return false, nil, fmt.Errorf("committing delete: %w", err)
	}
	return true, usedIn, nil
}

// imageRewrite is what renaming an image did to its owner's text.
type imageRewrite struct {
	updated []models.ImagePlace
	left    []models.ImagePlace
}

// rewriteImageRefs keeps an owner's text in step with the rename of its
// image img to newName, inside the rename's transaction: every reference
// to the image in the ticket's description and the owner's markdown and
// HTML documents is rewritten to the new name in the form it was written
// (imageref.Rename). A rewritten document is a content save: its revision
// goes up and its readable text is worked out again, so an open editor
// sees the change like any other save. A rewritten description is a ticket
// update: its updated_at moves. A change of case alone rewrites nothing,
// since references ignore case.
func rewriteImageRefs(tx dbtx, owner DocumentOwner, img imageref.Image, newName string, now time.Time) (imageRewrite, error) {
	var done imageRewrite
	if strings.EqualFold(img.Name, newName) {
		return done, nil
	}
	texts, err := loadOwnerTexts(tx, owner)
	if err != nil {
		return done, err
	}
	for _, t := range texts {
		content, result := imageref.Rename(t.format, t.content, img, newName)
		if result.Left > 0 {
			done.left = append(done.left, t.place)
		}
		if content == t.content {
			continue
		}
		done.updated = append(done.updated, t.place)
		if t.place.Kind == models.ImagePlaceDescription {
			if _, err := tx.Exec("UPDATE tickets SET description = ?, updated_at = ? WHERE id = ?",
				content, stamp(now), owner.TicketID); err != nil {
				return done, fmt.Errorf("updating description: %w", err)
			}
			continue
		}
		if _, err := tx.Exec("UPDATE documents SET content = ?, revision = revision + 1, updated_at = ? WHERE id = ?",
			content, stamp(now), t.place.DocumentID); err != nil {
			return done, fmt.Errorf("updating %s: %w", t.place.Name, err)
		}
		if err := saveDocumentText(tx, t.place.DocumentID, t.revision+1, doctext.ReadableText(t.format, content)); err != nil {
			return done, err
		}
	}
	return done, nil
}

// withRewrite puts what a rename did to the owner's text on the document
// it returns.
func withRewrite(d *models.Document, done imageRewrite) *models.Document {
	if d != nil {
		d.ReferencesUpdated = done.updated
		d.ReferencesLeft = done.left
	}
	return d
}
