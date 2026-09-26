package mcp

import (
	"errors"
	"strconv"
	"strings"

	"github.com/tcarac/taskboard/internal/db"
	"github.com/tcarac/taskboard/internal/models"
)

// appendJournalArgs is append_project_journal's arguments.
type appendJournalArgs struct {
	ProjectID string `json:"projectId"`
	Author    string `json:"author"`
	Text      string `json:"text"`
}

// listJournalArgs is list_project_journal's arguments.
type listJournalArgs struct {
	ProjectID string `json:"projectId"`
	Before    string `json:"before"`
	Limit     *int   `json:"limit"`
}

// getProjectJournalHelp is what get_project's description says about the
// journal it carries.
var getProjectJournalHelp = "It also carries journal: the project's " + strconv.Itoa(db.JournalPreviewLimit) +
	" latest journal entries, newest first, with total, hasMore and nextBefore; " +
	"read older ones with list_project_journal, passing before: journal.nextBefore."

// journalToolDefs are the journal's two tools, listed with the project tools.
var journalToolDefs = [2]toolDef{
	{
		Name: "append_project_journal",
		Description: "Append an entry to a project's journal: a dated log of what happened on the project " +
			"(run reports, decisions, current state), kept apart from its description. Entries are never edited or " +
			"deleted, so write each one to stand on its own, and write a new entry to correct an old one. " +
			"Answers with the new entry: {id, projectId, author, text, createdAt}.",
		InputSchema: jsonSchema{
			Type: "object",
			Properties: map[string]schemaProp{
				"projectId": {Type: "string", Description: "Project ID or prefix (case-insensitive)"},
				"author": {Type: "string", Description: "Who writes the entry, as a name, for example your role in the run " +
					"(\"orchestrator\", \"reviewer\") or the person's name. At most " + strconv.Itoa(db.JournalAuthorMaxLength) + " characters."},
				"text": {Type: "string", Description: "The entry, in Markdown."},
			},
			Required: []string{"projectId", "author", "text"},
		},
	},
	{
		Name: "list_project_journal",
		Description: "Read a project's journal, newest first, one page at a time: {entries, total, hasMore, nextBefore}. " +
			"get_project already carries the latest entries; when hasMore is true, call again with before set to nextBefore for older ones.",
		InputSchema: jsonSchema{
			Type: "object",
			Properties: map[string]schemaProp{
				"projectId": {Type: "string", Description: "Project ID or prefix (case-insensitive)"},
				"before":    {Type: "string", Description: "Start after this entry: the previous page's nextBefore. Leave it out to start with the newest entry."},
				"limit": {Type: "integer", Description: "Page size: at most this many entries (1 to " + strconv.Itoa(db.JournalMaxLimit) +
					", default " + strconv.Itoa(db.JournalDefaultLimit) + ")."},
			},
			Required: []string{"projectId"},
		},
	},
}

// resolveJournalProject resolves a journal tool's projectId, which must be given.
func (s *MCPServer) resolveJournalProject(ref string) (string, error) {
	if strings.TrimSpace(ref) == "" {
		return "", errors.New("projectId is required")
	}
	return s.store.ResolveProjectRef(ref)
}

func (s *MCPServer) appendProjectJournal(a appendJournalArgs) (*models.JournalEntry, error) {
	projectID, err := s.resolveJournalProject(a.ProjectID)
	if err != nil {
		return nil, err
	}
	return s.store.AppendJournalEntry(projectID, models.AppendJournalEntryRequest{Author: a.Author, Text: a.Text})
}

func (s *MCPServer) listProjectJournal(a listJournalArgs) (models.JournalPage, error) {
	projectID, err := s.resolveJournalProject(a.ProjectID)
	if err != nil {
		return models.JournalPage{}, err
	}
	limit := db.JournalDefaultLimit
	if a.Limit != nil {
		limit = *a.Limit
	}
	return s.store.ListJournal(projectID, a.Before, limit)
}
