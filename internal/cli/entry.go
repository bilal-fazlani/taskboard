package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tcarac/taskboard/internal/db"
	"github.com/tcarac/taskboard/internal/models"
)

// defaultAuthor is the person who writes an entry given no author: the
// user running the command.
func defaultAuthor() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return os.Getenv("USER")
}

// entryOwnerFlags are the flags that name what an entry sits on: --ticket,
// --epic (with --project when it is a name), or --project on its own.
type entryOwnerFlags struct {
	ticket, epic, project string
}

func (f *entryOwnerFlags) add(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.ticket, "ticket", "", "ticket ID or key")
	cmd.Flags().StringVar(&f.epic, "epic", "", "epic ID, or its name with --project")
	cmd.Flags().StringVar(&f.project, "project", "", "project ID or prefix: the project itself, or the epic's project when --epic is a name")
}

var errEntryOwnerFlags = errors.New("pass one of --ticket, --epic (with --project when it is a name) or --project")

// resolve resolves the flags to the owner's id.
func (f entryOwnerFlags) resolve(store *db.Store) (models.EntryOwner, error) {
	switch {
	case f.ticket != "" && (f.epic != "" || f.project != ""):
		return models.EntryOwner{}, errEntryOwnerFlags
	case f.ticket != "":
		id, err := store.ResolveTicketID(f.ticket)
		return models.EntryOwner{TicketID: id}, err
	case f.epic != "":
		id, err := resolveEpicArg(store, f.epic, f.project)
		return models.EntryOwner{EpicID: id}, err
	case f.project != "":
		id, err := store.ResolveProjectRef(f.project)
		return models.EntryOwner{ProjectID: id}, err
	}
	return models.EntryOwner{}, errEntryOwnerFlags
}

// describe names the owner as the flags gave it, for messages.
func (f entryOwnerFlags) describe() string {
	switch {
	case f.ticket != "":
		return f.ticket
	case f.epic != "":
		return "epic " + f.epic
	default:
		return f.project
	}
}

// flags repeats the owner flags, for the command that reads the next page.
func (f entryOwnerFlags) flags() string {
	var parts []string
	for _, p := range [][2]string{{"--ticket", f.ticket}, {"--epic", f.epic}, {"--project", f.project}} {
		if p[1] != "" {
			parts = append(parts, p[0]+" "+strconv.Quote(p[1]))
		}
	}
	return strings.Join(parts, " ")
}

// entryCommands returns the `entry` command group: add an entry, list an
// owner's entries, and mark a note handled. Entries are never edited, so
// there is nothing else.
func entryCommands() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "entry",
		Short: "Write and read entries: short typed records of why the work is as it is, on a ticket, an epic or a project",
		Long: "Entries are short typed records on a ticket, an epic or a project: " + strings.Join(models.EntryTypes, ", ") +
			". hand_off, proof and review go on a ticket only. An entry is one or two sentences, the outcome and the reason. " +
			"Entries are never edited: a new one replaces an old one with --replaces.",
	}

	var addOwner entryOwnerFlags
	var add models.CreateEntryRequest
	var findings []string
	addCmd := &cobra.Command{
		Use:   "add [text]",
		Short: "Add an entry; text \"-\" reads it from standard input",
		Long: "Add an entry on a ticket, an epic or a project. It is written by the agent given with --agent, " +
			"or else by the person: --author, or your user name. A decision takes --source (agent or person); a note " +
			"is the person's and may point at an entry with --about; a review takes --verdict, --finding severity=count " +
			"(repeatable) and --report-document.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			add.Text = args[0]
			if add.Text == "-" {
				data, err := io.ReadAll(cmd.InOrStdin())
				if err != nil {
					return fmt.Errorf("reading standard input: %w", err)
				}
				add.Text = string(data)
			}
			if add.AgentID != "" && add.AuthorName != "" {
				return errors.New("pass --agent or --author, not both")
			}
			if add.AgentID == "" && strings.TrimSpace(add.AuthorName) == "" {
				add.AuthorName = defaultAuthor()
			}
			if len(findings) > 0 {
				add.Findings = map[string]int{}
				for _, f := range findings {
					sev, count, ok := strings.Cut(f, "=")
					n, err := strconv.Atoi(strings.TrimSpace(count))
					if !ok || err != nil {
						return fmt.Errorf("--finding %q: give it as severity=count, e.g. minor=2", f)
					}
					add.Findings[strings.TrimSpace(sev)] = n
				}
			}
			store, err := openStore()
			if err != nil {
				return err
			}
			if add.EntryOwner, err = addOwner.resolve(store); err != nil {
				return err
			}
			e, err := store.CreateEntry(add)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Added %s on %s by %s (%s)\n", e.Type, addOwner.describe(), entryAuthor(*e), e.ID)
			return nil
		},
	}
	addOwner.add(addCmd)
	addCmd.Flags().StringVar(&add.Type, "type", "", "the entry's type: "+strings.Join(models.EntryTypes, ", "))
	addCmd.Flags().StringVar(&add.AgentID, "agent", "", "the id of the agent writing it")
	addCmd.Flags().StringVar(&add.AuthorName, "author", "", "the person writing it (default: your user name)")
	addCmd.Flags().StringVar(&add.Source, "source", "", "a decision's source: "+strings.Join(models.DecisionSources, " or "))
	addCmd.Flags().StringVar(&add.Replaces, "replaces", "", "the id of the entry this one replaces")
	addCmd.Flags().StringVar(&add.About, "about", "", "a note's: the id of the entry it points at")
	addCmd.Flags().StringVar(&add.Verdict, "verdict", "", "a review's verdict: "+strings.Join(models.ReviewVerdicts, " or "))
	addCmd.Flags().StringArrayVar(&findings, "finding", nil, "a review's findings of one severity, as severity=count ("+
		strings.Join(models.ReviewSeverities, ", ")+"); repeatable")
	addCmd.Flags().StringVar(&add.ReportDocument, "report-document", "", "a review's: the ticket's document holding the full report")
	_ = addCmd.MarkFlagRequired("type")

	var listOwner entryOwnerFlags
	var types []string
	var all bool
	var before string
	var limit int
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List the entries on a ticket, an epic or a project, newest first",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			owner, err := listOwner.resolve(store)
			if err != nil {
				return err
			}
			var filter models.EntryFilter
			filter.IncludeReplaced = all
			for _, v := range types {
				for _, t := range strings.Split(v, ",") {
					if t = strings.TrimSpace(t); t != "" {
						filter.Types = append(filter.Types, t)
					}
				}
			}
			page, err := store.ListEntries(owner, filter, before, limit)
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), formatEntries(listOwner, before, page))
			return nil
		},
	}
	listOwner.add(listCmd)
	listCmd.Flags().StringArrayVar(&types, "type", nil, "only entries of this type; repeat or comma-separate for several")
	listCmd.Flags().BoolVar(&all, "all", false, "also list entries a later entry replaced")
	listCmd.Flags().StringVar(&before, "before", "", "start after this entry: the id the previous page names")
	listCmd.Flags().IntVar(&limit, "limit", db.EntryDefaultLimit, fmt.Sprintf("at most this many entries (1 to %d)", db.EntryMaxLimit))

	var agent string
	handleCmd := &cobra.Command{
		Use:   "handle [note-id]",
		Short: "Mark a note handled by the agent given with --agent",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			e, err := store.MarkNoteHandled(args[0], agent)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Marked note %s handled by agent %s\n", e.ID, e.HandledBy)
			return nil
		},
	}
	handleCmd.Flags().StringVar(&agent, "agent", "", "the id of the agent that handled the note")

	cmd.AddCommand(addCmd, listCmd, handleCmd)
	return cmd
}

// entryAuthor names who wrote an entry: its agent, or the person by name.
func entryAuthor(e models.Entry) string {
	if e.AgentID != "" {
		return "agent " + e.AgentID
	}
	return e.AuthorName
}

// entryState says what else a reader should know about an entry beside its
// type and author: a decision's source, whether a note is open, and what
// replaced it.
func entryState(e models.Entry) string {
	var parts []string
	if e.Source != "" {
		parts = append(parts, e.Source+"'s call")
	}
	if e.Verdict != "" {
		parts = append(parts, e.Verdict)
	}
	if e.Type == models.EntryNote {
		if e.Open() {
			parts = append(parts, "open")
		} else if e.HandledBy != "" {
			parts = append(parts, "handled by agent "+e.HandledBy)
		}
	}
	if e.ReplacedBy != "" {
		parts = append(parts, "replaced by "+e.ReplacedBy)
	}
	if len(parts) == 0 {
		return ""
	}
	return "  [" + strings.Join(parts, ", ") + "]"
}

// formatEntries renders one page of entries, each one's time, type, author
// and id on a line and its text indented under it, then the command for the
// next page when there is one.
func formatEntries(owner entryOwnerFlags, before string, page models.EntryPage) string {
	var b strings.Builder
	if len(page.Entries) == 0 {
		if before != "" {
			fmt.Fprintf(&b, "No older entries on %s.\n", owner.describe())
		} else {
			fmt.Fprintf(&b, "No entries on %s.\n", owner.describe())
		}
		return b.String()
	}
	fmt.Fprintf(&b, "Entries on %s, newest first (%d of %d):\n", owner.describe(), len(page.Entries), page.Total)
	for _, e := range page.Entries {
		fmt.Fprintf(&b, "  %s  %s  %s  (%s)%s\n", e.CreatedAt.Local().Format("2006-01-02 15:04"), e.Type, entryAuthor(e), e.ID, entryState(e))
		for _, line := range strings.Split(e.Text, "\n") {
			if line == "" {
				b.WriteString("\n")
				continue
			}
			fmt.Fprintf(&b, "      %s\n", line)
		}
	}
	if page.HasMore {
		fmt.Fprintf(&b, "Older entries: taskboard entry list %s --before %s\n", owner.flags(), page.NextBefore)
	}
	return b.String()
}
