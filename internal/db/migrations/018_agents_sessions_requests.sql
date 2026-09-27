-- Sessions, agents, the agent holding a ticket, and requests for user input.
--
-- A session is one conversation in a vendor's tool (Claude Code, Codex, ...):
-- the vendor, the vendor's own session ID, the machine it ran on, the
-- command that resumes it, and a web link when the vendor has one ('' when
-- not). A resumed chat keeps its session, so a session is found again by its
-- vendor and the vendor's session ID, which together are unique.
CREATE TABLE IF NOT EXISTS sessions (
    id                TEXT PRIMARY KEY,
    vendor            TEXT NOT NULL CHECK (vendor <> ''),
    vendor_session_id TEXT NOT NULL CHECK (vendor_session_id <> ''),
    machine           TEXT NOT NULL,
    resume_command    TEXT NOT NULL,
    web_url           TEXT NOT NULL DEFAULT '',
    created_at        DATETIME NOT NULL,
    UNIQUE (vendor, vendor_session_id)
);

-- An agent is one worker in a session: the main agent, or a subagent it
-- launched, with its role and model and the provider of that model. It lives
-- as long as its process, so it is not a lasting identity: several agents
-- share one session, and a fresh chat is a new agent in a new session.
-- Deleting a session that still has agents fails; nothing deletes either yet.
-- provider is free text checked against models.Providers, the way a ticket's
-- status is, so a new provider is a change to that list and not to this
-- table.
CREATE TABLE IF NOT EXISTS agents (
    id           TEXT PRIMARY KEY,
    session_id   TEXT NOT NULL REFERENCES sessions(id),
    role         TEXT NOT NULL,
    model        TEXT NOT NULL,
    provider     TEXT NOT NULL,
    created_at   DATETIME NOT NULL,
    last_seen_at DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_agents_session_id ON agents(session_id);

-- The agent holding a ticket, if any. Every existing ticket has none.
-- Deleting the agent frees the ticket.
ALTER TABLE tickets ADD COLUMN agent_id TEXT REFERENCES agents(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_tickets_agent_id ON tickets(agent_id);

-- A request for user input on a ticket: the agent that asked, what it asks
-- (prompt), the choices it offers as a JSON array ([] when the answer is
-- free), and, once answered, the answer, who gave it and when. The agent that
-- asked is kept for good: after a takeover the ticket's holder is another
-- agent, and this is the only record of who asked and who collects the
-- answer, so deleting an agent that has requests fails. type is the type of
-- user input (approval, question, ...). It is free text checked against
-- models.UserInputTypes, the way a ticket's status is, so a new type is a
-- change to that list and not to this table. who answered is the local
-- person today and the account once there are teams, so it is text rather
-- than a reference to anything.
--
-- The answer, who gave it and when are set together or not at all. A ticket
-- has at most one unanswered request. Requests go with their ticket.
CREATE TABLE IF NOT EXISTS ticket_requests (
    id          TEXT PRIMARY KEY,
    ticket_id   TEXT NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    agent_id    TEXT NOT NULL REFERENCES agents(id),
    type        TEXT NOT NULL,
    prompt      TEXT NOT NULL,
    choices     TEXT NOT NULL DEFAULT '[]'
        CHECK (json_valid(choices) AND json_type(choices) = 'array'),
    answer      TEXT,
    answered_by TEXT,
    created_at  DATETIME NOT NULL,
    answered_at DATETIME,
    CHECK ((answer IS NULL) = (answered_at IS NULL)
       AND (answered_by IS NULL) = (answered_at IS NULL))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_ticket_requests_one_open
    ON ticket_requests(ticket_id) WHERE answered_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_ticket_requests_ticket
    ON ticket_requests(ticket_id, created_at);

CREATE INDEX IF NOT EXISTS idx_ticket_requests_agent_id ON ticket_requests(agent_id);
