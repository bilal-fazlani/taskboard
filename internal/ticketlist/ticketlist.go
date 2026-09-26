// Package ticketlist holds what MCP list_tickets and the CLI's ticket list
// share beyond the store's filtering: the short summary form of a ticket,
// for a caller that only needs to pick tickets, and paging through a long
// list.
//
// A caller that asks for neither gets the list as it always has: every
// match, in full. Asking for the summary or for a page (a limit or an
// offset) turns on paging, with DefaultLimit tickets to a page unless the
// caller sets the limit.
package ticketlist

import (
	"fmt"
	"strconv"

	"github.com/tcarac/taskboard/internal/models"
	"github.com/tcarac/taskboard/internal/weburl"
)

const (
	// DefaultLimit is the page size when paging is on and no limit is given.
	DefaultLimit = 50
	// MaxLimit is the largest page a caller may ask for.
	MaxLimit = 200
)

// Request is how a caller asked for the list: the form, and the page.
// Limit and Offset are nil when not given, which is how a request that
// sets neither is told apart from one that asks for the first page.
type Request struct {
	Summary bool
	Limit   *int
	Offset  *int
}

// Paged reports whether the answer is a page rather than the whole list.
// It is whenever the caller asked for the summary or for a page.
func (r Request) Paged() bool {
	return r.Summary || r.Limit != nil || r.Offset != nil
}

// Bounds is the page the request asks for: its size, DefaultLimit when no
// limit is given, and how many tickets to skip before it.
func (r Request) Bounds() (limit, offset int, err error) {
	limit = DefaultLimit
	if r.Limit != nil {
		limit = *r.Limit
		if limit < 1 || limit > MaxLimit {
			return 0, 0, fmt.Errorf("limit must be between 1 and %d", MaxLimit)
		}
	}
	if r.Offset != nil {
		offset = *r.Offset
		if offset < 0 {
			return 0, 0, fmt.Errorf("offset must be 0 or more")
		}
	}
	return limit, offset, nil
}

// Page is one page of a ticket list: the tickets, in summary or full form,
// how many tickets match in all, and where the next page starts. NextOffset
// is set only when HasMore is true.
type Page struct {
	Tickets    any  `json:"tickets"`
	Total      int  `json:"total"`
	Offset     int  `json:"offset"`
	Limit      int  `json:"limit"`
	HasMore    bool `json:"hasMore"`
	NextOffset *int `json:"nextOffset,omitempty"`
}

// NewPage describes a page of count tickets that starts at offset, out of
// total matches.
func NewPage(tickets any, count, total, offset, limit int) Page {
	p := Page{Tickets: tickets, Total: total, Offset: offset, Limit: limit}
	if next := offset + count; count > 0 && next < total {
		p.HasMore = true
		p.NextOffset = &next
	}
	return p
}

// Summary is the short form of a ticket: enough to pick it, not to work on
// it. The fields that would be empty are left out.
type Summary struct {
	// Key is the ticket's display key (ACP-12), or its id when its project
	// has no prefix, so it always names the ticket to get_ticket.
	Key       string       `json:"key"`
	Title     string       `json:"title"`
	Status    string       `json:"status"`
	Priority  string       `json:"priority"`
	Epic      string       `json:"epic,omitempty"`
	Labels    []string     `json:"labels,omitempty"`
	DependsOn []Dependency `json:"dependsOn,omitempty"`
	// Subtasks is the ticket's subtask progress, done/total (e.g. "2/5").
	Subtasks string `json:"subtasks,omitempty"`
	URL      string `json:"url"`
}

// Dependency is a ticket the summarised ticket depends on, and its status.
type Dependency struct {
	Key    string `json:"key"`
	Status string `json:"status"`
}

// Summarize is the summary of t, linking to the web UI at base. It reads
// only what a ticket list already carries, so it costs no query.
func Summarize(t models.Ticket, base string) Summary {
	ref := weburl.Ref(t)
	s := Summary{
		Key:      ref,
		Title:    t.Title,
		Status:   t.Status,
		Priority: t.Priority,
		URL:      weburl.Ticket(base, ref),
	}
	if t.Epic != nil {
		s.Epic = t.Epic.Name
	}
	for _, l := range t.Labels {
		s.Labels = append(s.Labels, l.Name)
	}
	for _, d := range t.DependsOn {
		s.DependsOn = append(s.DependsOn, Dependency{Key: d.Key, Status: d.Status})
	}
	if len(t.Subtasks) > 0 {
		done := 0
		for _, st := range t.Subtasks {
			if st.Completed {
				done++
			}
		}
		s.Subtasks = strconv.Itoa(done) + "/" + strconv.Itoa(len(t.Subtasks))
	}
	return s
}

// SummarizeAll is the summary of every ticket, linking to weburl.Base().
// It is never nil, so an empty page is [] in JSON.
func SummarizeAll(tickets []models.Ticket) []Summary {
	base := weburl.Base()
	out := make([]Summary, len(tickets))
	for i, t := range tickets {
		out[i] = Summarize(t, base)
	}
	return out
}
