ALTER TABLE tickets ADD COLUMN repo TEXT DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_tickets_repo ON tickets(repo);
CREATE INDEX IF NOT EXISTS idx_ticket_deps_blocked_by ON ticket_dependencies(blocked_by_id);
