package cli

import (
	"fmt"

	"github.com/spf13/cobra"
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
				fmt.Println("No labels found.")
				return nil
			}
			for _, l := range labels {
				fmt.Printf("%-20s %-8s %d tickets (%s)\n", l.Name, l.Color, l.TicketCount, l.ID)
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
			fmt.Printf("Created label %s (%s)\n", l.Name, l.ID)
			return nil
		},
	}
	createCmd.Flags().StringVar(&color, "color", "#6B7280", "hex color")

	deleteCmd := &cobra.Command{
		Use:   "delete [id]",
		Short: "Delete a label and remove it from all tickets",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			if err := store.DeleteLabel(args[0]); err != nil {
				return err
			}
			fmt.Println("Label deleted.")
			return nil
		},
	}

	cmd.AddCommand(listCmd, createCmd, deleteCmd)
	return cmd
}
