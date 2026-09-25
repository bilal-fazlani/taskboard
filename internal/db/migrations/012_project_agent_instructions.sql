-- Agent instructions: how agents should work on a project's tickets, kept
-- apart from the description, which says what the project is. Existing
-- projects start with none.
ALTER TABLE projects ADD COLUMN agent_instructions TEXT NOT NULL DEFAULT '';
