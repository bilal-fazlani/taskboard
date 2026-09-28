package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/tcarac/taskboard/internal/db"
	"github.com/tcarac/taskboard/internal/models"
)

// Entries are short typed records on a ticket, an epic or a project: why the
// work is as it is and what happened to it. Agents write them with
// write_entry and read them with list_entries; get_ticket carries a ticket's
// own. Notes are the person's, so agents never write one: they mark it
// handled once they have acted on it.

// agentEntryTypes are the types write_entry takes: every entry type but the
// person's note.
var agentEntryTypes = func() []string {
	var out []string
	for _, t := range models.EntryTypes {
		if t != models.EntryNote {
			out = append(out, t)
		}
	}
	return out
}()

// errEntryOwner is the answer to a call that names no owner, or more than
// one.
var errEntryOwner = errors.New("pass one of ticket, epic (with project when it is a name) or project: what the entries sit on")

// entryOwnerArgs name what an entry sits on: a ticket (id or display key),
// an epic (id, or name together with project), or a project on its own.
type entryOwnerArgs struct {
	Ticket string `json:"ticket"`
	Epic   string `json:"epic"`
	projectRefArg
}

// resolveEntryOwner resolves the owner arguments to ids. A ticket with an
// epic or a project is refused rather than resolved to either, since project
// on its own is an owner too.
func (s *MCPServer) resolveEntryOwner(a entryOwnerArgs) (models.EntryOwner, error) {
	ticket, epic, project := strings.TrimSpace(a.Ticket), strings.TrimSpace(a.Epic), a.projectRef()
	switch {
	case ticket != "" && (epic != "" || project != ""):
		return models.EntryOwner{}, errEntryOwner
	case ticket != "":
		id, err := s.store.ResolveTicketID(ticket)
		return models.EntryOwner{TicketID: id}, err
	case epic != "":
		id, err := s.resolveEpicRefOrError(epic, project)
		return models.EntryOwner{EpicID: id}, err
	case project != "":
		id, err := s.store.ResolveProjectRef(project)
		return models.EntryOwner{ProjectID: id}, err
	}
	return models.EntryOwner{}, errEntryOwner
}

// writeEntryArgs is write_entry's arguments.
type writeEntryArgs struct {
	entryOwnerArgs
	Type           string         `json:"type"`
	Text           string         `json:"text"`
	AgentID        string         `json:"agentId"`
	Source         string         `json:"source"`
	Replaces       string         `json:"replaces"`
	Verdict        string         `json:"verdict"`
	Findings       map[string]int `json:"findings"`
	ReportDocument string         `json:"reportDocument"`
}

// listEntriesArgs is list_entries' arguments.
type listEntriesArgs struct {
	entryOwnerArgs
	Types           []string `json:"types"`
	IncludeReplaced bool     `json:"includeReplaced"`
	Before          string   `json:"before"`
	Limit           *int     `json:"limit"`
}

// entryConfirmation is write_entry's answer, and handle_note's with
// handled set.
type entryConfirmation struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Created bool   `json:"created,omitempty"`
	Handled bool   `json:"handled,omitempty"`
}

// fullTicket is get_ticket's answer: the ticket with what it carries about
// entries.
type fullTicket struct {
	*models.Ticket
	models.TicketEntries
}

// bareEntries leaves out each entry's owner: the caller named it.
func bareEntries(page models.EntryPage) models.EntryPage {
	for i := range page.Entries {
		page.Entries[i].EntryOwner = models.EntryOwner{}
	}
	return page
}

// getTicketEntriesHelp is what get_ticket's description says about entries.
var getTicketEntriesHelp = " It also carries entries: a page of the ticket's " + strconv.Itoa(db.EntryDefaultLimit) +
	" newest current entries, {entries, total, hasMore, nextBefore} (older ones with list_entries, before: nextBefore); " +
	"openNotes, how many of the person's notes on it are open; and epicOpenNotes and projectOpenNotes, the open notes on " +
	"its epic and project, counted only. Each is left out when there are none, epicOpenNotes also when the ticket has no epic. " +
	"An open note can be older than the newest entries: read the notes with list_entries, types [\"note\"]; the open ones have no handledAt."

// entryOwnerProps are the owner arguments of the entry tools.
func entryOwnerProps(props map[string]schemaProp) map[string]schemaProp {
	props["ticket"] = schemaProp{Type: "string", Description: "Ticket ID or display key (e.g. BILL-2), case-insensitive"}
	props["epic"] = schemaProp{Type: "string", Description: "Epic ID, or its name together with project"}
	return withProjectRefProps("Project ID or prefix (case-insensitive): the project itself, or the epic's project when epic is a name.", props)
}

// entryToolDefs are the entry tools, listed after the ticket and subtask
// tools.
var entryToolDefs = [3]toolDef{
	{
		Name: "write_entry",
		Description: "Record on the board why the work is as it is: a new entry on a ticket, an epic (when it binds several tickets) " +
			"or a project (when project-wide); pass one of ticket, epic or project. One or two sentences: the outcome and the reason, " +
			"never a transcript or a list of steps. Types: decision (what was chosen, why, and what was rejected; source person when " +
			"it is the person's call or correction, agent when yours); learning (a gotcha the next agent should know); and on a ticket " +
			"only: hand_off (where the work stopped and the next step, whenever you stop short of done), proof (what was verified, " +
			"how, and the result), review (one review round: verdict, findings, text a one-line summary, reportDocument the " +
			"ticket's Review N document). Entries are never edited: to correct one, write a new entry of its type with replaces set " +
			"to its id. Answers {id, type, created: true}.",
		InputSchema: jsonSchema{
			Type: "object",
			Properties: entryOwnerProps(map[string]schemaProp{
				"type":    {Type: "string", Description: "The entry's type", Enum: agentEntryTypes},
				"text":    {Type: "string", Description: "One or two sentences: the outcome and the reason. A review's is its one-line summary."},
				"agentId": {Type: "string", Description: "Your agent's id on this board: every entry names the agent that wrote it."},
				"source": {Type: "string", Description: "A decision's only, and required there: whose call it was.",
					Enum: models.DecisionSources},
				"replaces": {Type: "string", Description: "The id of an entry of the same type on the same owner that this one replaces; " +
					"reads then leave the old one out."},
				"verdict": {Type: "string", Description: "A review's only, and required there.", Enum: models.ReviewVerdicts},
				"findings": {Type: "object", Description: "A review's finding counts by severity (" +
					strings.Join(models.ReviewSeverities, ", ") + "); leave out a severity with none."},
				"reportDocument": {Type: "string", Description: "A review's: the name of the ticket's document holding the full report."},
			}),
			Required: []string{"type", "text", "agentId"},
		},
	},
	{
		Name: "list_entries",
		Description: "Read the entries on a ticket, an epic or a project (pass one of ticket, epic or project), newest first, " +
			"one page at a time: {entries, total, hasMore, nextBefore}. Only current entries unless includeReplaced. " +
			"get_ticket already carries a ticket's newest entries; use this for an epic's or a project's, one type " +
			"(types: [\"note\"] for the notes to act on), or an older page (before: nextBefore).",
		InputSchema: jsonSchema{
			Type: "object",
			Properties: entryOwnerProps(map[string]schemaProp{
				"types":           {Type: "array", Description: "Only entries of these types", Items: &jsonSchema{Type: "string", Enum: models.EntryTypes}},
				"includeReplaced": {Type: "boolean", Description: "Also return entries a later entry replaced, each with replacedBy."},
				"before":          {Type: "string", Description: "Start after this entry: the previous page's nextBefore."},
				"limit": {Type: "integer", Description: "Page size: 1 to " + strconv.Itoa(db.EntryMaxLimit) +
					", default " + strconv.Itoa(db.EntryDefaultLimit) + "."},
			}),
		},
	},
	{
		Name: "handle_note",
		Description: "Mark one of the person's notes handled, once you have acted on it. A note is a one-way instruction: nobody " +
			"replies. Any call on a ticket that has open notes carries openNotes, their count; read them with list_entries, " +
			"types [\"note\"] (the open ones have no handledAt). A note about an entry challenges it: replace that entry, or confirm it with a new one, then handle " +
			"the note. Answers {id, type, handled: true}.",
		InputSchema: jsonSchema{
			Type: "object",
			Properties: withIDOrKeyProps("The note's id.", map[string]schemaProp{
				"agentId": {Type: "string", Description: "Your agent's id on this board: the agent that handled the note."},
			}),
			Required: []string{"agentId"},
		},
	},
}

// callEntryTool handles the entry tools. ok is false for any other name.
func (s *MCPServer) callEntryTool(name string, args json.RawMessage) (result any, ok bool, err error) {
	switch name {
	case "write_entry":
		var a writeEntryArgs
		if err := decodeArgs(args, &a); err != nil {
			return nil, true, err
		}
		result, err := s.writeEntry(a)
		return result, true, err
	case "list_entries":
		var a listEntriesArgs
		if err := decodeArgs(args, &a); err != nil {
			return nil, true, err
		}
		result, err := s.listEntries(a)
		return result, true, err
	case "handle_note":
		var a struct {
			idOrKeyArg
			AgentID string `json:"agentId"`
		}
		if err := decodeArgs(args, &a); err != nil {
			return nil, true, err
		}
		if a.ref() == "" {
			return nil, true, errors.New("id is required: the note's id")
		}
		e, err := s.store.MarkNoteHandled(a.ref(), a.AgentID)
		if err != nil {
			return nil, true, err
		}
		return entryConfirmation{ID: e.ID, Type: e.Type, Handled: true}, true, nil
	}
	return nil, false, nil
}

func (s *MCPServer) writeEntry(a writeEntryArgs) (any, error) {
	if strings.TrimSpace(a.Type) == models.EntryNote {
		return nil, errors.New("a note is the person's: agents mark notes handled (handle_note) and never write one")
	}
	if strings.TrimSpace(a.AgentID) == "" {
		return nil, errors.New("agentId is required: every entry names the agent that wrote it")
	}
	owner, err := s.resolveEntryOwner(a.entryOwnerArgs)
	if err != nil {
		return nil, err
	}
	e, err := s.store.CreateEntry(models.CreateEntryRequest{
		EntryOwner: owner, Type: a.Type, Text: a.Text, AgentID: a.AgentID, Source: a.Source, Replaces: a.Replaces,
		Verdict: a.Verdict, Findings: a.Findings, ReportDocument: a.ReportDocument,
	})
	if err != nil {
		return nil, err
	}
	return entryConfirmation{ID: e.ID, Type: e.Type, Created: true}, nil
}

func (s *MCPServer) listEntries(a listEntriesArgs) (models.EntryPage, error) {
	owner, err := s.resolveEntryOwner(a.entryOwnerArgs)
	if err != nil {
		return models.EntryPage{}, err
	}
	limit := db.EntryDefaultLimit
	if a.Limit != nil {
		limit = *a.Limit
	}
	page, err := s.store.ListEntries(owner, models.EntryFilter{IncludeReplaced: a.IncludeReplaced, Types: a.Types}, a.Before, limit)
	if err != nil {
		return models.EntryPage{}, err
	}
	return bareEntries(page), nil
}

// ticketOfCall returns the ticket a tool call is on, read from its
// arguments, or "" when it is on no one ticket or names none that exists.
// get_ticket and start_ticket are left out: get_ticket's answer carries
// openNotes itself, and start_ticket's the open notes themselves.
func (s *MCPServer) ticketOfCall(name string, args json.RawMessage) string {
	var a struct {
		idOrKeyArg
		TicketID string `json:"ticketId"`
		Ticket   string `json:"ticket"`
		Epic     string `json:"epic"`
		Request  string `json:"request"`
	}
	if len(args) == 0 || json.Unmarshal(args, &a) != nil {
		return ""
	}
	var ref string
	switch name {
	case "update_ticket", "move_ticket":
		ref = a.ref()
	case "create_subtask", "batch_create_subtasks":
		ref = a.TicketID
	case "toggle_subtask", "delete_subtask":
		st, err := s.store.GetSubtask(a.ID)
		if err != nil || st == nil {
			return ""
		}
		return st.TicketID
	case "list_documents", "create_document", "write_entry", "list_entries", "release_ticket", "request_user_input":
		ref = a.Ticket
	case "await_answer":
		r, err := s.store.GetRequest(a.Request)
		if err != nil || r == nil {
			return ""
		}
		return r.TicketID
	case "get_document", "update_document", "delete_document":
		if strings.TrimSpace(a.Ticket) == "" {
			if strings.TrimSpace(a.Epic) != "" {
				return ""
			}
			id, _ := s.store.DocumentTicketID(a.ref())
			return id
		}
		ref = a.Ticket
	case "handle_note":
		e, err := s.store.GetEntry(a.ref())
		if err != nil || e == nil {
			return ""
		}
		return e.TicketID
	}
	if strings.TrimSpace(ref) == "" {
		return ""
	}
	id, err := s.store.ResolveTicketID(ref)
	if err != nil {
		return ""
	}
	return id
}

// openNotesOn counts the open notes on the ticket, or 0 for none. A failed
// count is 0 too: the flag never fails the call it rides on.
func (s *MCPServer) openNotesOn(ticketID string) int {
	if ticketID == "" {
		return 0
	}
	n, err := s.store.CountOpenNotes(models.EntryOwner{TicketID: ticketID})
	if err != nil {
		return 0
	}
	return n
}

// openNotesContent is the flag as content of its own, for an answer it
// cannot join.
func openNotesContent(n int) textContent {
	return textContent{Type: "text", Text: fmt.Sprintf(`{"openNotes":%d}`, n)}
}

// withOpenNotes is a tool's answer, data, as content, carrying openNotes
// when n is above 0: as a field of an answer that is an object, or else as
// content of its own after it.
func withOpenNotes(data []byte, n int) []textContent {
	content := []textContent{{Type: "text", Text: string(data)}}
	if n == 0 {
		return content
	}
	if len(data) >= 2 && data[0] == '{' && data[len(data)-1] == '}' {
		sep := ","
		if bytes.Equal(data, []byte("{}")) {
			sep = ""
		}
		content[0].Text = fmt.Sprintf(`%s%s"openNotes":%d}`, data[:len(data)-1], sep, n)
		return content
	}
	return append(content, openNotesContent(n))
}
