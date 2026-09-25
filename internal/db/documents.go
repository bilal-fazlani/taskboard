package db

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/tcarac/taskboard/internal/doctext"
	"github.com/tcarac/taskboard/internal/models"
)

// The texts of the document rules. They are the whole error message, so the
// HTTP API, the MCP tools and the CLI all report exactly the same words.
const (
	msgDocNameRequired    = "Enter a name"
	msgDocNameChars       = "Use letters, digits, spaces, _ and - only."
	msgDocNameTooLong     = "Keep the name to 200 characters or fewer."
	msgDocRefRequired     = "Enter a document name or id."
	msgDocNothingToUpdate = "nothing to update: provide a name and/or content"
	msgDocFormat          = `Format must be "markdown" or "html".`
	msgDocImageFormat     = `Format must be "png", "jpeg", "gif" or "webp" for an image.`
	msgDocExtension       = "Only .md, .html, .htm, .png, .jpg, .jpeg, .gif and .webp files can be attached."
	msgDocOwner           = "A document belongs to one ticket or one epic."
	msgDocSVG             = "SVG images can't be attached. Use PNG, JPEG, GIF or WebP."
	msgDocImageFromFile   = "An image is made from its file, not from text."
	msgDocImageText       = "An image's content is replaced with a new image file, not text."
	msgDocNotImage        = "This document isn't an image."
)

const maxDocumentNameRunes = 200

// textFormats are the formats CreateDocument takes; images go through
// CreateImageDocument.
var textFormats = []string{models.DocumentFormatMarkdown, models.DocumentFormatHTML}

// formatByExtension maps a filename's extension, lower-cased, to a format.
var formatByExtension = map[string]string{
	".md":   models.DocumentFormatMarkdown,
	".html": models.DocumentFormatHTML,
	".htm":  models.DocumentFormatHTML,
	".png":  models.DocumentFormatPNG,
	".jpg":  models.DocumentFormatJPEG,
	".jpeg": models.DocumentFormatJPEG,
	".gif":  models.DocumentFormatGIF,
	".webp": models.DocumentFormatWebP,
}

// isSVGFilename reports a .svg file, which is refused with its own message.
func isSVGFilename(filename string) bool {
	return strings.EqualFold(filepath.Ext(filename), ".svg")
}

// isDocumentNameRune reports whether r may appear in a document name:
// letters in any script (with their combining marks), decimal digits, space,
// underscore and hyphen. Nothing that could pass for an extension or a path.
func isDocumentNameRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsMark(r) || unicode.IsDigit(r) || r == ' ' || r == '_' || r == '-'
}

// validateDocumentName applies the rules a name must meet on its own, and
// returns it trimmed, which is the form that is stored.
func validateDocumentName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", invalidInput(msgDocNameRequired)
	}
	if utf8.RuneCountInString(name) > maxDocumentNameRunes {
		return "", invalidInput(msgDocNameTooLong)
	}
	for _, r := range name {
		if !isDocumentNameRune(r) {
			return "", invalidInput(msgDocNameChars)
		}
	}
	return name, nil
}

// DocumentFormatFromFilename returns the format a file's extension names,
// ignoring case, and false for any other extension.
func DocumentFormatFromFilename(filename string) (string, bool) {
	format, ok := formatByExtension[strings.ToLower(filepath.Ext(filename))]
	return format, ok
}

// DocumentNameFromFilename turns a file's name into a document name and
// format: the extension picks the format and is removed, every character the
// name rules refuse becomes a space, and the result is trimmed and checked.
// The CLI calls it for `doc add --file` without --name.
func DocumentNameFromFilename(filename string) (name, format string, err error) {
	base := filepath.Base(filename)
	ext := filepath.Ext(base)
	format, ok := DocumentFormatFromFilename(base)
	if !ok {
		if isSVGFilename(base) {
			return "", "", invalidInput(msgDocSVG)
		}
		return "", "", invalidInput(msgDocExtension)
	}
	cleaned := strings.Map(func(r rune) rune {
		if isDocumentNameRune(r) {
			return r
		}
		return ' '
	}, strings.TrimSuffix(base, ext))
	name, err = validateDocumentName(cleaned)
	if err != nil {
		return "", "", err
	}
	return name, format, nil
}

func msgDocNameTaken(owner DocumentOwner, existing models.DocumentMeta) string {
	return fmt.Sprintf(`This %s already has a document called "%s".`,
		owner.noun(), models.DocumentDisplayName(existing.Name, existing.Format))
}

func msgDocTooLarge(size int) string {
	return fmt.Sprintf("This document is %s. The limit is 8 MB.", models.FormatSize(size))
}

// ImageTooLargeMessage is the refusal for an image file of size bytes, over
// the limit. The HTTP API uses it for an upload too large to read at all.
func ImageTooLargeMessage(size int) string {
	return fmt.Sprintf("This image is %s. The limit is 8 MB.", models.FormatSize(size))
}

// ErrDocumentConflict refuses a content save made from a revision the
// document has moved past. Current is the document as it is now, so the
// caller can show it.
type ErrDocumentConflict struct {
	Current *models.Document
}

func (e *ErrDocumentConflict) Error() string {
	return "This document changed since you started editing."
}

// DocumentOwner names what a document belongs to: exactly one of a ticket or
// an epic, by id. Callers resolve display keys and epic names first
// (ResolveTicketID, ResolveEpicRef).
type DocumentOwner struct {
	TicketID string
	EpicID   string
}

// check refuses an owner that names both a ticket and an epic, or neither.
func (o DocumentOwner) check() error {
	if (o.TicketID == "") == (o.EpicID == "") {
		return invalidInput(msgDocOwner)
	}
	return nil
}

// noun is how the owner is named in messages.
func (o DocumentOwner) noun() string {
	if o.EpicID != "" {
		return "epic"
	}
	return "ticket"
}

// where is the owner's condition on the documents table and its argument.
func (o DocumentOwner) where() (string, any) {
	if o.EpicID != "" {
		return "epic_id = ?", o.EpicID
	}
	return "ticket_id = ?", o.TicketID
}

// checkOwnerExists turns an unknown owner into an ErrInvalidInput rather than
// a foreign key failure or an empty list, and refuses an owner that is not
// exactly one ticket or one epic.
func checkOwnerExists(q dbtx, owner DocumentOwner) error {
	if err := owner.check(); err != nil {
		return err
	}
	table, id := "tickets", owner.TicketID
	if owner.EpicID != "" {
		table, id = "epics", owner.EpicID
	}
	var one int
	err := q.QueryRow("SELECT 1 FROM "+table+" WHERE id = ?", id).Scan(&one)
	if err == sql.ErrNoRows {
		return invalidInput("%s not found", owner.noun())
	}
	return err
}

// nullable stores "" as NULL, which the one-owner CHECK and the foreign keys
// need.
func nullable(id string) any {
	if id == "" {
		return nil
	}
	return id
}

// documentMetaColumns reads everything but the content. Size is measured in
// bytes, which is what the limit counts: an image's is kept on its row, a
// text document's is its content's length. Width and height are 0 for text.
const documentMetaColumns = `id, COALESCE(ticket_id, ''), COALESCE(epic_id, ''), name, format,
	COALESCE(size, LENGTH(CAST(content AS BLOB))), COALESCE(width, 0), COALESCE(height, 0),
	revision, created_at, updated_at`

func scanDocumentMeta(row interface{ Scan(...any) error }, extra ...any) (models.DocumentMeta, error) {
	var d models.DocumentMeta
	dest := append([]any{&d.ID, &d.TicketID, &d.EpicID, &d.Name, &d.Format, &d.Size, &d.Width, &d.Height,
		&d.Revision, &d.CreatedAt, &d.UpdatedAt}, extra...)
	err := row.Scan(dest...)
	return d, err
}

// loadOwnerDocuments reads an owner's documents in the order they were
// added. The result is never nil.
func loadOwnerDocuments(q dbtx, owner DocumentOwner) ([]models.DocumentMeta, error) {
	cond, arg := owner.where()
	rows, err := q.Query("SELECT "+documentMetaColumns+" FROM documents WHERE "+cond+" ORDER BY created_at, id", arg)
	if err != nil {
		return nil, fmt.Errorf("loading documents: %w", err)
	}
	defer rows.Close()
	docs := []models.DocumentMeta{}
	for rows.Next() {
		d, err := scanDocumentMeta(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning document: %w", err)
		}
		docs = append(docs, d)
	}
	return docs, rows.Err()
}

// checkDocumentName applies the name rules and the owner's uniqueness, and
// returns the name trimmed. exceptID is the document being renamed, so it may
// keep its own name in different capitals; "" on create. Names are folded in
// Go with strings.EqualFold because SQLite's NOCASE folds ASCII only.
func checkDocumentName(q dbtx, owner DocumentOwner, raw, exceptID string) (string, error) {
	name, err := validateDocumentName(raw)
	if err != nil {
		return "", err
	}
	docs, err := loadOwnerDocuments(q, owner)
	if err != nil {
		return "", err
	}
	for _, d := range docs {
		if d.ID != exceptID && strings.EqualFold(d.Name, name) {
			return "", invalidInput("%s", msgDocNameTaken(owner, d))
		}
	}
	return name, nil
}

// ListDocuments returns an owner's documents without their content, in the
// order they were added. An unknown owner is an ErrInvalidInput.
func (s *Store) ListDocuments(owner DocumentOwner) ([]models.DocumentMeta, error) {
	if err := checkOwnerExists(s.db, owner); err != nil {
		return nil, err
	}
	return loadOwnerDocuments(s.db, owner)
}

// GetDocument returns (nil, nil) for an unknown id, like GetTicket.
func (s *Store) GetDocument(id string) (*models.Document, error) {
	var content string
	meta, err := scanDocumentMeta(
		s.db.QueryRow("SELECT "+documentMetaColumns+", content FROM documents WHERE id = ?", id),
		&content,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &models.Document{DocumentMeta: meta, Content: content}, nil
}

// ResolveDocumentRef finds one of an owner's documents by id, or by name
// ignoring case, with or without its format's extension ("Plan", "plan.md";
// a JPEG answers to .jpeg as well as .jpg). A name with the wrong extension
// names nothing.
func (s *Store) ResolveDocumentRef(owner DocumentOwner, ref string) (string, error) {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" {
		return "", invalidInput(msgDocRefRequired)
	}
	docs, err := s.ListDocuments(owner)
	if err != nil {
		return "", err
	}
	for _, d := range docs {
		if d.ID == trimmed {
			return d.ID, nil
		}
	}
	for _, d := range docs {
		if strings.EqualFold(d.Name, trimmed) || strings.EqualFold(models.DocumentDisplayName(d.Name, d.Format), trimmed) ||
			(d.Format == models.DocumentFormatJPEG && strings.EqualFold(d.Name+".jpeg", trimmed)) {
			return d.ID, nil
		}
	}
	return "", invalidInput(`This %s has no document called "%s".`, owner.noun(), trimmed)
}

// CreateDocument checks the name and inserts in one transaction, so two
// writers cannot both pass the duplicate check. The readable text the search
// matches is worked out first, outside the transaction, and stored with it.
func (s *Store) CreateDocument(req models.CreateDocumentRequest) (*models.Document, error) {
	format := req.Format
	if format == "" {
		format = models.DocumentFormatMarkdown
	}
	if models.IsImageFormat(format) {
		return nil, invalidInput(msgDocImageFromFile)
	}
	if !slices.Contains(textFormats, format) {
		return nil, invalidInput("%s", msgFormat(format, false))
	}
	if len(req.Content) > models.MaxDocumentBytes {
		return nil, invalidInput("%s", msgDocTooLarge(len(req.Content)))
	}
	owner := DocumentOwner{TicketID: req.TicketID, EpicID: req.EpicID}
	text := doctext.ReadableText(format, req.Content)

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
		`INSERT INTO documents (id, ticket_id, epic_id, name, format, content, revision, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		id, nullable(owner.TicketID), nullable(owner.EpicID), name, format, req.Content, now, now,
	); err != nil {
		return nil, err
	}
	if err := saveDocumentText(tx, id, 1, text); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing document: %w", err)
	}
	return s.GetDocument(id)
}

// UpdateDocument renames a document and/or replaces its content. It returns
// (nil, nil) for an unknown id. A content save bumps the revision; a rename
// alone does not. Either bumps updated_at. A content save that names an
// ExpectedRevision the document has moved past is refused with
// ErrDocumentConflict and changes nothing, the rename included. A content
// save also replaces the readable text the search matches, worked out before
// the transaction; a rename leaves it as it is.
func (s *Store) UpdateDocument(id string, req models.UpdateDocumentRequest) (*models.Document, error) {
	if req.Name == nil && req.Content == nil {
		return nil, invalidInput(msgDocNothingToUpdate)
	}
	if req.Content != nil && len(*req.Content) > models.MaxDocumentBytes {
		return nil, invalidInput("%s", msgDocTooLarge(len(*req.Content)))
	}
	var text string
	if req.Content != nil {
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
		if models.IsImageFormat(format) {
			return nil, invalidInput(msgDocImageText)
		}
		text = doctext.ReadableText(format, *req.Content)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	var owner DocumentOwner
	var name string
	var revision int
	err = tx.QueryRow("SELECT COALESCE(ticket_id, ''), COALESCE(epic_id, ''), name, revision FROM documents WHERE id = ?", id).
		Scan(&owner.TicketID, &owner.EpicID, &name, &revision)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// The transaction is BEGIN IMMEDIATE, so no other save can land between
	// this check and the update below.
	if req.Content != nil && req.ExpectedRevision != nil && *req.ExpectedRevision != revision {
		// Release the write lock before reading on another connection; the
		// deferred rollback is then a no-op.
		tx.Rollback()
		current, err := s.GetDocument(id)
		if err != nil {
			return nil, err
		}
		if current == nil {
			return nil, nil
		}
		return nil, &ErrDocumentConflict{Current: current}
	}
	if req.Name != nil {
		if name, err = checkDocumentName(tx, owner, *req.Name, id); err != nil {
			return nil, err
		}
	}

	set := "name = ?, updated_at = ?"
	args := []any{name, time.Now()}
	if req.Content != nil {
		set += ", content = ?, revision = revision + 1"
		args = append(args, *req.Content)
	}
	args = append(args, id)
	if _, err := tx.Exec("UPDATE documents SET "+set+" WHERE id = ?", args...); err != nil {
		return nil, err
	}
	if req.Content != nil {
		if err := saveDocumentText(tx, id, revision+1, text); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing document: %w", err)
	}
	return s.GetDocument(id)
}

// DeleteDocument removes a document for good. It reports false, with no
// error, for an unknown id.
func (s *Store) DeleteDocument(id string) (bool, error) {
	res, err := s.db.Exec("DELETE FROM documents WHERE id = ?", id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// attachDocumentCounts fills DocumentCount for a page of tickets in one
// grouped query. index, placeholders and ids are attachListDetails' own.
func (s *Store) attachDocumentCounts(tickets []models.Ticket, index map[string]int, placeholders string, ids []any) error {
	rows, err := s.db.Query(`SELECT ticket_id, COUNT(*) FROM documents
		WHERE ticket_id IN (`+placeholders+`) GROUP BY ticket_id`, ids...)
	if err != nil {
		return fmt.Errorf("counting documents: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var ticketID string
		var n int
		if err := rows.Scan(&ticketID, &n); err != nil {
			return err
		}
		if i, ok := index[ticketID]; ok {
			tickets[i].DocumentCount = n
		}
	}
	return rows.Err()
}
