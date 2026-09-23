CREATE TABLE IF NOT EXISTS epics (
    id          TEXT PRIMARY KEY,
    project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    description TEXT DEFAULT '',
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- NOCASE folds ASCII only. The store also compares names with a Unicode-aware
-- fold before writing; this index is the backstop.
CREATE UNIQUE INDEX IF NOT EXISTS idx_epics_project_name ON epics(project_id, name COLLATE NOCASE);

ALTER TABLE tickets ADD COLUMN epic_id TEXT REFERENCES epics(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_tickets_epic_id ON tickets(epic_id);
