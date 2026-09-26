package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tcarac/taskboard/internal/models"
)

func projectCommands() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Manage projects",
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all projects",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			projects, err := store.ListProjects("")
			if err != nil {
				return err
			}
			if len(projects) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No projects found.")
				return nil
			}
			for _, p := range projects {
				icon := p.Icon
				if icon == "" {
					icon = " "
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s %s [%s] (%s) - %s\n", icon, p.Name, p.Prefix, p.Status, p.ID)
			}
			return nil
		},
	}

	var prefix, icon, color, description, agentInstructions string
	createCmd := &cobra.Command{
		Use:   "create [name]",
		Short: "Create a new project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			p, err := store.CreateProject(models.CreateProjectRequest{
				Name:              args[0],
				Prefix:            prefix,
				Description:       description,
				AgentInstructions: agentInstructions,
				Icon:              icon,
				Color:             color,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Created project %s [%s] (%s)\n", p.Name, p.Prefix, p.ID)
			return nil
		},
	}
	createCmd.Flags().StringVar(&prefix, "prefix", "", "project prefix (required)")
	createCmd.MarkFlagRequired("prefix")
	createCmd.Flags().StringVar(&icon, "icon", "", "emoji icon")
	createCmd.Flags().StringVar(&color, "color", "#3B82F6", "hex color")
	createCmd.Flags().StringVar(&description, "description", "",
		"what the project is: its goals, scope and context")
	createCmd.Flags().StringVar(&agentInstructions, "agent-instructions", "",
		"how agents should work on the project's tickets (agents read it through get_project; the board never acts on it)")

	deleteCmd := &cobra.Command{
		Use:   "delete [id-or-prefix]",
		Short: "Delete a project, by id or prefix (case-insensitive)",
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
			if err := store.DeleteProject(projectID); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Project deleted.")
			return nil
		},
	}

	cmd.AddCommand(listCmd, createCmd, deleteCmd)
	return cmd
}
