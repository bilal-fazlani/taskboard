package models

import "time"

// JournalEntry is one entry in a project's journal. Entries are appended and
// never rewritten; they go only with their project.
type JournalEntry struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	// Author is the name the writer gave, free text: there are no agent
	// identities yet, so it refers to nothing else on the board.
	Author    string    `json:"author"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"createdAt"`
}

// JournalPage is a run of a project's journal entries, newest first. When
// HasMore is true, the next page is the one read with Before set to
// NextBefore, the id of this page's oldest entry. Paging by entry rather than
// by count keeps pages exact while entries are appended between reads.
type JournalPage struct {
	Entries []JournalEntry `json:"entries"`
	// Total is how many entries the project's journal has in all.
	Total      int    `json:"total"`
	HasMore    bool   `json:"hasMore"`
	NextBefore string `json:"nextBefore,omitempty"`
}

// AppendJournalEntryRequest is the body of an append to a project's journal.
type AppendJournalEntryRequest struct {
	Author string `json:"author"`
	Text   string `json:"text"`
}
