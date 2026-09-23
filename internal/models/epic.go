package models

import "time"

// NoEpic is the epic filter value that means "tickets without an epic". It is
// reserved: no epic may be named "none" in any case, so the value can never
// be mistaken for a real epic's name.
const NoEpic = "none"

// Epic groups tickets within one project. A ticket belongs to at most one
// epic. There is no order between epics and no stored status: whether an
// epic is complete is derived from its tickets (see EpicProgress).
type Epic struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"projectId"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`

	// EpicProgress is derived from the epic's tickets on every read; none of
	// it is stored. Its fields sit at the top level of the epic's JSON.
	EpicProgress
}

// EpicProgress summarises a set of tickets: an epic's, or a project's tickets
// that have no epic.
type EpicProgress struct {
	// Counts has one entry per status in Statuses, zero included, so a
	// client can read any column without checking for a missing key.
	Counts map[string]int `json:"counts"`
	Total  int            `json:"total"`
	// Complete is true when there is at least one ticket and every one is
	// done. An empty epic is not complete.
	Complete bool `json:"complete"`
	// LastActivityAt is the latest updatedAt among the tickets, or null when
	// there are none.
	LastActivityAt *time.Time `json:"lastActivityAt"`
}

// NewEpicProgress returns the progress of no tickets: every status at zero.
func NewEpicProgress() EpicProgress {
	counts := make(map[string]int, len(Statuses))
	for _, status := range Statuses {
		counts[status] = 0
	}
	return EpicProgress{Counts: counts}
}

// EpicRef is the epic a ticket carries: enough to show and link it without a
// second fetch.
type EpicRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type CreateEpicRequest struct {
	// ProjectID accepts a project id or prefix (case-insensitive), like
	// CreateTicketRequest.ProjectID.
	ProjectID   string `json:"projectId"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// UpdateEpicRequest changes only the fields that are non-nil. An epic cannot
// move to another project.
type UpdateEpicRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
}
