package models

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// The kinds of dependency. A ticket that needs work depends on what the
// other ticket builds; one that is conflict only could be done without it
// and waits only so the two don't change the same files at once. Both keep
// a ticket out of the ready list until the other ticket is done.
const (
	DependencyNeedsWork    = "needs_work"
	DependencyConflictOnly = "conflict_only"
)

// DependencyKinds lists every dependency kind, needs_work first: it is the
// kind a dependency gets when none is given.
var DependencyKinds = []string{DependencyNeedsWork, DependencyConflictOnly}

// ValidDependencyKind reports whether kind is one of DependencyKinds.
func ValidDependencyKind(kind string) bool {
	for _, k := range DependencyKinds {
		if k == kind {
			return true
		}
	}
	return false
}

// DependencyInput is one entry of a create or update request's dependsOn:
// the ticket depended on (an id or display key), its kind (needs_work when
// left out) and an optional note, such as the files a conflict-only
// dependency waits on. A request's dependsOn is always the whole list.
type DependencyInput struct {
	Ticket string `json:"ticket"`
	Kind   string `json:"kind,omitempty"`
	Note   string `json:"note,omitempty"`
}

// DependencyShape is the shape of a dependsOn entry, for error messages.
const DependencyShape = `{"ticket": "BILL-2", "kind": "needs_work" or "conflict_only" (optional, needs_work by default), "note": "..." (optional)}`

// DependencyInputError is a dependsOn entry that is not a dependency
// object: a plain string, or an object with a field it doesn't have. The
// HTTP API answers it with a 400 carrying its message, rather than a bare
// "invalid JSON".
type DependencyInputError struct {
	Msg string
}

func (e *DependencyInputError) Error() string { return e.Msg }

// UnmarshalJSON reads a dependsOn entry, refusing a plain string (the form
// dependsOn took before it had kinds) and unknown fields with an error that
// names the object shape.
func (d *DependencyInput) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	if len(trimmed) > 0 && trimmed[0] != '{' {
		return &DependencyInputError{Msg: fmt.Sprintf(
			"each dependsOn entry is an object %s, not %s", DependencyShape, string(trimmed))}
	}
	type plain DependencyInput
	var p plain
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return &DependencyInputError{Msg: fmt.Sprintf(
			"dependsOn entry %s is not a dependency: %v; each entry is an object %s", string(trimmed), err, DependencyShape)}
	}
	*d = DependencyInput(p)
	return nil
}

// DependOn is a dependsOn list of the given tickets, each needing work with
// no note. It is nil for no tickets.
func DependOn(tickets ...string) []DependencyInput {
	if tickets == nil {
		return nil
	}
	deps := make([]DependencyInput, len(tickets))
	for i, t := range tickets {
		deps[i] = DependencyInput{Ticket: t}
	}
	return deps
}

// NoSurfacedFrom is the surfacedFrom value, other than "", that removes a
// ticket's surfaced-from link, matched in any case like NoEpic.
const NoSurfacedFrom = "none"

// NormalizeDependencyKind trims and lowercases a kind as given; "" stays ""
// and means needs_work.
func NormalizeDependencyKind(kind string) string {
	return strings.ToLower(strings.TrimSpace(kind))
}
