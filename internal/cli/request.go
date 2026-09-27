package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

// defaultAwaitTimeout bounds how long `request await` blocks with no
// --timeout given: long enough for a person to notice and answer, short
// enough that a script calling it never hangs unbounded.
const defaultAwaitTimeout = 30 * time.Second

// requestCommands returns the `request` command group: await an answer, and
// give one as the person.
func requestCommands() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "request",
		Short: "Wait on a request for user input, or answer one as the person",
	}

	var awaitTimeout time.Duration
	var awaitJSON bool
	awaitCmd := &cobra.Command{
		Use:   "await [request-id]",
		Short: "Wait for a request to be answered, up to --timeout",
		Long: "Wait for the request to be answered, or until --timeout passes. It waits on this request only, " +
			"never on whatever else is open on its ticket, so an agent acts only on the answer it asked for. " +
			"A timed-out call prints that it is still waiting; call it again to keep waiting.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if awaitTimeout <= 0 {
				return fmt.Errorf("timeout must be a positive duration, such as 30s")
			}
			store, err := openStore()
			if err != nil {
				return err
			}
			r, err := store.AwaitAnswer(cmd.Context(), args[0], awaitTimeout)
			if err != nil {
				return err
			}
			if awaitJSON {
				return encodeJSON(cmd.OutOrStdout(), r)
			}
			if !r.Answered() {
				fmt.Fprintf(cmd.OutOrStdout(), "Request %s is still waiting on the person, after %s: call await again.\n", r.ID, awaitTimeout)
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Request %s answered by %s: %s\n", r.ID, r.AnsweredBy, r.Answer)
			return nil
		},
	}
	awaitCmd.Flags().DurationVar(&awaitTimeout, "timeout", defaultAwaitTimeout, "how long to wait before giving up, e.g. 30s or 5m")
	awaitCmd.Flags().BoolVar(&awaitJSON, "json", false, "print the request as JSON instead of readable text")

	var answer string
	var answerJSON bool
	answerCmd := &cobra.Command{
		Use:   "answer [request-id]",
		Short: "Answer a request for user input, as the person running this shell",
		Long: "Answer a request for user input. This is the person's own command: the answer is recorded as " +
			"given by the local user (the same default author `entry add` uses), never by an agent. When the " +
			"request offers choices, --answer must match one of them (matched ignoring case).",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openStore()
			if err != nil {
				return err
			}
			r, err := store.AnswerRequest(args[0], answer, defaultAuthor())
			if err != nil {
				return err
			}
			if answerJSON {
				return encodeJSON(cmd.OutOrStdout(), r)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Answered request %s as %s: %s\n", r.ID, r.AnsweredBy, r.Answer)
			return nil
		},
	}
	answerCmd.Flags().StringVar(&answer, "answer", "", "the answer to give (required); must be one of the request's choices when it has any")
	_ = answerCmd.MarkFlagRequired("answer")
	answerCmd.Flags().BoolVar(&answerJSON, "json", false, "print the answered request as JSON instead of readable text")

	cmd.AddCommand(awaitCmd, answerCmd)
	return cmd
}
