package models

import (
	"slices"
	"time"
)

// The types of entry: short typed records that sit on one project, epic or
// ticket and say why the work is as it is and what happened to it.
const (
	// EntryDecision is what was chosen, why, and what was rejected. It
	// records its source (DecisionSources).
	EntryDecision = "decision"
	// EntryLearning is a gotcha the next agent should know.
	EntryLearning = "learning"
	// EntryHandOff is where the work stopped, and the next step.
	EntryHandOff = "hand_off"
	// EntryProof is what was verified, how, and the result.
	EntryProof = "proof"
	// EntryReview is one review round: its verdict, finding counts by
	// severity and a one-line summary, with the full report in a `Review N`
	// document.
	EntryReview = "review"
	// EntryNote is the person's one-way instruction, open until an agent
	// marks it handled. It may point at another entry, which is how the
	// person challenges that entry.
	EntryNote = "note"
)

// EntryTypes is the single list of valid entry types. The entries table
// stores the type as free text, the way a ticket's status is stored, so
// adding a type is a change to this list and not to the table.
var EntryTypes = []string{EntryDecision, EntryLearning, EntryHandOff, EntryProof, EntryReview, EntryNote}

// TicketOnlyEntryTypes are the entry types that only a ticket can have.
var TicketOnlyEntryTypes = []string{EntryHandOff, EntryProof, EntryReview}

// ValidEntryType reports whether t is one of EntryTypes.
func ValidEntryType(t string) bool {
	return slices.Contains(EntryTypes, t)
}

// TicketOnlyEntryType reports whether t is one of TicketOnlyEntryTypes.
func TicketOnlyEntryType(t string) bool {
	return slices.Contains(TicketOnlyEntryTypes, t)
}

// Where a decision came from. A correction from the person is a decision
// whose source is the person, even when an agent writes it down.
const (
	DecisionSourceAgent  = "agent"
	DecisionSourcePerson = "person"
)

// DecisionSources is the single list of a decision's valid sources.
var DecisionSources = []string{DecisionSourceAgent, DecisionSourcePerson}

// ValidDecisionSource reports whether s is one of DecisionSources.
func ValidDecisionSource(s string) bool {
	return slices.Contains(DecisionSources, s)
}

// A review's verdicts.
const (
	ReviewApprove = "approve"
	ReviewChanges = "changes"
)

// ReviewVerdicts is the single list of a review entry's valid verdicts.
var ReviewVerdicts = []string{ReviewApprove, ReviewChanges}

// ValidReviewVerdict reports whether v is one of ReviewVerdicts.
func ValidReviewVerdict(v string) bool {
	return slices.Contains(ReviewVerdicts, v)
}

// The severities a review's findings are counted by, most severe first.
const (
	SeverityBlocker = "blocker"
	SeverityMajor   = "major"
	SeverityMinor   = "minor"
	SeverityNit     = "nit"
)

// ReviewSeverities is the single list of finding severities. A review
// entry's finding counts are keyed by these, so adding a severity is a
// change to this list and not to the table.
var ReviewSeverities = []string{SeverityBlocker, SeverityMajor, SeverityMinor, SeverityNit}

// ValidReviewSeverity reports whether s is one of ReviewSeverities.
func ValidReviewSeverity(s string) bool {
	return slices.Contains(ReviewSeverities, s)
}

// EntryOwner is what an entry sits on: exactly one of a project, an epic or
// a ticket, by id.
type EntryOwner struct {
	ProjectID string `json:"projectId,omitempty"`
	EpicID    string `json:"epicId,omitempty"`
	TicketID  string `json:"ticketId,omitempty"`
}

// Entry is one entry. Entries are never edited: a later entry on the same
// owner can replace one, which is then kept but left out of reads unless
// they ask for it. The only change an entry ever takes is a note being
// marked handled, once.
type Entry struct {
	ID string `json:"id"`
	EntryOwner
	// Type is one of EntryTypes.
	Type string `json:"type"`
	// Text is the entry itself: one or two sentences, the outcome and the
	// reason. A review's text is its one-line summary.
	Text string `json:"text"`
	// Exactly one of AgentID and AuthorName says who wrote the entry: the
	// agent (its session is the agent's), or else the person, by name.
	// Entries that came from the project journal keep the name their writer
	// gave there.
	AgentID    string `json:"agentId,omitempty"`
	AuthorName string `json:"authorName,omitempty"`
	// Source is where a decision came from, one of DecisionSources; empty
	// for every other type.
	Source string `json:"source,omitempty"`
	// Replaces is the entry this one replaces, and ReplacedBy the entry that
	// replaced this one, when there is one.
	Replaces   string `json:"replaces,omitempty"`
	ReplacedBy string `json:"replacedBy,omitempty"`
	// About is the entry a note points at: the entry the person challenges.
	About string `json:"about,omitempty"`
	// HandledBy is the agent that marked a note handled, and HandledAt when;
	// both empty while the note is open, and for every other type.
	HandledBy string     `json:"handledBy,omitempty"`
	HandledAt *time.Time `json:"handledAt,omitempty"`
	// A review's verdict (one of ReviewVerdicts), its finding counts keyed
	// by every one of ReviewSeverities, and the name of the ticket's
	// document holding the full report, when it names one.
	Verdict        string         `json:"verdict,omitempty"`
	Findings       map[string]int `json:"findings,omitempty"`
	ReportDocument string         `json:"reportDocument,omitempty"`
	CreatedAt      time.Time      `json:"createdAt"`
}

// Open reports whether the entry is a note no agent has marked handled and
// no later note has replaced: a revised note is open once, as its newest
// version.
func (e Entry) Open() bool {
	return e.Type == EntryNote && e.HandledAt == nil && e.ReplacedBy == ""
}

// CreateEntryRequest is a new entry. The owner is given by id, or a project
// by its prefix and a ticket by its display key. Each type-specific field is
// for its type only: Source for a decision, About for a note, and Verdict,
// Findings and ReportDocument for a review. Findings leaves out severities
// with no findings.
type CreateEntryRequest struct {
	EntryOwner
	Type           string         `json:"type"`
	Text           string         `json:"text"`
	AgentID        string         `json:"agentId,omitempty"`
	AuthorName     string         `json:"authorName,omitempty"`
	Source         string         `json:"source,omitempty"`
	Replaces       string         `json:"replaces,omitempty"`
	About          string         `json:"about,omitempty"`
	Verdict        string         `json:"verdict,omitempty"`
	Findings       map[string]int `json:"findings,omitempty"`
	ReportDocument string         `json:"reportDocument,omitempty"`
}

// EntryFilter narrows a read of one owner's entries. Replaced entries are
// left out unless IncludeReplaced is set; Types, when not empty, keeps only
// entries of those types.
type EntryFilter struct {
	IncludeReplaced bool
	Types           []string
}

// EntryPage is a run of one owner's entries, newest first. When HasMore is
// true, the next page is the one read with before set to NextBefore, the id
// of this page's oldest entry.
type EntryPage struct {
	Entries []Entry `json:"entries"`
	// Total is how many entries the read matches in all.
	Total      int    `json:"total"`
	HasMore    bool   `json:"hasMore"`
	NextBefore string `json:"nextBefore,omitempty"`
}

// TicketEntries is what a read of one ticket carries about entries, beside
// the ticket: the first page of its own current entries, how many of its
// notes are open, and how many notes are open on its epic and on its
// project, counted but not read. Every read of a ticket pays for these, so
// each is left out when there are none. It is kept apart from Ticket so
// that lists of tickets never pay for it.
type TicketEntries struct {
	// Entries is nil when the ticket has no current entries.
	Entries          *EntryPage `json:"entries,omitempty"`
	OpenNotes        int        `json:"openNotes,omitempty"`
	EpicOpenNotes    int        `json:"epicOpenNotes,omitempty"`
	ProjectOpenNotes int        `json:"projectOpenNotes,omitempty"`
}
