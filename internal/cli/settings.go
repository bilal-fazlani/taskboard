package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/tcarac/taskboard/internal/models"
)

// The install's settings, by the names `taskboard settings` takes.
const (
	settingStaleAfter = "stale-after"
	settingLease      = "lease"
)

var settingNames = []string{settingStaleAfter, settingLease}

// settingsCommands reads and changes the install's settings. They live in
// the database, so every process that opens it (the web server, each MCP
// server, the CLI) uses the same values.
func settingsCommands() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "settings",
		Short: "Show or change the install's settings",
		Long: "Show or change the install's settings, one value each for the whole install:\n\n" +
			"  stale-after  how long an agent can go unseen before it is stale and another agent's start takes its ticket over (default 30m)\n" +
			"  lease        how long an agent can go unseen before the ticket it holds is given back to todo (default 2h)",
	}

	getCmd := &cobra.Command{
		Use:   "get",
		Short: "Show the install's settings",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			settings, err := store.AgentSettings()
			if err != nil {
				return err
			}
			printSettings(cmd, settings)
			return nil
		},
	}

	setCmd := &cobra.Command{
		Use:   "set <name> <duration>",
		Short: "Change one of the install's settings: stale-after or lease, e.g. settings set stale-after 45m",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := strings.TrimSpace(args[0])
			d, err := time.ParseDuration(strings.TrimSpace(args[1]))
			if err != nil {
				return fmt.Errorf("%s: %q is not a duration: use one such as 45m or 2h", name, args[1])
			}
			var req models.UpdateAgentSettingsRequest
			switch name {
			case settingStaleAfter:
				req.StaleAfter = &d
			case settingLease:
				req.Lease = &d
			default:
				return fmt.Errorf("%q is not a setting: use one of %s", name, strings.Join(settingNames, ", "))
			}
			store, err := openStore()
			if err != nil {
				return err
			}
			settings, err := store.UpdateAgentSettings(req)
			if err != nil {
				return err
			}
			printSettings(cmd, settings)
			return nil
		},
	}

	cmd.AddCommand(getCmd, setCmd)
	return cmd
}

func printSettings(cmd *cobra.Command, settings models.AgentSettings) {
	fmt.Fprintf(cmd.OutOrStdout(), "%-12s %s\n", settingStaleAfter, models.FormatDuration(settings.StaleAfter))
	fmt.Fprintf(cmd.OutOrStdout(), "%-12s %s\n", settingLease, models.FormatDuration(settings.Lease))
}
