package db

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/tcarac/taskboard/internal/models"
)

// EntryAgents reads the agents named on entries, each with its session: the
// agent that wrote each entry and the agent that handled each note. It
// answers keyed by agent id, and with an empty map when the entries name no
// agent.
func (s *Store) EntryAgents(entries []models.Entry) (map[string]models.EntryAgent, error) {
	out := map[string]models.EntryAgent{}
	var ids []string
	for _, e := range entries {
		for _, id := range []string{e.AgentID, e.HandledBy} {
			if id != "" {
				ids = append(ids, id)
			}
		}
	}
	ids = slices.Compact(slices.Sorted(slices.Values(ids)))
	if len(ids) == 0 {
		return out, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	// Each agent is read the way GetAgent reads one (scanAgent, then
	// markStale with the install's settings), so its session's last seen and
	// whether it is stale agree with every other read of it.
	settings, err := s.AgentSettings()
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT s.id, s.vendor, s.vendor_session_id, s.machine, s.resume_command, s.web_url, s.created_at,
			`+agentColumns+`
		FROM agents a JOIN sessions s ON s.id = a.session_id
		WHERE a.id IN (?`+strings.Repeat(`, ?`, len(ids)-1)+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("reading entry agents: %w", err)
	}
	defer rows.Close()
	now := time.Now()
	for rows.Next() {
		var session models.Session
		agent, err := scanAgent(rows, &session.ID, &session.Vendor, &session.VendorSessionID, &session.Machine,
			&session.ResumeCommand, &session.WebURL, &session.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("reading entry agents: %w", err)
		}
		markStale(settings, &agent, now)
		out[agent.ID] = models.EntryAgent{Agent: agent, Session: session}
	}
	return out, rows.Err()
}
