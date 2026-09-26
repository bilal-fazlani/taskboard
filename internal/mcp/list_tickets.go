package mcp

import (
	"encoding/json"
	"errors"
	"strconv"

	"github.com/tcarac/taskboard/internal/models"
	"github.com/tcarac/taskboard/internal/ticketlist"
	"github.com/tcarac/taskboard/internal/weburl"
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
	Summary      bool       `json:"summary"`
	Limit        *int       `json:"limit"`
	Offset       *int       `json:"offset"`
}

// list_tickets's help on the summary form and paging: when to use each
// form, and how to get the next page.
var (
	listTicketsFormHelp = "To pick tickets, pass summary: true: each ticket comes back as its key, title, status, priority, epic, labels, " +
		"the keys and statuses of the tickets it depends on (kind: conflict_only on one it waits for only to avoid a conflict, and its note if any), " +
		"the key of the ticket it was surfaced from (surfacedFrom), subtask progress (done/total) and url, a small fraction of the full form; " +
		"then read the ones you need with get_ticket. Leave summary out only when you need every ticket's full details " +
		"(description, subtasks, repos, dates), and page through them with a small limit. " +
		"Without summary, limit or offset the answer is an array of every match in full. " +
		"With any of them it is one page, {tickets, total, offset, limit, hasMore, nextOffset}, of up to limit tickets " +
		"(" + strconv.Itoa(ticketlist.DefaultLimit) + " by default, at most " + strconv.Itoa(ticketlist.MaxLimit) + "); " +
		"when hasMore is true, call again with the same arguments and offset set to nextOffset for the next page."
	listTicketsSummaryHelp = "Return each ticket in the short summary form instead of in full, one page at a time. Use it to pick tickets."
	listTicketsLimitHelp   = "Page size: at most this many tickets (1 to " + strconv.Itoa(ticketlist.MaxLimit) +
		", default " + strconv.Itoa(ticketlist.DefaultLimit) + "). Turns on paging."
	listTicketsOffsetHelp = "How many matching tickets to skip before the page starts (default 0); pass the previous page's nextOffset. Turns on paging."
)

// listTickets answers list_tickets. Without summary, limit or offset the
// answer is every match in full, a JSON array, as it has always been; with
// any of them it is a ticketlist.Page of summaries or full tickets.
func (s *MCPServer) listTickets(a listTicketsArgs) (any, error) {
	req := ticketlist.Request{Summary: a.Summary, Limit: a.Limit, Offset: a.Offset}
	if !req.Paged() {
		tickets, err := s.store.ListTickets(a.filter())
		if err != nil {
			return nil, err
		}
		return weburl.FillAll(tickets), nil
	}
	limit, offset, err := req.Bounds()
	if err != nil {
		return nil, err
	}
	tickets, total, err := s.store.ListTicketsPage(a.filter(), limit, offset)
	if err != nil {
		return nil, err
	}
	var page any
	if a.Summary {
		page = ticketlist.SummarizeAll(tickets)
	} else {
		if tickets == nil {
			tickets = []models.Ticket{}
		}
		page = weburl.FillAll(tickets)
	}
	return ticketlist.NewPage(page, len(tickets), total, offset, limit), nil
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
