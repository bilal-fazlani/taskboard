package models

import (
	"fmt"
	"slices"
	"time"
)

// Session is one conversation in a vendor's tool, such as a Claude Code or
// Codex chat. A resumed chat keeps its session, so the vendor and the
// vendor's own session ID together name one session.
type Session struct {
	ID string `json:"id"`
	// Vendor is the tool's vendor, and VendorSessionID the session's ID in
	// that tool.
	Vendor          string `json:"vendor"`
	VendorSessionID string `json:"vendorSessionId"`
	// Machine is the machine the session ran on.
	Machine string `json:"machine"`
	// ResumeCommand is the command that resumes the session, such as
	// `claude --resume <id>`.
	ResumeCommand string `json:"resumeCommand"`
	// WebURL is the session's page on the vendor's site, when it has one.
	WebURL    string    `json:"webUrl,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// Agent is one worker in a session: the main agent, or a subagent it
// launched. It lives as long as its process and is not a lasting identity;
// several agents can share one session. The agent holding a ticket is one
// of these.
type Agent struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionId"`
	// Role is what the agent does in its session, such as orchestrator,
	// implementer or reviewer.
	Role string `json:"role"`
	// Model is the model the agent runs on, and Provider the model's
	// provider, one of Providers.
	Model      string    `json:"model"`
	Provider   string    `json:"provider"`
	CreatedAt  time.Time `json:"createdAt"`
	LastSeenAt time.Time `json:"lastSeenAt"`
	// SessionLastSeenAt is when any agent of the agent's session was last
	// seen: the latest LastSeenAt among them. A session owns the tickets its
	// agents hold, so this is what says whether the work is still alive.
	SessionLastSeenAt time.Time `json:"sessionLastSeenAt"`
	// Stale is whether the agent's session has gone unseen, no agent of it
	// seen, for longer than the stale threshold (AgentSettings.StaleAfter) as
	// of the read. It is never stored: the store works it out on every read,
	// the same way it decides whether a claim may take a ticket over, so the
	// two always agree.
	Stale bool `json:"stale"`
}

// AgentListItem is one agent as GET /api/agents lists it, or GET
// /api/agents/{id} answers it: the agent with its session (EntryAgent,
// which entries already use to name their author), and the tickets it
// currently holds (by key, title and status only, the same shape a
// dependency link uses).
type AgentListItem struct {
	EntryAgent
	HeldTickets []TicketRef `json:"heldTickets"`
}

// IdentifyAgentRequest names a session and a new agent in it. The session is
// found by its vendor and the vendor's session ID, or created with the
// machine, resume command and web link given here; a session found keeps the
// link it was created with. The agent is always new: agents have no name to
// find them by, and each process that identifies is its own agent.
type IdentifyAgentRequest struct {
	Vendor          string `json:"vendor"`
	VendorSessionID string `json:"vendorSessionId"`
	Machine         string `json:"machine,omitempty"`
	ResumeCommand   string `json:"resumeCommand,omitempty"`
	WebURL          string `json:"webUrl,omitempty"`
	Role            string `json:"role"`
	Model           string `json:"model"`
	// Provider is one of Providers.
	Provider string `json:"provider"`
}

// AgentSettings are the install's two agent timings, one of each for the
// whole install. They live in the database (the agent_settings table), so
// every process that opens it reads the same values.
type AgentSettings struct {
	// StaleAfter is how long an agent can go unseen before it is stale: its
	// ticket shows as stale, and another agent's claim takes it over.
	StaleAfter time.Duration
	// Lease is how long an agent can go unseen before the ticket it holds is
	// given back to todo.
	Lease time.Duration
}

// The default agent timings, which a new database starts with.
const (
	DefaultStaleAfter = 30 * time.Minute
	DefaultLease      = 2 * time.Hour
)

// DefaultAgentSettings are the timings an install has unless it sets its own.
func DefaultAgentSettings() AgentSettings {
	return AgentSettings{StaleAfter: DefaultStaleAfter, Lease: DefaultLease}
}

// UpdateAgentSettingsRequest changes the agent timings it sets and leaves
// the rest alone. Each is a positive duration, stored to the second.
type UpdateAgentSettingsRequest struct {
	StaleAfter *time.Duration
	Lease      *time.Duration
}

// FormatDuration writes d the way a person says it: 30m, 2h, 1h30m, 45s.
func FormatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	switch {
	case d >= time.Hour && d%time.Hour == 0:
		return fmt.Sprintf("%dh", d/time.Hour)
	case d >= time.Hour && d%time.Minute == 0:
		return fmt.Sprintf("%dh%dm", d/time.Hour, (d%time.Hour)/time.Minute)
	case d >= time.Minute && d%time.Minute == 0:
		return fmt.Sprintf("%dm", d/time.Minute)
	}
	return d.String()
}

// Claim is what claiming a ticket did: the ticket as it now stands, held by
// the claiming agent, and, when the claim took the ticket over from a stale
// agent, that agent and the ticket's latest current hand-off, whoever wrote
// it, if there is one.
type Claim struct {
	Ticket    *Ticket `json:"ticket"`
	TakenFrom *Agent  `json:"takenFrom,omitempty"`
	HandOff   *Entry  `json:"handOff,omitempty"`
}

// How an agent releases the ticket it holds.
const (
	// ReleaseGiveBack returns the ticket to todo for another agent, with a
	// hand-off: where the work stopped and the next step.
	ReleaseGiveBack = "give_back"
	// ReleaseFinish moves the ticket to done, with its proof: what was
	// verified, how, and the result.
	ReleaseFinish = "finish"
)

// ReleaseOutcomes is the single list of the ways to release a ticket.
var ReleaseOutcomes = []string{ReleaseGiveBack, ReleaseFinish}

// ReleaseTicketRequest is an agent releasing the ticket it holds. Giving it
// back takes HandOff and finishing takes Proof; each is written as an entry
// of its type (EntryHandOff, EntryProof) on the ticket, by the agent.
type ReleaseTicketRequest struct {
	AgentID string `json:"agentId"`
	// Outcome is one of ReleaseOutcomes.
	Outcome string `json:"outcome"`
	HandOff string `json:"handOff,omitempty"`
	Proof   string `json:"proof,omitempty"`
}

// The providers of an agent's model. Other covers every provider not named.
const (
	ProviderAnthropic = "anthropic"
	ProviderOpenAI    = "openai"
	ProviderGoogle    = "google"
	ProviderOther     = "other"
)

// Providers is the single list of valid providers. The agents table stores
// the provider as free text, the way a ticket's status is stored, so adding
// a provider is a change to this list and not to the table.
var Providers = []string{ProviderAnthropic, ProviderOpenAI, ProviderGoogle, ProviderOther}

// ValidProvider reports whether provider is one of Providers.
func ValidProvider(provider string) bool {
	return slices.Contains(Providers, provider)
}
