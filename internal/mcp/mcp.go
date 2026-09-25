package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/tcarac/taskboard/internal/db"
	"github.com/tcarac/taskboard/internal/models"
	"github.com/tcarac/taskboard/internal/weburl"
)

type MCPServer struct {
	store *db.Store
}

func NewServer(store *db.Store) *MCPServer {
	return &MCPServer{store: store}
}

type jsonrpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonrpcResponse struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id,omitempty"`
	Result  any       `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type toolDef struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	InputSchema jsonSchema `json:"inputSchema"`
}

type jsonSchema struct {
	Type       string                `json:"type"`
	Properties map[string]schemaProp `json:"properties,omitempty"`
	Required   []string              `json:"required,omitempty"`
}

type schemaProp struct {
	Type        string      `json:"type"`
	Description string      `json:"description"`
	Enum        []string    `json:"enum,omitempty"`
	Items       *jsonSchema `json:"items,omitempty"`
}

type textContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// imageContent is a picture in a tool result, which a client shows to the
// model as an image.
type imageContent struct {
	Type     string `json:"type"`
	Data     string `json:"data"`
	MimeType string `json:"mimeType"`
}

// contentResult is a tool result given as MCP content items, sent as they
// are, rather than a value sent as JSON text. get_document uses it to return
// an image's details and the picture itself.
type contentResult []any

func (s *MCPServer) Run() error {
	reader := bufio.NewReader(os.Stdin)
	writer := os.Stdout

	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("reading stdin: %w", err)
		}

		var req jsonrpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			continue
		}

		resp := s.handleRequest(req)
		if resp == nil {
			continue
		}

		data, err := json.Marshal(resp)
		if err != nil {
			continue
		}
		data = append(data, '\n')
		writer.Write(data)
	}
}

func (s *MCPServer) handleRequest(req jsonrpcRequest) *jsonrpcResponse {
	switch req.Method {
	case "initialize":
		return &jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities": map[string]any{
					"tools": map[string]any{},
				},
				"serverInfo": map[string]any{
					"name":    "taskboard",
					"version": "0.6.0",
				},
			},
		}

	case "notifications/initialized":
		return nil

	case "tools/list":
		return &jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"tools": s.toolDefinitions(),
			},
		}

	case "tools/call":
		return s.handleToolCall(req)

	default:
		return &jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &rpcError{Code: -32601, Message: "method not found: " + req.Method},
		}
	}
}

func (s *MCPServer) handleToolCall(req jsonrpcRequest) *jsonrpcResponse {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return &jsonrpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32602, Message: "invalid params"}}
	}

	result, err := s.callTool(params.Name, params.Arguments)
	if err != nil {
		return &jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"content": []textContent{{Type: "text", Text: fmt.Sprintf("Error: %s", err.Error())}},
				"isError": true,
			},
		}
	}

	if content, ok := result.(contentResult); ok {
		return &jsonrpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"content": content}}
	}
	data, _ := json.MarshalIndent(result, "", "  ")
	return &jsonrpcResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"content": []textContent{{Type: "text", Text: string(data)}},
		},
	}
}

func (s *MCPServer) callTool(name string, args json.RawMessage) (any, error) {
	switch name {
	case "list_projects":
		var a struct {
			Status string `json:"status"`
		}
		json.Unmarshal(args, &a)
		return s.store.ListProjects(a.Status)

	case "get_project":
		var a struct {
			ID string `json:"id"`
		}
		json.Unmarshal(args, &a)
		projectID, err := s.resolveProjectRefOrError(a.ID)
		if err != nil {
			return nil, err
		}
		p, err := s.store.GetProject(projectID)
		if p == nil && err == nil {
			return nil, fmt.Errorf("project not found")
		}
		return p, err

	case "create_project":
		var a models.CreateProjectRequest
		json.Unmarshal(args, &a)
		return s.store.CreateProject(a)

	case "update_project":
		var a struct {
			ID string `json:"id"`
			models.UpdateProjectRequest
		}
		json.Unmarshal(args, &a)
		projectID, err := s.resolveProjectRefOrError(a.ID)
		if err != nil {
			return nil, err
		}
		return s.store.UpdateProject(projectID, a.UpdateProjectRequest)

	case "delete_project":
		var a struct {
			ID string `json:"id"`
		}
		json.Unmarshal(args, &a)
		projectID, err := s.resolveProjectRefOrError(a.ID)
		if err != nil {
			return nil, err
		}
		return map[string]bool{"deleted": true}, s.store.DeleteProject(projectID)

	case "list_tickets":
		var a models.TicketFilter
		json.Unmarshal(args, &a)
		tickets, err := s.store.ListTickets(a)
		if err != nil {
			return nil, err
		}
		return weburl.FillAll(tickets), nil

	case "list_epics":
		var a struct {
			ProjectID string `json:"projectId"`
		}
		json.Unmarshal(args, &a)
		if strings.TrimSpace(a.ProjectID) == "" {
			return nil, fmt.Errorf("projectId is required")
		}
		projectID, err := s.store.ResolveProjectRef(a.ProjectID)
		if err != nil {
			return nil, err
		}
		epics, err := s.store.ListEpics(projectID)
		if err != nil {
			return nil, err
		}
		noEpic, err := s.store.NoEpicProgress(projectID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"epics": epics, "noEpic": noEpic}, nil

	case "create_epic":
		var a models.CreateEpicRequest
		json.Unmarshal(args, &a)
		return s.store.CreateEpic(a)

	case "update_epic":
		var a struct {
			ID      string `json:"id"`
			Project string `json:"project"`
			models.UpdateEpicRequest
		}
		json.Unmarshal(args, &a)
		if a.Name == nil && a.Description == nil {
			return nil, fmt.Errorf("nothing to update: provide name and/or description")
		}
		epicID, err := s.resolveEpicRefOrError(a.ID, a.Project)
		if err != nil {
			return nil, err
		}
		e, err := s.store.UpdateEpic(epicID, a.UpdateEpicRequest)
		if err != nil {
			return nil, err
		}
		if e == nil {
			return nil, fmt.Errorf("epic not found")
		}
		return e, nil

	case "delete_epic":
		var a struct {
			ID      string `json:"id"`
			Project string `json:"project"`
		}
		json.Unmarshal(args, &a)
		epicID, err := s.resolveEpicRefOrError(a.ID, a.Project)
		if err != nil {
			return nil, err
		}
		count, err := s.store.DeleteEpic(epicID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"deleted": true, "clearedFromTickets": count}, nil

	case "list_labels":
		return s.store.ListLabels()

	case "create_label":
		var a models.CreateLabelRequest
		json.Unmarshal(args, &a)
		if strings.TrimSpace(a.Name) == "" {
			return nil, fmt.Errorf("name is required")
		}
		if strings.TrimSpace(a.Color) == "" {
			a.Color = db.DefaultLabelColor
		}
		return s.store.CreateLabel(a)

	case "update_label":
		var a struct {
			ID string `json:"id"`
			models.UpdateLabelRequest
		}
		json.Unmarshal(args, &a)
		if a.Name == nil && a.Color == nil {
			return nil, fmt.Errorf("nothing to update: provide name and/or color")
		}
		labelID, err := s.resolveLabelRefOrError(a.ID)
		if err != nil {
			return nil, err
		}
		l, err := s.store.UpdateLabel(labelID, a.UpdateLabelRequest)
		if err != nil {
			return nil, err
		}
		if l == nil {
			return nil, fmt.Errorf("label not found")
		}
		return l, nil

	case "delete_label":
		var a struct {
			ID string `json:"id"`
		}
		json.Unmarshal(args, &a)
		labelID, err := s.resolveLabelRefOrError(a.ID)
		if err != nil {
			return nil, err
		}
		count, err := s.store.DeleteLabel(labelID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"deleted": true, "detachedFromTickets": count}, nil

	case "get_ticket":
		var a struct {
			ID string `json:"id"`
		}
		json.Unmarshal(args, &a)
		ticketID, err := s.resolveTicketRefOrError(a.ID)
		if err != nil {
			return nil, err
		}
		t, err := s.store.GetTicket(ticketID)
		if err != nil {
			return nil, err
		}
		if t == nil {
			return nil, fmt.Errorf("ticket not found")
		}
		if t.History, err = s.store.ListStatusChanges(ticketID); err != nil {
			return nil, err
		}
		return weburl.Fill(t), nil

	case "create_ticket":
		var a models.CreateTicketRequest
		json.Unmarshal(args, &a)
		t, err := s.store.CreateTicket(a)
		if err != nil {
			return nil, err
		}
		return weburl.Fill(t), nil

	case "update_ticket":
		var a struct {
			ID string `json:"id"`
			models.UpdateTicketRequest
		}
		json.Unmarshal(args, &a)
		ticketID, err := s.resolveTicketRefOrError(a.ID)
		if err != nil {
			return nil, err
		}
		// Until agents have identities (ACP-4), "agent" means "came through MCP"; the rule should then key off the actor.
		t, err := s.store.UpdateTicket(ticketID, a.UpdateTicketRequest, db.RequireNoteLeavingReview())
		if err != nil {
			return nil, err
		}
		return weburl.Fill(t), nil

	case "move_ticket":
		var a struct {
			ID string `json:"id"`
			models.MoveTicketRequest
		}
		json.Unmarshal(args, &a)
		ticketID, err := s.resolveTicketRefOrError(a.ID)
		if err != nil {
			return nil, err
		}
		// Until agents have identities (ACP-4), "agent" means "came through MCP"; the rule should then key off the actor.
		return s.store.MoveTicket(ticketID, a.MoveTicketRequest, db.RequireNoteLeavingReview())

	case "delete_ticket":
		var a struct {
			ID string `json:"id"`
		}
		json.Unmarshal(args, &a)
		ticketID, err := s.resolveTicketRefOrError(a.ID)
		if err != nil {
			return nil, err
		}
		return map[string]bool{"deleted": true}, s.store.DeleteTicket(ticketID)

	case "get_board":
		var a struct {
			ProjectID string `json:"projectId"`
		}
		json.Unmarshal(args, &a)
		return s.store.GetBoard(a.ProjectID)

	case "create_subtask":
		var a struct {
			TicketID string `json:"ticketId"`
			Title    string `json:"title"`
		}
		json.Unmarshal(args, &a)
		if a.TicketID == "" || a.Title == "" {
			return nil, fmt.Errorf("ticketId and title are required")
		}
		ticketID, err := s.resolveTicketRefOrError(a.TicketID)
		if err != nil {
			return nil, err
		}
		return s.store.AddSubtask(ticketID, models.CreateSubtaskRequest{Title: a.Title})

	case "batch_create_subtasks":
		var a struct {
			TicketID string `json:"ticketId"`
			Subtasks []struct {
				Title string `json:"title"`
			} `json:"subtasks"`
		}
		json.Unmarshal(args, &a)
		if a.TicketID == "" || len(a.Subtasks) == 0 {
			return nil, fmt.Errorf("ticketId and at least one subtask are required")
		}
		ticketID, err := s.resolveTicketRefOrError(a.TicketID)
		if err != nil {
			return nil, err
		}
		var created []models.Subtask
		for _, sub := range a.Subtasks {
			st, err := s.store.AddSubtask(ticketID, models.CreateSubtaskRequest{Title: sub.Title})
			if err != nil {
				return nil, fmt.Errorf("creating subtask %q: %w", sub.Title, err)
			}
			created = append(created, *st)
		}
		return created, nil

	case "delete_subtask":
		var a struct {
			ID string `json:"id"`
		}
		json.Unmarshal(args, &a)
		return map[string]bool{"deleted": true}, s.store.DeleteSubtask(a.ID)

	case "toggle_subtask":
		var a struct {
			ID string `json:"id"`
		}
		json.Unmarshal(args, &a)
		return s.store.ToggleSubtask(a.ID)

	default:
		if result, ok, err := s.callDocumentTool(name, args); ok {
			return result, err
		}
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}

// resolveTicketRefOrError resolves a ticket id-or-display-key argument the
// way get_ticket, update_ticket, move_ticket, delete_ticket, create_subtask
// and batch_create_subtasks accept it: a ULID is a literal ticket id,
// anything else is a case-insensitive PREFIX-NUMBER display key like BILL-2.
// An empty argument is rejected up front with a clear message instead of
// reaching the store as an empty id.
func (s *MCPServer) resolveTicketRefOrError(ref string) (string, error) {
	if strings.TrimSpace(ref) == "" {
		return "", fmt.Errorf("id is required")
	}
	return s.store.ResolveTicketID(ref)
}

// resolveProjectRefOrError resolves a project id-or-prefix argument the way
// get_project, update_project and delete_project accept it, turning an empty
// argument into a clear error up front instead of reaching the store as an
// empty id.
func (s *MCPServer) resolveProjectRefOrError(ref string) (string, error) {
	if strings.TrimSpace(ref) == "" {
		return "", fmt.Errorf("id is required")
	}
	return s.store.ResolveProjectRef(ref)
}

// resolveLabelRefOrError resolves a label id-or-exact-name argument the way
// update_label and delete_label accept it, turning an empty argument or an
// unresolvable reference into a clear error instead of silently operating on
// no label (store.UpdateLabel/DeleteLabel on "" would match nothing or, for
// delete, harmlessly no-op).
func (s *MCPServer) resolveLabelRefOrError(ref string) (string, error) {
	if strings.TrimSpace(ref) == "" {
		return "", fmt.Errorf("id is required")
	}
	labelID, err := s.store.ResolveLabelRef(ref)
	if err != nil {
		return "", err
	}
	if labelID == "" {
		return "", fmt.Errorf("no label matches %q", ref)
	}
	return labelID, nil
}

// resolveEpicRefOrError resolves an epic argument the way update_epic and
// delete_epic accept it: an epic id on its own, or a name together with its
// project (id or prefix, case-insensitive), since an epic name is only
// unique within its project. When project is given, resolution goes through
// ResolveEpicRef, which also gives a clear error for an id that belongs to a
// different project. When project is omitted, ref must be a literal epic id;
// GetEpic checks it exists up front, since DeleteEpic/UpdateEpic otherwise
// report an unknown id as a silent no-op rather than an error.
func (s *MCPServer) resolveEpicRefOrError(ref, projectRef string) (string, error) {
	if strings.TrimSpace(ref) == "" {
		return "", fmt.Errorf("id is required")
	}
	if strings.TrimSpace(projectRef) != "" {
		return s.store.ResolveEpicRef(projectRef, ref)
	}
	e, err := s.store.GetEpic(ref)
	if err != nil {
		return "", err
	}
	if e == nil {
		return "", fmt.Errorf("no epic matches %q; pass project when addressing by name", ref)
	}
	return e.ID, nil
}

// noteParamDescription documents the note move_ticket and update_ticket take.
const noteParamDescription = "Why the status is changing, saved with the change in the ticket's status history. " +
	"Required when moving a ticket out of agent_review: say why it is leaving review, either that it was approved and landed, " +
	"or the review findings it is being sent back to fix. Optional for every other change; ignored when the status does not change."

func (s *MCPServer) toolDefinitions() []toolDef {
	defs := []toolDef{
		// --- Projects (top-level grouping) ---
		{
			Name:        "list_projects",
			Description: "List all projects with optional status filter. Projects are the top-level grouping — use them like epics or initiatives to organize related work.",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]schemaProp{
					"status": {Type: "string", Description: "Filter by status", Enum: []string{"active", "archived"}},
				},
			},
		},
		{
			Name:        "get_project",
			Description: "Get detailed project information by ID",
			InputSchema: jsonSchema{
				Type:       "object",
				Properties: map[string]schemaProp{"id": {Type: "string", Description: "Project ID or prefix (case-insensitive)"}},
				Required:   []string{"id"},
			},
		},
		{
			Name: "create_project",
			Description: "Create a new project. Projects are the top-level organizational unit (like epics/initiatives). " +
				"Create a new project for each distinct body of work instead of creating umbrella tickets. " +
				"Hierarchy: Project → Ticket → Subtask. Never create 'epic' or 'umbrella' tickets — use a project for that. " +
				"Use the description field to capture the project's goals, scope, and any high-level context.",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]schemaProp{
					"name":        {Type: "string", Description: "Project name"},
					"prefix":      {Type: "string", Description: "Short prefix for ticket keys (e.g. AUTH)"},
					"description": {Type: "string", Description: "Project description — goals, scope, context, and any high-level details about this body of work"},
					"icon":        {Type: "string", Description: "Emoji icon"},
					"color":       {Type: "string", Description: "Hex color code"},
				},
				Required: []string{"name", "prefix"},
			},
		},
		{
			Name:        "update_project",
			Description: "Update project properties",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]schemaProp{
					"id":          {Type: "string", Description: "Project ID or prefix (case-insensitive)"},
					"name":        {Type: "string", Description: "Project name"},
					"prefix":      {Type: "string", Description: "Short prefix"},
					"description": {Type: "string", Description: "Project description — goals, scope, context"},
					"icon":        {Type: "string", Description: "Emoji icon"},
					"color":       {Type: "string", Description: "Hex color"},
					"status":      {Type: "string", Description: "Status", Enum: []string{"active", "archived"}},
				},
				Required: []string{"id"},
			},
		},
		{
			Name:        "delete_project",
			Description: "Delete a project and all its tickets",
			InputSchema: jsonSchema{
				Type:       "object",
				Properties: map[string]schemaProp{"id": {Type: "string", Description: "Project ID or prefix (case-insensitive)"}},
				Required:   []string{"id"},
			},
		},
		// --- Epics (project-scoped grouping of tickets) ---
		{
			Name: "list_epics",
			Description: "List a project's epics, each with progress: counts per status, total, complete (true once it has " +
				"at least one ticket and all are done), and lastActivityAt (latest ticket update, null when empty). Also " +
				"returns a noEpic summary with the same counts for the project's tickets that have no epic. An epic is a " +
				"project-scoped grouping — each ticket belongs to at most one epic, optionally.",
			InputSchema: jsonSchema{
				Type:       "object",
				Properties: map[string]schemaProp{"projectId": {Type: "string", Description: "Project ID or prefix (case-insensitive)"}},
				Required:   []string{"projectId"},
			},
		},
		{
			Name: "create_epic",
			Description: "Create an epic within a project. An epic is an optional, project-scoped grouping for tickets — " +
				"a ticket belongs to at most one epic. Epic names must be unique within their project, case-insensitively; " +
				"\"none\" is reserved, since it means \"without an epic\" everywhere an epic is filtered or cleared.",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]schemaProp{
					"projectId":   {Type: "string", Description: "Project ID or prefix (case-insensitive)"},
					"name":        {Type: "string", Description: "Epic name, unique within the project (case-insensitive); \"none\" is reserved"},
					"description": {Type: "string", Description: "Epic description"},
				},
				Required: []string{"projectId", "name"},
			},
		},
		{
			Name:        "update_epic",
			Description: "Update an epic's name and/or description.",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]schemaProp{
					"id":          {Type: "string", Description: "Epic ID, or its name together with project"},
					"project":     {Type: "string", Description: "Project ID or prefix (case-insensitive); required when id is a name rather than an epic id"},
					"name":        {Type: "string", Description: "New name, unique within the project (case-insensitive); \"none\" is reserved"},
					"description": {Type: "string", Description: "New description"},
				},
				Required: []string{"id"},
			},
		},
		{
			Name: "delete_epic",
			Description: "Delete an epic. Its tickets are not deleted; they simply lose the epic. The response's " +
				"clearedFromTickets field reports how many tickets that was. Its own documents are deleted for good " +
				"with it and cannot be restored; its tickets' documents stay.",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]schemaProp{
					"id":      {Type: "string", Description: "Epic ID, or its name together with project"},
					"project": {Type: "string", Description: "Project ID or prefix (case-insensitive); required when id is a name rather than an epic id"},
				},
				Required: []string{"id"},
			},
		},
		// --- Labels (global, informational tags on tickets) ---
		{
			Name:        "list_labels",
			Description: "List all labels with the number of tickets carrying each. Labels are global across projects.",
			InputSchema: jsonSchema{Type: "object", Properties: map[string]schemaProp{}},
		},
		{
			Name: "create_label",
			Description: "Create a label with a name and color, for control over color that create_ticket's implicit " +
				"label creation doesn't give. Fails if a label with the same name (case-insensitive) already exists.",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]schemaProp{
					"name":  {Type: "string", Description: "Label name"},
					"color": {Type: "string", Description: "Hex color code. Defaults to the standard gray when omitted."},
				},
				Required: []string{"name"},
			},
		},
		{
			Name: "update_label",
			Description: "Update a label's name and/or color; at least one of name or color must be given. Renaming " +
				"keeps the label attached to every ticket that carries it. A name that is blank after trimming is " +
				"rejected; a blank color is treated as not given and leaves the existing color unchanged. The id field " +
				"accepts either the label's id or its exact name, matched case-insensitively.",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]schemaProp{
					"id":    {Type: "string", Description: "Label id, or its exact name (case-insensitive)"},
					"name":  {Type: "string", Description: "New name; a blank name is rejected"},
					"color": {Type: "string", Description: "New hex color code; a blank value leaves the color unchanged"},
				},
				Required: []string{"id"},
			},
		},
		{
			Name: "delete_label",
			Description: "Delete a label, detaching it from every ticket that carries it. The response's " +
				"detachedFromTickets field reports how many tickets it was removed from, so an accidental delete of a " +
				"busy label is obvious. The id field accepts either the label's id or its exact name, matched " +
				"case-insensitively.",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]schemaProp{
					"id": {Type: "string", Description: "Label id, or its exact name (case-insensitive)"},
				},
				Required: []string{"id"},
			},
		},
		// --- Tickets (tasks within a project) ---
		{
			Name:        "list_tickets",
			Description: "List tickets with optional filters by project, status, priority, repo, label, and epic",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]schemaProp{
					"projectId": {Type: "string", Description: "Filter by project ID or prefix (case-insensitive); an unknown one returns no tickets rather than an error"},
					"status":    {Type: "string", Description: "Filter by status", Enum: models.Statuses},
					"priority":  {Type: "string", Description: "Filter by priority", Enum: []string{"urgent", "high", "medium", "low"}},
					"repo":      {Type: "string", Description: "Filter to tickets attached to this repo, matched exactly"},
					"label":     {Type: "string", Description: "Filter by label name, case-insensitive"},
					"epic":      {Type: "string", Description: "Filter by epic name (case-insensitive) or id, or \"none\" for tickets without an epic. Without projectId, a name matches that epic in every project, and \"none\" spans every project's tickets without an epic too — pass projectId to scope the filter to one project."},
				},
			},
		},
		{
			Name: "get_ticket",
			Description: "Get detailed ticket information including subtasks, labels, the epic it belongs to (if any), the tickets it depends on, the tickets it blocks, " +
				"its documents (name, format, size, updated time and a link; read one with get_document), " +
				"and its status history (newest first, each change with its note; the first entry, with an empty fromStatus, is its creation). " +
				"reviewRounds counts how many times it has entered agent_review.",
			InputSchema: jsonSchema{
				Type:       "object",
				Properties: map[string]schemaProp{"id": {Type: "string", Description: "Ticket ID or display key (e.g. BILL-2), case-insensitive"}},
				Required:   []string{"id"},
			},
		},
		{
			Name: "create_ticket",
			Description: "Create a ticket (task) within a project. Tickets are concrete, actionable units of work. " +
				"Do NOT create 'umbrella' tickets — an epic (see create_epic) is the grouping inside a project; use " +
				"create_project instead for a distinct, larger body of work. " +
				"Use create_subtask or batch_create_subtasks to break tickets into steps. " +
				"Hierarchy: Project → Epic (optional) → Ticket → Subtask.",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]schemaProp{
					"projectId":   {Type: "string", Description: "Project ID or prefix (case-insensitive)"},
					"title":       {Type: "string", Description: "Ticket title"},
					"description": {Type: "string", Description: "Rich text description"},
					"status":      {Type: "string", Description: "Initial status", Enum: models.Statuses},
					"priority":    {Type: "string", Description: "Priority level", Enum: []string{"urgent", "high", "medium", "low"}},
					"repos": {
						Type:        "array",
						Description: "Free-form repository identifiers, for example acme/billing-api. Matched exactly, so case matters.",
						Items:       &jsonSchema{Type: "string"},
					},
					"dueDate": {Type: "string", Description: "Due date (YYYY-MM-DD)"},
					"epic":    {Type: "string", Description: "Epic name (case-insensitive) or id, within this project, to put the ticket in. One optional epic per ticket. Omit, or pass \"\" or \"none\" (any case), for no epic."},
					"labels": {
						Type:        "array",
						Description: "Label names. Matched case-insensitively; unknown names are created automatically.",
						Items:       &jsonSchema{Type: "string"},
					},
					"dependsOn": {
						Type:        "array",
						Description: "Ticket IDs or display keys like BILL-2 that this ticket depends on. Informational only: dependencies never block a status change.",
						Items:       &jsonSchema{Type: "string"},
					},
				},
				Required: []string{"projectId", "title"},
			},
		},
		{
			Name:        "update_ticket",
			Description: "Update ticket properties. Changing the status out of agent_review requires a note.",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]schemaProp{
					"id":          {Type: "string", Description: "Ticket ID or display key (e.g. BILL-2), case-insensitive"},
					"title":       {Type: "string", Description: "Ticket title"},
					"description": {Type: "string", Description: "Description"},
					"status":      {Type: "string", Description: "Status", Enum: models.Statuses},
					"note":        {Type: "string", Description: noteParamDescription},
					"priority":    {Type: "string", Description: "Priority", Enum: []string{"urgent", "high", "medium", "low"}},
					"repos": {
						Type:        "array",
						Description: "Free-form repository identifiers, for example acme/billing-api. Matched exactly, so case matters.",
						Items:       &jsonSchema{Type: "string"},
					},
					"dueDate": {Type: "string", Description: "Due date (YYYY-MM-DD). Omit the field or pass null to leave the current due date unchanged; pass an empty string to clear it."},
					"epic":    {Type: "string", Description: "Epic name (case-insensitive) or id, within the ticket's project. Omit or pass null to leave the ticket's epic unchanged; pass \"\" or \"none\" (any case) to remove it from its epic."},
					"labels": {
						Type:        "array",
						Description: "Label names. Matched case-insensitively; unknown names are created automatically.",
						Items:       &jsonSchema{Type: "string"},
					},
					"dependsOn": {
						Type:        "array",
						Description: "Ticket IDs or display keys like BILL-2 that this ticket depends on. Informational only: dependencies never block a status change.",
						Items:       &jsonSchema{Type: "string"},
					},
				},
				Required: []string{"id"},
			},
		},
		{
			Name:        "move_ticket",
			Description: "Move ticket to a different status column. Moving it out of agent_review requires a note.",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]schemaProp{
					"id":     {Type: "string", Description: "Ticket ID or display key (e.g. BILL-2), case-insensitive"},
					"status": {Type: "string", Description: "Target status", Enum: models.Statuses},
					"note":   {Type: "string", Description: noteParamDescription},
				},
				Required: []string{"id", "status"},
			},
		},
		{
			Name:        "delete_ticket",
			Description: "Delete a ticket",
			InputSchema: jsonSchema{
				Type:       "object",
				Properties: map[string]schemaProp{"id": {Type: "string", Description: "Ticket ID or display key (e.g. BILL-2), case-insensitive"}},
				Required:   []string{"id"},
			},
		},
		// --- Board ---
		{
			Name:        "get_board",
			Description: fmt.Sprintf("Get full Kanban board grouped by status columns (%s)", strings.Join(models.Statuses, ", ")),
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]schemaProp{
					"projectId": {Type: "string", Description: "Filter by project ID or prefix (optional, case-insensitive); an unknown one returns no tickets rather than an error"},
				},
			},
		},
		// --- Subtasks (steps within a ticket) ---
		{
			Name: "create_subtask",
			Description: "Create a subtask (checklist item) on a ticket. Subtasks break a ticket into verifiable steps. " +
				"Hierarchy: Project → Ticket → Subtask. Use batch_create_subtasks when adding multiple subtasks at once.",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]schemaProp{
					"ticketId": {Type: "string", Description: "Parent ticket ID or display key (e.g. BILL-2), case-insensitive"},
					"title":    {Type: "string", Description: "Subtask description"},
				},
				Required: []string{"ticketId", "title"},
			},
		},
		{
			Name: "batch_create_subtasks",
			Description: "Create multiple subtasks on a ticket at once. More efficient than calling create_subtask repeatedly. " +
				"Use this when breaking a ticket into its implementation steps.",
			InputSchema: jsonSchema{
				Type: "object",
				Properties: map[string]schemaProp{
					"ticketId": {Type: "string", Description: "Parent ticket ID or display key (e.g. BILL-2), case-insensitive"},
					"subtasks": {Type: "array", Description: "Array of subtask objects, each with a 'title' field", Items: &jsonSchema{
						Type: "object",
						Properties: map[string]schemaProp{
							"title": {Type: "string", Description: "Subtask description"},
						},
						Required: []string{"title"},
					}},
				},
				Required: []string{"ticketId", "subtasks"},
			},
		},
		{
			Name:        "toggle_subtask",
			Description: "Toggle subtask completion status",
			InputSchema: jsonSchema{
				Type:       "object",
				Properties: map[string]schemaProp{"id": {Type: "string", Description: "Subtask ID"}},
				Required:   []string{"id"},
			},
		},
		{
			Name:        "delete_subtask",
			Description: "Delete a subtask from a ticket",
			InputSchema: jsonSchema{
				Type:       "object",
				Properties: map[string]schemaProp{"id": {Type: "string", Description: "Subtask ID"}},
				Required:   []string{"id"},
			},
		},
	}
	return append(defs, s.documentToolDefinitions()...)
}
