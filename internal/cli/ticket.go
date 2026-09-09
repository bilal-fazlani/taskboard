package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tcarac/taskboard/internal/models"
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
			for _, t := range tickets {
				line := fmt.Sprintf("[%s] %s - %s (%s", t.DisplayKey(), t.Title, t.Status, t.Priority)
				if t.Repo != "" {
					line += ", " + t.Repo
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
				fmt.Printf("%s  (%s)\n", line, t.ID)
			}
			return nil
		},
	}
	listCmd.Flags().StringVar(&projectID, "project", "", "filter by project ID")
	listCmd.Flags().StringVar(&status, "status", "", "filter by status (todo|in_progress|done)")
	listCmd.Flags().StringVar(&priority, "priority", "", "filter by priority (urgent|high|medium|low)")
	listCmd.Flags().StringVar(&listRepo, "repo", "", "filter by repo")
	listCmd.Flags().StringVar(&listLabel, "label", "", "filter by label name")

	var createProject, createPriority, createDue, createRepo string
	var createLabels, createDependsOn []string
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
				Repo:      createRepo,
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
			fmt.Printf("Created ticket %s: %s (%s)\n", t.DisplayKey(), t.Title, t.ID)
			return nil
		},
	}
	createCmd.Flags().StringVar(&createProject, "project", "", "project ID (required)")
	createCmd.MarkFlagRequired("project")
	createCmd.Flags().String("title", "", "ticket title (required)")
	createCmd.MarkFlagRequired("title")
	createCmd.Flags().StringVar(&createPriority, "priority", "medium", "priority (urgent|high|medium|low)")
	createCmd.Flags().StringVar(&createDue, "due", "", "due date (YYYY-MM-DD)")
	createCmd.Flags().StringVar(&createRepo, "repo", "", "repository identifier")
	createCmd.Flags().StringSliceVar(&createLabels, "label", nil, "label name; comma-separated or repeated")
	createCmd.Flags().StringSliceVar(&createDependsOn, "depends-on", nil, "ticket ID or key this depends on; comma-separated or repeated")

	var moveStatus string
	moveCmd := &cobra.Command{
		Use:   "move [id]",
		Short: "Move ticket to different status",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			t, err := store.MoveTicket(args[0], models.MoveTicketRequest{Status: moveStatus})
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
		Use:   "delete [id]",
		Short: "Delete a ticket",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			if err := store.DeleteTicket(args[0]); err != nil {
				return err
			}
			fmt.Println("Ticket deleted.")
			return nil
		},
	}

	var (
		updTitle, updDescription, updStatus, updPriority, updDue, updRepo string
		updLabels, updLabelAlias, updDependsOn                            []string
	)
	updateCmd := &cobra.Command{
		Use:   "update [id]",
		Short: "Update ticket fields",
		Long: "Update ticket fields.\n\n" +
			"Omitting a flag leaves that field untouched. Passing --labels or " +
			"--depends-on replaces the existing set, so passing one with an empty " +
			"value clears it. --label is accepted as an alias for --labels, matching " +
			"the spelling used by 'ticket create' and 'ticket list'.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
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
			if cmd.Flags().Changed("repo") {
				req.Repo = &updRepo
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

			t, err := store.UpdateTicket(args[0], req)
			if err != nil {
				return err
			}
			if t == nil {
				return fmt.Errorf("ticket not found")
			}
			fmt.Printf("Updated %s: %s\n", t.DisplayKey(), t.Title)
			return nil
		},
	}
	updateCmd.Flags().StringVar(&updTitle, "title", "", "new title")
	updateCmd.Flags().StringVar(&updDescription, "description", "", "new description")
	updateCmd.Flags().StringVar(&updStatus, "status", "", "status (todo|in_progress|done)")
	updateCmd.Flags().StringVar(&updPriority, "priority", "", "priority (urgent|high|medium|low)")
	updateCmd.Flags().StringVar(&updDue, "due", "", "due date (YYYY-MM-DD)")
	updateCmd.Flags().StringVar(&updRepo, "repo", "", "repository identifier")
	updateCmd.Flags().StringSliceVar(&updLabels, "labels", nil, "replace labels; comma-separated or repeated, empty value clears")
	updateCmd.Flags().StringSliceVar(&updLabelAlias, "label", nil, "alias for --labels")
	updateCmd.Flags().StringSliceVar(&updDependsOn, "depends-on", nil, "replace dependencies; comma-separated or repeated, empty value clears")

	cmd.AddCommand(listCmd, createCmd, moveCmd, deleteCmd, updateCmd)
	return cmd
}
