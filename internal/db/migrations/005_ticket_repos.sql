CREATE TABLE IF NOT EXISTS ticket_repos (
    ticket_id TEXT NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    repo      TEXT NOT NULL,
    PRIMARY KEY (ticket_id, repo)
);

-- Carry every existing single repo across before the column goes away.
INSERT OR IGNORE INTO ticket_repos (ticket_id, repo)
    SELECT id, TRIM(repo) FROM tickets WHERE repo IS NOT NULL AND TRIM(repo) != '';

DROP INDEX IF EXISTS idx_tickets_repo;
ALTER TABLE tickets DROP COLUMN repo;

CREATE INDEX IF NOT EXISTS idx_ticket_repos_repo ON ticket_repos(repo);
