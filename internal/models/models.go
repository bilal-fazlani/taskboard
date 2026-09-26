package models

import (
	"strings"
	"time"
)

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
	// AgentInstructions says how agents should work on the project's
	// tickets; Description says what the project is. Reads of one project
	// always set it, to "" when there are none. Lists never read the text and
	// leave it nil, so it is left out of their JSON.
	AgentInstructions *string `json:"agentInstructions,omitempty"`
	// HasAgentInstructions is whether the project has agent instructions.
	// Every read sets it. It is not part of the project's JSON: the HTTP
	// project list adds it for each project, and MCP never shows it.
	HasAgentInstructions bool `json:"-"`
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
	ProjectPrefix string   `json:"projectPrefix,omitempty"`
	Repos         []string `json:"repos,omitempty"`
	// Labels are the ticket's labels, without a ticketCount: computing each
	// label's ticket count here would mean an extra query per label on every
	// ticket in a list (an N+1 cost), and nothing reads it on an embedded
	// label. Use list_labels, or another call that returns a Label on its
	// own, for the real count.
	Labels    []EmbeddedLabel `json:"labels,omitempty"`
	Subtasks  []Subtask       `json:"subtasks,omitempty"`
	DependsOn []TicketRef     `json:"dependsOn,omitempty"`
	Blocks    []TicketRef     `json:"blocks,omitempty"`

	// ReviewRounds is how many times the ticket has entered agent_review,
	// counted from its status history. A ticket created in agent_review
	// counts that as its first round. Tickets older than the history have
	// only the rounds since it began.
	ReviewRounds int `json:"reviewRounds"`

	// DocumentCount is how many documents the ticket has. Lists and the full
	// ticket both carry it; the card's paperclip reads it.
	DocumentCount int `json:"documentCount"`

	// Documents lists the ticket's documents in the order they were added,
	// without their content. Only the full ticket carries it.
	Documents []DocumentMeta `json:"documents,omitempty"`

	// History is the ticket's status changes, newest first. Only the MCP
	// get_ticket tool fills it in; everywhere else it is left out, and the
	// HTTP API serves it from its own endpoint.
	History []StatusChange `json:"history,omitempty"`

	// URL is where the ticket opens in the web UI. The CLI and the MCP server
	// fill it in so an agent can print a link; the HTTP API leaves it empty,
	// since a browser already knows where the board is. See internal/weburl.
	URL string `json:"url,omitempty"`
}

// StatusChange is one entry in a ticket's status history. The first entry,
// written when the ticket is created, has an empty FromStatus. Note is what
// the caller said about the change, if anything.
type StatusChange struct {
	ID         string    `json:"id"`
	TicketID   string    `json:"ticketId"`
	FromStatus string    `json:"fromStatus"`
	ToStatus   string    `json:"toStatus"`
	Note       string    `json:"note"`
	CreatedAt  time.Time `json:"createdAt"`
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

// EmbeddedLabel is a label as it appears inside a ticket: no ticketCount, so
// there is no way for it to misreport a count as 0 that was never computed.
// A caller that needs a label's real ticket count fetches Label on its own
// (list_labels, create_label, update_label, or the matching HTTP/CLI calls).
type EmbeddedLabel struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
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
	Name              string `json:"name"`
	Prefix            string `json:"prefix"`
	Description       string `json:"description,omitempty"`
	AgentInstructions string `json:"agentInstructions,omitempty"`
	Icon              string `json:"icon,omitempty"`
	Color             string `json:"color,omitempty"`
}

type UpdateProjectRequest struct {
	Name        *string `json:"name,omitempty"`
	Prefix      *string `json:"prefix,omitempty"`
	Description *string `json:"description,omitempty"`
	// AgentInstructions left out (nil) leaves them unchanged; "" clears them.
	AgentInstructions *string `json:"agentInstructions,omitempty"`
	Icon              *string `json:"icon,omitempty"`
	Color             *string `json:"color,omitempty"`
	Status            *string `json:"status,omitempty"`
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
	// AppendDescription adds text to the end of the description instead of
	// replacing it, joined by AppendToDescription's rule, so a caller adding
	// a line never resends (or mangles) the rest. The store reads the
	// description and writes the result inside its write transaction, so two
	// appends at the same moment both survive. It cannot be combined with
	// Description, and text that is empty or only whitespace is an
	// ErrInvalidInput; either way nothing in the request is applied.
	AppendDescription *string `json:"appendDescription,omitempty"`
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
	// Note goes into the status history with the change, when the request
	// changes the status. It is otherwise ignored.
	Note string `json:"note,omitempty"`
}

type MoveTicketRequest struct {
	Status   string   `json:"status"`
	Position *float64 `json:"position,omitempty"`
	// Note goes into the status history with the change, when the move
	// changes the status. A move within a column writes no history.
	Note string `json:"note,omitempty"`
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

// TicketFilter narrows ListTickets. Every field that is set must match, so
// the filters combine as an AND; within Statuses any one value matches.
type TicketFilter struct {
	ProjectID string
	// Statuses keeps tickets in any of these statuses. Empty, or only blank
	// values, means every status.
	Statuses []string
	Priority string
	Repo     string
	Label    string
	// Epic is an epic name (case-insensitive) or id, or models.NoEpic
	// ("none", any case) for tickets without an epic. Without a ProjectID a
	// name matches that epic in every project.
	Epic string
	// Ready keeps only tickets that can start now: status todo, with every
	// ticket they depend on done. A todo ticket with no dependencies is
	// ready. Like every other filter it is ANDed with the rest, so Ready
	// with Statuses that leave out todo matches nothing.
	Ready bool
	// ExcludeLabel drops tickets carrying this label, matched by name
	// case-insensitively like Label. A name no label has drops nothing.
	ExcludeLabel string
}

// SetAgentInstructions sets the project's agent instructions, trimmed of
// leading and trailing whitespace, and whether it has any. The store sets
// them through here before writing, so whitespace-only instructions are
// stored empty whichever surface sent them.
func (p *Project) SetAgentInstructions(text string) {
	text = strings.TrimSpace(text)
	p.AgentInstructions = &text
	p.HasAgentInstructions = text != ""
}

// AppendToDescription is the one joining rule for appending to a ticket's
// description, the same through MCP, HTTP and the CLI. An empty description
// becomes text as given. Otherwise text starts a new paragraph: the existing
// text is kept byte for byte, followed by only the newlines it needs to end
// in a blank line (two when it ends without one, one when it ends in a
// single newline, none when it already ends in a blank line), then text.
// text itself is never changed.
func AppendToDescription(existing, text string) string {
	switch {
	case existing == "":
		return text
	case strings.HasSuffix(existing, "\n\n"):
		return existing + text
	case strings.HasSuffix(existing, "\n"):
		return existing + "\n" + text
	default:
		return existing + "\n\n" + text
	}
}
