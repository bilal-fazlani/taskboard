package models

// ActivityEntry is one status change in a project's activity feed: the
// change itself, with enough of its ticket to show and open it without a
// second fetch. The ticket's title and epic are as they are now, not as they
// were when the change was made.
type ActivityEntry struct {
	StatusChange
	TicketKey   string   `json:"ticketKey"`
	TicketTitle string   `json:"ticketTitle"`
	Epic        *EpicRef `json:"epic,omitempty"`
}

// ActivityPage is a run of a project's activity, newest first. When HasMore
// is true, the next page is the one read with Before set to NextBefore: an
// opaque position just after this page's oldest entry. Unlike EntryPage's,
// it is not an entry id, so it stays valid when that entry is deleted with
// its ticket.
type ActivityPage struct {
	Entries    []ActivityEntry `json:"entries"`
	HasMore    bool            `json:"hasMore"`
	NextBefore string          `json:"nextBefore,omitempty"`
}
