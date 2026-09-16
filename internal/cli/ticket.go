package cli

import (
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

	var projectID, status, priority, listRepo, listLabel string
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
			})
			if err != nil {
				return err
			}
			if len(tickets) == 0 {
				fmt.Println("No tickets found.")
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
				if len(t.DependsOn) > 0 {
					keys := make([]string, len(t.DependsOn))
					for i, d := range t.DependsOn {
						keys[i] = d.Key
					}
					line += " depends on " + strings.Join(keys, ", ")
				}
				fmt.Printf("%s  (%s)  %s\n", line, t.ID, weburl.Ticket(base, weburl.Ref(t)))
			}
			return nil
		},
	}
	listCmd.Flags().StringVar(&projectID, "project", "", "filter by project ID or prefix (case-insensitive); an unknown one returns no tickets rather than an error")
	listCmd.Flags().StringVar(&status, "status", "", fmt.Sprintf("filter by status (%s)", strings.Join(models.Statuses, "|")))
	listCmd.Flags().StringVar(&priority, "priority", "", "filter by priority (urgent|high|medium|low)")
	listCmd.Flags().StringVar(&listRepo, "repo", "", "filter by repo")
	listCmd.Flags().StringVar(&listLabel, "label", "", "filter by label name")

	var createProject, createPriority, createDue string
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
			req.Labels = createLabels
			req.DependsOn = createDependsOn
			t, err := store.CreateTicket(req)
			if err != nil {
				return err
			}
			fmt.Printf("Created ticket %s: %s (%s)\n  %s\n", t.DisplayKey(), t.Title, t.ID, weburl.Ticket(weburl.Base(), weburl.Ref(*t)))
			return nil
		},
	}
	createCmd.Flags().StringVar(&createProject, "project", "", "project ID or prefix (case-insensitive, required)")
	createCmd.MarkFlagRequired("project")
	createCmd.Flags().String("title", "", "ticket title (required)")
	createCmd.MarkFlagRequired("title")
	createCmd.Flags().StringVar(&createPriority, "priority", "medium", "priority (urgent|high|medium|low)")
	createCmd.Flags().StringVar(&createDue, "due", "", "due date (YYYY-MM-DD)")
	createCmd.Flags().StringSliceVar(&createRepos, "repo", nil, "repository identifier; comma-separated or repeated")
	createCmd.Flags().StringSliceVar(&createLabels, "label", nil, "label name; comma-separated or repeated")
	createCmd.Flags().StringSliceVar(&createDependsOn, "depends-on", nil, "ticket ID or key this depends on; comma-separated or repeated")

	var moveStatus string
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
			t, err := store.MoveTicket(ticketID, models.MoveTicketRequest{Status: moveStatus})
			if err != nil {
				return err
			}
			if t == nil {
				return fmt.Errorf("ticket not found")
			}
			fmt.Printf("Moved %s to %s\n", t.DisplayKey(), t.Status)
			return nil
		},
	}
	moveCmd.Flags().StringVar(&moveStatus, "status", "", "target status (required)")
	moveCmd.MarkFlagRequired("status")

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
			fmt.Println("Ticket deleted.")
			return nil
		},
	}

	var (
		updTitle, updDescription, updStatus, updPriority, updDue string
		updRepos, updLabels, updLabelAlias, updDependsOn         []string
	)
	updateCmd := &cobra.Command{
		Use:   "update [id-or-key]",
		Short: "Update ticket fields, by id or display key (e.g. BILL-2)",
		Long: "Update ticket fields, identified by id or display key (e.g. BILL-2, case-insensitive).\n\n" +
			"Omitting a flag leaves that field untouched. Passing --repo, --labels " +
			"or --depends-on replaces the existing set, so passing one with an empty " +
			"value clears it. --label is accepted as an alias for --labels, matching " +
			"the spelling used by 'ticket create' and 'ticket list'. --due follows the " +
			"same rule: omit it to leave the due date alone, or pass --due=\"\" to clear it.",
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
			if cmd.Flags().Changed("priority") {
				req.Priority = &updPriority
			}
			if cmd.Flags().Changed("due") {
				req.DueDate = &updDue
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
			fmt.Printf("Updated %s: %s\n  %s\n", t.DisplayKey(), t.Title, weburl.Ticket(weburl.Base(), weburl.Ref(*t)))
			return nil
		},
	}
	updateCmd.Flags().StringVar(&updTitle, "title", "", "new title")
	updateCmd.Flags().StringVar(&updDescription, "description", "", "new description")
	updateCmd.Flags().StringVar(&updStatus, "status", "", fmt.Sprintf("status (%s)", strings.Join(models.Statuses, "|")))
	updateCmd.Flags().StringVar(&updPriority, "priority", "", "priority (urgent|high|medium|low)")
	updateCmd.Flags().StringVar(&updDue, "due", "", "due date (YYYY-MM-DD); empty value clears it")
	updateCmd.Flags().StringSliceVar(&updRepos, "repo", nil, "replace repos; comma-separated or repeated, empty value clears")
	updateCmd.Flags().StringSliceVar(&updLabels, "labels", nil, "replace labels; comma-separated or repeated, empty value clears")
	updateCmd.Flags().StringSliceVar(&updLabelAlias, "label", nil, "alias for --labels")
	updateCmd.Flags().StringSliceVar(&updDependsOn, "depends-on", nil, "replace dependencies; comma-separated or repeated, empty value clears")

	cmd.AddCommand(listCmd, createCmd, moveCmd, deleteCmd, updateCmd)
	return cmd
}
