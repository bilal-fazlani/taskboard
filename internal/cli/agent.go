package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tcarac/taskboard/internal/models"
)

// agentCommands returns the `agent` command group: identify a new agent in
// a session, and list every agent with its held tickets.
func agentCommands() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Identify agents in a session, and list them",
	}

	var req models.IdentifyAgentRequest
	var identifyJSON bool
	identifyCmd := &cobra.Command{
		Use:   "identify",
		Short: "Identify a new agent, in the session named by --vendor and --session-id",
		Long: "Identify a new agent: an agent has no name of its own, so every call creates one, always in " +
			"the session named by --vendor and --session-id, found or created. A session found keeps the " +
			"--machine, --resume-command and --web-url it was created with; a resumed chat should pass the " +
			"same --vendor and --session-id every time so it keeps its session, and with it its tickets.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			a, err := store.IdentifyAgent(req)
			if err != nil {
				return err
			}
			if identifyJSON {
				return encodeJSON(cmd.OutOrStdout(), a)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Identified agent %s: %s on %s (%s), session %s (%s)\n",
				a.ID, a.Role, a.Model, a.Provider, a.SessionID, req.Vendor)
			return nil
		},
	}
	identifyCmd.Flags().StringVar(&req.Vendor, "vendor", "", "the tool the session runs in, such as claude_code or codex (required)")
	identifyCmd.Flags().StringVar(&req.VendorSessionID, "session-id", "", "the session's ID in its tool (required)")
	identifyCmd.Flags().StringVar(&req.Machine, "machine", "", "the machine the session runs on")
	identifyCmd.Flags().StringVar(&req.ResumeCommand, "resume-command", "", "the command that resumes the session, such as \"claude --resume <id>\"")
	identifyCmd.Flags().StringVar(&req.WebURL, "web-url", "", "the session's page on the vendor's site, if it has one")
	identifyCmd.Flags().StringVar(&req.Role, "role", "",
		"what the agent does in its session, such as orchestrator, implementer or reviewer (required)")
	identifyCmd.Flags().StringVar(&req.Model, "model", "", "the model the agent runs on (required)")
	identifyCmd.Flags().StringVar(&req.Provider, "provider", "",
		"the model's provider: "+strings.Join(models.Providers, ", ")+" (required)")
	identifyCmd.Flags().BoolVar(&identifyJSON, "json", false, "print the agent as JSON instead of readable text")
	_ = identifyCmd.MarkFlagRequired("vendor")
	_ = identifyCmd.MarkFlagRequired("session-id")
	_ = identifyCmd.MarkFlagRequired("role")
	_ = identifyCmd.MarkFlagRequired("model")
	_ = identifyCmd.MarkFlagRequired("provider")

	var listJSON bool
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List agents with their last seen, stale state and the tickets each holds",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			agents, err := store.ListAgents()
			if err != nil {
				return err
			}
			if listJSON {
				return encodeJSON(cmd.OutOrStdout(), agents)
			}
			fmt.Fprint(cmd.OutOrStdout(), formatAgents(agents))
			return nil
		},
	}
	listCmd.Flags().BoolVar(&listJSON, "json", false, "print the agents as JSON instead of readable text")

	cmd.AddCommand(identifyCmd, listCmd)
	return cmd
}

// encodeJSON writes v to w as indented JSON, the way every command's --json
// flag prints its result.
func encodeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// formatAgents renders `agent list`'s agents, most recently seen first, one
// line each with its session (vendor and resume command, the session's
// link, not its ULID) and the tickets it holds indented under it.
func formatAgents(agents []models.AgentListItem) string {
	var b strings.Builder
	if len(agents) == 0 {
		return "No agents.\n"
	}
	fmt.Fprintf(&b, "Agents (%d):\n", len(agents))
	for _, a := range agents {
		state := "live"
		if a.Stale {
			state = "stale"
		}
		session := a.Session.Vendor
		if a.Session.ResumeCommand != "" {
			session += ": " + a.Session.ResumeCommand
		}
		fmt.Fprintf(&b, "  %s  %s/%s/%s  %s, last seen %s  (%s)\n",
			a.ID, a.Role, a.Model, a.Provider, state, a.LastSeenAt.Local().Format("2006-01-02 15:04"), session)
		if len(a.HeldTickets) == 0 {
			continue
		}
		keys := make([]string, len(a.HeldTickets))
		for i, t := range a.HeldTickets {
			keys[i] = t.Key
		}
		fmt.Fprintf(&b, "      holding: %s\n", strings.Join(keys, ", "))
	}
	return b.String()
}
