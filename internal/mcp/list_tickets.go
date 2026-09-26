package mcp

import (
	"encoding/json"
	"errors"

	"github.com/tcarac/taskboard/internal/models"
)

// listTicketsArgs is list_tickets's arguments. Status takes one status or
// several; the filters combine as an AND, the same as models.TicketFilter.
type listTicketsArgs struct {
	ProjectID    string     `json:"projectId"`
	Status       statusList `json:"status"`
	Priority     string     `json:"priority"`
	Repo         string     `json:"repo"`
	Label        string     `json:"label"`
	Epic         string     `json:"epic"`
	Ready        bool       `json:"ready"`
	ExcludeLabel string     `json:"excludeLabel"`
}

// filter is the store filter these arguments ask for.
func (a listTicketsArgs) filter() models.TicketFilter {
	return models.TicketFilter{
		ProjectID:    a.ProjectID,
		Statuses:     a.Status,
		Priority:     a.Priority,
		Repo:         a.Repo,
		Label:        a.Label,
		Epic:         a.Epic,
		Ready:        a.Ready,
		ExcludeLabel: a.ExcludeLabel,
	}
}

// statusList decodes list_tickets's status, given either as one string or as
// an array of strings, so a caller that always sent one status keeps working
// now that it takes several.
type statusList []string

func (l *statusList) UnmarshalJSON(data []byte) error {
	var one string
	if err := json.Unmarshal(data, &one); err == nil {
		*l = statusList{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(data, &many); err != nil {
		return errors.New("status must be a string or an array of strings")
	}
	*l = many
	return nil
}
