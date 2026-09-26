-- Where a ticket's work lives and where it landed, which until now was free
-- text in its description ("Worktree: ...", "Landed in: 6bafa19"). Commits
-- never name their ticket, so these rows are the only link between the two,
-- and a commit's sha finds its ticket through idx_ticket_landed_commits_sha.
--
-- A ticket has a ticket_delivery row only while one of its text fields is
-- set. Its landed commits are one row each, in the order they were given
-- (position), each with the repo it landed in, so one ticket can land in
-- several repos. Both go with the ticket when it is deleted.
CREATE TABLE IF NOT EXISTS ticket_delivery (
    ticket_id TEXT PRIMARY KEY REFERENCES tickets(id) ON DELETE CASCADE,
    branch    TEXT NOT NULL DEFAULT '',
    worktree  TEXT NOT NULL DEFAULT '',
    pr_url    TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS ticket_landed_commits (
    ticket_id TEXT NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    repo      TEXT NOT NULL,
    sha       TEXT NOT NULL,
    position  INTEGER NOT NULL,
    PRIMARY KEY (ticket_id, repo, sha)
);

CREATE INDEX IF NOT EXISTS idx_ticket_landed_commits_sha ON ticket_landed_commits(sha);
