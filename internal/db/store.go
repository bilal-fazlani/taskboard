package db

import (
	"crypto/rand"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/tcarac/taskboard/internal/models"
)

type Store struct {
	db *sql.DB
}

func NewStore(database *sql.DB) *Store {
	return &Store{db: database}
}

func (s *Store) ClearData() error {
	tables := []string{
		"ticket_dependencies",
		"ticket_labels",
		"subtasks",
		"tickets",
		"labels",
		"projects",
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}

	for _, table := range tables {
		if _, err := tx.Exec("DELETE FROM " + table); err != nil {
			tx.Rollback()
			return fmt.Errorf("clearing %s: %w", table, err)
		}
	}

	return tx.Commit()
}

func newID() string {
	return ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader).String()
}

func (s *Store) ListProjects(status string) ([]models.Project, error) {
	query := "SELECT id, name, prefix, description, icon, color, status, created_at, updated_at FROM projects"
	args := []any{}
	if status != "" {
		query += " WHERE status = ?"
		args = append(args, status)
	}
	query += " ORDER BY created_at DESC"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var projects []models.Project
	for rows.Next() {
		var p models.Project
		if err := rows.Scan(&p.ID, &p.Name, &p.Prefix, &p.Description, &p.Icon, &p.Color, &p.Status, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

func (s *Store) GetProject(id string) (*models.Project, error) {
	var p models.Project
	err := s.db.QueryRow(
		"SELECT id, name, prefix, description, icon, color, status, created_at, updated_at FROM projects WHERE id = ?", id,
	).Scan(&p.ID, &p.Name, &p.Prefix, &p.Description, &p.Icon, &p.Color, &p.Status, &p.CreatedAt, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &p, err
}

func (s *Store) CreateProject(req models.CreateProjectRequest) (*models.Project, error) {
	p := models.Project{
		ID:          newID(),
		Name:        req.Name,
		Prefix:      req.Prefix,
		Description: req.Description,
		Icon:        req.Icon,
		Color:       req.Color,
		Status:      "active",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if p.Color == "" {
		p.Color = "#3B82F6"
	}

	_, err := s.db.Exec(
		"INSERT INTO projects (id, name, prefix, description, icon, color, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		p.ID, p.Name, p.Prefix, p.Description, p.Icon, p.Color, p.Status, p.CreatedAt, p.UpdatedAt,
	)
	return &p, err
}

func (s *Store) UpdateProject(id string, req models.UpdateProjectRequest) (*models.Project, error) {
	p, err := s.GetProject(id)
	if err != nil || p == nil {
		return nil, err
	}

	if req.Name != nil {
		p.Name = *req.Name
	}
	if req.Prefix != nil {
		p.Prefix = *req.Prefix
	}
	if req.Description != nil {
		p.Description = *req.Description
	}
	if req.Icon != nil {
		p.Icon = *req.Icon
	}
	if req.Color != nil {
		p.Color = *req.Color
	}
	if req.Status != nil {
		p.Status = *req.Status
	}
	p.UpdatedAt = time.Now()

	_, err = s.db.Exec(
		"UPDATE projects SET name=?, prefix=?, description=?, icon=?, color=?, status=?, updated_at=? WHERE id=?",
		p.Name, p.Prefix, p.Description, p.Icon, p.Color, p.Status, p.UpdatedAt, p.ID,
	)
	return p, err
}

func (s *Store) DeleteProject(id string) error {
	_, err := s.db.Exec("DELETE FROM projects WHERE id = ?", id)
	return err
}

func (s *Store) nextTicketNumber(projectID string) (int, error) {
	var num int
	err := s.db.QueryRow("SELECT COALESCE(MAX(number), 0) + 1 FROM tickets WHERE project_id = ?", projectID).Scan(&num)
	return num, err
}

func (s *Store) ListTickets(filter models.TicketFilter) ([]models.Ticket, error) {
	query := `SELECT t.id, t.project_id, t.number, t.title, t.description,
		t.status, t.priority, t.repo, t.due_date, t.position, t.created_at, t.updated_at,
		COALESCE(p.prefix, '') as project_prefix
		FROM tickets t LEFT JOIN projects p ON t.project_id = p.id WHERE 1=1`
	args := []any{}

	if filter.ProjectID != "" {
		query += " AND t.project_id = ?"
		args = append(args, filter.ProjectID)
	}
	if filter.Status != "" {
		query += " AND t.status = ?"
		args = append(args, filter.Status)
	}
	if filter.Priority != "" {
		query += " AND t.priority = ?"
		args = append(args, filter.Priority)
	}
	if filter.Repo != "" {
		query += " AND t.repo = ?"
		args = append(args, filter.Repo)
	}
	if filter.Label != "" {
		query += ` AND EXISTS (
			SELECT 1 FROM ticket_labels tl JOIN labels l ON l.id = tl.label_id
			WHERE tl.ticket_id = t.id AND LOWER(l.name) = LOWER(?))`
		args = append(args, filter.Label)
	}
	query += " ORDER BY t.position ASC, t.created_at DESC"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tickets []models.Ticket
	for rows.Next() {
		var t models.Ticket
		if err := rows.Scan(&t.ID, &t.ProjectID, &t.Number, &t.Title, &t.Description,
			&t.Status, &t.Priority, &t.Repo, &t.DueDate, &t.Position, &t.CreatedAt, &t.UpdatedAt,
			&t.ProjectPrefix); err != nil {
			return nil, err
		}
		tickets = append(tickets, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if err := s.attachListDetails(tickets); err != nil {
		return nil, err
	}

	return tickets, nil
}

// attachListDetails fills Labels, Subtasks, and DependsOn for a page of tickets
// using one query per relation rather than one per ticket. Blocks is not filled;
// no list view renders it.
func (s *Store) attachListDetails(tickets []models.Ticket) error {
	if len(tickets) == 0 {
		return nil
	}

	ids := make([]any, len(tickets))
	index := make(map[string]int, len(tickets))
	for i, t := range tickets {
		ids[i] = t.ID
		index[t.ID] = i
		tickets[i].Labels = nil
		tickets[i].Subtasks = nil
		tickets[i].DependsOn = nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")

	labelRows, err := s.db.Query(
		`SELECT tl.ticket_id, l.id, l.name, l.color
		FROM ticket_labels tl JOIN labels l ON l.id = tl.label_id
		WHERE tl.ticket_id IN (`+placeholders+`) ORDER BY l.name`, ids...)
	if err != nil {
		return fmt.Errorf("loading labels: %w", err)
	}
	for labelRows.Next() {
		var ticketID string
		var l models.Label
		if err := labelRows.Scan(&ticketID, &l.ID, &l.Name, &l.Color); err != nil {
			labelRows.Close()
			return err
		}
		if i, ok := index[ticketID]; ok {
			tickets[i].Labels = append(tickets[i].Labels, l)
		}
	}
	labelRows.Close()
	if err := labelRows.Err(); err != nil {
		return err
	}

	subtaskRows, err := s.db.Query(
		`SELECT id, ticket_id, title, completed, position FROM subtasks
		WHERE ticket_id IN (`+placeholders+`) ORDER BY position`, ids...)
	if err != nil {
		return fmt.Errorf("loading subtasks: %w", err)
	}
	for subtaskRows.Next() {
		var st models.Subtask
		if err := subtaskRows.Scan(&st.ID, &st.TicketID, &st.Title, &st.Completed, &st.Position); err != nil {
			subtaskRows.Close()
			return err
		}
		if i, ok := index[st.TicketID]; ok {
			tickets[i].Subtasks = append(tickets[i].Subtasks, st)
		}
	}
	subtaskRows.Close()
	if err := subtaskRows.Err(); err != nil {
		return err
	}

	// The key expression MUST stay character-identical to the one in
	// ticketRefSelect (Task 3). Both have a known cosmetic flaw for an empty
	// project prefix; keeping them identical means that is one fix, not two.
	depRows, err := s.db.Query(
		`SELECT d.ticket_id, t.id,
		COALESCE(p.prefix, '') || '-' || t.number, t.title, t.status
		FROM ticket_dependencies d
		JOIN tickets t ON t.id = d.blocked_by_id
		LEFT JOIN projects p ON p.id = t.project_id
		WHERE d.ticket_id IN (`+placeholders+`) ORDER BY t.number`, ids...)
	if err != nil {
		return fmt.Errorf("loading dependencies: %w", err)
	}
	defer depRows.Close()
	for depRows.Next() {
		var ticketID string
		var r models.TicketRef
		if err := depRows.Scan(&ticketID, &r.ID, &r.Key, &r.Title, &r.Status); err != nil {
			return err
		}
		if i, ok := index[ticketID]; ok {
			tickets[i].DependsOn = append(tickets[i].DependsOn, r)
		}
	}
	return depRows.Err()
}

func (s *Store) GetTicket(id string) (*models.Ticket, error) {
	var t models.Ticket
	err := s.db.QueryRow(
		`SELECT t.id, t.project_id, t.number, t.title, t.description,
		t.status, t.priority, t.repo, t.due_date, t.position, t.created_at, t.updated_at,
		COALESCE(p.prefix, '') as project_prefix
		FROM tickets t LEFT JOIN projects p ON t.project_id = p.id WHERE t.id = ?`, id,
	).Scan(&t.ID, &t.ProjectID, &t.Number, &t.Title, &t.Description,
		&t.Status, &t.Priority, &t.Repo, &t.DueDate, &t.Position, &t.CreatedAt, &t.UpdatedAt,
		&t.ProjectPrefix)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	t.Labels, _ = s.getTicketLabels(t.ID)
	t.Subtasks, _ = s.getTicketSubtasks(t.ID)
	if t.DependsOn, err = s.getTicketDependsOn(t.ID); err != nil {
		return nil, err
	}
	if t.Blocks, err = s.getTicketBlocks(t.ID); err != nil {
		return nil, err
	}

	return &t, nil
}

func (s *Store) CreateTicket(req models.CreateTicketRequest) (*models.Ticket, error) {
	num, err := s.nextTicketNumber(req.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("getting next ticket number: %w", err)
	}

	status := req.Status
	if status == "" {
		status = "todo"
	}
	priority := req.Priority
	if priority == "" {
		priority = "medium"
	}

	t := models.Ticket{
		ID:          newID(),
		ProjectID:   req.ProjectID,
		Number:      num,
		Title:       req.Title,
		Description: req.Description,
		Status:      status,
		Priority:    priority,
		Repo:        req.Repo,
		Position:    float64(num) * 1000,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if req.DueDate != nil {
		parsed, err := time.Parse("2006-01-02", *req.DueDate)
		if err == nil {
			t.DueDate = &parsed
		}
	}

	_, err = s.db.Exec(
		`INSERT INTO tickets (id, project_id, number, title, description, status, priority, repo, due_date, position, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.ProjectID, t.Number, t.Title, t.Description, t.Status, t.Priority, t.Repo, t.DueDate, t.Position, t.CreatedAt, t.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	if len(req.Labels) > 0 {
		labelIDs, err := s.resolveLabelNames(req.Labels)
		if err != nil {
			return nil, err
		}
		for _, labelID := range labelIDs {
			if _, err := s.db.Exec(
				"INSERT OR IGNORE INTO ticket_labels (ticket_id, label_id) VALUES (?, ?)",
				t.ID, labelID,
			); err != nil {
				return nil, fmt.Errorf("attaching label: %w", err)
			}
		}
	}

	if len(req.DependsOn) > 0 {
		depIDs, err := s.resolveTicketRefs(t.ID, req.DependsOn)
		if err != nil {
			return nil, err
		}
		for _, depID := range depIDs {
			if _, err := s.db.Exec(
				"INSERT OR IGNORE INTO ticket_dependencies (ticket_id, blocked_by_id) VALUES (?, ?)",
				t.ID, depID,
			); err != nil {
				return nil, fmt.Errorf("attaching dependency: %w", err)
			}
		}
	}

	return s.GetTicket(t.ID)
}

func (s *Store) UpdateTicket(id string, req models.UpdateTicketRequest) (*models.Ticket, error) {
	t, err := s.GetTicket(id)
	if err != nil || t == nil {
		return nil, err
	}

	if req.Title != nil {
		t.Title = *req.Title
	}
	if req.Description != nil {
		t.Description = *req.Description
	}
	if req.Status != nil {
		t.Status = *req.Status
	}
	if req.Priority != nil {
		t.Priority = *req.Priority
	}
	if req.Repo != nil {
		t.Repo = *req.Repo
	}
	if req.Position != nil {
		t.Position = *req.Position
	}
	if req.DueDate != nil {
		parsed, err := time.Parse("2006-01-02", *req.DueDate)
		if err == nil {
			t.DueDate = &parsed
		}
	}
	t.UpdatedAt = time.Now()

	_, err = s.db.Exec(
		`UPDATE tickets SET title=?, description=?, status=?, priority=?, repo=?, due_date=?, position=?, updated_at=? WHERE id=?`,
		t.Title, t.Description, t.Status, t.Priority, t.Repo, t.DueDate, t.Position, t.UpdatedAt, t.ID,
	)
	if err != nil {
		return nil, err
	}

	if req.Labels != nil {
		labelIDs, err := s.resolveLabelNames(req.Labels)
		if err != nil {
			return nil, err
		}
		if _, err := s.db.Exec("DELETE FROM ticket_labels WHERE ticket_id = ?", id); err != nil {
			return nil, fmt.Errorf("clearing labels: %w", err)
		}
		for _, labelID := range labelIDs {
			if _, err := s.db.Exec(
				"INSERT OR IGNORE INTO ticket_labels (ticket_id, label_id) VALUES (?, ?)",
				id, labelID,
			); err != nil {
				return nil, fmt.Errorf("attaching label: %w", err)
			}
		}
	}

	if req.DependsOn != nil {
		depIDs, err := s.resolveTicketRefs(id, req.DependsOn)
		if err != nil {
			return nil, err
		}
		if _, err := s.db.Exec("DELETE FROM ticket_dependencies WHERE ticket_id = ?", id); err != nil {
			return nil, fmt.Errorf("clearing dependencies: %w", err)
		}
		for _, depID := range depIDs {
			if _, err := s.db.Exec(
				"INSERT OR IGNORE INTO ticket_dependencies (ticket_id, blocked_by_id) VALUES (?, ?)",
				id, depID,
			); err != nil {
				return nil, fmt.Errorf("attaching dependency: %w", err)
			}
		}
	}

	return s.GetTicket(id)
}

func (s *Store) MoveTicket(id string, req models.MoveTicketRequest) (*models.Ticket, error) {
	now := time.Now()
	position := float64(0)
	if req.Position != nil {
		position = *req.Position
	} else {
		var maxPos float64
		s.db.QueryRow("SELECT COALESCE(MAX(position), 0) + 1000 FROM tickets WHERE status = ?", req.Status).Scan(&maxPos)
		position = maxPos
	}

	_, err := s.db.Exec("UPDATE tickets SET status=?, position=?, updated_at=? WHERE id=?",
		req.Status, position, now, id)
	if err != nil {
		return nil, err
	}
	return s.GetTicket(id)
}

func (s *Store) DeleteTicket(id string) error {
	_, err := s.db.Exec("DELETE FROM tickets WHERE id = ?", id)
	return err
}

func (s *Store) GetBoard(projectID string) (*models.Board, error) {
	statuses := []string{"todo", "in_progress", "done"}
	board := &models.Board{
		ProjectID: projectID,
		Columns:   make([]models.Column, len(statuses)),
	}

	for i, status := range statuses {
		filter := models.TicketFilter{Status: status}
		if projectID != "" {
			filter.ProjectID = projectID
		}
		tickets, err := s.ListTickets(filter)
		if err != nil {
			return nil, err
		}
		if tickets == nil {
			tickets = []models.Ticket{}
		}
		board.Columns[i] = models.Column{
			Status:  status,
			Tickets: tickets,
		}
	}

	return board, nil
}

const defaultLabelColor = "#6B7280"

// resolveLabelNames maps label names to label IDs, matching case-insensitively
// (Unicode-aware, via strings.EqualFold — SQLite's LOWER() is ASCII-only, so the
// fold happens entirely in Go). Names with no existing label are created with the
// default color, keeping the caller's original casing. This is what lets an agent
// pass ["bug"] without a lookup round trip.
func (s *Store) resolveLabelNames(names []string) ([]string, error) {
	type labelRef struct {
		id   string
		name string
	}

	rows, err := s.db.Query("SELECT id, name FROM labels")
	if err != nil {
		return nil, fmt.Errorf("loading labels: %w", err)
	}
	var candidates []labelRef
	for rows.Next() {
		var lr labelRef
		if scanErr := rows.Scan(&lr.id, &lr.name); scanErr != nil {
			rows.Close()
			return nil, fmt.Errorf("scanning label: %w", scanErr)
		}
		candidates = append(candidates, lr)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("loading labels: %w", err)
	}
	rows.Close()

	ids := make([]string, 0, len(names))
	seenIDs := make(map[string]bool, len(names))

	for _, name := range names {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}

		var id string
		for _, c := range candidates {
			if strings.EqualFold(c.name, trimmed) {
				id = c.id
				break
			}
		}
		if id == "" {
			id = newID()
			if _, err := s.db.Exec(
				"INSERT INTO labels (id, name, color) VALUES (?, ?, ?)",
				id, trimmed, defaultLabelColor,
			); err != nil {
				return nil, fmt.Errorf("creating label %q: %w", trimmed, err)
			}
			// Make the new label visible to later names in this same call, so a
			// name repeated with different casing still resolves to one label.
			candidates = append(candidates, labelRef{id: id, name: trimmed})
		}

		if seenIDs[id] {
			continue
		}
		seenIDs[id] = true
		ids = append(ids, id)
	}
	return ids, nil
}

// ErrInvalidInput marks a caller-supplied value that could not be resolved, as
// opposed to an internal failure. The HTTP layer maps it to 400.
type ErrInvalidInput struct{ Msg string }

func (e *ErrInvalidInput) Error() string { return e.Msg }

func invalidInput(format string, a ...any) error {
	return &ErrInvalidInput{Msg: fmt.Sprintf(format, a...)}
}

// resolveTicketRefs maps ticket IDs or display keys like "BILL-2" to ticket IDs.
// Keys are unique per project, so a key resolves on prefix and number together,
// which is what allows dependencies to cross projects. selfID is the ticket
// declaring the dependency and may be empty when it has no ID yet.
func (s *Store) resolveTicketRefs(selfID string, refs []string) ([]string, error) {
	ids := make([]string, 0, len(refs))
	seen := make(map[string]bool, len(refs))

	for _, ref := range refs {
		trimmed := strings.TrimSpace(ref)
		if trimmed == "" {
			continue
		}

		id, err := s.lookupTicketRef(trimmed)
		if err != nil {
			return nil, err
		}
		if selfID != "" && id == selfID {
			return nil, invalidInput("ticket %q cannot depend on itself", trimmed)
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids, nil
}

// lookupTicketRef resolves a single reference, trying a raw ID first and then a
// PREFIX-NUMBER display key.
func (s *Store) lookupTicketRef(ref string) (string, error) {
	var id string
	err := s.db.QueryRow("SELECT id FROM tickets WHERE id = ?", ref).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return "", fmt.Errorf("looking up ticket %q: %w", ref, err)
	}

	prefix, numStr, ok := strings.Cut(ref, "-")
	if !ok {
		return "", invalidInput("no ticket matches %q", ref)
	}
	number, convErr := strconv.Atoi(numStr)
	if convErr != nil {
		return "", invalidInput("no ticket matches %q", ref)
	}

	err = s.db.QueryRow(
		`SELECT t.id FROM tickets t
		JOIN projects p ON t.project_id = p.id
		WHERE LOWER(p.prefix) = LOWER(?) AND t.number = ?`,
		prefix, number,
	).Scan(&id)
	if err == sql.ErrNoRows {
		return "", invalidInput("no ticket matches %q", ref)
	}
	if err != nil {
		return "", fmt.Errorf("looking up ticket %q: %w", ref, err)
	}
	return id, nil
}

func (s *Store) ListLabels() ([]models.Label, error) {
	rows, err := s.db.Query("SELECT id, name, color FROM labels ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var labels []models.Label
	for rows.Next() {
		var l models.Label
		if err := rows.Scan(&l.ID, &l.Name, &l.Color); err != nil {
			return nil, err
		}
		labels = append(labels, l)
	}
	return labels, rows.Err()
}

func (s *Store) CreateLabel(req models.CreateLabelRequest) (*models.Label, error) {
	l := models.Label{ID: newID(), Name: req.Name, Color: req.Color}
	_, err := s.db.Exec("INSERT INTO labels (id, name, color) VALUES (?, ?, ?)", l.ID, l.Name, l.Color)
	return &l, err
}

func (s *Store) UpdateLabel(id string, req models.UpdateLabelRequest) (*models.Label, error) {
	var l models.Label
	err := s.db.QueryRow("SELECT id, name, color FROM labels WHERE id = ?", id).Scan(&l.ID, &l.Name, &l.Color)
	if err != nil {
		return nil, err
	}
	if req.Name != nil {
		l.Name = *req.Name
	}
	if req.Color != nil {
		l.Color = *req.Color
	}
	_, err = s.db.Exec("UPDATE labels SET name=?, color=? WHERE id=?", l.Name, l.Color, l.ID)
	return &l, err
}

func (s *Store) DeleteLabel(id string) error {
	_, err := s.db.Exec("DELETE FROM labels WHERE id = ?", id)
	return err
}

func (s *Store) AddSubtask(ticketID string, req models.CreateSubtaskRequest) (*models.Subtask, error) {
	var maxPos int
	s.db.QueryRow("SELECT COALESCE(MAX(position), -1) + 1 FROM subtasks WHERE ticket_id = ?", ticketID).Scan(&maxPos)

	st := models.Subtask{
		ID:       newID(),
		TicketID: ticketID,
		Title:    req.Title,
		Position: maxPos,
	}
	_, err := s.db.Exec("INSERT INTO subtasks (id, ticket_id, title, completed, position) VALUES (?, ?, ?, ?, ?)",
		st.ID, st.TicketID, st.Title, st.Completed, st.Position)
	return &st, err
}

func (s *Store) ToggleSubtask(id string) (*models.Subtask, error) {
	_, err := s.db.Exec("UPDATE subtasks SET completed = NOT completed WHERE id = ?", id)
	if err != nil {
		return nil, err
	}
	var st models.Subtask
	err = s.db.QueryRow("SELECT id, ticket_id, title, completed, position FROM subtasks WHERE id = ?", id).
		Scan(&st.ID, &st.TicketID, &st.Title, &st.Completed, &st.Position)
	return &st, err
}

func (s *Store) DeleteSubtask(id string) error {
	_, err := s.db.Exec("DELETE FROM subtasks WHERE id = ?", id)
	return err
}

func (s *Store) getTicketLabels(ticketID string) ([]models.Label, error) {
	rows, err := s.db.Query(
		"SELECT l.id, l.name, l.color FROM labels l JOIN ticket_labels tl ON l.id = tl.label_id WHERE tl.ticket_id = ?",
		ticketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var labels []models.Label
	for rows.Next() {
		var l models.Label
		if err := rows.Scan(&l.ID, &l.Name, &l.Color); err != nil {
			return nil, err
		}
		labels = append(labels, l)
	}
	return labels, rows.Err()
}

func (s *Store) getTicketSubtasks(ticketID string) ([]models.Subtask, error) {
	rows, err := s.db.Query(
		"SELECT id, ticket_id, title, completed, position FROM subtasks WHERE ticket_id = ? ORDER BY position",
		ticketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var subtasks []models.Subtask
	for rows.Next() {
		var st models.Subtask
		if err := rows.Scan(&st.ID, &st.TicketID, &st.Title, &st.Completed, &st.Position); err != nil {
			return nil, err
		}
		subtasks = append(subtasks, st)
	}
	return subtasks, rows.Err()
}

const ticketRefSelect = `SELECT t.id,
	COALESCE(p.prefix, '') || '-' || t.number AS key,
	t.title, t.status
	FROM tickets t LEFT JOIN projects p ON t.project_id = p.id`

func scanTicketRefs(rows *sql.Rows) ([]models.TicketRef, error) {
	defer rows.Close()
	var refs []models.TicketRef
	for rows.Next() {
		var r models.TicketRef
		if err := rows.Scan(&r.ID, &r.Key, &r.Title, &r.Status); err != nil {
			return nil, err
		}
		refs = append(refs, r)
	}
	return refs, rows.Err()
}

// getTicketDependsOn returns the tickets this ticket declares a dependency on.
func (s *Store) getTicketDependsOn(ticketID string) ([]models.TicketRef, error) {
	rows, err := s.db.Query(ticketRefSelect+
		` JOIN ticket_dependencies d ON d.blocked_by_id = t.id
		WHERE d.ticket_id = ? ORDER BY t.number`, ticketID)
	if err != nil {
		return nil, err
	}
	return scanTicketRefs(rows)
}

// getTicketBlocks returns the tickets that declare a dependency on this one.
// Nothing writes this direction; it is the same table read backwards.
func (s *Store) getTicketBlocks(ticketID string) ([]models.TicketRef, error) {
	rows, err := s.db.Query(ticketRefSelect+
		` JOIN ticket_dependencies d ON d.ticket_id = t.id
		WHERE d.blocked_by_id = ? ORDER BY t.number`, ticketID)
	if err != nil {
		return nil, err
	}
	return scanTicketRefs(rows)
}
