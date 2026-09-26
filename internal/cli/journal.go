package cli

import (
	"fmt"
	"io"
	"os"
	"os/user"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tcarac/taskboard/internal/db"
	"github.com/tcarac/taskboard/internal/models"
)

// journalCommands returns the `project journal` command group: append an
// entry, and list entries newest first a page at a time. Entries are never
// edited or deleted, so there is nothing else.
func journalCommands() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "journal",
		Short: "Append to and read a project's journal, a dated log that is never rewritten",
	}

	var author string
	appendCmd := &cobra.Command{
		Use:   "append [id-or-prefix] [text]",
		Short: "Append an entry to a project's journal; text \"-\" reads it from standard input",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			text := args[1]
			if text == "-" {
				data, err := io.ReadAll(cmd.InOrStdin())
				if err != nil {
					return fmt.Errorf("reading standard input: %w", err)
				}
				text = string(data)
			}
			if strings.TrimSpace(author) == "" {
				author = defaultJournalAuthor()
			}
			store, err := openStore()
			if err != nil {
				return err
			}
			projectID, err := store.ResolveProjectRef(args[0])
			if err != nil {
				return err
			}
			e, err := store.AppendJournalEntry(projectID, models.AppendJournalEntryRequest{Author: author, Text: text})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Appended to the journal as %s at %s (%s)\n",
				e.Author, e.CreatedAt.Local().Format("2006-01-02 15:04"), e.ID)
			return nil
		},
	}
	appendCmd.Flags().StringVar(&author, "author", "",
		"who writes the entry (default: your user name)")

	var before string
	var limit int
	listCmd := &cobra.Command{
		Use:   "list [id-or-prefix]",
		Short: "List a project's journal entries, newest first",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			projectID, err := store.ResolveProjectRef(args[0])
			if err != nil {
				return err
			}
			page, err := store.ListJournal(projectID, before, limit)
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), formatJournal(args[0], before, page))
			return nil
		},
	}
	listCmd.Flags().StringVar(&before, "before", "", "start after this entry: the id the previous page names")
	listCmd.Flags().IntVar(&limit, "limit", db.JournalDefaultLimit,
		fmt.Sprintf("at most this many entries (1 to %d)", db.JournalMaxLimit))

	cmd.AddCommand(appendCmd, listCmd)
	return cmd
}

// defaultJournalAuthor is the author of an entry appended without --author:
// the name of the user running the command.
func defaultJournalAuthor() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return os.Getenv("USER")
}

// formatJournal renders one page of a journal, each entry's time and author
// on a line and its text indented under it, then the command for the next
// page when there is one. ref is the project as the caller named it.
func formatJournal(ref, before string, page models.JournalPage) string {
	var b strings.Builder
	if len(page.Entries) == 0 {
		if before != "" {
			fmt.Fprintf(&b, "No older journal entries for %s.\n", ref)
		} else {
			fmt.Fprintf(&b, "No journal entries for %s.\n", ref)
		}
		return b.String()
	}
	fmt.Fprintf(&b, "Journal for %s, newest first (%d of %d):\n", ref, len(page.Entries), page.Total)
	for _, e := range page.Entries {
		fmt.Fprintf(&b, "  %s  %s\n", e.CreatedAt.Local().Format("2006-01-02 15:04"), e.Author)
		for _, line := range strings.Split(e.Text, "\n") {
			if line == "" {
				b.WriteString("\n")
				continue
			}
			fmt.Fprintf(&b, "      %s\n", line)
		}
	}
	if page.HasMore {
		fmt.Fprintf(&b, "Older entries: taskboard project journal list %s --before %s\n", ref, page.NextBefore)
	}
	return b.String()
}
