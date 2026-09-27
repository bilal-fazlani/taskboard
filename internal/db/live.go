package db

// Deleting a project archives it (DeleteProject): its status becomes
// ProjectArchived and no row is removed, but from then on the project and
// everything in it are gone from every read the API, MCP and CLI serve. That
// is the whole meaning of archived; nothing but DeleteProject sets it and
// nothing restores it.
//
// The sources below are that one rule, written once. A read of projects,
// tickets, epics, documents or subtasks takes its rows from them rather than
// from the table, so it cannot forget the rule. Every source is a subquery
// with the table's own columns, used in place of the table name:
// `FROM ` + liveTickets + ` t`. TestReadsGoThroughLiveSources fails a new
// FROM or JOIN of one of these five tables that does not go through them and
// is not on its short list of reads that must see archived rows.
//
// Writes are not checked by that test. Each one finds its row through these
// sources first, by a read or an `id IN (SELECT id FROM live...)` condition
// on the write itself, so an archived project's rows are as not-found to a
// write as to a read; a new write has to do the same.
//
// They are subqueries in Go rather than views in the database so that a
// migration rebuilding one of these tables never has to drop and recreate a
// view that names it.
const (
	// DeleteProjectHelp is what deleting a project does, in the words the
	// MCP delete_project tool and the CLI's project delete both show.
	DeleteProjectHelp = "Delete a project: it and its tickets, epics and documents disappear from every view and tool; " +
		"nothing is removed from the database and nothing restores it"

	// ProjectActive and ProjectArchived are a project's two statuses.
	ProjectActive   = "active"
	ProjectArchived = "archived"

	// liveProjectIDs is the ids of the projects that are not archived.
	liveProjectIDs = `SELECT id FROM projects WHERE status <> '` + ProjectArchived + `'`

	liveProjects  = `(SELECT * FROM projects WHERE status <> '` + ProjectArchived + `')`
	liveTickets   = `(SELECT * FROM tickets WHERE project_id IN (` + liveProjectIDs + `))`
	liveEpics     = `(SELECT * FROM epics WHERE project_id IN (` + liveProjectIDs + `))`
	liveTicketIDs = `SELECT id FROM tickets WHERE project_id IN (` + liveProjectIDs + `)`
	liveEpicIDs   = `SELECT id FROM epics WHERE project_id IN (` + liveProjectIDs + `)`
	// A document belongs to a ticket or an epic, so it is live when its
	// owner is.
	liveDocuments = `(SELECT * FROM documents WHERE ticket_id IN (` + liveTicketIDs + `)
		OR epic_id IN (` + liveEpicIDs + `))`
	liveSubtasks = `(SELECT * FROM subtasks WHERE ticket_id IN (` + liveTicketIDs + `))`
)
