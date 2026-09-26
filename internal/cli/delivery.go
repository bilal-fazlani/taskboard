package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tcarac/taskboard/internal/models"
	"github.com/tcarac/taskboard/internal/weburl"
)

// deliveryFlagsHelp is the part of `ticket update`'s help about its delivery
// flags; the rules are models.DeliveryUpdate's, shared with HTTP and MCP.
const deliveryFlagsHelp = "--branch, --worktree, --pr-url and --landed-commit set where the work " +
	"lives and where it landed. Omit one to leave it alone; pass it with an empty value to clear it. " +
	"--landed-commit replaces the whole list, in the order given: each is a sha (7 to 64 hex " +
	"characters, full or short) with the repo it landed in in front, as repo@sha " +
	"(acme/billing-api@6bafa19). A bare sha takes the ticket's repo when it has exactly one. " +
	"'ticket find-by-commit' finds a ticket by one of its landed commits."

// deliveryUpdateFromFlags builds the delivery part of `ticket update` from
// the flags that were passed, or nil when none was.
func deliveryUpdateFromFlags(cmd *cobra.Command, branch, worktree, prURL string, commits []string) (*models.DeliveryUpdate, error) {
	var d models.DeliveryUpdate
	set := false
	if cmd.Flags().Changed("branch") {
		d.Branch, set = &branch, true
	}
	if cmd.Flags().Changed("worktree") {
		d.Worktree, set = &worktree, true
	}
	if cmd.Flags().Changed("pr-url") {
		d.PRURL, set = &prURL, true
	}
	if cmd.Flags().Changed("landed-commit") {
		// Like --repo, the flag passed with an empty value clears the list.
		landed := []models.LandedCommit{}
		for _, c := range commits {
			if strings.TrimSpace(c) == "" {
				continue
			}
			landed = append(landed, parseLandedCommit(c))
		}
		d.LandedCommits, set = &landed, true
	}
	if !set {
		return nil, nil
	}
	return &d, nil
}

// parseLandedCommit reads a --landed-commit value, repo@sha or a bare sha. It
// splits at the last @, since a sha never has one; the store checks the sha.
func parseLandedCommit(value string) models.LandedCommit {
	if i := strings.LastIndex(value, "@"); i >= 0 {
		return models.LandedCommit{Repo: value[:i], SHA: value[i+1:]}
	}
	return models.LandedCommit{SHA: value}
}

// formatDelivery renders a ticket's delivery for `ticket get`, one field per
// line and each landed commit on its own line as repo@sha; nothing when none
// is set.
func formatDelivery(d *models.Delivery) string {
	if d == nil {
		return ""
	}
	var b strings.Builder
	if d.Branch != "" {
		fmt.Fprintf(&b, "Branch: %s\n", d.Branch)
	}
	if d.Worktree != "" {
		fmt.Fprintf(&b, "Worktree: %s\n", d.Worktree)
	}
	if d.PRURL != "" {
		fmt.Fprintf(&b, "PR: %s\n", d.PRURL)
	}
	if len(d.LandedCommits) > 0 {
		fmt.Fprintln(&b, "Landed commits:")
		for _, c := range d.LandedCommits {
			fmt.Fprintf(&b, "  %s@%s\n", c.Repo, c.SHA)
		}
	}
	return b.String()
}

// findByCommitCommand is `ticket find-by-commit`: the tickets that landed a
// commit, by its full or short sha.
func findByCommitCommand() *cobra.Command {
	var repo string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "find-by-commit [sha]",
		Short: "Find the tickets that landed a commit, by its full or short sha",
		Long: "Find the tickets that landed a commit, from the landed commits set with " +
			"'ticket update --landed-commit'. The sha may be full or short (7 to 64 hex characters, any " +
			"case): a landed commit matches when either sha starts with the other. Prints each ticket " +
			"with its link and the commits of it that matched, or says that no ticket landed it.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			found, err := store.FindTicketsByCommit(args[0], repo)
			if err != nil {
				return err
			}
			weburl.FillCommitTickets(found)
			out := cmd.OutOrStdout()
			if asJSON {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(found)
			}
			if len(found) == 0 {
				fmt.Fprintf(out, "No ticket landed %s.\n", strings.TrimSpace(args[0]))
				return nil
			}
			for _, f := range found {
				fmt.Fprintf(out, "[%s] %s - %s\n  %s  (%s)\n", f.Key, f.Title, f.Status, f.URL, f.ID)
				for _, c := range f.Commits {
					fmt.Fprintf(out, "  landed %s@%s\n", c.Repo, c.SHA)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&repo, "repo", "", "only commits landed in this repo, matched exactly")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the tickets as JSON instead of readable text")
	return cmd
}
