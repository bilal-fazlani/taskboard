package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tcarac/taskboard/internal/models"
	"github.com/tcarac/taskboard/internal/weburl"
)

func ticketCommands() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ticket",
		Short: "Manage tickets",
	}

	var projectID, status, priority, listRepo, listLabel, listEpic string
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List tickets",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			tickets, err := store.ListTickets(models.TicketFilter{
				ProjectID: projectID,
				Status:    status,
				Priority:  priority,
				Repo:      listRepo,
				Label:     listLabel,
				Epic:      listEpic,
			})
			if err != nil {
				return err
			}
			if len(tickets) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No tickets found.")
				return nil
			}
			base := weburl.Base()
			for _, t := range tickets {
				line := fmt.Sprintf("[%s] %s - %s (%s", t.DisplayKey(), t.Title, t.Status, t.Priority)
				if len(t.Repos) > 0 {
					line += ", " + strings.Join(t.Repos, " ")
				}
				line += ")"
				if len(t.Labels) > 0 {
					names := make([]string, len(t.Labels))
					for i, l := range t.Labels {
						names[i] = l.Name
					}
					line += " [" + strings.Join(names, ", ") + "]"
				}
				if t.Epic != nil {
					line += " epic:" + t.Epic.Name
				}
				if len(t.DependsOn) > 0 {
					keys := make([]string, len(t.DependsOn))
					for i, d := range t.DependsOn {
						keys[i] = d.Key
					}
					line += " depends on " + strings.Join(keys, ", ")
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s  (%s)  %s\n", line, t.ID, weburl.Ticket(base, weburl.Ref(t)))
			}
			return nil
		},
	}
	listCmd.Flags().StringVar(&projectID, "project", "", "filter by project ID or prefix (case-insensitive); an unknown one returns no tickets rather than an error")
	listCmd.Flags().StringVar(&status, "status", "", fmt.Sprintf("filter by status (%s)", strings.Join(models.Statuses, "|")))
	listCmd.Flags().StringVar(&priority, "priority", "", "filter by priority (urgent|high|medium|low)")
	listCmd.Flags().StringVar(&listRepo, "repo", "", "filter by repo")
	listCmd.Flags().StringVar(&listLabel, "label", "", "filter by label name")
	listCmd.Flags().StringVar(&listEpic, "epic", "", `filter by epic name (case-insensitive) or id, or "none" for tickets without an epic`)

	var getJSON bool
	getCmd := &cobra.Command{
		Use:   "get [id-or-key]",
		Short: "Show a ticket with its labels, dependencies and subtasks, by id or display key (e.g. BILL-2)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			ticketID, err := store.ResolveTicketID(args[0])
			if err != nil {
				return err
			}
			t, err := store.GetTicket(ticketID)
			if err != nil {
				return err
			}
			if t == nil {
				return fmt.Errorf("ticket not found")
			}
			weburl.Fill(t)
			if getJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(t)
			}
			fmt.Fprint(cmd.OutOrStdout(), formatTicketDetail(*t))
			return nil
		},
	}
	getCmd.Flags().BoolVar(&getJSON, "json", false, "print the full ticket as JSON instead of readable text")

	var createProject, createPriority, createDue, createEpic string
	var createRepos, createLabels, createDependsOn []string
	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new ticket",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			title, _ := cmd.Flags().GetString("title")
			req := models.CreateTicketRequest{
				ProjectID: createProject,
				Title:     title,
				Priority:  createPriority,
				Repos:     createRepos,
			}
			if createDue != "" {
				req.DueDate = &createDue
			}
			if createEpic != "" {
				req.Epic = &createEpic
			}
			req.Labels = createLabels
			req.DependsOn = createDependsOn
			t, err := store.CreateTicket(req)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Created ticket %s: %s (%s)\n  %s\n", t.DisplayKey(), t.Title, t.ID, weburl.Ticket(weburl.Base(), weburl.Ref(*t)))
			if t.Epic != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "  Epic: %s\n", t.Epic.Name)
			}
			return nil
		},
	}
	createCmd.Flags().StringVar(&createProject, "project", "", "project ID or prefix (case-insensitive, required)")
	createCmd.MarkFlagRequired("project")
	createCmd.Flags().String("title", "", "ticket title (required)")
	createCmd.MarkFlagRequired("title")
	createCmd.Flags().StringVar(&createPriority, "priority", "medium", "priority (urgent|high|medium|low)")
	createCmd.Flags().StringVar(&createDue, "due", "", "due date (YYYY-MM-DD)")
	createCmd.Flags().StringVar(&createEpic, "epic", "", "epic name (case-insensitive) or id, within --project")
	createCmd.Flags().StringSliceVar(&createRepos, "repo", nil, "repository identifier; comma-separated or repeated")
	createCmd.Flags().StringSliceVar(&createLabels, "label", nil, "label name; comma-separated or repeated")
	createCmd.Flags().StringSliceVar(&createDependsOn, "depends-on", nil, "ticket ID or key this depends on; comma-separated or repeated")

	var moveStatus, moveNote string
	moveCmd := &cobra.Command{
		Use:   "move [id-or-key]",
		Short: "Move ticket to different status, by id or display key (e.g. BILL-2)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			ticketID, err := store.ResolveTicketID(args[0])
			if err != nil {
				return err
			}
			t, err := store.MoveTicket(ticketID, models.MoveTicketRequest{Status: moveStatus, Note: moveNote})
			if err != nil {
				return err
			}
			if t == nil {
				return fmt.Errorf("ticket not found")
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Moved %s to %s\n", t.DisplayKey(), t.Status)
			return nil
		},
	}
	moveCmd.Flags().StringVar(&moveStatus, "status", "", fmt.Sprintf("target status (%s, required)", strings.Join(models.Statuses, "|")))
	moveCmd.MarkFlagRequired("status")
	moveCmd.Flags().StringVar(&moveNote, "note", "", noteFlagUsage)

	historyCmd := &cobra.Command{
		Use:   "history [id-or-key]",
		Short: "Show a ticket's status changes, newest first, by id or display key (e.g. BILL-2)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			ticketID, err := store.ResolveTicketID(args[0])
			if err != nil {
				return err
			}
			t, err := store.GetTicket(ticketID)
			if err != nil {
				return err
			}
			if t == nil {
				return fmt.Errorf("ticket not found")
			}
			changes, err := store.ListStatusChanges(ticketID)
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), formatHistory(t.DisplayKey(), changes))
			return nil
		},
	}

	deleteCmd := &cobra.Command{
		Use:   "delete [id-or-key]",
		Short: "Delete a ticket, by id or display key (e.g. BILL-2)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			ticketID, err := store.ResolveTicketID(args[0])
			if err != nil {
				return err
			}
			if err := store.DeleteTicket(ticketID); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Ticket deleted.")
			return nil
		},
	}

	var (
		updTitle, updDescription, updStatus, updPriority, updDue, updEpic, updNote string
		updRepos, updLabels, updLabelAlias, updDependsOn                           []string
	)
	updateCmd := &cobra.Command{
		Use:   "update [id-or-key]",
		Short: "Update ticket fields, by id or display key (e.g. BILL-2)",
		Long: "Update ticket fields, identified by id or display key (e.g. BILL-2, case-insensitive).\n\n" +
			"Omitting a flag leaves that field untouched. Passing --repo, --labels " +
			"or --depends-on replaces the existing set, so passing one with an empty " +
			"value clears it. --label is accepted as an alias for --labels, matching " +
			"the spelling used by 'ticket create' and 'ticket list'. --due follows the " +
			"same rule: omit it to leave the due date alone, or pass --due=\"\" to clear it. " +
			"--epic follows the same rule as --due: omit it to leave the epic alone, or " +
			"pass --epic=\"\" or --epic=none to clear it; anything else names an epic " +
			"(by name, case-insensitive, or id) in the ticket's own project.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			ticketID, err := store.ResolveTicketID(args[0])
			if err != nil {
				return err
			}

			var req models.UpdateTicketRequest
			if cmd.Flags().Changed("title") {
				req.Title = &updTitle
			}
			if cmd.Flags().Changed("description") {
				req.Description = &updDescription
			}
			if cmd.Flags().Changed("status") {
				req.Status = &updStatus
			}
			req.Note = updNote
			if cmd.Flags().Changed("priority") {
				req.Priority = &updPriority
			}
			if cmd.Flags().Changed("due") {
				req.DueDate = &updDue
			}
			if cmd.Flags().Changed("epic") {
				req.Epic = &updEpic
			}
			// Like --labels, a non-nil slice replaces the set, so --repo=""
			// clears it.
			if cmd.Flags().Changed("repo") {
				req.Repos = updRepos
				if req.Repos == nil {
					req.Repos = []string{}
				}
			}
			// --label is an alias for --labels; either spelling (or both) yields a
			// non-nil slice, which is what makes an empty value clear the set.
			if cmd.Flags().Changed("labels") || cmd.Flags().Changed("label") {
				combined := []string{}
				combined = append(combined, updLabels...)
				combined = append(combined, updLabelAlias...)
				req.Labels = combined
			}
			if cmd.Flags().Changed("depends-on") {
				req.DependsOn = updDependsOn
				if req.DependsOn == nil {
					req.DependsOn = []string{}
				}
			}

			t, err := store.UpdateTicket(ticketID, req)
			if err != nil {
				return err
			}
			if t == nil {
				return fmt.Errorf("ticket not found")
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Updated %s: %s\n  %s\n", t.DisplayKey(), t.Title, weburl.Ticket(weburl.Base(), weburl.Ref(*t)))
			if t.Epic != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "  Epic: %s\n", t.Epic.Name)
			}
			return nil
		},
	}
	updateCmd.Flags().StringVar(&updTitle, "title", "", "new title")
	updateCmd.Flags().StringVar(&updDescription, "description", "", "new description")
	updateCmd.Flags().StringVar(&updStatus, "status", "", fmt.Sprintf("status (%s)", strings.Join(models.Statuses, "|")))
	updateCmd.Flags().StringVar(&updNote, "note", "", noteFlagUsage)
	updateCmd.Flags().StringVar(&updPriority, "priority", "", "priority (urgent|high|medium|low)")
	updateCmd.Flags().StringVar(&updDue, "due", "", "due date (YYYY-MM-DD); empty value clears it")
	updateCmd.Flags().StringVar(&updEpic, "epic", "", `epic name (case-insensitive) or id; "" or "none" clears it`)
	updateCmd.Flags().StringSliceVar(&updRepos, "repo", nil, "replace repos; comma-separated or repeated, empty value clears")
	updateCmd.Flags().StringSliceVar(&updLabels, "labels", nil, "replace labels; comma-separated or repeated, empty value clears")
	updateCmd.Flags().StringSliceVar(&updLabelAlias, "label", nil, "alias for --labels")
	updateCmd.Flags().StringSliceVar(&updDependsOn, "depends-on", nil, "replace dependencies; comma-separated or repeated, empty value clears")

	cmd.AddCommand(listCmd, getCmd, createCmd, moveCmd, deleteCmd, updateCmd, historyCmd, subtaskCommands())
	return cmd
}

// subtaskCommands returns the `ticket subtask` command group: add, toggle and
// delete. Subtasks have no display key of their own, so only `add` takes a
// ticket argument (id or display key); toggle and delete address the subtask
// directly by id, the same id `ticket get` and the MCP tools print.
func subtaskCommands() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "subtask",
		Short: "Manage a ticket's subtasks (checklist items)",
	}

	addCmd := &cobra.Command{
		Use:   "add [id-or-key] [title]",
		Short: "Add a subtask to a ticket, by id or display key (e.g. BILL-2)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(args[1]) == "" {
				return fmt.Errorf("title is required")
			}
			store, err := openStore()
			if err != nil {
				return err
			}
			ticketID, err := store.ResolveTicketID(args[0])
			if err != nil {
				return err
			}
			st, err := store.AddSubtask(ticketID, models.CreateSubtaskRequest{Title: args[1]})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Added subtask %s (%s)\n", st.Title, st.ID)
			return nil
		},
	}

	toggleCmd := &cobra.Command{
		Use:   "toggle [subtask-id]",
		Short: "Flip a subtask between done and not done",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			st, err := store.ToggleSubtask(args[0])
			if err != nil {
				return err
			}
			state := "not done"
			if st.Completed {
				state = "done"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s is now %s (%s)\n", st.Title, state, st.ID)
			return nil
		},
	}

	deleteCmd := &cobra.Command{
		Use:   "delete [subtask-id]",
		Short: "Delete a subtask",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			// store.DeleteSubtask now reports a clear "subtask not found"
			// error itself for an unknown id (ACP-121), the same not-found
			// check every other Delete* store method got in ACP-63, so this
			// no longer needs its own existence check first.
			if err := store.DeleteSubtask(args[0]); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Subtask deleted.")
			return nil
		},
	}

	cmd.AddCommand(addCmd, toggleCmd, deleteCmd)
	return cmd
}

// formatTicketDetail renders a ticket for `ticket get`'s readable (non-JSON)
// output: a summary line like `ticket list`'s, then its description,
// dependencies and subtasks, one section per line so a person can scan it and
// an agent can grep it.
func formatTicketDetail(t models.Ticket) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[%s] %s - %s (%s)\n", t.DisplayKey(), t.Title, t.Status, t.Priority)
	fmt.Fprintf(&b, "  %s  (%s)\n", t.URL, t.ID)
	if t.Description != "" {
		fmt.Fprintf(&b, "\n%s\n\n", t.Description)
	}
	if len(t.Repos) > 0 {
		fmt.Fprintf(&b, "Repos: %s\n", strings.Join(t.Repos, ", "))
	}
	if len(t.Labels) > 0 {
		names := make([]string, len(t.Labels))
		for i, l := range t.Labels {
			names[i] = l.Name
		}
		fmt.Fprintf(&b, "Labels: %s\n", strings.Join(names, ", "))
	}
	if t.Epic != nil {
		fmt.Fprintf(&b, "Epic: %s\n", t.Epic.Name)
	}
	if t.DueDate != nil {
		fmt.Fprintf(&b, "Due: %s\n", t.DueDate.Format("2006-01-02"))
	}
	if len(t.DependsOn) > 0 {
		keys := make([]string, len(t.DependsOn))
		for i, d := range t.DependsOn {
			keys[i] = d.Key
		}
		fmt.Fprintf(&b, "Depends on: %s\n", strings.Join(keys, ", "))
	}
	if len(t.Blocks) > 0 {
		keys := make([]string, len(t.Blocks))
		for i, d := range t.Blocks {
			keys[i] = d.Key
		}
		fmt.Fprintf(&b, "Blocks: %s\n", strings.Join(keys, ", "))
	}
	if len(t.Subtasks) > 0 {
		fmt.Fprintln(&b, "Subtasks:")
		for _, s := range t.Subtasks {
			mark := " "
			if s.Completed {
				mark = "x"
			}
			fmt.Fprintf(&b, "  [%s] %s  (%s)\n", mark, s.Title, s.ID)
		}
	}
	return b.String()
}

const noteFlagUsage = "why the status changed, saved with the change in the ticket's history (optional; ignored when the status does not change)"

// formatHistory renders a ticket's status changes, newest first, one per
// line, with each note indented under its change. The first change, with no
// from status, is the ticket's creation.
func formatHistory(key string, changes []models.StatusChange) string {
	var b strings.Builder
	if len(changes) == 0 {
		fmt.Fprintf(&b, "No status history for %s.\n", key)
		return b.String()
	}
	fmt.Fprintf(&b, "Status history for %s, newest first:\n", key)
	for _, c := range changes {
		when := c.CreatedAt.Local().Format("2006-01-02 15:04")
		if c.FromStatus == "" {
			fmt.Fprintf(&b, "  %s  created in %s\n", when, c.ToStatus)
		} else {
			fmt.Fprintf(&b, "  %s  %s -> %s\n", when, c.FromStatus, c.ToStatus)
		}
		if c.Note == "" {
			continue
		}
		for _, line := range strings.Split(c.Note, "\n") {
			fmt.Fprintf(&b, "      %s\n", line)
		}
	}
	return b.String()
}
