package cli

import (
	"fmt"
	"strings"

	"github.com/tcarac/taskboard/internal/models"
)

// Help for the dependency flags of ticket create and ticket update.
const (
	dependsOnFlagHelp = "ticket ID or key this depends on, as KEY or KEY:kind where kind is needs_work (the default) " +
		"or conflict_only (waits only to avoid a conflict); comma-separated or repeated"
	dependsOnNoteFlagHelp = "note on one --depends-on ticket, as KEY=text (e.g. the files a conflict_only dependency waits on); " +
		"repeat it for several; commas in the text are kept"
)

// dependenciesFromFlags turns --depends-on entries (KEY or KEY:kind) and
// --depends-on-note entries (KEY=text) into a dependsOn list. A note's ticket
// must be one --depends-on lists: both are resolved with resolve, so an id
// and a display key for the same ticket match. The store checks the kinds.
// It returns an empty, non-nil list for no entries, which clears the set.
func dependenciesFromFlags(entries, notes []string, resolve func(string) (string, error)) ([]models.DependencyInput, error) {
	deps := make([]models.DependencyInput, 0, len(entries))
	for _, entry := range entries {
		ticket, kind, _ := strings.Cut(entry, ":")
		if strings.TrimSpace(ticket) == "" && strings.TrimSpace(kind) == "" {
			continue
		}
		deps = append(deps, models.DependencyInput{Ticket: strings.TrimSpace(ticket), Kind: strings.TrimSpace(kind)})
	}
	if len(notes) == 0 {
		return deps, nil
	}

	ids := make([]string, len(deps))
	for i, d := range deps {
		id, err := resolve(d.Ticket)
		if err != nil {
			return nil, err
		}
		ids[i] = id
	}
	noted := map[string]bool{}
	for _, raw := range notes {
		ticket, text, found := strings.Cut(raw, "=")
		ticket = strings.TrimSpace(ticket)
		if !found || ticket == "" {
			return nil, fmt.Errorf("--depends-on-note takes KEY=text, got %q", raw)
		}
		id, err := resolve(ticket)
		if err != nil {
			return nil, err
		}
		if noted[id] {
			return nil, fmt.Errorf("--depends-on-note gives %s two notes", ticket)
		}
		matched := false
		for i := range deps {
			if ids[i] == id {
				deps[i].Note = text
				matched = true
			}
		}
		if !matched {
			return nil, fmt.Errorf("--depends-on-note names %s, which --depends-on does not list", ticket)
		}
		noted[id] = true
	}
	return deps, nil
}

// dependencyKeys lists dependency keys for readable output, marking each one
// that is only there to avoid a conflict and giving its note, if any:
// "BILL-1, BILL-2 (conflict only; note: store.go)".
func dependencyKeys(refs []models.TicketRef) string {
	keys := make([]string, len(refs))
	for i, r := range refs {
		keys[i] = r.Key + dependencyDetail(r.Kind, r.Note)
	}
	return strings.Join(keys, ", ")
}

// dependencyDetail is what readable output adds after a dependency's key:
// " (conflict only)", " (note: …)", both, or nothing.
func dependencyDetail(kind, note string) string {
	var parts []string
	if kind == models.DependencyConflictOnly {
		parts = append(parts, "conflict only")
	}
	if note != "" {
		parts = append(parts, "note: "+note)
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, "; ") + ")"
}
