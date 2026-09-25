package mcp

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tcarac/taskboard/internal/db"
	"github.com/tcarac/taskboard/internal/models"
	"github.com/tcarac/taskboard/internal/weburl"
)

const documentIDDescription = "Document ID, or its name (with or without its .md or .html extension) together with ticket"
const documentTicketDescription = "Ticket ID or display key (e.g. BILL-2), case-insensitive; required when id is a name"

func (s *MCPServer) documentToolDefinitions() []toolDef {
	return []toolDef{
		{
			Name: "get_document",
			Description: "Read one document attached to a ticket, with its full content. get_ticket lists a ticket's " +
				"documents (name, format, size, updated time, link) without content.",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]schemaProp{
					"id":     {Type: "string", Description: documentIDDescription},
					"ticket": {Type: "string", Description: documentTicketDescription},
				},
				Required: []string{"id"},
			},
		},
		{
			Name: "create_document",
			Description: "Attach a markdown or HTML document to a ticket, for longer write-ups (plans, research notes, " +
				"findings, reports) that would clutter the description. Names hold letters, digits, spaces, _ and - " +
				"only, with no extension, and are unique per ticket ignoring case. Content is at most 8 MB. " +
				"Returns the document with a url that opens it in the web UI.",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]schemaProp{
					"ticket":  {Type: "string", Description: "Ticket ID or display key (e.g. BILL-2), case-insensitive"},
					"name":    {Type: "string", Description: "Document name: letters, digits, spaces, _ and - only; no extension"},
					"content": {Type: "string", Description: "The whole document, as markdown or HTML"},
					"format": {Type: "string", Description: "markdown (default) or html. HTML is shown in a sandboxed frame " +
						"that may run scripts and load from the internet; people cannot edit it in the web UI.",
						Enum: []string{models.DocumentFormatMarkdown, models.DocumentFormatHTML}},
				},
				Required: []string{"ticket", "name", "content"},
			},
		},
		{
			Name: "update_document",
			Description: "Rename a document and/or replace its content. content replaces the whole document; there " +
				"are no partial edits. The format never changes.",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]schemaProp{
					"id":      {Type: "string", Description: documentIDDescription},
					"ticket":  {Type: "string", Description: documentTicketDescription},
					"name":    {Type: "string", Description: "New name: letters, digits, spaces, _ and - only; no extension"},
					"content": {Type: "string", Description: "The whole new content"},
				},
				Required: []string{"id"},
			},
		},
		{
			Name:        "delete_document",
			Description: "Delete a document for good. It cannot be restored.",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]schemaProp{
					"id":     {Type: "string", Description: documentIDDescription},
					"ticket": {Type: "string", Description: documentTicketDescription},
				},
				Required: []string{"id"},
			},
		},
	}
}

// callDocumentTool handles the document tools. ok is false for any other
// name, so callTool can report the tool as unknown.
func (s *MCPServer) callDocumentTool(name string, args json.RawMessage) (result any, ok bool, err error) {
	switch name {
	case "get_document":
		var a struct {
			ID     string `json:"id"`
			Ticket string `json:"ticket"`
		}
		json.Unmarshal(args, &a)
		id, err := s.resolveDocumentRefOrError(a.ID, a.Ticket)
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
			Ticket  string `json:"ticket"`
			Name    string `json:"name"`
			Format  string `json:"format"`
			Content string `json:"content"`
		}
		json.Unmarshal(args, &a)
		if strings.TrimSpace(a.Ticket) == "" {
			return nil, true, fmt.Errorf("ticket is required")
		}
		ticketID, err := s.store.ResolveTicketID(a.Ticket)
		if err != nil {
			return nil, true, err
		}
		d, err := s.store.CreateDocument(models.CreateDocumentRequest{
			TicketID: ticketID, Name: a.Name, Format: a.Format, Content: a.Content,
		})
		if err != nil {
			return nil, true, err
		}
		return s.withDocumentURL(d), true, nil

	case "update_document":
		var a struct {
			ID      string  `json:"id"`
			Ticket  string  `json:"ticket"`
			Name    *string `json:"name"`
			Content *string `json:"content"`
		}
		json.Unmarshal(args, &a)
		if a.Name == nil && a.Content == nil {
			return nil, true, fmt.Errorf("nothing to update: provide a name and/or content")
		}
		id, err := s.resolveDocumentRefOrError(a.ID, a.Ticket)
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
			ID     string `json:"id"`
			Ticket string `json:"ticket"`
		}
		json.Unmarshal(args, &a)
		id, err := s.resolveDocumentRefOrError(a.ID, a.Ticket)
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

// resolveDocumentRefOrError resolves a document argument: an id on its own,
// or a name together with the ticket it belongs to, since a name is only
// unique within its ticket.
func (s *MCPServer) resolveDocumentRefOrError(ref, ticketRef string) (string, error) {
	if strings.TrimSpace(ref) == "" {
		return "", fmt.Errorf("id is required")
	}
	if strings.TrimSpace(ticketRef) != "" {
		ticketID, err := s.store.ResolveTicketID(ticketRef)
		if err != nil {
			return "", err
		}
		return s.store.ResolveDocumentRef(db.DocumentOwner{TicketID: ticketID}, ref)
	}
	d, err := s.store.GetDocument(ref)
	if err != nil {
		return "", err
	}
	if d == nil {
		return "", fmt.Errorf("no document matches %q; pass ticket when addressing by name", ref)
	}
	return d.ID, nil
}

// withDocumentURL fills in the link that opens the document in the web UI.
func (s *MCPServer) withDocumentURL(d *models.Document) *models.Document {
	t, err := s.store.GetTicket(d.TicketID)
	if err == nil && t != nil {
		d.URL = weburl.TicketDocument(weburl.Base(), weburl.Ref(*t), models.DocumentDisplayName(d.Name, d.Format))
	}
	return d
}
