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
	Repo        string     `json:"repo,omitempty"`
	DueDate     *time.Time `json:"dueDate,omitempty"`
	Position    float64    `json:"position"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`

	// Populated fields (not stored directly)
	ProjectPrefix string      `json:"projectPrefix,omitempty"`
	Labels        []Label     `json:"labels,omitempty"`
	Subtasks      []Subtask   `json:"subtasks,omitempty"`
	DependsOn     []TicketRef `json:"dependsOn,omitempty"`
	Blocks        []TicketRef `json:"blocks,omitempty"`
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
	Repo        string   `json:"repo,omitempty"`
	DueDate     *string  `json:"dueDate,omitempty"`
	Labels      []string `json:"labels,omitempty"`
	DependsOn   []string `json:"dependsOn,omitempty"`
}

type UpdateTicketRequest struct {
	Title       *string  `json:"title,omitempty"`
	Description *string  `json:"description,omitempty"`
	Status      *string  `json:"status,omitempty"`
	Priority    *string  `json:"priority,omitempty"`
	Repo        *string  `json:"repo,omitempty"`
	DueDate     *string  `json:"dueDate,omitempty"`
	Position    *float64 `json:"position,omitempty"`
	Labels      []string `json:"labels,omitempty"`
	DependsOn   []string `json:"dependsOn,omitempty"`
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
}
