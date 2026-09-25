package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/tcarac/taskboard/internal/imagedoc"
	"github.com/tcarac/taskboard/internal/models"
)

// msgFormat is the refusal for a format that is not one of the text formats
// (or, for an image, not one of the image formats). SVG gets its own words.
func msgFormat(format string, image bool) string {
	switch {
	case strings.EqualFold(strings.TrimSpace(format), "svg"):
		return msgDocSVG
	case image:
		return msgDocImageFormat
	}
	return msgDocFormat
}

// prepareImage applies the image rules to a file for format: the size
// limit, then imagedoc's content checks, metadata removal and thumbnail.
// Its refusals are ErrInvalidInput with imagedoc's words.
func prepareImage(format string, data []byte) (*imagedoc.Prepared, error) {
	if len(data) > models.MaxDocumentBytes {
		return nil, invalidInput("%s", ImageTooLargeMessage(len(data)))
	}
	p, err := imagedoc.Prepare(format, data)
	var refusal *imagedoc.Refusal
	if errors.As(err, &refusal) {
		return nil, invalidInput("%s", refusal.Msg)
	}
	return p, err
}

// CreateImageDocument attaches an image to a ticket or an epic. The file
// must be the format named (png, jpeg, gif or webp), by its content, and at
// most 8 MB; its metadata is removed and a thumbnail made before the
// transaction, which then checks the name and writes the document and its
// file together. The document's content is empty and it has no search text:
// images are found by name only.
func (s *Store) CreateImageDocument(req models.CreateImageRequest) (*models.Document, error) {
	format := strings.TrimSpace(req.Format)
	if !models.IsImageFormat(format) {
		return nil, invalidInput("%s", msgFormat(format, true))
	}
	owner := DocumentOwner{TicketID: req.TicketID, EpicID: req.EpicID}
	if err := owner.check(); err != nil {
		return nil, err
	}
	img, err := prepareImage(format, req.Data)
	if err != nil {
		return nil, err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	if err := checkOwnerExists(tx, owner); err != nil {
		return nil, err
	}
	name, err := checkDocumentName(tx, owner, req.Name, "")
	if err != nil {
		return nil, err
	}
	now := time.Now()
	id := newID()
	if _, err := tx.Exec(
		`INSERT INTO documents (id, ticket_id, epic_id, name, format, revision, size, width, height, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 1, ?, ?, ?, ?, ?)`,
		id, nullable(owner.TicketID), nullable(owner.EpicID), name, format,
		len(img.Data), img.Width, img.Height, now, now,
	); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(
		`INSERT INTO document_images (document_id, thumbnail_type, thumbnail, data) VALUES (?, ?, ?, ?)`,
		id, img.ThumbnailType, img.Thumbnail, img.Data,
	); err != nil {
		return nil, fmt.Errorf("saving image: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing document: %w", err)
	}
	return s.GetDocument(id)
}

// ReplaceDocumentImage replaces an image's file with a new one of the same
// format: a content save, so the revision goes up and the thumbnail is made
// anew, while the name, and every reference by it, stays. It returns
// (nil, nil) for an unknown id, and refuses a document that is not an image.
func (s *Store) ReplaceDocumentImage(id string, data []byte) (*models.Document, error) {
	// A document's format never changes, so reading it ahead of the
	// transaction is safe.
	var format string
	err := s.db.QueryRow("SELECT format FROM documents WHERE id = ?", id).Scan(&format)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !models.IsImageFormat(format) {
		return nil, invalidInput(msgDocNotImage)
	}
	img, err := prepareImage(format, data)
	if err != nil {
		return nil, err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.Exec(`UPDATE documents SET size = ?, width = ?, height = ?, revision = revision + 1, updated_at = ?
		WHERE id = ?`, len(img.Data), img.Width, img.Height, time.Now(), id)
	if err != nil {
		return nil, err
	}
	if n, err := res.RowsAffected(); err != nil {
		return nil, err
	} else if n == 0 {
		return nil, nil // deleted meanwhile
	}
	if _, err := tx.Exec(`UPDATE document_images SET thumbnail_type = ?, thumbnail = ?, data = ? WHERE document_id = ?`,
		img.ThumbnailType, img.Thumbnail, img.Data, id); err != nil {
		return nil, fmt.Errorf("saving image: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing document: %w", err)
	}
	return s.GetDocument(id)
}

// ImageFile is an image's file or thumbnail, with the document it belongs to.
type ImageFile struct {
	Document    models.DocumentMeta
	ContentType string
	Data        []byte
}

// GetDocumentImage returns an image's file, as stored (metadata removed). It
// returns (nil, nil) for an unknown id, and refuses a document that is not
// an image.
func (s *Store) GetDocumentImage(id string) (*ImageFile, error) {
	return s.getImageFile(id, false)
}

// GetDocumentThumbnail is GetDocumentImage for the image's thumbnail.
func (s *Store) GetDocumentThumbnail(id string) (*ImageFile, error) {
	return s.getImageFile(id, true)
}

func (s *Store) getImageFile(id string, thumbnail bool) (*ImageFile, error) {
	// One statement, so the details and the bytes are of the same revision.
	// A text document has no document_images row, hence the LEFT JOIN.
	column := "data"
	if thumbnail {
		column = "thumbnail"
	}
	var contentType sql.NullString
	var data []byte
	meta, err := scanDocumentMeta(s.db.QueryRow("SELECT "+documentMetaColumns+", thumbnail_type, "+column+`
		FROM documents LEFT JOIN document_images ON document_id = id WHERE id = ?`, id), &contentType, &data)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading image: %w", err)
	}
	if !models.IsImageFormat(meta.Format) {
		return nil, invalidInput(msgDocNotImage)
	}
	if !contentType.Valid {
		return nil, fmt.Errorf("image %s has no file", id)
	}
	f := &ImageFile{Document: meta, ContentType: models.ImageMediaType(meta.Format), Data: data}
	if thumbnail {
		f.ContentType = contentType.String
	}
	return f, nil
}
