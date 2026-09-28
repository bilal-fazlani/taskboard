-- Work the person stopped: the agent that held the ticket when the person
-- stopped it, who stopped it and when. Stopping clears the ticket's agent and
-- returns it to todo; this row is how the stopped agent's session learns of
-- it, since agents pull: its next write on the ticket is refused, except its
-- hand-off. A later claim by an agent of that session removes the row, and
-- the row goes with its ticket or its agent. The status history keeps the
-- lasting record.
CREATE TABLE IF NOT EXISTS ticket_stops (
    ticket_id  TEXT NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    agent_id   TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    stopped_by TEXT NOT NULL CHECK (stopped_by <> ''),
    stopped_at DATETIME NOT NULL,
    PRIMARY KEY (ticket_id, agent_id)
);

CREATE INDEX IF NOT EXISTS idx_ticket_stops_agent_id ON ticket_stops(agent_id);
