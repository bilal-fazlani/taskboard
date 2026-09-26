package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/tcarac/taskboard/internal/models"
	"github.com/tcarac/taskboard/internal/weburl"
)

// Change tools (create_*, update_*, move_ticket and the subtask tools) answer
// with a short confirmation by default: enough to know the change landed and
// to address the record again, without the description, subtasks, labels,
// agent instructions or document content an agent rarely reads after writing
// them. A caller that wants the whole record passes full: true.

// fullArg is the opt-in every change tool takes for the whole record.
type fullArg struct {
	Full bool `json:"full"`
}

// fullProp is the schema of the full argument, for a tool whose whole answer
// is described by whole ("the whole ticket").
func fullProp(whole string) schemaProp {
	return schemaProp{Type: "boolean", Description: "Answer with " + whole + " instead of the short confirmation. " +
		"Leave it out unless you will read what comes back."}
}

// changedHelp says how an update's short confirmation reports what changed.
const changedHelp = "changed: the names of the fields the call changed ([] when it changed nothing)"

// What each record's short confirmation holds, and what full: true returns
// instead, for the tool descriptions.
const (
	ticketHolds = "the ticket's id, key, title and status, %s, updatedAt and url"
	ticketWhole = "the whole ticket (description, labels, subtasks, dependencies, documents)"

	subtaskWhole = "the whole ticket (all its subtasks included)"

	projectHolds = "the project's id, prefix, name and status, %s, and updatedAt"
	projectWhole = "the whole project (description and agentInstructions included)"

	epicHolds = "the epic's id, name and project prefix (project), %s, updatedAt and url"
	epicWhole = "the whole epic (with its progress and documents)"

	labelHolds = "the label's id, name and color, and %s"
	labelWhole = "the whole label (with its ticketCount)"

	documentHolds = "the document's id, name and format, %s, updatedAt and url"
	documentWhole = "the whole document (content included)"
)

// shortAnswerHelp is the sentence a change tool's description ends with:
// what its short confirmation holds and how to get the whole record. holds
// may carry one %s, filled with what, the way the confirmation says what the
// call did.
func shortAnswerHelp(holds, what, whole string) string {
	if what != "" {
		holds = fmt.Sprintf(holds, what)
	}
	return fmt.Sprintf(" By default it answers with a short confirmation: %s. Pass full: true to get %s instead.", holds, whole)
}

// change is what a change tool did, as its short confirmation reports it:
// created, deleted, or the fields it changed. Changed is a pointer so that an
// update that changed nothing still says so, as an empty list.
type change struct {
	Created bool      `json:"created,omitempty"`
	Deleted bool      `json:"deleted,omitempty"`
	Changed *[]string `json:"changed,omitempty"`
}

var (
	createdChange = change{Created: true}
	deletedChange = change{Deleted: true}
)

// changedFields compares a record before and after a call, field by field in
// its JSON form, and reports the fields among fields that differ, in the
// order given. A field left out of the JSON on both sides is unchanged.
func changedFields(before, after any, fields ...string) change {
	b, a := jsonFields(before), jsonFields(after)
	changed := []string{}
	for _, f := range fields {
		if !bytes.Equal(b[f], a[f]) {
			changed = append(changed, f)
		}
	}
	return change{Changed: &changed}
}

func jsonFields(v any) map[string]json.RawMessage {
	fields := map[string]json.RawMessage{}
	if data, err := json.Marshal(v); err == nil {
		_ = json.Unmarshal(data, &fields)
	}
	return fields
}

// The fields each record's "changed" can name: those its change tools set.
// A ticket's position is left out, since move_ticket moves the ticket to the
// end of its new column as a side effect of every move.
var (
	ticketFields   = []string{"title", "description", "status", "priority", "dueDate", "epic", "repos", "labels", "dependsOn", "delivery", "surfacedFrom"}
	projectFields  = []string{"name", "prefix", "description", "agentInstructions", "icon", "color", "status"}
	epicFields     = []string{"name", "description"}
	labelFields    = []string{"name", "color"}
	documentFields = []string{"name", "content"}
)

// ticketConfirmation is the short answer of the ticket tools.
type ticketConfirmation struct {
	ID     string `json:"id"`
	Key    string `json:"key,omitempty"`
	Title  string `json:"title"`
	Status string `json:"status"`
	change
	UpdatedAt time.Time `json:"updatedAt"`
	URL       string    `json:"url"`
}

// ticketAnswer is a ticket tool's answer: the whole ticket when full, the
// short confirmation otherwise.
func ticketAnswer(t *models.Ticket, c change, full bool) any {
	t = weburl.Fill(t)
	if full {
		return t
	}
	key := ""
	if t.ProjectPrefix != "" {
		key = t.DisplayKey()
	}
	return ticketConfirmation{
		ID: t.ID, Key: key, Title: t.Title, Status: t.Status,
		change: c, UpdatedAt: t.UpdatedAt, URL: t.URL,
	}
}

// subtaskConfirmation is the short answer of create_subtask, toggle_subtask
// and delete_subtask. Ticket is the parent's display key (its id when its
// project has no prefix), and URL opens the parent.
type subtaskConfirmation struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Completed bool   `json:"completed"`
	Ticket    string `json:"ticket"`
	change
	URL string `json:"url"`
}

// subtasksConfirmation is batch_create_subtasks' short answer.
type subtasksConfirmation struct {
	Ticket string `json:"ticket"`
	change
	Subtasks []subtaskRef `json:"subtasks"`
	URL      string       `json:"url"`
}

type subtaskRef struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// subtaskParent reads the ticket a subtask belongs to, with its URL, for a
// subtask tool's answer.
func (s *MCPServer) subtaskParent(ticketID string) (*models.Ticket, error) {
	t, err := s.store.GetTicket(ticketID)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, fmt.Errorf("ticket not found")
	}
	return weburl.Fill(t), nil
}

// subtaskAnswer is a subtask tool's answer: the whole parent ticket when
// full, the short confirmation otherwise.
func (s *MCPServer) subtaskAnswer(st *models.Subtask, c change, full bool) (any, error) {
	t, err := s.subtaskParent(st.TicketID)
	if err != nil {
		return nil, err
	}
	if full {
		return t, nil
	}
	return subtaskConfirmation{
		ID: st.ID, Title: st.Title, Completed: st.Completed,
		Ticket: weburl.Ref(*t), change: c, URL: t.URL,
	}, nil
}

// subtasksAnswer is batch_create_subtasks' answer.
func (s *MCPServer) subtasksAnswer(ticketID string, subtasks []models.Subtask, full bool) (any, error) {
	t, err := s.subtaskParent(ticketID)
	if err != nil {
		return nil, err
	}
	if full {
		return t, nil
	}
	refs := make([]subtaskRef, len(subtasks))
	for i, st := range subtasks {
		refs[i] = subtaskRef{ID: st.ID, Title: st.Title}
	}
	return subtasksConfirmation{Ticket: weburl.Ref(*t), change: createdChange, Subtasks: refs, URL: t.URL}, nil
}

// projectConfirmation is the short answer of create_project and
// update_project. Projects have no URL of their own.
type projectConfirmation struct {
	ID     string `json:"id"`
	Prefix string `json:"prefix"`
	Name   string `json:"name"`
	Status string `json:"status"`
	change
	UpdatedAt time.Time `json:"updatedAt"`
}

func projectAnswer(p *models.Project, c change, full bool) any {
	if full {
		return p
	}
	return projectConfirmation{ID: p.ID, Prefix: p.Prefix, Name: p.Name, Status: p.Status, change: c, UpdatedAt: p.UpdatedAt}
}

// epicConfirmation is the short answer of create_epic and update_epic.
// Project is the epic's project prefix, and URL opens the epic on the Epics
// view.
type epicConfirmation struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Project string `json:"project"`
	change
	UpdatedAt time.Time `json:"updatedAt"`
	URL       string    `json:"url,omitempty"`
}

func (s *MCPServer) epicAnswer(e *models.Epic, c change, full bool) (any, error) {
	if full {
		return e, nil
	}
	p, err := s.store.GetProject(e.ProjectID)
	if err != nil {
		return nil, err
	}
	answer := epicConfirmation{ID: e.ID, Name: e.Name, change: c, UpdatedAt: e.UpdatedAt}
	if p != nil {
		answer.Project = p.Prefix
		answer.URL = weburl.Epic(weburl.Base(), p.Prefix, e.Name)
	}
	return answer, nil
}

// labelConfirmation is the short answer of create_label and update_label.
// Labels have no timestamps and no URL.
type labelConfirmation struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
	change
}

func labelAnswer(l *models.Label, c change, full bool) any {
	if full {
		return l
	}
	return labelConfirmation{ID: l.ID, Name: l.Name, Color: l.Color, change: c}
}

// labelByID reads one label, for update_label to compare against; nil when
// there is none.
func (s *MCPServer) labelByID(id string) (*models.Label, error) {
	labels, err := s.store.ListLabels()
	if err != nil {
		return nil, err
	}
	for i := range labels {
		if labels[i].ID == id {
			return &labels[i], nil
		}
	}
	return nil, nil
}

// documentConfirmation is the short answer of create_document and
// update_document. An image rename's rewritten and left references stay in
// it, since they say what else the call changed.
type documentConfirmation struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Format string `json:"format"`
	change
	UpdatedAt         time.Time           `json:"updatedAt"`
	URL               string              `json:"url,omitempty"`
	ReferencesUpdated []models.ImagePlace `json:"referencesUpdated,omitempty"`
	ReferencesLeft    []models.ImagePlace `json:"referencesLeft,omitempty"`
}

// documentResult is a document tool's answer: the whole document (content
// included) when full, the short confirmation otherwise.
func (s *MCPServer) documentResult(d *models.Document, c change, full bool) any {
	d = s.withDocumentURL(d)
	if full {
		return d
	}
	return documentConfirmation{
		ID: d.ID, Name: d.Name, Format: d.Format, change: c, UpdatedAt: d.UpdatedAt, URL: d.URL,
		ReferencesUpdated: d.ReferencesUpdated, ReferencesLeft: d.ReferencesLeft,
	}
}
