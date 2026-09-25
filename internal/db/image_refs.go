package db

import (
	"database/sql"

	"github.com/tcarac/taskboard/internal/models"
)

// ReferencedImage returns the image an HTML document refers to by name
// (models.IsImageRef): one of the same owner's images, never another
// owner's, and never a document that is not an image. It returns (nil, nil)
// when fromID is not an HTML document or ref names none of its owner's
// images, so a caller cannot tell those apart.
func (s *Store) ReferencedImage(fromID, ref string) (*ImageFile, error) {
	from, err := scanDocumentMeta(s.db.QueryRow("SELECT "+documentMetaColumns+" FROM documents WHERE id = ?", fromID))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if from.Format != models.DocumentFormatHTML {
		return nil, nil
	}
	docs, err := loadOwnerDocuments(s.db, DocumentOwner{TicketID: from.TicketID, EpicID: from.EpicID})
	if err != nil {
		return nil, err
	}
	for _, d := range docs {
		if models.IsImageRef(d.Name, d.Format, ref) {
			return s.GetDocumentImage(d.ID)
		}
	}
	return nil, nil
}
