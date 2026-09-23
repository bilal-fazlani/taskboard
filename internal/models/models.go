package models

import "time"

type Project struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Prefix      string    `json:"prefix"`
	Description string    `json:"description,omitempty"`
	Icon        string    `json:"icon,omitempty"`
	Color       string    `json:"color,omitempty"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type Ticket struct {
	ID          string     `json:"id"`
	ProjectID   string     `json:"projectId"`
	Number      int        `json:"number"`
	Title       string     `json:"title"`
	Description string     `json:"description,omitempty"`
	Status      string     `json:"status"`
	Priority    string     `json:"priority"`
	DueDate     *time.Time `json:"dueDate,omitempty"`
	Position    float64    `json:"position"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`

	// Epic is the epic the ticket belongs to, if any. It is always in the
	// ticket's own project.
	Epic *EpicRef `json:"epic,omitempty"`

	// Populated fields (not stored directly)
	ProjectPrefix string      `json:"projectPrefix,omitempty"`
	Repos         []string    `json:"repos,omitempty"`
	Labels        []Label     `json:"labels,omitempty"`
	Subtasks      []Subtask   `json:"subtasks,omitempty"`
	DependsOn     []TicketRef `json:"dependsOn,omitempty"`
	Blocks        []TicketRef `json:"blocks,omitempty"`

	// URL is where the ticket opens in the web UI. The CLI and the MCP server
	// fill it in so an agent can print a link; the HTTP API leaves it empty,
	// since a browser already knows where the board is. See internal/weburl.
	URL string `json:"url,omitempty"`
}

// TicketRef is a lightweight pointer to another ticket, carrying enough
// context for a client to render it without a second fetch.
type TicketRef struct {
	ID     string `json:"id"`
	Key    string `json:"key"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

// DisplayKey returns the human-readable ticket key like "AUTH-1"
func (t Ticket) DisplayKey() string {
	if t.ProjectPrefix != "" {
		return t.ProjectPrefix + "-" + itoa(t.Number)
	}
	return itoa(t.Number)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	s := ""
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	for i > 0 {
		s = string(rune('0'+i%10)) + s
		i /= 10
	}
	if neg {
		s = "-" + s
	}
	return s
}

type Label struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Color       string `json:"color"`
	TicketCount int    `json:"ticketCount"`
}

type Subtask struct {
	ID        string `json:"id"`
	TicketID  string `json:"ticketId"`
	Title     string `json:"title"`
	Completed bool   `json:"completed"`
	Position  int    `json:"position"`
}

// Board represents the kanban board view
type Board struct {
	ProjectID string   `json:"projectId,omitempty"`
	Columns   []Column `json:"columns"`
}

type Column struct {
	Status  string   `json:"status"`
	Tickets []Ticket `json:"tickets"`
}

type CreateProjectRequest struct {
	Name        string `json:"name"`
	Prefix      string `json:"prefix"`
	Description string `json:"description,omitempty"`
	Icon        string `json:"icon,omitempty"`
	Color       string `json:"color,omitempty"`
}

type UpdateProjectRequest struct {
	Name        *string `json:"name,omitempty"`
	Prefix      *string `json:"prefix,omitempty"`
	Description *string `json:"description,omitempty"`
	Icon        *string `json:"icon,omitempty"`
	Color       *string `json:"color,omitempty"`
	Status      *string `json:"status,omitempty"`
}

type CreateTicketRequest struct {
	ProjectID   string   `json:"projectId"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Status      string   `json:"status,omitempty"`
	Priority    string   `json:"priority,omitempty"`
	Repos       []string `json:"repos,omitempty"`
	// DueDate follows the same "omitted vs explicit" contract as
	// UpdateTicketRequest.DueDate below, though on create there is nothing to
	// clear: a nil pointer and a pointer to "" both just mean no due date.
	DueDate *string `json:"dueDate,omitempty"`
	// Epic follows the same contract as UpdateTicketRequest.Epic; on create a
	// nil pointer, a pointer to "" and a pointer to "none" all mean no epic.
	Epic      *string  `json:"epic,omitempty"`
	Labels    []string `json:"labels,omitempty"`
	DependsOn []string `json:"dependsOn,omitempty"`
}

type UpdateTicketRequest struct {
	Title       *string  `json:"title,omitempty"`
	Description *string  `json:"description,omitempty"`
	Status      *string  `json:"status,omitempty"`
	Priority    *string  `json:"priority,omitempty"`
	Repos       []string `json:"repos,omitempty"`
	// DueDate is nil when the caller omitted the field (or sent JSON null),
	// meaning "leave the due date unchanged" — this is what makes it safe for
	// a caller to send only the fields it actually edited, rather than the
	// whole ticket, without ever touching a due date it didn't mean to touch.
	// A non-nil pointer to "" is an explicit clear. A non-nil pointer to
	// anything else must parse as YYYY-MM-DD or the store rejects the whole
	// request with an ErrInvalidInput (HTTP 400) instead of silently dropping
	// it. This applies identically whether the request came from the HTTP
	// API, an MCP tool call, or the CLI.
	DueDate *string `json:"dueDate,omitempty"`
	// Epic has the DueDate contract: nil (omitted or JSON null) leaves the
	// ticket's epic unchanged, and a pointer to "" removes it from its epic.
	// So does "none" (any case, surrounding spaces ignored), the value that
	// means "no epic" everywhere, filters included. A value of only spaces
	// is an ErrInvalidInput, as it is for a due date. Anything else is an
	// epic id or name (case-insensitive) in the ticket's own project; a
	// value that matches no epic there is an ErrInvalidInput. On any error
	// nothing in the request is applied.
	Epic      *string  `json:"epic,omitempty"`
	Position  *float64 `json:"position,omitempty"`
	Labels    []string `json:"labels,omitempty"`
	DependsOn []string `json:"dependsOn,omitempty"`
}

type MoveTicketRequest struct {
	Status   string   `json:"status"`
	Position *float64 `json:"position,omitempty"`
}

type CreateSubtaskRequest struct {
	Title string `json:"title"`
}

type CreateLabelRequest struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

type UpdateLabelRequest struct {
	Name  *string `json:"name,omitempty"`
	Color *string `json:"color,omitempty"`
}

type TicketFilter struct {
	ProjectID string
	Status    string
	Priority  string
	Repo      string
	Label     string
	// Epic is an epic name (case-insensitive) or id, or models.NoEpic
	// ("none", any case) for tickets without an epic. Without a ProjectID a
	// name matches that epic in every project.
	Epic string
}
