package mcp

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tcarac/taskboard/internal/db"
	"github.com/tcarac/taskboard/internal/models"
	"github.com/tcarac/taskboard/internal/weburl"
)

const documentIDDescription = "Document ID, or its name (with or without its .md or .html extension) together with ticket, or with epic"
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
				"without content (name, format, size, updated time), each with a link that opens it in the web UI.",
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
			Description: "Read one document attached to a ticket or an epic, with its full content. get_ticket and " +
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
				"Names hold letters, digits, spaces, _ and - only, with no extension, and are unique per ticket or " +
				"epic ignoring case. Content is at most 8 MB. Returns the document with a url that opens it in the web UI.",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: documentOwnerProps("Ticket ID or display key (e.g. BILL-2), case-insensitive", map[string]schemaProp{
					"name":    {Type: "string", Description: "Document name: letters, digits, spaces, _ and - only; no extension"},
					"content": {Type: "string", Description: "The whole document, as markdown or HTML"},
					"format": {Type: "string", Description: "markdown (default) or html. HTML is shown in a sandboxed frame " +
						"that may run scripts and load from the internet; people cannot edit it in the web UI.",
						Enum: []string{models.DocumentFormatMarkdown, models.DocumentFormatHTML}},
				}),
				Required: []string{"name", "content"},
			},
		},
		{
			Name: "update_document",
			Description: "Rename a document and/or replace its content. content replaces the whole document; there " +
				"are no partial edits. The format never changes.",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: documentOwnerProps(documentTicketDescription, map[string]schemaProp{
					"id":      {Type: "string", Description: documentIDDescription},
					"name":    {Type: "string", Description: "New name: letters, digits, spaces, _ and - only; no extension"},
					"content": {Type: "string", Description: "The whole new content"},
				}),
				Required: []string{"id"},
			},
		},
		{
			Name:        "delete_document",
			Description: "Delete a document for good. It cannot be restored.",
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
		json.Unmarshal(args, &a)
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
		json.Unmarshal(args, &a)
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
		return s.withDocumentURL(d), true, nil

	case "create_document":
		var a struct {
			documentOwnerArgs
			Name    string `json:"name"`
			Format  string `json:"format"`
			Content string `json:"content"`
		}
		json.Unmarshal(args, &a)
		owner, err := s.requireDocumentOwner(a.documentOwnerArgs)
		if err != nil {
			return nil, true, err
		}
		d, err := s.store.CreateDocument(models.CreateDocumentRequest{
			TicketID: owner.TicketID, EpicID: owner.EpicID, Name: a.Name, Format: a.Format, Content: a.Content,
		})
		if err != nil {
			return nil, true, err
		}
		return s.withDocumentURL(d), true, nil

	case "update_document":
		var a struct {
			ID string `json:"id"`
			documentOwnerArgs
			Name    *string `json:"name"`
			Content *string `json:"content"`
		}
		json.Unmarshal(args, &a)
		if a.Name == nil && a.Content == nil {
			return nil, true, fmt.Errorf("nothing to update: provide a name and/or content")
		}
		id, err := s.resolveDocumentRefOrError(a.ID, a.documentOwnerArgs)
		if err != nil {
			return nil, true, err
		}
		d, err := s.store.UpdateDocument(id, models.UpdateDocumentRequest{Name: a.Name, Content: a.Content})
		if err != nil {
			return nil, true, err
		}
		if d == nil {
			return nil, true, fmt.Errorf("document not found")
		}
		return s.withDocumentURL(d), true, nil

	case "delete_document":
		var a struct {
			ID string `json:"id"`
			documentOwnerArgs
		}
		json.Unmarshal(args, &a)
		id, err := s.resolveDocumentRefOrError(a.ID, a.documentOwnerArgs)
		if err != nil {
			return nil, true, err
		}
		deleted, err := s.store.DeleteDocument(id)
		if err != nil {
			return nil, true, err
		}
		if !deleted {
			return nil, true, fmt.Errorf("document not found")
		}
		return map[string]bool{"deleted": true}, true, nil
	}
	return nil, false, nil
}

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
