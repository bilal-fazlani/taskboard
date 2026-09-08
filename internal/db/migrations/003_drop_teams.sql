DROP INDEX IF EXISTS idx_tickets_team_id;
ALTER TABLE tickets DROP COLUMN team_id;
DROP TABLE IF EXISTS teams;
