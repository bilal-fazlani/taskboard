-- A project's journal: dated entries, each with its author, that are
-- appended and never rewritten. Running notes used to live in the project
-- description, which every read carried whole and every update resent whole.
-- An entry goes only when its project is deleted (ON DELETE CASCADE).
--
-- author is free text the caller supplies: there are no agent identities yet,
-- so it is whatever name the writer gives, not a reference to anything.
CREATE TABLE IF NOT EXISTS project_journal_entries (
    id         TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    author     TEXT NOT NULL,
    text       TEXT NOT NULL,
    created_at DATETIME NOT NULL
);

-- Reads are one project's entries, newest first.
CREATE INDEX IF NOT EXISTS idx_project_journal_entries_project
    ON project_journal_entries(project_id, created_at);

-- Append-only: no entry is ever changed after it is written, whichever
-- program opens the file.
CREATE TRIGGER IF NOT EXISTS project_journal_entries_append_only
BEFORE UPDATE ON project_journal_entries
BEGIN
    SELECT RAISE(ABORT, 'project journal entries are append-only');
END;
