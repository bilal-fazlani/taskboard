package models

// Start is what starting a ticket answers: the one read an agent makes at
// the beginning of every ticket, so it carries everything needed to begin
// and nothing twice. Each part is left out when it is empty, and no entry
// carries its owner, since the part it sits in names it.
type Start struct {
	// Ticket is the ticket as it now stands, held by the calling agent, in
	// the start's lean shape (StartTicket). Its Agent is left out: it is the
	// caller.
	Ticket *StartTicket `json:"ticket"`
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

// StartTicket is the ticket as a start carries it: the whole ticket, with
// its lists lean. A subtask keeps its id (toggle_subtask takes it) but not
// its ticket's id or its position, the list's order; a label keeps its name
// and color; a linked ticket (dependsOn, blocks, surfacedFrom, surfaced)
// keeps its key, title, status, and a dependency's kind and note, but not
// its id: an agent names a ticket by its key. Each lean list is declared
// here with the ticket's own JSON name, so it shadows the embedded
// ticket's; NewStartTicket moves the ticket's lists into them.
type StartTicket struct {
	*Ticket
	Labels       []StartLabel     `json:"labels,omitempty"`
	Subtasks     []StartSubtask   `json:"subtasks,omitempty"`
	DependsOn    []StartTicketRef `json:"dependsOn,omitempty"`
	Blocks       []StartTicketRef `json:"blocks,omitempty"`
	SurfacedFrom *StartTicketRef  `json:"surfacedFrom,omitempty"`
	Surfaced     []StartTicketRef `json:"surfaced,omitempty"`
}

// StartSubtask is a subtask in a start: its id, title and whether it is
// done.
type StartSubtask struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Completed bool   `json:"completed"`
}

// StartLabel is a label in a start: its name and color.
type StartLabel struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

// StartTicketRef is a linked ticket in a start: a TicketRef without its id.
type StartTicketRef struct {
	Key    string `json:"key"`
	Title  string `json:"title"`
	Status string `json:"status"`
	Kind   string `json:"kind,omitempty"`
	Note   string `json:"note,omitempty"`
}

// NewStartTicket is t in the start's lean shape. It moves t's labels,
// subtasks and links into their lean lists, leaving t's own nil, so each is
// held once.
func NewStartTicket(t *Ticket) *StartTicket {
	st := &StartTicket{Ticket: t}
	for _, l := range t.Labels {
		st.Labels = append(st.Labels, StartLabel{Name: l.Name, Color: l.Color})
	}
	for _, s := range t.Subtasks {
		st.Subtasks = append(st.Subtasks, StartSubtask{ID: s.ID, Title: s.Title, Completed: s.Completed})
	}
	st.DependsOn = startTicketRefs(t.DependsOn)
	st.Blocks = startTicketRefs(t.Blocks)
	st.Surfaced = startTicketRefs(t.Surfaced)
	if t.SurfacedFrom != nil {
		ref := startTicketRef(*t.SurfacedFrom)
		st.SurfacedFrom = &ref
	}
	t.Labels, t.Subtasks, t.DependsOn, t.Blocks, t.SurfacedFrom, t.Surfaced = nil, nil, nil, nil, nil, nil
	return st
}

func startTicketRef(r TicketRef) StartTicketRef {
	return StartTicketRef{Key: r.Key, Title: r.Title, Status: r.Status, Kind: r.Kind, Note: r.Note}
}

func startTicketRefs(refs []TicketRef) []StartTicketRef {
	var out []StartTicketRef
	for _, r := range refs {
		out = append(out, startTicketRef(r))
	}
	return out
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
//
// The agent instructions come on each agent's first start in the project,
// and again once they change: an agent that was given these instructions
// by an earlier start gets AgentInstructionsLeftOut in their place, saying
// so and how to read them again. A project with no agent instructions has
// neither.
type StartProject struct {
	Name                     string     `json:"name"`
	Description              string     `json:"description,omitempty"`
	AgentInstructions        string     `json:"agentInstructions,omitempty"`
	AgentInstructionsLeftOut string     `json:"agentInstructionsLeftOut,omitempty"`
	Entries                  *EntryPage `json:"entries,omitempty"`
}
