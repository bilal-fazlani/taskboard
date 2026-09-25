package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/tcarac/taskboard/internal/db"
	"github.com/tcarac/taskboard/internal/models"
	"github.com/tcarac/taskboard/internal/weburl"
)

func documentCommands() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doc",
		Short: "Manage documents attached to tickets and epics",
		Long: "Manage documents attached to tickets and epics. A document's owner is a ticket (by id or key), " +
			"or an epic given with --epic (its id, or its name together with --project).",
	}

	var listEpic, listProject string
	listCmd := &cobra.Command{
		Use:   "list [ticket]",
		Short: "List a ticket's documents (by ticket id or key) or an epic's (--epic), in the order they were added",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			owner, err := requireDocumentOwnerArg(store, firstArg(args), listEpic, listProject)
			if err != nil {
				return err
			}
			docs, err := store.ListDocuments(owner)
			if err != nil {
				return err
			}
			if len(docs) == 0 {
				fmt.Println("No documents.")
			}
			for _, d := range docs {
				fmt.Printf("%s  %s  updated %s (%s)\n  %s\n",
					models.DocumentDisplayName(d.Name, d.Format), models.FormatSize(d.Size),
					d.UpdatedAt.Local().Format("2006-01-02 15:04"), d.ID, documentURL(store, &d))
			}
			return nil
		},
	}

	listCmd.Flags().StringVar(&listEpic, "epic", "", documentEpicFlagUsage)
	listCmd.Flags().StringVar(&listProject, "project", "", documentProjectFlagUsage)

	var showTicket, showEpic, showProject string
	showCmd := &cobra.Command{
		Use:   "show [document]",
		Short: "Print a document's content, by id, or by name with --ticket or --epic",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			d, err := loadDocumentArg(store, args[0], showTicket, showEpic, showProject)
			if err != nil {
				return err
			}
			fmt.Print(d.Content)
			return nil
		},
	}
	addOwnerFlags(showCmd, &showTicket, &showEpic, &showProject)

	var addFile, addName, addFormat, addEpic, addProject string
	addCmd := &cobra.Command{
		Use:   "add [ticket]",
		Short: "Attach a document to a ticket or an epic (--epic) from a file (--file PATH) or standard input (--file -)",
		Long: "Attach a document to a ticket, or to an epic with --epic (its id, or its name with --project). Without --name, the name and format come from the file's name: " +
			"the extension (.md, .html or .htm) is removed and other symbols become spaces. With --name, the " +
			"format still comes from a .md, .html or .htm extension unless --format is given. Names hold letters, " +
			"digits, spaces, _ and - only.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, format := addName, addFormat
			if name == "" {
				if addFile == "" || addFile == "-" {
					if addFile == "" {
						return fmt.Errorf("provide --file PATH, or --file - to read standard input")
					}
					return fmt.Errorf("provide --name when reading standard input")
				}
				n, f, err := db.DocumentNameFromFilename(addFile)
				if err != nil {
					return err
				}
				name = n
				if format == "" {
					format = f
				}
			} else if format == "" && addFile != "-" {
				// A typed name still takes the format from a known extension,
				// so --name Report --file report.html is an HTML document.
				if f, ok := db.DocumentFormatFromFilename(addFile); ok {
					format = f
				}
			}
			content, err := readDocumentContent(cmd, addFile)
			if err != nil {
				return err
			}
			store, err := openStore()
			if err != nil {
				return err
			}
			owner, err := requireDocumentOwnerArg(store, firstArg(args), addEpic, addProject)
			if err != nil {
				return err
			}
			d, err := store.CreateDocument(models.CreateDocumentRequest{
				TicketID: owner.TicketID, EpicID: owner.EpicID, Name: name, Format: format, Content: content,
			})
			if err != nil {
				return err
			}
			fmt.Printf("Added document %s (%s)\n%s\n",
				models.DocumentDisplayName(d.Name, d.Format), d.ID, documentURL(store, &d.DocumentMeta))
			return nil
		},
	}
	addCmd.Flags().StringVar(&addFile, "file", "", "file to read, or - for standard input")
	addCmd.Flags().StringVar(&addName, "name", "", "document name; required with --file -")
	addCmd.Flags().StringVar(&addFormat, "format", "", "document format (markdown|html); defaults to the file's extension, else markdown")
	addCmd.Flags().StringVar(&addEpic, "epic", "", documentEpicFlagUsage)
	addCmd.Flags().StringVar(&addProject, "project", "", documentProjectFlagUsage)

	var writeTicket, writeEpic, writeProject, writeFile string
	writeCmd := &cobra.Command{
		Use:   "write [document]",
		Short: "Replace a document's whole content from a file (--file PATH) or standard input (--file -)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			content, err := readDocumentContent(cmd, writeFile)
			if err != nil {
				return err
			}
			store, err := openStore()
			if err != nil {
				return err
			}
			id, err := resolveDocumentArg(store, args[0], writeTicket, writeEpic, writeProject)
			if err != nil {
				return err
			}
			d, err := store.UpdateDocument(id, models.UpdateDocumentRequest{Content: &content})
			if err != nil {
				return err
			}
			if d == nil {
				return fmt.Errorf("document not found: %s", args[0])
			}
			fmt.Printf("Saved %s (%s)\n%s\n",
				models.DocumentDisplayName(d.Name, d.Format), d.ID, documentURL(store, &d.DocumentMeta))
			return nil
		},
	}
	addOwnerFlags(writeCmd, &writeTicket, &writeEpic, &writeProject)
	writeCmd.Flags().StringVar(&writeFile, "file", "", "file to read, or - for standard input")

	var renameTicket, renameEpic, renameProject string
	renameCmd := &cobra.Command{
		Use:   "rename [document] [new-name]",
		Short: "Rename a document, by id, or by name with --ticket or --epic",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			id, err := resolveDocumentArg(store, args[0], renameTicket, renameEpic, renameProject)
			if err != nil {
				return err
			}
			d, err := store.UpdateDocument(id, models.UpdateDocumentRequest{Name: &args[1]})
			if err != nil {
				return err
			}
			if d == nil {
				return fmt.Errorf("document not found: %s", args[0])
			}
			fmt.Printf("Renamed to %s (%s)\n%s\n",
				models.DocumentDisplayName(d.Name, d.Format), d.ID, documentURL(store, &d.DocumentMeta))
			return nil
		},
	}
	addOwnerFlags(renameCmd, &renameTicket, &renameEpic, &renameProject)

	var deleteTicket, deleteEpic, deleteProject string
	deleteCmd := &cobra.Command{
		Use:   "delete [document]",
		Short: "Delete a document for good, by id, or by name with --ticket or --epic",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			d, err := loadDocumentArg(store, args[0], deleteTicket, deleteEpic, deleteProject)
			if err != nil {
				return err
			}
			if _, err := store.DeleteDocument(d.ID); err != nil {
				return err
			}
			fmt.Printf("Deleted %s\n", models.DocumentDisplayName(d.Name, d.Format))
			return nil
		},
	}
	addOwnerFlags(deleteCmd, &deleteTicket, &deleteEpic, &deleteProject)

	cmd.AddCommand(listCmd, showCmd, addCmd, writeCmd, renameCmd, deleteCmd)
	return cmd
}

const (
	documentTicketFlagUsage  = "ticket ID or key; addresses the document by name instead of id"
	documentEpicFlagUsage    = "epic ID, or its name with --project; the epic the document belongs to"
	documentProjectFlagUsage = "project ID or prefix; required when --epic is a name"
)

// addOwnerFlags gives a command that names one document the flags for its
// owner: --ticket, or --epic with --project.
func addOwnerFlags(cmd *cobra.Command, ticket, epic, project *string) {
	cmd.Flags().StringVar(ticket, "ticket", "", documentTicketFlagUsage)
	cmd.Flags().StringVar(epic, "epic", "", documentEpicFlagUsage)
	cmd.Flags().StringVar(project, "project", "", documentProjectFlagUsage)
}

func firstArg(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

// documentOwnerArg resolves where a document lives: a ticket (the positional
// argument or --ticket), or --epic, with --project when it is a name. Naming
// both is refused; a zero owner means neither was given.
func documentOwnerArg(store *db.Store, ticket, epic, project string) (db.DocumentOwner, error) {
	switch {
	case ticket != "" && epic != "":
		return db.DocumentOwner{}, fmt.Errorf("pass a ticket or --epic, not both")
	case ticket != "":
		id, err := store.ResolveTicketID(ticket)
		return db.DocumentOwner{TicketID: id}, err
	case epic != "":
		id, err := resolveEpicArg(store, epic, project)
		return db.DocumentOwner{EpicID: id}, err
	}
	return db.DocumentOwner{}, nil
}

// requireDocumentOwnerArg is documentOwnerArg for list and add, which need an
// owner.
func requireDocumentOwnerArg(store *db.Store, ticket, epic, project string) (db.DocumentOwner, error) {
	owner, err := documentOwnerArg(store, ticket, epic, project)
	if err != nil {
		return db.DocumentOwner{}, err
	}
	if owner == (db.DocumentOwner{}) {
		return db.DocumentOwner{}, fmt.Errorf("provide a ticket, or --epic with --project")
	}
	return owner, nil
}

// resolveDocumentArg resolves a document argument to an id: directly when it
// is an id, or by name within the ticket or epic given.
func resolveDocumentArg(store *db.Store, ref, ticket, epic, project string) (string, error) {
	owner, err := documentOwnerArg(store, ticket, epic, project)
	if err != nil {
		return "", err
	}
	if owner != (db.DocumentOwner{}) {
		return store.ResolveDocumentRef(owner, ref)
	}
	d, err := store.GetDocument(ref)
	if err != nil {
		return "", err
	}
	if d == nil {
		return "", fmt.Errorf("document not found: %q (to use a name, pass --ticket, or --epic with --project)", ref)
	}
	return d.ID, nil
}

func loadDocumentArg(store *db.Store, ref, ticket, epic, project string) (*models.Document, error) {
	id, err := resolveDocumentArg(store, ref, ticket, epic, project)
	if err != nil {
		return nil, err
	}
	d, err := store.GetDocument(id)
	if err != nil {
		return nil, err
	}
	if d == nil {
		return nil, fmt.Errorf("document not found: %s", ref)
	}
	return d, nil
}

// readDocumentContent reads --file: a path, or - for standard input.
func readDocumentContent(cmd *cobra.Command, file string) (string, error) {
	switch file {
	case "":
		return "", fmt.Errorf("provide --file PATH, or --file - to read standard input")
	case "-":
		data, err := io.ReadAll(cmd.InOrStdin())
		return string(data), err
	default:
		data, err := os.ReadFile(file)
		return string(data), err
	}
}

// documentURL is the link that opens a document in the web UI: on its
// ticket, or on its epic's modal in the Epics view. It is "" if the owner
// cannot be read.
func documentURL(store *db.Store, d *models.DocumentMeta) string {
	display := models.DocumentDisplayName(d.Name, d.Format)
	if d.EpicID != "" {
		e, err := store.GetEpic(d.EpicID)
		if err != nil || e == nil {
			return ""
		}
		p, err := store.GetProject(e.ProjectID)
		if err != nil || p == nil {
			return ""
		}
		return weburl.EpicDocument(weburl.Base(), p.Prefix, e.Name, display)
	}
	t, err := store.GetTicket(d.TicketID)
	if err != nil || t == nil {
		return ""
	}
	return weburl.TicketDocument(weburl.Base(), weburl.Ref(*t), display)
}
