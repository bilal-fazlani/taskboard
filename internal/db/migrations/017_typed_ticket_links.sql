-- Typed links between tickets, replacing two conventions that lived only in
-- description text.
--
-- A dependency now has a kind: needs_work (the ticket needs the other
-- ticket's work) or conflict_only (it waits for the other ticket only to
-- avoid a merge conflict). Every existing dependency needs work, which is
-- what a dependency meant before kinds existed. It also has an optional
-- note, such as the files a conflict-only dependency waits on.
ALTER TABLE ticket_dependencies ADD COLUMN kind TEXT NOT NULL DEFAULT 'needs_work'
    CHECK (kind IN ('needs_work', 'conflict_only'));
ALTER TABLE ticket_dependencies ADD COLUMN note TEXT NOT NULL DEFAULT '';

-- "Surfaced from": the ticket during whose work this one was found. A ticket
-- has at most one; deleting either ticket removes the link.
CREATE TABLE IF NOT EXISTS ticket_surfaced_from (
    ticket_id TEXT PRIMARY KEY REFERENCES tickets(id) ON DELETE CASCADE,
    source_id TEXT NOT NULL REFERENCES tickets(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_ticket_surfaced_from_source_id ON ticket_surfaced_from(source_id);
