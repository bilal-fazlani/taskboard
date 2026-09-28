package models

// Start is what starting a ticket answers: the one read an agent makes at
// the beginning of every ticket, so it carries everything needed to begin
// and nothing twice. Each part is left out when it is empty, and no entry
// carries its owner, since the part it sits in names it.
type Start struct {
	// Ticket is the ticket as it now stands, held by the calling agent. Its
	// Agent is left out: it is the caller.
	Ticket *Ticket `json:"ticket"`
	// TakenFrom is the stale agent the start took the ticket over from.
	TakenFrom *Agent `json:"takenFrom,omitempty"`
	// Stopped is who stopped this session's work on the ticket, and when,
	// when this start takes it up again after the person stopped it.
	Stopped *Stop `json:"stopped,omitempty"`
	// HandOff is the ticket's latest current hand-off, whoever wrote it:
	// where the work stands. It is not repeated in Entries.
	HandOff *Entry `json:"handOff,omitempty"`
	// Entries are the ticket's other current entries, newest first, all of
	// them, open notes aside.
	Entries []Entry `json:"entries,omitempty"`
	// Notes are the person's open notes on the ticket, its epic and its
	// project, all of them: instructions to act on, then mark handled.
	Notes *StartNotes `json:"notes,omitempty"`
	// Epic is the ticket's epic, beyond the id and name the ticket carries.
	Epic *StartEpic `json:"epic,omitempty"`
	// Project is the ticket's project, beyond the id and prefix the ticket
	// carries.
	Project StartProject `json:"project"`
	// UnfinishedDependencies are the keys of the tickets this one depends
	// on that are not done. They never block a start; the ticket's dependsOn
	// carries each one's title, status and kind.
	UnfinishedDependencies []string `json:"unfinishedDependencies,omitempty"`
}

// StartNotes are the open notes at each level, newest first.
type StartNotes struct {
	Ticket  []Entry `json:"ticket,omitempty"`
	Epic    []Entry `json:"epic,omitempty"`
	Project []Entry `json:"project,omitempty"`
}

// Empty reports whether no level has an open note.
func (n StartNotes) Empty() bool {
	return len(n.Ticket) == 0 && len(n.Epic) == 0 && len(n.Project) == 0
}

// StartEpic is the ticket's epic in a start: its description, the names of
// its documents (read one with its name), and its current entries, all of
// them, open notes aside.
type StartEpic struct {
	Description string   `json:"description,omitempty"`
	Documents   []string `json:"documents,omitempty"`
	Entries     []Entry  `json:"entries,omitempty"`
}

// Empty reports whether the epic has nothing to say beyond its name.
func (e StartEpic) Empty() bool {
	return e.Description == "" && len(e.Documents) == 0 && len(e.Entries) == 0
}

// StartProject is the ticket's project in a start: its name, description
// and agent instructions, and a page of its newest current entries, open
// notes aside, with their total and the cursor for the rest. The total
// leaves out the open notes (they are under Start.Notes), where list_entries
// counts them, so paging on from the cursor with list_entries can return an
// open note the start already gave.
type StartProject struct {
	Name              string     `json:"name"`
	Description       string     `json:"description,omitempty"`
	AgentInstructions string     `json:"agentInstructions,omitempty"`
	Entries           *EntryPage `json:"entries,omitempty"`
}
