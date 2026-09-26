package mcp

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/tcarac/taskboard/internal/db"
	"github.com/tcarac/taskboard/internal/imagedoc"
	"github.com/tcarac/taskboard/internal/models"
	"github.com/tcarac/taskboard/internal/weburl"
)

const documentIDDescription = "Document ID, or its name (with or without its extension: .md, .html, .png, .jpg, .gif or .webp) together with ticket, or with epic"
const documentDataDescription = "An image file's bytes in base64 (a data: URL prefix is fine): PNG, JPEG, GIF or WebP, at most 8 MB. " +
	"The content must really be that format; SVG is refused. Location, camera and other metadata are removed on upload " +
	"without re-encoding the picture. For a file on this machine, pass path instead: data puts the whole file " +
	"through your context."
const documentTicketDescription = "Ticket ID or display key (e.g. BILL-2), case-insensitive; the owner to look a name up in"
const documentEpicDescription = "Epic ID, or its name together with project; instead of ticket"
const documentProjectDescription = "Project ID or prefix (case-insensitive); required when epic is a name"

// documentOwnerProps are the owner arguments every document tool takes:
// ticket, or epic (with project when it is a name).
func documentOwnerProps(ticketDescription string, props map[string]schemaProp) map[string]schemaProp {
	props["ticket"] = schemaProp{Type: "string", Description: ticketDescription}
	props["epic"] = schemaProp{Type: "string", Description: documentEpicDescription}
	props["project"] = schemaProp{Type: "string", Description: documentProjectDescription}
	return props
}

func (s *MCPServer) documentToolDefinitions() []toolDef {
	return []toolDef{
		{
			Name: "list_documents",
			Description: "List the documents attached to a ticket or an epic, in the order they were added, " +
				"without content (name, format, size, updated time; width and height for images), each with a link that opens it in the web UI.",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: documentOwnerProps(
					"Ticket ID or display key (e.g. BILL-2), case-insensitive",
					map[string]schemaProp{},
				),
			},
		},
		{
			Name: "get_document",
			Description: "Read one document attached to a ticket or an epic, with its full content. For an image " +
				"(png, jpeg, gif, webp) it returns its details (name, format, size, width, height, link, downloadUrl) and the picture " +
				"itself as image content, so you can look at screenshots and mocks; an image over 1568 px on its long side or " +
				"over 3.5 MB comes as a scaled-down copy fitting 1568 px, which the details' preview field describes. get_ticket and " +
				"list_documents list documents (name, format, size, updated time, link) without content.",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: documentOwnerProps(documentTicketDescription, map[string]schemaProp{
					"id": {Type: "string", Description: documentIDDescription},
				}),
				Required: []string{"id"},
			},
		},
		{
			Name: "create_document",
			Description: "Attach a markdown or HTML document to a ticket or an epic (pass ticket, or epic and project), " +
				"for longer write-ups (plans, research notes, findings, reports) that would clutter the description. " +
				"Or attach an image (a screenshot, a mock) by passing data, its file in base64, instead of content, " +
				"with format png, jpeg, gif or webp, or a name ending in that extension (\"Login screen.png\"). " +
				"For a file you have locally, pass path, its absolute path, instead of content or data: this MCP server's " +
				"own process reads it, so the file never passes through your context; prefer it to content and data " +
				"whenever the file is on this machine. Pass only one of content, data or path. " +
				"Names hold letters, digits, spaces, _ and - only, with no extension, and are unique per ticket or " +
				"epic ignoring case. Content is at most 8 MB." +
				shortAnswerHelp(documentHolds, "created: true", documentWhole) +
				" The url opens the document in the web UI.",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: documentOwnerProps("Ticket ID or display key (e.g. BILL-2), case-insensitive", map[string]schemaProp{
					"name": {Type: "string", Description: "Document name: letters, digits, spaces, _ and - only; no extension, " +
						"except that an image's name may end in its format's extension (.png, .jpg, .jpeg, .gif, .webp)"},
					"content": {Type: "string", Description: "The whole document, as markdown or HTML; for a file on this machine, pass path instead"},
					"data":    {Type: "string", Description: documentDataDescription + " Pass it instead of content."},
					"path": {Type: "string", Description: documentPathDescription + " Pass it instead of content or data; " +
						"format, when given, overrides the extension."},
					"format": {Type: "string", Description: "markdown (default) or html for content; png, jpeg, gif or webp for data; " +
						"for path, taken from its extension unless given. " +
						"HTML is shown in a sandboxed frame that may run scripts and load from the internet; people cannot edit it in the web UI.",
						Enum: []string{models.DocumentFormatMarkdown, models.DocumentFormatHTML,
							models.DocumentFormatPNG, models.DocumentFormatJPEG, models.DocumentFormatGIF, models.DocumentFormatWebP}},
					"full": fullProp(documentWhole),
				}),
				Required: []string{"name"},
			},
		},
		{
			Name: "update_document",
			Description: "Rename a document and/or replace its content. content replaces the whole document; there " +
				"are no partial edits. An image's picture is replaced with data instead, a new file of the same format. " +
				"For a file you have locally, pass path, its absolute path, instead of content or data: this MCP server's " +
				"own process reads it, so the file never passes through your context; prefer it to content and data " +
				"whenever the file is on this machine. " +
				"The format never changes. Renaming an image also rewrites every reference to it by name in its owner's " +
				"text (the ticket's description and the ticket's or epic's markdown and HTML documents), in the same " +
				"form, together with the rename: each rewritten document gets a new revision. The result's " +
				"referencesUpdated lists the places rewritten, and referencesLeft any place where a reference could not " +
				"be rewritten and now shows a missing image; both are in the short confirmation too." +
				shortAnswerHelp(documentHolds, "changed: the names of the fields the call changed ([] when it changed nothing; "+
					"data whenever an image's picture is replaced)", documentWhole),
			InputSchema: jsonSchema{
				Type: "object",
				Properties: documentOwnerProps(documentTicketDescription, map[string]schemaProp{
					"id":      {Type: "string", Description: documentIDDescription},
					"name":    {Type: "string", Description: "New name: letters, digits, spaces, _ and - only; no extension"},
					"content": {Type: "string", Description: "The whole new content; for a file on this machine, pass path instead"},
					"data":    {Type: "string", Description: documentDataDescription + " Replaces an image's picture; it must be the image's format."},
					"path": {Type: "string", Description: documentPathDescription + " Replaces a text document's content or an " +
						"image's picture, instead of content or data; its extension must be the document's format, when it names one."},
					"full": fullProp(documentWhole),
				}),
				Required: []string{"id"},
			},
		},
		{
			Name: "delete_document",
			Description: "Delete a document for good. It cannot be restored. Deleting an image that its owner's " +
				"text still uses (the ticket's description, or the ticket's or epic's markdown and HTML documents) is " +
				"not refused: those places show a missing image afterwards, and the result's usedIn lists them. " +
				"Decide before deleting whether that is what you want.",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: documentOwnerProps(documentTicketDescription, map[string]schemaProp{
					"id": {Type: "string", Description: documentIDDescription},
				}),
				Required: []string{"id"},
			},
		},
	}
}

// callDocumentTool handles the document tools. ok is false for any other
// name, so callTool can report the tool as unknown.
func (s *MCPServer) callDocumentTool(name string, args json.RawMessage) (result any, ok bool, err error) {
	switch name {
	case "list_documents":
		var a documentOwnerArgs
		if err := decodeArgs(args, &a); err != nil {
			return nil, true, err
		}
		owner, err := s.requireDocumentOwner(a)
		if err != nil {
			return nil, true, err
		}
		docs, err := s.store.ListDocuments(owner)
		if err != nil {
			return nil, true, err
		}
		for i := range docs {
			docs[i].URL = s.documentURL(&docs[i])
		}
		return docs, true, nil

	case "get_document":
		var a struct {
			ID string `json:"id"`
			documentOwnerArgs
		}
		if err := decodeArgs(args, &a); err != nil {
			return nil, true, err
		}
		id, err := s.resolveDocumentRefOrError(a.ID, a.documentOwnerArgs)
		if err != nil {
			return nil, true, err
		}
		d, err := s.store.GetDocument(id)
		if err != nil {
			return nil, true, err
		}
		if d == nil {
			return nil, true, fmt.Errorf("document not found")
		}
		if models.IsImageFormat(d.Format) {
			result, err := s.imageResult(d)
			return result, true, err
		}
		return s.withDocumentURL(d), true, nil

	case "create_document":
		var a struct {
			documentOwnerArgs
			Name    string  `json:"name"`
			Format  string  `json:"format"`
			Content string  `json:"content"`
			Data    *string `json:"data"`
			Path    *string `json:"path"`
			fullArg
		}
		if err := decodeArgs(args, &a); err != nil {
			return nil, true, err
		}
		if a.Data != nil && a.Content != "" {
			return nil, true, fmt.Errorf("pass content for a text document or data for an image, not both")
		}
		if a.Path != nil && (a.Content != "" || a.Data != nil) {
			return nil, true, errOnePathContentOrData
		}
		owner, err := s.requireDocumentOwner(a.documentOwnerArgs)
		if err != nil {
			return nil, true, err
		}
		if a.Path != nil {
			name, format, err := imageNameAndFormat(a.Name, a.Format)
			if err != nil {
				return nil, true, err
			}
			format = documentPathFormat(*a.Path, format)
			file, err := readDocumentPath(*a.Path)
			if err != nil {
				return nil, true, err
			}
			var d *models.Document
			if models.IsImageFormat(format) || format == "svg" {
				d, err = s.store.CreateImageDocument(models.CreateImageRequest{
					TicketID: owner.TicketID, EpicID: owner.EpicID, Name: name, Format: format, Data: file,
				})
			} else {
				content, textErr := documentPathText(*a.Path, file)
				if textErr != nil {
					return nil, true, textErr
				}
				d, err = s.store.CreateDocument(models.CreateDocumentRequest{
					TicketID: owner.TicketID, EpicID: owner.EpicID, Name: name, Format: format, Content: content,
				})
			}
			if err != nil {
				return nil, true, err
			}
			return s.documentResult(d, createdChange, a.Full), true, nil
		}
		if a.Data != nil {
			data, err := decodeImageData(*a.Data)
			if err != nil {
				return nil, true, err
			}
			name, format, err := imageNameAndFormat(a.Name, a.Format)
			if err != nil {
				return nil, true, err
			}
			d, err := s.store.CreateImageDocument(models.CreateImageRequest{
				TicketID: owner.TicketID, EpicID: owner.EpicID, Name: name, Format: format, Data: data,
			})
			if err != nil {
				return nil, true, err
			}
			return s.documentResult(d, createdChange, a.Full), true, nil
		}
		d, err := s.store.CreateDocument(models.CreateDocumentRequest{
			TicketID: owner.TicketID, EpicID: owner.EpicID, Name: a.Name, Format: a.Format, Content: a.Content,
		})
		if err != nil {
			return nil, true, err
		}
		return s.documentResult(d, createdChange, a.Full), true, nil

	case "update_document":
		var a struct {
			ID string `json:"id"`
			documentOwnerArgs
			Name    *string `json:"name"`
			Content *string `json:"content"`
			Data    *string `json:"data"`
			Path    *string `json:"path"`
			fullArg
		}
		if err := decodeArgs(args, &a); err != nil {
			return nil, true, err
		}
		if a.Name == nil && a.Content == nil && a.Data == nil && a.Path == nil {
			return nil, true, fmt.Errorf("nothing to update: provide a name and/or content, data or path")
		}
		if a.Data != nil && a.Content != nil {
			return nil, true, fmt.Errorf("pass content for a text document or data for an image, not both")
		}
		if a.Path != nil && (a.Content != nil || a.Data != nil) {
			return nil, true, errOnePathContentOrData
		}
		id, err := s.resolveDocumentRefOrError(a.ID, a.documentOwnerArgs)
		if err != nil {
			return nil, true, err
		}
		before, err := s.store.GetDocument(id)
		if err != nil {
			return nil, true, err
		}
		if before == nil {
			return nil, true, fmt.Errorf("document not found")
		}
		var picture []byte
		replacePicture := false
		if a.Path != nil {
			file, err := readDocumentPath(*a.Path)
			if err != nil {
				return nil, true, err
			}
			if err := checkPathFitsDocument(*a.Path, before); err != nil {
				return nil, true, err
			}
			if models.IsImageFormat(before.Format) {
				picture, replacePicture = file, true
			} else {
				content, err := documentPathText(*a.Path, file)
				if err != nil {
					return nil, true, err
				}
				a.Content = &content
			}
		}
		if a.Data != nil {
			picture, err = decodeImageData(*a.Data)
			if err != nil {
				return nil, true, err
			}
			replacePicture = true
		}
		if replacePicture {
			// The picture and the name together: a refusal of either
			// leaves both as they were.
			d, err := s.store.ReplaceDocumentImage(id, picture, a.Name)
			if err != nil {
				return nil, true, err
			}
			if d == nil {
				return nil, true, fmt.Errorf("document not found")
			}
			// A new picture is a change even when it is the same file.
			c := changedFields(before, d, documentFields...)
			*c.Changed = append(*c.Changed, "data")
			return s.documentResult(d, c, a.Full), true, nil
		}
		d, err := s.store.UpdateDocument(id, models.UpdateDocumentRequest{Name: a.Name, Content: a.Content})
		if err != nil {
			return nil, true, err
		}
		if d == nil {
			return nil, true, fmt.Errorf("document not found")
		}
		return s.documentResult(d, changedFields(before, d, documentFields...), a.Full), true, nil

	case "delete_document":
		var a struct {
			ID string `json:"id"`
			documentOwnerArgs
		}
		if err := decodeArgs(args, &a); err != nil {
			return nil, true, err
		}
		id, err := s.resolveDocumentRefOrError(a.ID, a.documentOwnerArgs)
		if err != nil {
			return nil, true, err
		}
		usedIn, err := s.store.DeleteDocumentReportingUse(id)
		if err != nil {
			return nil, true, err
		}
		result := map[string]any{"deleted": true}
		if len(usedIn) > 0 {
			result["usedIn"] = usedIn
		}
		return result, true, nil
	}
	return nil, false, nil
}

// errOnePathContentOrData refuses a call that passes path together with
// content or data: the document's content comes from one of them.
var errOnePathContentOrData = fmt.Errorf("pass one of content, data or path, not several")

// documentOwnerArgs are the arguments that name a document's owner: ticket
// (id or display key), or epic (id, or name together with project).
type documentOwnerArgs struct {
	Ticket  string `json:"ticket"`
	Epic    string `json:"epic"`
	Project string `json:"project"`
}

// resolveDocumentOwner resolves the owner arguments. Naming both a ticket and
// an epic is refused rather than resolved to either; a zero owner means
// neither was given.
func (s *MCPServer) resolveDocumentOwner(a documentOwnerArgs) (db.DocumentOwner, error) {
	ticket, epic := strings.TrimSpace(a.Ticket), strings.TrimSpace(a.Epic)
	switch {
	case ticket != "" && epic != "":
		return db.DocumentOwner{}, fmt.Errorf("pass ticket or epic, not both")
	case ticket != "":
		id, err := s.store.ResolveTicketID(ticket)
		return db.DocumentOwner{TicketID: id}, err
	case epic != "":
		id, err := s.resolveEpicRefOrError(epic, a.Project)
		return db.DocumentOwner{EpicID: id}, err
	}
	return db.DocumentOwner{}, nil
}

// requireDocumentOwner is resolveDocumentOwner for the tools that need an
// owner: create_document and list_documents.
func (s *MCPServer) requireDocumentOwner(a documentOwnerArgs) (db.DocumentOwner, error) {
	owner, err := s.resolveDocumentOwner(a)
	if err != nil {
		return db.DocumentOwner{}, err
	}
	if owner == (db.DocumentOwner{}) {
		return db.DocumentOwner{}, fmt.Errorf("ticket or epic is required")
	}
	return owner, nil
}

// resolveDocumentRefOrError resolves a document argument: an id on its own,
// or a name together with the ticket or epic it belongs to, since a name is
// only unique within its owner.
func (s *MCPServer) resolveDocumentRefOrError(ref string, a documentOwnerArgs) (string, error) {
	if strings.TrimSpace(ref) == "" {
		return "", fmt.Errorf("id is required")
	}
	owner, err := s.resolveDocumentOwner(a)
	if err != nil {
		return "", err
	}
	if owner != (db.DocumentOwner{}) {
		return s.store.ResolveDocumentRef(owner, ref)
	}
	d, err := s.store.GetDocument(ref)
	if err != nil {
		return "", err
	}
	if d == nil {
		return "", fmt.Errorf("no document matches %q; pass ticket, or epic and project, when addressing by name", ref)
	}
	return d.ID, nil
}

// documentURL is the link that opens a document in the web UI: on its
// ticket, or on its epic's modal in the Epics view. It is "" when the owner
// cannot be read.
func (s *MCPServer) documentURL(d *models.DocumentMeta) string {
	display := models.DocumentDisplayName(d.Name, d.Format)
	if d.EpicID != "" {
		e, err := s.store.GetEpic(d.EpicID)
		if err != nil || e == nil {
			return ""
		}
		p, err := s.store.GetProject(e.ProjectID)
		if err != nil || p == nil {
			return ""
		}
		return weburl.EpicDocument(weburl.Base(), p.Prefix, e.Name, display)
	}
	t, err := s.store.GetTicket(d.TicketID)
	if err != nil || t == nil {
		return ""
	}
	return weburl.TicketDocument(weburl.Base(), weburl.Ref(*t), display)
}

// withDocumentURL fills in the link that opens the document in the web UI.
func (s *MCPServer) withDocumentURL(d *models.Document) *models.Document {
	d.URL = s.documentURL(&d.DocumentMeta)
	return d
}

// imageDetails is the text part of get_document's answer for an image.
type imageDetails struct {
	models.DocumentMeta
	// DownloadURL fetches the file at full size from the board.
	DownloadURL string `json:"downloadUrl"`
	// Preview is set when the picture sent is a scaled-down copy.
	Preview *imagePreview `json:"preview,omitempty"`
}

type imagePreview struct {
	Note     string `json:"note"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	Size     int    `json:"size"`
	MimeType string `json:"mimeType"`
}

// imageResult is get_document's answer for an image: its details as JSON
// text, with the links that open and download it, then the picture as image
// content. A large image (imagedoc.NeedsPreview) is sent as a scaled-down
// copy, which the details say, since model APIs and MCP clients refuse
// pictures of several megabytes; the stored file is never changed.
func (s *MCPServer) imageResult(d *models.Document) (contentResult, error) {
	f, err := s.store.GetDocumentImage(d.ID)
	if err != nil {
		return nil, err
	}
	if f == nil {
		return nil, fmt.Errorf("document not found")
	}
	details := imageDetails{DocumentMeta: f.Document, DownloadURL: weburl.DocumentDownload(weburl.Base(), f.Document.ID)}
	details.URL = s.documentURL(&details.DocumentMeta)
	picture, mimeType := f.Data, f.ContentType
	if imagedoc.NeedsPreview(f.Document.Width, f.Document.Height, f.Document.Size) {
		p, err := imagedoc.MakePreview(f.Document.Format, f.Data)
		if err != nil {
			return nil, fmt.Errorf("making a preview: %w", err)
		}
		picture, mimeType = p.Data, p.ContentType
		details.Preview = &imagePreview{
			Note: fmt.Sprintf("The picture below is a scaled-down copy (%d×%d %s, %s) of this %d×%d, %s image; "+
				"the stored file is unchanged. Download it at full size from downloadUrl.",
				p.Width, p.Height, models.ImageFormatName(strings.TrimPrefix(p.ContentType, "image/")), models.FormatSize(len(p.Data)),
				f.Document.Width, f.Document.Height, models.FormatSize(f.Document.Size)),
			Width: p.Width, Height: p.Height, Size: len(p.Data), MimeType: p.ContentType,
		}
	}
	text, err := json.Marshal(details)
	if err != nil {
		return nil, err
	}
	return contentResult{
		textContent{Type: "text", Text: string(text)},
		imageContent{Type: "image", Data: base64.StdEncoding.EncodeToString(picture), MimeType: mimeType},
	}, nil
}

// decodeImageData reads the data argument: base64, standard or unpadded,
// with or without a data: URL prefix and line breaks.
func decodeImageData(raw string) ([]byte, error) {
	s := strings.TrimSpace(raw)
	if strings.HasPrefix(s, "data:") {
		if _, rest, ok := strings.Cut(s, ","); ok {
			s = rest
		}
	}
	s = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
	data, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		data, err = base64.RawStdEncoding.DecodeString(s)
	}
	if err != nil || len(data) == 0 {
		return nil, fmt.Errorf("data must be the image file's bytes in base64")
	}
	return data, nil
}

// imageNameAndFormat lets an image's name carry its extension, as agents
// naturally write it ("Login screen.png"): a known image extension is taken
// off the name and sets the format, and must agree with format when both are
// given. "jpg" is taken for jpeg, and a .svg name is an SVG.
func imageNameAndFormat(name, format string) (string, string, error) {
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "jpg" {
		format = models.DocumentFormatJPEG
	}
	trimmed := strings.TrimSpace(name)
	ext := filepath.Ext(trimmed)
	if f, ok := db.DocumentFormatFromFilename(trimmed); ok && models.IsImageFormat(f) {
		if format != "" && format != f {
			return "", "", fmt.Errorf("the name ends in %s but format is %q; pass one or make them agree", ext, format)
		}
		return strings.TrimSuffix(trimmed, ext), f, nil
	}
	if format == "" && strings.EqualFold(ext, ".svg") {
		return name, "svg", nil // refused in the store's words
	}
	return name, format, nil
}
