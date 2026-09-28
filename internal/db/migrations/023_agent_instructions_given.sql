-- The project agent instructions each agent has been given by a start: one
-- row per agent and project, with a SHA-256 of the text it was given (hex)
-- and when. A start carries the project's agent instructions only when the
-- starting agent has no row for the project, or its row's hash is not the
-- instructions' hash now (they changed since), and then records them here;
-- otherwise it says where to read them again. Agents, not sessions: the
-- agents of one session (an orchestrator and the implementers and reviewers
-- it launches) each need the instructions once. The row goes with its agent
-- or its project. Existing agents have no rows, so each is given the
-- instructions again on its next start.
CREATE TABLE IF NOT EXISTS agent_instructions_given (
    agent_id          TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    project_id        TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    instructions_hash TEXT NOT NULL,
    given_at          DATETIME NOT NULL,
    PRIMARY KEY (agent_id, project_id)
);
