package db

import "database/sql"

// TicketExists reports whether a ticket with the given id exists, without
// loading the rest of the row. Callers that only need to choose between a
// 200 and a 404 (ticketHistory in internal/server) can use this instead of
// paying for a full GetTicket.
func (s *Store) TicketExists(id string) (bool, error) {
	var one int
	err := s.db.QueryRow(`SELECT 1 FROM tickets WHERE id = ?`, id).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
