-- Gives each project a counter of the last ticket number it handed out, so
-- a number is never given out twice. Before this, CreateTicket numbered a
-- ticket MAX(number) + 1, so deleting a project's latest ticket freed its
-- number and the next ticket took it: a display key (BILL-7) named in
-- commits, notes, other tickets and old links then pointed at a different
-- ticket. CreateTicket now takes the next number from this counter, which
-- only ever goes up.
--
-- Existing projects start from their current MAX(number), and projects with
-- no tickets from 0. Numbers already freed by a deletion before this
-- migration cannot be recovered, so a project whose latest ticket was
-- deleted continues from the highest number still present. No ticket's
-- number changes.

ALTER TABLE projects ADD COLUMN last_ticket_number INTEGER NOT NULL DEFAULT 0;

UPDATE projects SET last_ticket_number = COALESCE(
    (SELECT MAX(number) FROM tickets WHERE tickets.project_id = projects.id),
    0
);
