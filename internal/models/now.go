package models

import "time"

// Now is what is moving right now across the board, for the web UI's Now
// page: every ticket in progress, waiting on the person or in review, and
// the tickets that landed in the last day. It is one answer rather than a
// ticket list plus a read per ticket, and it carries only what the page
// shows.
type Now struct {
	InProgress []NowTicket `json:"inProgress"`
	// Waiting are the tickets in needs_user_input: an agent asked the person
	// for user input and waits on the answer.
	Waiting  []NowTicket    `json:"waiting"`
	InReview []NowTicket    `json:"inReview"`
	Landed   []LandedTicket `json:"landed"`
}

// The review states of a ticket in agent_review, from the verdict of its
// latest `Review <round>` document.
const (
	// NowReviewRunning: no review document is newer than the ticket's
	// latest entry into agent_review, so its reviewer is still at work.
	NowReviewRunning = "running"
	// NowReviewApproved: the latest review says VERDICT: APPROVE and is
	// newer than that entry, so it waits on the person to land it.
	NowReviewApproved = "approved"
	// NowReviewChanges: the latest review says VERDICT: CHANGES and is
	// newer than that entry, so it is about to go back to its implementer.
	NowReviewChanges = "changes"
)

// NowTicket is a ticket in progress, waiting on the person or in review, as
// the Now page shows it.
type NowTicket struct {
	ID            string `json:"id"`
	Key           string `json:"key"`
	Title         string `json:"title"`
	Status        string `json:"status"`
	ProjectPrefix string `json:"projectPrefix"`
	SubtasksDone  int    `json:"subtasksDone"`
	SubtasksTotal int    `json:"subtasksTotal"`
	// ReviewRounds counts the ticket's entries into agent_review.
	ReviewRounds int `json:"reviewRounds"`
	// Since is when the ticket last entered its current status, or when it
	// was created if its history has no such entry (it got there before the
	// history began).
	Since time.Time `json:"since"`
	// Review is one of the NowReview states, set only on a ticket in
	// agent_review.
	Review string `json:"review,omitempty"`
}

// LandedTicket is a ticket that recently moved to done, with the commits it
// landed as.
type LandedTicket struct {
	ID            string `json:"id"`
	Key           string `json:"key"`
	Title         string `json:"title"`
	ProjectPrefix string `json:"projectPrefix"`
	// DoneAt is when the ticket last moved to done.
	DoneAt time.Time `json:"doneAt"`
	// Commits are its landed commits in the order they were given; empty
	// when none were recorded.
	Commits []LandedCommit `json:"commits"`
}
