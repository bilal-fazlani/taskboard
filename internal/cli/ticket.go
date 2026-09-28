package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tcarac/taskboard/internal/models"
	"github.com/tcarac/taskboard/internal/ticketlist"
	"github.com/tcarac/taskboard/internal/weburl"
)

func ticketCommands() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ticket",
		Short: "Manage tickets",
	}

	var projectID, priority, listRepo, listLabel, listEpic, listExcludeLabel string
	var listStatuses []string
	var listReady, listSummary bool
	var listLimit, listOffset int
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List tickets",
		Long: "List tickets, every match in full. To pick tickets, --summary prints one short line per ticket instead. " +
			"With --summary, --limit or --offset the list comes a page at a time, and ends by saying where the next page starts.",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			filter := models.TicketFilter{
				ProjectID:    projectID,
				Statuses:     listStatuses,
				Priority:     priority,
				Repo:         listRepo,
				Label:        listLabel,
				Epic:         listEpic,
				Ready:        listReady,
				ExcludeLabel: listExcludeLabel,
			}
			req := ticketlist.Request{Summary: listSummary}
			if cmd.Flags().Changed("limit") {
				req.Limit = &listLimit
			}
			if cmd.Flags().Changed("offset") {
				req.Offset = &listOffset
			}
			if req.Paged() {
				return printTicketPage(cmd.OutOrStdout(), store, filter, req)
			}
			tickets, err := store.ListTickets(filter)
			if err != nil {
				return err
			}
			if len(tickets) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No tickets found.")
				return nil
			}
			printTicketLines(cmd.OutOrStdout(), tickets)
			return nil
		},
	}
	listCmd.Flags().StringVar(&projectID, "project", "", "filter by project ID or prefix (case-insensitive); an unknown one returns no tickets rather than an error")
	listCmd.Flags().StringSliceVar(&listStatuses, "status", nil, fmt.Sprintf("filter by status (%s); comma-separated or repeated for any of several", strings.Join(models.KnownStatuses, "|")))
	listCmd.Flags().StringVar(&priority, "priority", "", "filter by priority (urgent|high|medium|low)")
	listCmd.Flags().StringVar(&listRepo, "repo", "", "filter by repo")
	listCmd.Flags().StringVar(&listLabel, "label", "", "filter by label name")
	listCmd.Flags().StringVar(&listEpic, "epic", "", `filter by epic name (case-insensitive) or id, or "none" for tickets without an epic`)
	listCmd.Flags().BoolVar(&listReady, "ready", false, "only tickets ready to start: todo, with every ticket they depend on done; combined with --status, a list without todo matches nothing")
	listCmd.Flags().StringVar(&listExcludeLabel, "exclude-label", "", "leave out tickets with this label name (case-insensitive), e.g. hold")
	listCmd.Flags().BoolVar(&listSummary, "summary", false, "one short line per ticket (key, title, status, priority, epic, labels, dependencies with their status, subtask progress, url), a page at a time; use it to pick tickets")
	listCmd.Flags().IntVar(&listLimit, "limit", ticketlist.DefaultLimit, fmt.Sprintf("page size, 1 to %d; turns on paging", ticketlist.MaxLimit))
	listCmd.Flags().IntVar(&listOffset, "offset", 0, "how many matching tickets to skip before the page starts; turns on paging")

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
	var createSurfacedFrom string
	var createDependsOnNotes []string
	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new ticket",
		Long:  "Create a new ticket. " + ticketImageRefsHelp,
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
			if len(createDependsOn) > 0 || len(createDependsOnNotes) > 0 {
				if req.DependsOn, err = dependenciesFromFlags(createDependsOn, createDependsOnNotes, store.ResolveTicketID); err != nil {
					return err
				}
			}
			if createSurfacedFrom != "" {
				req.SurfacedFrom = &createSurfacedFrom
			}
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
	createCmd.Flags().StringSliceVar(&createDependsOn, "depends-on", nil, dependsOnFlagHelp)
	createCmd.Flags().StringArrayVar(&createDependsOnNotes, "depends-on-note", nil, dependsOnNoteFlagHelp)
	createCmd.Flags().StringVar(&createSurfacedFrom, "surfaced-from", "", "ticket ID or key of the ticket during whose work this one was found")

	var moveStatus, moveNote string
	moveCmd := &cobra.Command{
		Use:   "move [id-or-key]",
		Short: "Move ticket to different status, by id or display key (e.g. BILL-2)",
		Long:  "Move a ticket, by id or display key (e.g. BILL-2, case-insensitive), to the end of another status column.\n\n" + waitingStatusHelp,
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
		updSurfacedFrom                                                            string
		updDependsOnNotes                                                          []string
		updRepos, updLabels, updLabelAlias, updDependsOn                           []string
		updAppendDescription                                                       string
		updBranch, updWorktree, updPRURL                                           string
		updLandedCommits                                                           []string
	)
	updateCmd := &cobra.Command{
		Use:   "update [id-or-key]",
		Short: "Update ticket fields, by id or display key (e.g. BILL-2)",
		Long: "Update ticket fields, identified by id or display key (e.g. BILL-2, case-insensitive).\n\n" +
			waitingStatusHelp + "\n\n" +
			"Omitting a flag leaves that field untouched. Passing --repo, --labels " +
			"or --depends-on replaces the existing set, so passing one with an empty " +
			"value clears it. --label is accepted as an alias for --labels, matching " +
			"the spelling used by 'ticket create' and 'ticket list'. --due follows the " +
			"same rule: omit it to leave the due date alone, or pass --due=\"\" to clear it. " +
			"--epic follows the same rule as --due: omit it to leave the epic alone, or " +
			"pass --epic=\"\" or --epic=none to clear it; anything else names an epic " +
			"(by name, case-insensitive, or id) in the ticket's own project.\n\n" +
			"Each --depends-on entry may end in a kind: BILL-2:conflict_only when the " +
			"ticket waits for BILL-2 only to avoid a conflict, BILL-2:needs_work (the " +
			"default) when it needs BILL-2's work. --depends-on-note BILL-2=\"store.go, mcp.go\" " +
			"gives a listed dependency a note, such as the files it waits on. The list " +
			"given is the whole set, kinds and notes included, so a dependency passed " +
			"without a kind or note needs work and has none. " +
			"--surfaced-from follows the same rule as --epic: omit it to leave the " +
			"link alone, or pass --surfaced-from=\"\" or --surfaced-from=none to remove it.\n\n" +
			"--append-description adds text to the end of the description instead of " +
			"replacing it, leaving the existing text untouched: on a non-empty " +
			"description the text starts a new paragraph (a blank line before it), on " +
			"an empty one it becomes the description. It cannot be combined with " +
			"--description and must not be empty.\n\n" +
			ticketImageRefsHelp + "\n\n" +
			deliveryFlagsHelp,
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
			if cmd.Flags().Changed("append-description") {
				req.AppendDescription = &updAppendDescription
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
			if cmd.Flags().Changed("surfaced-from") {
				req.SurfacedFrom = &updSurfacedFrom
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
			if cmd.Flags().Changed("depends-on-note") && !cmd.Flags().Changed("depends-on") {
				return fmt.Errorf("--depends-on-note needs --depends-on: the dependencies are replaced as a whole, so list them all")
			}
			if cmd.Flags().Changed("depends-on") {
				if req.DependsOn, err = dependenciesFromFlags(updDependsOn, updDependsOnNotes, store.ResolveTicketID); err != nil {
					return err
				}
			}
			if req.Delivery, err = deliveryUpdateFromFlags(cmd, updBranch, updWorktree, updPRURL, updLandedCommits); err != nil {
				return err
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
	updateCmd.Flags().StringVar(&updAppendDescription, "append-description", "", "text to add to the description as a new paragraph")
	updateCmd.MarkFlagsMutuallyExclusive("description", "append-description")
	updateCmd.Flags().StringVar(&updStatus, "status", "", fmt.Sprintf("status (%s)", strings.Join(models.Statuses, "|")))
	updateCmd.Flags().StringVar(&updNote, "note", "", noteFlagUsage)
	updateCmd.Flags().StringVar(&updPriority, "priority", "", "priority (urgent|high|medium|low)")
	updateCmd.Flags().StringVar(&updDue, "due", "", "due date (YYYY-MM-DD); empty value clears it")
	updateCmd.Flags().StringVar(&updEpic, "epic", "", `epic name (case-insensitive) or id; "" or "none" clears it`)
	updateCmd.Flags().StringSliceVar(&updRepos, "repo", nil, "replace repos; comma-separated or repeated, empty value clears")
	updateCmd.Flags().StringSliceVar(&updLabels, "labels", nil, "replace labels; comma-separated or repeated, empty value clears")
	updateCmd.Flags().StringSliceVar(&updLabelAlias, "label", nil, "alias for --labels")
	updateCmd.Flags().StringSliceVar(&updDependsOn, "depends-on", nil, "replace dependencies: "+dependsOnFlagHelp+"; empty value clears")
	updateCmd.Flags().StringArrayVar(&updDependsOnNotes, "depends-on-note", nil, dependsOnNoteFlagHelp)
	updateCmd.Flags().StringVar(&updSurfacedFrom, "surfaced-from", "", `ticket ID or key this was found during; "" or "none" removes the link`)
	updateCmd.Flags().StringVar(&updBranch, "branch", "", "the branch the work is on; empty value clears")
	updateCmd.Flags().StringVar(&updWorktree, "worktree", "", "the worktree path the work is in; empty value clears")
	updateCmd.Flags().StringVar(&updPRURL, "pr-url", "", "the pull request's http or https url; empty value clears")
	updateCmd.Flags().StringSliceVar(&updLandedCommits, "landed-commit", nil, "replace the landed commits, in order, each [repo@]sha; comma-separated or repeated, empty value clears")

	var startAgent string
	var startJSON bool
	startCmd := &cobra.Command{
		Use:   "start [id-or-key]",
		Short: "Begin work on a ticket as an agent: claim it and print everything needed to begin, as JSON",
		Long: "Begin work on a ticket: claim it for --agent and print, as compact JSON, the same answer the MCP " +
			"start_ticket tool and POST /api/tickets/{id}/start give: the ticket, lean (subtasks with id, title and " +
			"completed; labels with name and color; linked tickets by key, title, status, and a dependency's kind " +
			"and note, with no ids), its project and epic, their entries (the project's 5 newest; each entry an " +
			"agent wrote names its agentRole and agentModel), the project's agent instructions (on the agent's " +
			"first start in the project and again once they change; later starts carry agentInstructionsLeftOut, " +
			"saying where to read them again, instead), the latest hand-off, every open note, the agent it was taken " +
			"over from, the person's stop this start lifts (stopped: by and at), the ticket's answeredRequests " +
			"(its answered requests for user input, newest first: type, prompt, answer, note, answeredBy, " +
			"answeredAt; a request closed by Stop work instead of an answer carries stopped: true and no answer) " +
			"and the unfinished dependencies, " +
			"each only when there is one. Refused while an agent of " +
			"another session holds the ticket and is live, naming it and when its session was last seen; once that " +
			"session is stale, the ticket is taken over. A done ticket is refused too, naming it and how to reopen " +
			"it on purpose: move it to todo, then start it; the ticket stays done with no holder. There is no " +
			"separate claim command.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			start, err := store.StartTicket(args[0], startAgent)
			if err != nil {
				return err
			}
			weburl.Fill(start.Ticket.Ticket)
			// Compact, as MCP sends it, with or without --json: the start is
			// read by agents, and indentation is only tokens.
			return json.NewEncoder(cmd.OutOrStdout()).Encode(start)
		},
	}
	startCmd.Flags().StringVar(&startAgent, "agent", "", "the id of the agent beginning work (required)")
	_ = startCmd.MarkFlagRequired("agent")
	startCmd.Flags().BoolVar(&startJSON, "json", false,
		"print the start as JSON, like the other agent commands; it is compact JSON either way")

	var releaseAgent, releaseStopped, releaseNext, releaseProof string
	var releaseDone, releaseJSON bool
	releaseCmd := &cobra.Command{
		Use:   "release [id-or-key]",
		Short: "Give a ticket back to todo, or finish it, freeing the agent that holds it",
		Long: "Give the ticket back with --stopped and --next (where the work stopped and the next step), " +
			"or finish it with --done and --proof (what was verified, how, and the result); refused without " +
			"them, naming what is missing. --agent is the agent releasing the ticket, or another agent of " +
			"its session.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			// Trimmed first: a flag given as only whitespace is blank, the
			// same as not given at all, so it is refused by name rather than
			// silently joined into a hollow hand-off (Review 1).
			stopped := strings.TrimSpace(releaseStopped)
			next := strings.TrimSpace(releaseNext)
			proof := strings.TrimSpace(releaseProof)

			var req models.ReleaseTicketRequest
			req.AgentID = releaseAgent
			switch {
			case releaseDone && (stopped != "" || next != ""):
				return fmt.Errorf("--done finishes the ticket; --stopped and --next give it back, not both")
			case releaseDone:
				if proof == "" {
					return fmt.Errorf("finishing a ticket needs --proof: what was verified, how, and the result")
				}
				req.Outcome = models.ReleaseFinish
				req.Proof = proof
			case proof != "":
				return fmt.Errorf("--proof finishes the ticket: pass --done too")
			case stopped != "" || next != "":
				if stopped == "" {
					return fmt.Errorf("giving a ticket back needs --stopped too: where the work stopped")
				}
				if next == "" {
					return fmt.Errorf("giving a ticket back needs --next too: the next step")
				}
				req.Outcome = models.ReleaseGiveBack
				req.HandOff = formatHandOff(stopped, next)
			default:
				return fmt.Errorf("give the ticket back with --stopped and --next, or finish it with --done and --proof")
			}
			t, err := store.ReleaseTicket(args[0], req)
			if err != nil {
				return err
			}
			if releaseJSON {
				return encodeJSON(cmd.OutOrStdout(), t)
			}
			verb := "Gave back"
			if req.Outcome == models.ReleaseFinish {
				verb = "Finished"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s: now %s\n", verb, t.DisplayKey(), t.Status)
			return nil
		},
	}
	releaseCmd.Flags().StringVar(&releaseAgent, "agent", "", "the id of the agent releasing the ticket (required)")
	_ = releaseCmd.MarkFlagRequired("agent")
	releaseCmd.Flags().StringVar(&releaseStopped, "stopped", "", "where the work stopped, to give the ticket back")
	releaseCmd.Flags().StringVar(&releaseNext, "next", "", "the next step, to give the ticket back")
	releaseCmd.Flags().BoolVar(&releaseDone, "done", false, "finish the ticket instead of giving it back")
	releaseCmd.Flags().StringVar(&releaseProof, "proof", "", "what was verified, how, and the result, to finish the ticket")
	releaseCmd.Flags().BoolVar(&releaseJSON, "json", false, "print the released ticket as JSON instead of readable text")

	var stopJSON bool
	stopCmd := &cobra.Command{
		Use:   "stop [id-or-key]",
		Short: "Stop the work an agent does on a ticket, as the person running this shell",
		Long: "Stop the work an agent does on a ticket, live or stale: the agent is cleared and the ticket goes back " +
			"to todo at once, recorded as stopped by the local user (the same default author `entry add` uses). " +
			"This is the person's own command; agents give work back with `ticket release`. The agent is not asked: " +
			"its next write on the ticket is refused as stopped by the person, all but its hand-off.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			t, err := store.StopWork(args[0], defaultAuthor())
			if err != nil {
				return err
			}
			if stopJSON {
				return encodeJSON(cmd.OutOrStdout(), t)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Stopped work on %s: now %s\n", t.DisplayKey(), t.Status)
			return nil
		},
	}
	stopCmd.Flags().BoolVar(&stopJSON, "json", false, "print the stopped ticket as JSON instead of readable text")

	var askAgent, askType, askPrompt string
	var askChoices []string
	var askJSON bool
	askCmd := &cobra.Command{
		Use:   "ask [id-or-key]",
		Short: "Ask the person for user input on a ticket, moving it to needs_user_input",
		Long: "Ask the person for user input on a ticket an agent (or its session) holds; prints the new " +
			"request's id. --type is " + strings.Join(models.UserInputTypes, " or ") + "; --choice " +
			"(repeatable) suggests an answer for a question, which the person may still answer with something " +
			"else instead, or leave it out for a free answer. An approval's answer is approved or " +
			"declined, whatever choices it offers, so --choice changes nothing for it: state exactly what the " +
			"agent will do if approved, since it acts only on that approval, as stated. If the person stops work " +
			"on the ticket first, either type closes with \"" + models.StoppedAnswer + "\": the ticket is no " +
			"longer the agent's.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			id, err := store.CreateRequest(models.CreateUserInputRequest{
				TicketID: args[0], AgentID: askAgent, Type: askType, Prompt: askPrompt, Choices: askChoices,
			})
			if err != nil {
				return err
			}
			if askJSON {
				r, err := store.GetRequest(id)
				if err != nil {
					return err
				}
				return encodeJSON(cmd.OutOrStdout(), r)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Request %s (%s) created on %s\n", id, askType, args[0])
			return nil
		},
	}
	askCmd.Flags().StringVar(&askAgent, "agent", "", "the id of the agent asking (required)")
	_ = askCmd.MarkFlagRequired("agent")
	askCmd.Flags().StringVar(&askType, "type", "", "the type of user input: "+strings.Join(models.UserInputTypes, ", ")+" (required)")
	_ = askCmd.MarkFlagRequired("type")
	askCmd.Flags().StringVar(&askPrompt, "prompt", "", "what the person is asked (required)")
	_ = askCmd.MarkFlagRequired("prompt")
	askCmd.Flags().StringArrayVar(&askChoices, "choice", nil, "a suggested answer for a question (repeatable), which the person may still answer freely instead; changes nothing for an approval")
	askCmd.Flags().BoolVar(&askJSON, "json", false, "print the new request as JSON instead of readable text")

	cmd.AddCommand(listCmd, getCmd, createCmd, moveCmd, deleteCmd, updateCmd, historyCmd, startCmd, releaseCmd, stopCmd, askCmd,
		findByCommitCommand(), subtaskCommands())
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
		fmt.Fprintf(&b, "Depends on: %s\n", dependencyKeys(t.DependsOn))
	}
	if len(t.Blocks) > 0 {
		fmt.Fprintf(&b, "Blocks: %s\n", dependencyKeys(t.Blocks))
	}
	if t.SurfacedFrom != nil {
		fmt.Fprintf(&b, "Surfaced from: %s\n", t.SurfacedFrom.Key)
	}
	if len(t.Surfaced) > 0 {
		keys := make([]string, len(t.Surfaced))
		for i, r := range t.Surfaced {
			keys[i] = r.Key
		}
		fmt.Fprintf(&b, "Surfaced: %s\n", strings.Join(keys, ", "))
	}
	b.WriteString(formatDelivery(t.Delivery))
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

// waitingStatusHelp is the rule ticket move and ticket update --status share
// for a ticket that waits on the person.
const waitingStatusHelp = "A ticket in needs_user_input cannot be moved out while its request is open, " +
	"and the command is refused: only the person takes it out, by answering the request " +
	"(on the ticket page, or with 'request answer') or by stopping the work ('ticket stop')."

// formatHandOff joins where the work stopped and the next step into the one
// hand-off entry `ticket release` gives the ticket back with.
func formatHandOff(stopped, next string) string {
	stopped = strings.TrimSpace(stopped)
	if last := stopped[len(stopped)-1:]; last != "." && last != "!" && last != "?" {
		stopped += "."
	}
	return fmt.Sprintf("%s Next: %s", stopped, strings.TrimSpace(next))
}

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

// imageRefsHelp tells how text refers to its owner's images, in the wording
// of the MCP tool descriptions.
const imageRefsHelp = "To show one of the owner's own images in text, refer to it by name with its extension: " +
	"![](Login screen.png), ![](<Login screen.png>) or ![](Login%20screen.png) in markdown, " +
	"<img src=\"Login screen.png\"> in an HTML document. Only the ticket's or epic's own images resolve; " +
	"a name that matches none shows as a missing image."

// ticketImageRefsHelp is imageRefsHelp for a ticket's description, which shows
// only that ticket's own images.
const ticketImageRefsHelp = "To show one of the ticket's own images in its description, refer to it by name with its extension: " +
	"![](Login screen.png), ![](<Login screen.png>) or ![](Login%20screen.png). Only that ticket's own images " +
	"show, not its epic's; a name that matches none shows as a missing image."
