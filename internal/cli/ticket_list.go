package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/tcarac/taskboard/internal/db"
	"github.com/tcarac/taskboard/internal/models"
	"github.com/tcarac/taskboard/internal/ticketlist"
	"github.com/tcarac/taskboard/internal/weburl"
)

// printTicketLines prints each ticket on one line in full: key, title,
// status, priority, repos, labels, epic, dependencies, surfaced from, id and url.
func printTicketLines(w io.Writer, tickets []models.Ticket) {
	base := weburl.Base()
	for _, t := range tickets {
		line := fmt.Sprintf("[%s] %s - %s (%s", t.DisplayKey(), t.Title, t.Status, t.Priority)
		if len(t.Repos) > 0 {
			line += ", " + strings.Join(t.Repos, " ")
		}
		line += ")"
		if len(t.Labels) > 0 {
			names := make([]string, len(t.Labels))
			for i, l := range t.Labels {
				names[i] = l.Name
			}
			line += " [" + strings.Join(names, ", ") + "]"
		}
		if t.Epic != nil {
			line += " epic:" + t.Epic.Name
		}
		if len(t.DependsOn) > 0 {
			line += " depends on " + dependencyKeys(t.DependsOn)
		}
		if t.SurfacedFrom != nil {
			line += " surfaced from " + t.SurfacedFrom.Key
		}
		fmt.Fprintf(w, "%s  (%s)  %s\n", line, t.ID, weburl.Ticket(base, weburl.Ref(t)))
	}
}

// printSummaryLines prints each ticket's summary on one line: key, title,
// status and priority, then its epic, labels, dependencies with their
// status and subtask progress when it has them, then its url.
func printSummaryLines(w io.Writer, summaries []ticketlist.Summary) {
	for _, s := range summaries {
		parts := []string{s.Key, s.Title, "(" + s.Status + ", " + s.Priority + ")"}
		if s.Epic != "" {
			parts = append(parts, "epic: "+s.Epic)
		}
		if len(s.Labels) > 0 {
			parts = append(parts, "labels: "+strings.Join(s.Labels, ", "))
		}
		if len(s.DependsOn) > 0 {
			deps := make([]string, len(s.DependsOn))
			for i, d := range s.DependsOn {
				deps[i] = d.Key + " " + d.Status + dependencyDetail(d.Kind, d.Note)
			}
			parts = append(parts, "depends on: "+strings.Join(deps, ", "))
		}
		if s.SurfacedFrom != "" {
			parts = append(parts, "surfaced from: "+s.SurfacedFrom)
		}
		if s.Subtasks != "" {
			parts = append(parts, "subtasks: "+s.Subtasks)
		}
		parts = append(parts, s.URL)
		fmt.Fprintln(w, strings.Join(parts, "  "))
	}
}

// printTicketPage prints one page of the list, in summary or full form,
// and a last line saying which tickets it showed and, when more follow,
// the --offset of the next page.
func printTicketPage(w io.Writer, store *db.Store, filter models.TicketFilter, req ticketlist.Request) error {
	limit, offset, err := req.Bounds()
	if err != nil {
		return err
	}
	tickets, total, err := store.ListTicketsPage(filter, limit, offset)
	if err != nil {
		return err
	}
	if total == 0 {
		fmt.Fprintln(w, "No tickets found.")
		return nil
	}
	if len(tickets) == 0 {
		fmt.Fprintf(w, "No tickets at offset %d; %d match in all.\n", offset, total)
		return nil
	}
	if req.Summary {
		printSummaryLines(w, ticketlist.SummarizeAll(tickets))
	} else {
		printTicketLines(w, tickets)
	}
	page := ticketlist.NewPage(nil, len(tickets), total, offset, limit)
	fmt.Fprintf(w, "Showing %d-%d of %d tickets.", offset+1, offset+len(tickets), total)
	if page.HasMore {
		fmt.Fprintf(w, " Next page: --offset %d", *page.NextOffset)
	}
	fmt.Fprintln(w)
	return nil
}
