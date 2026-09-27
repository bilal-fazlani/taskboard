package models

import (
	"slices"
	"time"
)

// The types of user input an agent can ask the person for. Whatever the
// type, a ticket waiting on one is in StatusNeedsUserInput.
const (
	// UserInputApproval asks the person to approve the work so far.
	UserInputApproval = "approval"
	// UserInputQuestion asks the person a question.
	UserInputQuestion = "question"
)

// UserInputTypes is the single list of valid types of user input. The
// ticket_requests table stores the type as free text, the way a ticket's
// status is stored, so adding a type is a change to this list and not to
// the table.
var UserInputTypes = []string{UserInputApproval, UserInputQuestion}

// ValidUserInputType reports whether t is one of UserInputTypes.
func ValidUserInputType(t string) bool {
	return slices.Contains(UserInputTypes, t)
}

// TicketRequest is an agent's request for user input on a ticket. A ticket
// has at most one unanswered request. Answer, AnsweredBy and AnsweredAt are
// set together, once the person answers.
type TicketRequest struct {
	ID       string `json:"id"`
	TicketID string `json:"ticketId"`
	// AgentID is the agent that made the request, and collects its answer.
	// After a takeover the ticket's holder is another agent, so this is the
	// only record of who asked.
	AgentID string `json:"agentId"`
	// Type is the type of user input, one of UserInputTypes.
	Type   string `json:"type"`
	Prompt string `json:"prompt"`
	// Choices are the answers offered, empty when the answer is free.
	Choices []string `json:"choices"`
	Answer  string   `json:"answer,omitempty"`
	// AnsweredBy is who answered: the local person today, the account once
	// there are teams.
	AnsweredBy string     `json:"answeredBy,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	AnsweredAt *time.Time `json:"answeredAt,omitempty"`
}

// Answered reports whether the request has been answered.
func (r TicketRequest) Answered() bool {
	return r.AnsweredAt != nil
}
