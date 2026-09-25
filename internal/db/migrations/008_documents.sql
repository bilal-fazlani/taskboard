-- Documents attached to a ticket or, from the epics ticket on, to an epic.
-- Exactly one owner is set. revision counts content saves, so a client can
-- tell whether the content changed since it read it; a rename does not bump
-- it. Names are unique per owner ignoring case: the store compares with a
-- Unicode-aware fold before writing, and these NOCASE indexes (ASCII only)
-- are the backstop, as for epics.
CREATE TABLE IF NOT EXISTS documents (
    id         TEXT PRIMARY KEY,
    ticket_id  TEXT REFERENCES tickets(id) ON DELETE CASCADE,
    epic_id    TEXT REFERENCES epics(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    format     TEXT NOT NULL CHECK (format IN ('markdown', 'html')),
    content    TEXT NOT NULL DEFAULT '',
    revision   INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    CHECK ((ticket_id IS NULL) != (epic_id IS NULL))
);

CREATE INDEX IF NOT EXISTS idx_documents_ticket_id ON documents(ticket_id);
CREATE INDEX IF NOT EXISTS idx_documents_epic_id ON documents(epic_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_ticket_name
    ON documents(ticket_id, name COLLATE NOCASE) WHERE ticket_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_epic_name
    ON documents(epic_id, name COLLATE NOCASE) WHERE epic_id IS NOT NULL;
