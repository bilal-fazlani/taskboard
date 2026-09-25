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
		Short: "Manage documents attached to tickets",
	}

	listCmd := &cobra.Command{
		Use:   "list [ticket]",
		Short: "List a ticket's documents, by ticket id or key, in the order they were added",
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
			docs, err := store.ListDocuments(db.DocumentOwner{TicketID: ticketID})
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

	var showTicket string
	showCmd := &cobra.Command{
		Use:   "show [document]",
		Short: "Print a document's content, by id, or by name with --ticket",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			d, err := loadDocumentArg(store, args[0], showTicket)
			if err != nil {
				return err
			}
			fmt.Print(d.Content)
			return nil
		},
	}
	showCmd.Flags().StringVar(&showTicket, "ticket", "", documentTicketFlagUsage)

	var addFile, addName, addFormat string
	addCmd := &cobra.Command{
		Use:   "add [ticket]",
		Short: "Attach a document to a ticket from a file (--file PATH) or standard input (--file -)",
		Long: "Attach a document to a ticket. Without --name, the name and format come from the file's name: " +
			"the extension (.md) is removed and other symbols become spaces. Names hold letters, digits, " +
			"spaces, _ and - only.",
		Args: cobra.ExactArgs(1),
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
			}
			content, err := readDocumentContent(cmd, addFile)
			if err != nil {
				return err
			}
			store, err := openStore()
			if err != nil {
				return err
			}
			ticketID, err := store.ResolveTicketID(args[0])
			if err != nil {
				return err
			}
			d, err := store.CreateDocument(models.CreateDocumentRequest{
				TicketID: ticketID, Name: name, Format: format, Content: content,
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
	addCmd.Flags().StringVar(&addFormat, "format", "", "document format (markdown); defaults to the file's extension, else markdown")

	var writeTicket, writeFile string
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
			id, err := resolveDocumentArg(store, args[0], writeTicket)
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
	writeCmd.Flags().StringVar(&writeTicket, "ticket", "", documentTicketFlagUsage)
	writeCmd.Flags().StringVar(&writeFile, "file", "", "file to read, or - for standard input")

	var renameTicket string
	renameCmd := &cobra.Command{
		Use:   "rename [document] [new-name]",
		Short: "Rename a document, by id, or by name with --ticket",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			id, err := resolveDocumentArg(store, args[0], renameTicket)
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
			fmt.Printf("Renamed to %s (%s)\n", models.DocumentDisplayName(d.Name, d.Format), d.ID)
			return nil
		},
	}
	renameCmd.Flags().StringVar(&renameTicket, "ticket", "", documentTicketFlagUsage)

	var deleteTicket string
	deleteCmd := &cobra.Command{
		Use:   "delete [document]",
		Short: "Delete a document for good, by id, or by name with --ticket",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			d, err := loadDocumentArg(store, args[0], deleteTicket)
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
	deleteCmd.Flags().StringVar(&deleteTicket, "ticket", "", documentTicketFlagUsage)

	cmd.AddCommand(listCmd, showCmd, addCmd, writeCmd, renameCmd, deleteCmd)
	return cmd
}

const documentTicketFlagUsage = "ticket ID or key; addresses the document by name instead of id"

// resolveDocumentArg resolves a document argument to an id: directly when it
// is an id, or by name within ticketRef when one is given.
func resolveDocumentArg(store *db.Store, ref, ticketRef string) (string, error) {
	if ticketRef != "" {
		ticketID, err := store.ResolveTicketID(ticketRef)
		if err != nil {
			return "", err
		}
		return store.ResolveDocumentRef(db.DocumentOwner{TicketID: ticketID}, ref)
	}
	d, err := store.GetDocument(ref)
	if err != nil {
		return "", err
	}
	if d == nil {
		return "", fmt.Errorf("document not found: %q (to use a name, pass --ticket)", ref)
	}
	return d.ID, nil
}

func loadDocumentArg(store *db.Store, ref, ticketRef string) (*models.Document, error) {
	id, err := resolveDocumentArg(store, ref, ticketRef)
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

// documentURL is the link that opens a document in the web UI, or "" if its
// ticket cannot be read.
func documentURL(store *db.Store, d *models.DocumentMeta) string {
	t, err := store.GetTicket(d.TicketID)
	if err != nil || t == nil {
		return ""
	}
	return weburl.TicketDocument(weburl.Base(), weburl.Ref(*t), models.DocumentDisplayName(d.Name, d.Format))
}
