package models

import (
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
