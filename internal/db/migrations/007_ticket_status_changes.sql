-- One row per status change, whichever surface made it. Creating a ticket
-- writes the first row, with an empty from_status, so a ticket's history
-- starts at its birth; tickets that existed before this migration get no
-- backfill. There is no actor column yet: agent identity (M4) adds one.
CREATE TABLE IF NOT EXISTS ticket_status_changes (
    id          TEXT PRIMARY KEY,
    ticket_id   TEXT NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    from_status TEXT NOT NULL DEFAULT '',
    to_status   TEXT NOT NULL,
    note        TEXT NOT NULL DEFAULT '',
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_ticket_status_changes_ticket_id ON ticket_status_changes(ticket_id);
