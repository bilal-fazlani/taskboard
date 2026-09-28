package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/tcarac/taskboard/internal/models"
	"github.com/tcarac/taskboard/internal/weburl"
)

// The agent protocol over MCP: an agent identifies once, starts a ticket
// (claiming it and reading everything needed to begin, in one call), asks
// the person for user input and collects the answer, and releases the ticket
// it holds with its record. No tool claims on its own: claiming is part of
// start_ticket. Subagents share their parent's MCP connection, so the server
// remembers no agent per connection: every tool but identify_agent takes the
// calling agent's id, and each call touches that agent.

// How long await_answer waits when asked to, and at most. The default stays
// under the shortest tool-call timeout MCP clients commonly set (60s), so a
// plain call never times out on the client's side; a longer wait is capped
// rather than refused.
const (
	awaitDefaultTimeout = 50 * time.Second
	awaitMaxTimeout     = 10 * time.Minute
)

// identifyArgs is identify_agent's arguments: the session, then the agent.
type identifyArgs struct {
	models.IdentifyAgentRequest
}

// startArgs is start_ticket's arguments.
type startArgs struct {
	Ticket  string `json:"ticket"`
	AgentID string `json:"agentId"`
}

// releaseArgs is release_ticket's arguments.
type releaseArgs struct {
	Ticket string `json:"ticket"`
	models.ReleaseTicketRequest
}

// requestInputArgs is request_user_input's arguments.
type requestInputArgs struct {
	Ticket  string   `json:"ticket"`
	AgentID string   `json:"agentId"`
	Type    string   `json:"type"`
	Prompt  string   `json:"prompt"`
	Choices []string `json:"choices"`
}

// awaitArgs is await_answer's arguments. TimeoutSeconds is a pointer so
// that 0, a check that does not wait, differs from leaving it out.
type awaitArgs struct {
	Request        string `json:"request"`
	AgentID        string `json:"agentId"`
	TimeoutSeconds *int   `json:"timeoutSeconds"`
}

// releaseAnswer is release_ticket's short confirmation.
type releaseAnswer struct {
	Key      string `json:"key"`
	Status   string `json:"status"`
	Released bool   `json:"released"`
}

// awaitAnswerResult is await_answer's answer: the request, answered or not.
type awaitAnswerResult struct {
	ID         string `json:"id"`
	Answered   bool   `json:"answered"`
	Answer     string `json:"answer,omitempty"`
	AnsweredBy string `json:"answeredBy,omitempty"`
}

// agentIDProp is the agentId argument of every tool an agent calls as
// itself.
var agentIDProp = schemaProp{Type: "string", Description: "Your agentId, from identify_agent."}

// agentToolDefs are the agent protocol's tools, listed after the entry
// tools.
var agentToolDefs = [5]toolDef{
	{
		Name: "identify_agent",
		Description: "Call once when you start, before any tool that takes agentId: names your session and you as one agent " +
			"in it, and answers {agentId}; pass it as agentId on every call that takes one. A subagent identifies itself " +
			"with its parent's vendor and vendorSessionId; a resumed chat identifies again with the same ones and keeps " +
			"its session's tickets.",
		InputSchema: jsonSchema{
			Type: "object",
			Properties: map[string]schemaProp{
				"vendor":          {Type: "string", Description: "The tool the session runs in, e.g. claude_code or codex."},
				"vendorSessionId": {Type: "string", Description: "The session's ID in that tool."},
				"resumeCommand":   {Type: "string", Description: "The command that resumes the session, e.g. claude --resume <id>; the ticket page's session chip copies it, so pass it when your tool has one."},
				"machine":         {Type: "string", Description: "The machine the session runs on; defaults to this server's host name."},
				"webUrl":          {Type: "string", Description: "The session's page on the vendor's site, if it has one."},
				"role":            {Type: "string", Description: "What you do in the session, e.g. orchestrator, implementer or reviewer."},
				"model":           {Type: "string", Description: "The model you run on."},
				"provider":        {Type: "string", Description: "Your model's provider.", Enum: models.Providers},
			},
			Required: []string{"vendor", "vendorSessionId", "role", "model", "provider"},
		},
	},
	{
		Name: "start_ticket",
		Description: "How you begin work on a ticket: one call claims it for you and answers everything needed to begin, " +
			"so read nothing else first. Answers {ticket, project: {name, description, agentInstructions, entries}} and, " +
			"each only when there is one: takenFrom; handOff, where the work stands; entries, the ticket's other current " +
			"entries; notes, the person's open notes on the ticket, its epic and its project (act on each, then handle_note); " +
			"epic {description, documents, entries}; unfinishedDependencies, keys that never block you (decide whether to go " +
			"on). project.entries is the newest page of the project's entries: older ones with list_entries, project and " +
			"before: nextBefore. Refused while an agent of another session holds the ticket and is live: the error names it " +
			"and when its session was last seen; leave the ticket to it. Once that session has gone stale, the start takes " +
			"the ticket over: takenFrom names the agent it was taken from and handOff says where its work stood; carry on " +
			"from there. Starting a ticket your session holds continues it.",
		InputSchema: jsonSchema{
			Type: "object",
			Properties: map[string]schemaProp{
				"ticket":  {Type: "string", Description: ticketIDDescription},
				"agentId": agentIDProp,
			},
			Required: []string{"ticket", "agentId"},
		},
	},
	{
		Name: "release_ticket",
		Description: "Let go of the ticket you hold, leaving its record: outcome finish with proof (what was verified, how, " +
			"and the result) moves it to done; give_back with handOff (where the work stopped and the next step) returns " +
			"it to todo for the next agent. Refused without that record, while a request waits on the person, or for " +
			"an agent outside the holder's session. Answers {key, status, released: true}.",
		InputSchema: jsonSchema{
			Type: "object",
			Properties: map[string]schemaProp{
				"ticket":  {Type: "string", Description: ticketIDDescription},
				"agentId": agentIDProp,
				"outcome": {Type: "string", Description: "finish or give_back.", Enum: models.ReleaseOutcomes},
				"handOff": {Type: "string", Description: "give_back's: where the work stopped and the next step."},
				"proof":   {Type: "string", Description: "finish's: what was verified, how, and the result."},
			},
			Required: []string{"ticket", "agentId", "outcome"},
		},
	},
	{
		Name: "request_user_input",
		Description: "Ask the person for input you cannot go on without, on a ticket being worked, then collect it with " +
			"await_answer. Answers {id, created: true} at once; the ticket waits in needs_user_input until the person " +
			"answers, one open request per ticket. approval: state exactly what you will do on yes, and act only on that " +
			"approval, as stated. question: ask what you need; choices are optional, and the person can always answer " +
			"in their own words.",
		InputSchema: jsonSchema{
			Type: "object",
			Properties: map[string]schemaProp{
				"ticket":  {Type: "string", Description: ticketIDDescription},
				"agentId": agentIDProp,
				"type":    {Type: "string", Description: "The type of user input.", Enum: models.UserInputTypes},
				"prompt":  {Type: "string", Description: "What you ask; for an approval, exactly what you will do on yes."},
				"choices": {Type: "array", Description: "Answers to offer (optional).", Items: &jsonSchema{Type: "string"}},
			},
			Required: []string{"ticket", "agentId", "type", "prompt"},
		},
	},
	{
		Name: "await_answer",
		Description: "Wait for the answer to a request you made with request_user_input. Answers {id, answered, answer, " +
			"answeredBy} once answered, or {id, answered: false} when the timeout passes: call again to keep waiting. " +
			"Waiting keeps your session live.",
		InputSchema: jsonSchema{
			Type: "object",
			Properties: map[string]schemaProp{
				"request": {Type: "string", Description: "The request's id, from request_user_input."},
				"agentId": agentIDProp,
				"timeoutSeconds": {Type: "integer", Description: "How long to wait: default " +
					strconv.Itoa(int(awaitDefaultTimeout/time.Second)) + ", at most " + strconv.Itoa(int(awaitMaxTimeout/time.Second)) +
					"; 0 checks without waiting. Keep it under your client's tool-call timeout."},
			},
			Required: []string{"request", "agentId"},
		},
	},
}

// callAgentTool handles the agent protocol's tools. ok is false for any
// other name.
func (s *MCPServer) callAgentTool(ctx context.Context, name string, args json.RawMessage) (result any, ok bool, err error) {
	switch name {
	case "identify_agent":
		var a identifyArgs
		if err := decodeArgs(args, &a); err != nil {
			return nil, true, err
		}
		result, err := s.identifyAgent(a)
		return result, true, err
	case "start_ticket":
		var a startArgs
		if err := decodeArgs(args, &a); err != nil {
			return nil, true, err
		}
		result, err := s.startTicket(a)
		return result, true, err
	case "release_ticket":
		var a releaseArgs
		if err := decodeArgs(args, &a); err != nil {
			return nil, true, err
		}
		result, err := s.releaseTicket(a)
		return result, true, err
	case "request_user_input":
		var a requestInputArgs
		if err := decodeArgs(args, &a); err != nil {
			return nil, true, err
		}
		result, err := s.requestUserInput(a)
		return result, true, err
	case "await_answer":
		var a awaitArgs
		if err := decodeArgs(args, &a); err != nil {
			return nil, true, err
		}
		result, err := s.awaitAnswer(ctx, a)
		return result, true, err
	}
	return nil, false, nil
}

func (s *MCPServer) identifyAgent(a identifyArgs) (map[string]string, error) {
	req := a.IdentifyAgentRequest
	// A stdio server runs where its agent does, so its host is the session's
	// machine.
	if strings.TrimSpace(req.Machine) == "" {
		req.Machine, _ = os.Hostname()
	}
	agent, err := s.store.IdentifyAgent(req)
	if err != nil {
		return nil, err
	}
	return map[string]string{"agentId": agent.ID}, nil
}

func (s *MCPServer) startTicket(a startArgs) (*models.Start, error) {
	if strings.TrimSpace(a.Ticket) == "" {
		return nil, errors.New("ticket is required: the ticket to begin work on")
	}
	start, err := s.store.StartTicket(a.Ticket, a.AgentID)
	if err != nil {
		return nil, err
	}
	weburl.Fill(start.Ticket)
	return start, nil
}

func (s *MCPServer) releaseTicket(a releaseArgs) (releaseAnswer, error) {
	if strings.TrimSpace(a.Ticket) == "" {
		return releaseAnswer{}, errors.New("ticket is required: the ticket you hold")
	}
	t, err := s.store.ReleaseTicket(a.Ticket, a.ReleaseTicketRequest)
	if err != nil {
		return releaseAnswer{}, err
	}
	return releaseAnswer{Key: t.DisplayKey(), Status: t.Status, Released: true}, nil
}

func (s *MCPServer) requestUserInput(a requestInputArgs) (map[string]any, error) {
	id, err := s.store.CreateRequest(models.CreateUserInputRequest{
		TicketID: a.Ticket, AgentID: a.AgentID, Type: a.Type, Prompt: a.Prompt, Choices: a.Choices,
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"id": id, "created": true}, nil
}

// awaitAnswer waits on one request, the store touching its asker on every
// poll. The caller must be of the asker's session, since an agent acts only
// on the answer its own session asked for; a resumed chat's new agent
// collects its predecessor's.
func (s *MCPServer) awaitAnswer(ctx context.Context, a awaitArgs) (awaitAnswerResult, error) {
	timeout, err := awaitTimeout(a.TimeoutSeconds)
	if err != nil {
		return awaitAnswerResult{}, err
	}
	requestID := strings.TrimSpace(a.Request)
	if requestID == "" {
		return awaitAnswerResult{}, errors.New("request is required: the id request_user_input answered with")
	}
	if err := s.store.TouchAgent(a.AgentID); err != nil {
		return awaitAnswerResult{}, err
	}
	r, err := s.store.GetRequest(requestID)
	if err != nil {
		return awaitAnswerResult{}, err
	}
	if r == nil {
		return awaitAnswerResult{}, fmt.Errorf("request not found: %q", requestID)
	}
	if err := s.sameSession(strings.TrimSpace(a.AgentID), r); err != nil {
		return awaitAnswerResult{}, err
	}
	r, err = s.store.AwaitAnswer(ctx, requestID, timeout)
	if err != nil {
		return awaitAnswerResult{}, err
	}
	return awaitAnswerResult{ID: r.ID, Answered: r.Answered(), Answer: r.Answer, AnsweredBy: r.AnsweredBy}, nil
}

// awaitTimeout is how long await_answer waits for timeoutSeconds: the
// default when it is left out, and at most awaitMaxTimeout.
func awaitTimeout(seconds *int) (time.Duration, error) {
	if seconds == nil {
		return awaitDefaultTimeout, nil
	}
	if *seconds < 0 {
		return 0, fmt.Errorf("timeoutSeconds is %d: pass 0 to check without waiting, or more to wait", *seconds)
	}
	if *seconds >= int(awaitMaxTimeout/time.Second) {
		return awaitMaxTimeout, nil
	}
	return time.Duration(*seconds) * time.Second, nil
}

// sameSession refuses an agent waiting on a request another session made.
func (s *MCPServer) sameSession(agentID string, r *models.TicketRequest) error {
	if agentID == r.AgentID {
		return nil
	}
	caller, err := s.store.GetAgent(agentID)
	if err != nil {
		return err
	}
	asker, err := s.store.GetAgent(r.AgentID)
	if err != nil {
		return err
	}
	if caller == nil || asker == nil || caller.SessionID != asker.SessionID {
		return fmt.Errorf("request %s was made by agent %s of another session: an agent waits only on its own session's requests",
			r.ID, r.AgentID)
	}
	return nil
}
