package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tcarac/taskboard/internal/db"
	"github.com/tcarac/taskboard/internal/models"
)

func epicCommands() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "epic",
		Short: "Manage epics",
	}

	listCmd := &cobra.Command{
		Use:   "list [project]",
		Short: "List a project's epics, by id or prefix (case-insensitive), with their progress",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			epics, err := store.ListEpics(args[0])
			if err != nil {
				return err
			}
			for _, e := range epics {
				fmt.Println(formatEpicProgress(e.Name, e.EpicProgress, e.ID))
			}
			noEpic, err := store.NoEpicProgress(args[0])
			if err != nil {
				return err
			}
			fmt.Println(formatEpicProgress("No epic", *noEpic, ""))
			return nil
		},
	}

	var createDescription string
	createCmd := &cobra.Command{
		Use:   "create [project] [name]",
		Short: "Create an epic in a project, by id or prefix (case-insensitive)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			e, err := store.CreateEpic(models.CreateEpicRequest{
				ProjectID:   args[0],
				Name:        args[1],
				Description: createDescription,
			})
			if err != nil {
				return err
			}
			fmt.Printf("Created epic %s (%s)\n", e.Name, e.ID)
			return nil
		},
	}
	createCmd.Flags().StringVar(&createDescription, "description", "", "epic description")

	var updateProject, updateName, updateDescription string
	updateCmd := &cobra.Command{
		Use:   "update [epic]",
		Short: "Update an epic's name or description, by id, or by name with --project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("name") && !cmd.Flags().Changed("description") {
				return fmt.Errorf("nothing to update: provide --name and/or --description")
			}
			store, err := openStore()
			if err != nil {
				return err
			}
			epicID, err := resolveEpicArg(store, args[0], updateProject)
			if err != nil {
				return err
			}
			var req models.UpdateEpicRequest
			if cmd.Flags().Changed("name") {
				req.Name = &updateName
			}
			if cmd.Flags().Changed("description") {
				req.Description = &updateDescription
			}
			e, err := store.UpdateEpic(epicID, req)
			if err != nil {
				return err
			}
			if e == nil {
				return fmt.Errorf("epic not found: %s", args[0])
			}
			fmt.Printf("Updated epic %s (%s)\n", e.Name, e.ID)
			return nil
		},
	}
	updateCmd.Flags().StringVar(&updateProject, "project", "", "project ID or prefix (case-insensitive); addresses the epic by name instead of id")
	updateCmd.Flags().StringVar(&updateName, "name", "", "new name")
	updateCmd.Flags().StringVar(&updateDescription, "description", "", "new description")

	var deleteProject string
	deleteCmd := &cobra.Command{
		Use:   "delete [epic]",
		Short: "Delete an epic, by id, or by name with --project; its tickets stay but lose the epic, and its documents are deleted for good",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			// resolveEpicArg already confirms the epic exists (GetEpic for a
			// bare id, ResolveEpicRef for a name), so by the time we reach
			// DeleteEpic here epicID is known good. That matters because
			// DeleteEpic itself reports (0, nil) for an unknown id, which
			// would otherwise look like a silent no-op instead of a clear
			// "not found" error.
			epicID, err := resolveEpicArg(store, args[0], deleteProject)
			if err != nil {
				return err
			}
			count, err := store.DeleteEpic(epicID)
			if err != nil {
				return err
			}
			fmt.Printf("Epic deleted. Cleared from %d ticket(s).\n", count)
			return nil
		},
	}
	deleteCmd.Flags().StringVar(&deleteProject, "project", "", "project ID or prefix (case-insensitive); addresses the epic by name instead of id")

	cmd.AddCommand(listCmd, createCmd, updateCmd, deleteCmd)
	return cmd
}

// resolveEpicArg resolves a caller-supplied epic argument to an id: directly,
// when the argument is already an epic id, or by name within projectRef when
// one is given. This is the "an epic is addressed by id, or by name with a
// --project flag" contract that epic update and delete document.
func resolveEpicArg(store *db.Store, ref, projectRef string) (string, error) {
	if projectRef != "" {
		return store.ResolveEpicRef(projectRef, ref)
	}
	e, err := store.GetEpic(ref)
	if err != nil {
		return "", err
	}
	if e == nil {
		return "", fmt.Errorf("epic not found: %q (to use a name, pass --project)", ref)
	}
	return e.ID, nil
}

// formatEpicProgress renders one line of `epic list` output: the name, its
// done/total progress, whether it's complete, and the id when there is one
// (the "No epic" summary has none).
func formatEpicProgress(name string, p models.EpicProgress, id string) string {
	complete := ""
	if p.Complete {
		complete = " - complete"
	}
	line := fmt.Sprintf("%-20s %d/%d done%s", name, p.Counts[models.StatusDone], p.Total, complete)
	if id != "" {
		line += fmt.Sprintf(" (%s)", id)
	}
	return line
}
