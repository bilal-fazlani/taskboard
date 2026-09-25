package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tcarac/taskboard/internal/db"
	"github.com/tcarac/taskboard/internal/models"
)

func labelCommands() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "label",
		Short: "Manage labels",
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List labels",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			labels, err := store.ListLabels()
			if err != nil {
				return err
			}
			if len(labels) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No labels found.")
				return nil
			}
			for _, l := range labels {
				fmt.Fprintf(cmd.OutOrStdout(), "%-20s %-8s %d tickets (%s)\n", l.Name, l.Color, l.TicketCount, l.ID)
			}
			return nil
		},
	}

	var color string
	createCmd := &cobra.Command{
		Use:   "create [name]",
		Short: "Create a label",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			l, err := store.CreateLabel(models.CreateLabelRequest{Name: args[0], Color: color})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Created label %s (%s)\n", l.Name, l.ID)
			return nil
		},
	}
	createCmd.Flags().StringVar(&color, "color", db.DefaultLabelColor, "hex color")

	deleteCmd := &cobra.Command{
		Use:   "delete [id-or-name]",
		Short: "Delete a label (by id or exact name) and remove it from all tickets",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			resolvedID, err := store.ResolveLabelRef(args[0])
			if err != nil {
				return err
			}
			if resolvedID == "" {
				return fmt.Errorf("label not found: %s", args[0])
			}
			count, err := store.DeleteLabel(resolvedID)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Label deleted. Detached from %d ticket(s).\n", count)
			return nil
		},
	}

	var updateName, updateColor string
	updateCmd := &cobra.Command{
		Use:   "update [id-or-name]",
		Short: "Update a label's name or color, by id or exact name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("name") && !cmd.Flags().Changed("color") {
				return fmt.Errorf("nothing to update: provide --name and/or --color")
			}
			store, err := openStore()
			if err != nil {
				return err
			}
			resolvedID, err := store.ResolveLabelRef(args[0])
			if err != nil {
				return err
			}
			if resolvedID == "" {
				return fmt.Errorf("label not found: %s", args[0])
			}
			var req models.UpdateLabelRequest
			if cmd.Flags().Changed("name") {
				req.Name = &updateName
			}
			if cmd.Flags().Changed("color") {
				req.Color = &updateColor
			}
			l, err := store.UpdateLabel(resolvedID, req)
			if err != nil {
				return err
			}
			if l == nil {
				return fmt.Errorf("label not found: %s", args[0])
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Updated label %s (%s)\n", l.Name, l.ID)
			return nil
		},
	}
	updateCmd.Flags().StringVar(&updateName, "name", "", "new name")
	updateCmd.Flags().StringVar(&updateColor, "color", "", "new hex color")

	cmd.AddCommand(listCmd, createCmd, deleteCmd, updateCmd)
	return cmd
}
