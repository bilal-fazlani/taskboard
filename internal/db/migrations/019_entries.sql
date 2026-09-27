-- Entries: short typed records on one project, epic or ticket, saying why the
-- work is as it is and what happened to it (decisions, learnings, hand-offs,
-- proof, reviews, the person's notes). The project journal's entries move in
-- here, and the journal's table goes.
--
-- type is free text checked against models.EntryTypes, the way a ticket's
-- status is, so a new type is a change to that list and not to this table.
-- The same goes for a decision's source (models.DecisionSources), a review's
-- verdict (models.ReviewVerdicts) and the severities its findings are
-- counted by (models.ReviewSeverities, the keys of the findings object).
-- Which fields each type takes, and which types only a ticket can have, are
-- the store's rules, so no check below names a type.
--
-- An entry sits on exactly one project, epic or ticket, and goes with it.
-- It names the agent that wrote it (the session is the agent's), or else the
-- person by name (author_name; the account once there are teams). Deleting
-- an agent that wrote entries, or handled a note, fails.
--
-- Entries are never edited. A new entry can replace an earlier one on the
-- same owner (replaces_id); the replaced entry is kept, and each entry is
-- replaced at most once. The one change an entry takes is a note being marked
-- handled: handled_by and handled_at, set together and only once.
CREATE TABLE IF NOT EXISTS entries (
    id             TEXT PRIMARY KEY,
    project_id     TEXT REFERENCES projects(id) ON DELETE CASCADE,
    epic_id        TEXT REFERENCES epics(id) ON DELETE CASCADE,
    ticket_id      TEXT REFERENCES tickets(id) ON DELETE CASCADE,
    type           TEXT NOT NULL CHECK (type <> ''),
    text           TEXT NOT NULL CHECK (text <> ''),
    agent_id       TEXT REFERENCES agents(id),
    author_name    TEXT NOT NULL DEFAULT '',
    source         TEXT NOT NULL DEFAULT '',
    replaces_id    TEXT REFERENCES entries(id) ON DELETE CASCADE,
    about_id       TEXT REFERENCES entries(id) ON DELETE CASCADE,
    handled_by     TEXT REFERENCES agents(id),
    handled_at     DATETIME,
    verdict        TEXT NOT NULL DEFAULT '',
    findings       TEXT
        CHECK (findings IS NULL OR (json_valid(findings) AND json_type(findings) = 'object')),
    report_document TEXT NOT NULL DEFAULT '',
    created_at     DATETIME NOT NULL,
    CHECK ((project_id IS NOT NULL) + (epic_id IS NOT NULL) + (ticket_id IS NOT NULL) = 1),
    CHECK ((agent_id IS NULL) = (author_name <> '')),
    CHECK ((handled_by IS NULL) = (handled_at IS NULL)),
    CHECK (replaces_id IS NULL OR replaces_id <> id),
    CHECK (about_id IS NULL OR about_id <> id)
);

-- Reads are one owner's entries, newest first.
CREATE INDEX IF NOT EXISTS idx_entries_project ON entries(project_id, created_at) WHERE project_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_entries_epic ON entries(epic_id, created_at) WHERE epic_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_entries_ticket ON entries(ticket_id, created_at) WHERE ticket_id IS NOT NULL;

-- An entry is replaced at most once, and reads find its replacement here.
CREATE UNIQUE INDEX IF NOT EXISTS idx_entries_replaces ON entries(replaces_id) WHERE replaces_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_entries_about ON entries(about_id) WHERE about_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_entries_agent ON entries(agent_id) WHERE agent_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_entries_handled_by ON entries(handled_by) WHERE handled_by IS NOT NULL;

-- Never edited, whichever program opens the file: an update may only mark an
-- open entry handled.
CREATE TRIGGER IF NOT EXISTS entries_never_edited
BEFORE UPDATE ON entries
WHEN OLD.handled_at IS NOT NULL
  OR NEW.id IS NOT OLD.id
  OR NEW.project_id IS NOT OLD.project_id
  OR NEW.epic_id IS NOT OLD.epic_id
  OR NEW.ticket_id IS NOT OLD.ticket_id
  OR NEW.type IS NOT OLD.type
  OR NEW.text IS NOT OLD.text
  OR NEW.agent_id IS NOT OLD.agent_id
  OR NEW.author_name IS NOT OLD.author_name
  OR NEW.source IS NOT OLD.source
  OR NEW.replaces_id IS NOT OLD.replaces_id
  OR NEW.about_id IS NOT OLD.about_id
  OR NEW.verdict IS NOT OLD.verdict
  OR NEW.findings IS NOT OLD.findings
  OR NEW.report_document IS NOT OLD.report_document
  OR NEW.created_at IS NOT OLD.created_at
BEGIN
    SELECT RAISE(ABORT, 'entries are never edited: write a new entry that replaces this one');
END;

-- The project journal's entries become the project's decisions, keeping
-- their id, text, author and time. Their author is the name the writer gave,
-- which names no agent, so they are the person's, and so are the decisions.
-- Inserted in the journal's own order, so entries stamped with the same
-- instant keep their order through rowid. The journal never took a blank
-- author, but a row written some other way gets 'unknown' rather than
-- failing the whole migration.
INSERT INTO entries (id, project_id, type, text, author_name, source, created_at)
SELECT id, project_id, 'decision', text, COALESCE(NULLIF(TRIM(author), ''), 'unknown'), 'person', created_at
FROM project_journal_entries
ORDER BY rowid;

DROP TABLE project_journal_entries;
