-- The install's agent timings, one value each for the whole install, so
-- every process that opens the file (the web server, each MCP server, the
-- CLI) judges an agent's staleness the same way:
--
--   stale_after_seconds  how long an agent can go unseen before it is stale:
--                        its ticket shows as stale, and another agent's
--                        claim takes it over
--   lease_seconds        how long an agent can go unseen before the ticket
--                        it holds is given back to todo
--
-- The table holds exactly one row, created here with the defaults (30
-- minutes and 2 hours); `taskboard settings set` changes it.
CREATE TABLE IF NOT EXISTS agent_settings (
    id                  INTEGER PRIMARY KEY CHECK (id = 1),
    stale_after_seconds INTEGER NOT NULL CHECK (stale_after_seconds > 0),
    lease_seconds       INTEGER NOT NULL CHECK (lease_seconds > 0)
);

INSERT OR IGNORE INTO agent_settings (id, stale_after_seconds, lease_seconds) VALUES (1, 1800, 7200);
