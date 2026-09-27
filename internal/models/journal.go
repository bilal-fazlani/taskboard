package models

import "time"

// JournalEntry is one entry in a project's journal: one of the project's
// current entries (Entry), in the journal's older shape, kept until the
// journal's surfaces move to entries. Entries are never rewritten or removed;
// deleting their project hides them with it.
type JournalEntry struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	// Author is the name the writer gave, free text, or for an entry an
	// agent wrote, the agent's role.
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
