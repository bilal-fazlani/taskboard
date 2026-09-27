package db

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tcarac/taskboard/internal/models"
)

// NowLandedWindow and NowLandedLimit bound the Now page's Landed list: the
// tickets that moved to done in the last day, newest first, at most ten.
// Older history belongs to the project's activity feed.
const (
	NowLandedWindow = 24 * time.Hour
	NowLandedLimit  = 10
)

// Now reads what is moving at the instant now: every ticket in progress,
// waiting on the person (needs_user_input) or in review, longest in its
// status first, and the tickets that landed within NowLandedWindow of now.
// projectRef, an id or prefix, narrows every group to one project; ""
// means every project, and a project that does not resolve matches
// nothing, as a list filter does. It costs a fixed handful of queries,
// whatever the number of tickets.
func (s *Store) Now(projectRef string, now time.Time) (*models.Now, error) {
	out := &models.Now{
		InProgress: []models.NowTicket{},
		Waiting:    []models.NowTicket{},
		InReview:   []models.NowTicket{},
		Landed:     []models.LandedTicket{},
	}
	projectID := ""
	if strings.TrimSpace(projectRef) != "" {
		id, ok, err := resolveProjectFilterID(s.db, projectRef)
		if err != nil {
			return nil, err
		}
		if !ok {
			return out, nil
		}
		projectID = id
	}

	active, err := s.nowActive(projectID)
	if err != nil {
		return nil, err
	}
	for _, t := range active {
		switch t.Status {
		case models.StatusInProgress:
			out.InProgress = append(out.InProgress, t)
		case models.StatusNeedsUserInput:
			out.Waiting = append(out.Waiting, t)
		default:
			out.InReview = append(out.InReview, t)
		}
	}

	landed, err := s.nowLanded(projectID, now)
	if err != nil {
		return nil, err
	}
	out.Landed = landed
	return out, nil
}

// inList is the placeholders and arguments of an IN list of ids.
func inList(ids []string) (string, []any) {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return strings.TrimSuffix(strings.Repeat("?,", len(ids)), ","), args
}

// nowActive reads the tickets in progress, waiting on the person or in
// review, with their subtask progress, review rounds, time in their status
// and, in review, the state of their review.
func (s *Store) nowActive(projectID string) ([]models.NowTicket, error) {
	rows, err := s.db.Query(`SELECT t.id, COALESCE(p.prefix, ''), t.number, t.title, t.status, t.created_at
		FROM `+liveTickets+` t LEFT JOIN `+liveProjects+` p ON p.id = t.project_id
		WHERE t.status IN (?, ?, ?) AND (? = '' OR t.project_id = ?)`,
		models.StatusInProgress, models.StatusNeedsUserInput, models.StatusAgentReview, projectID, projectID)
	if err != nil {
		return nil, fmt.Errorf("loading active tickets: %w", err)
	}
	var tickets []models.NowTicket
	index := map[string]int{}
	for rows.Next() {
		var t models.NowTicket
		var number int
		if err := rows.Scan(&t.ID, &t.ProjectPrefix, &number, &t.Title, &t.Status, &t.Since); err != nil {
			rows.Close()
			return nil, err
		}
		t.Key = models.Ticket{ProjectPrefix: t.ProjectPrefix, Number: number}.DisplayKey()
		index[t.ID] = len(tickets)
		tickets = append(tickets, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(tickets) == 0 {
		return nil, nil
	}
	ids := make([]string, len(tickets))
	for i, t := range tickets {
		ids[i] = t.ID
	}
	placeholders, args := inList(ids)

	if err := s.nowSubtasks(tickets, index, placeholders, args); err != nil {
		return nil, err
	}
	if err := s.nowSince(tickets, index, placeholders, args); err != nil {
		return nil, err
	}
	if err := s.nowReviews(tickets, index); err != nil {
		return nil, err
	}

	// Review rounds, one grouped query, as for a list.
	roundRows, err := s.db.Query(reviewRoundsQuery+` AND ticket_id IN (`+placeholders+`) GROUP BY ticket_id`, args...)
	if err != nil {
		return nil, fmt.Errorf("loading review rounds: %w", err)
	}
	defer roundRows.Close()
	for roundRows.Next() {
		var id string
		var n int
		if err := roundRows.Scan(&id, &n); err != nil {
			return nil, err
		}
		if i, ok := index[id]; ok {
			tickets[i].ReviewRounds = n
		}
	}
	if err := roundRows.Err(); err != nil {
		return nil, err
	}

	// Longest running first; the key breaks a tie.
	sort.SliceStable(tickets, func(a, b int) bool {
		if !tickets[a].Since.Equal(tickets[b].Since) {
			return tickets[a].Since.Before(tickets[b].Since)
		}
		return tickets[a].Key < tickets[b].Key
	})
	return tickets, nil
}

// nowSubtasks fills each ticket's subtask progress.
func (s *Store) nowSubtasks(tickets []models.NowTicket, index map[string]int, placeholders string, args []any) error {
	rows, err := s.db.Query(`SELECT ticket_id, COUNT(*), COALESCE(SUM(CASE WHEN completed THEN 1 ELSE 0 END), 0)
		FROM `+liveSubtasks+` WHERE ticket_id IN (`+placeholders+`) GROUP BY ticket_id`, args...)
	if err != nil {
		return fmt.Errorf("loading subtask progress: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var total, done int
		if err := rows.Scan(&id, &total, &done); err != nil {
			return err
		}
		if i, ok := index[id]; ok {
			tickets[i].SubtasksTotal = total
			tickets[i].SubtasksDone = done
		}
	}
	return rows.Err()
}

// nowSince sets each ticket's Since to its latest entry into the status it
// has now. A ticket with no such entry keeps its created_at. A row that keeps
// the status (a takeover) is not an entry.
func (s *Store) nowSince(tickets []models.NowTicket, index map[string]int, placeholders string, args []any) error {
	// Oldest first, rowid for changes within the same instant, so the last
	// matching row read for a ticket is its latest entry.
	rows, err := s.db.Query(`SELECT ticket_id, to_status, created_at FROM ticket_status_changes
		WHERE ticket_id IN (`+placeholders+`) AND from_status <> to_status ORDER BY created_at ASC, rowid ASC`, args...)
	if err != nil {
		return fmt.Errorf("loading status history: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, to string
		var at time.Time
		if err := rows.Scan(&id, &to, &at); err != nil {
			return err
		}
		if i, ok := index[id]; ok && tickets[i].Status == to {
			tickets[i].Since = at
		}
	}
	return rows.Err()
}

// reviewDocument is a ticket's `Review <round>` document, as far as its
// verdict goes.
type reviewDocument struct {
	round   int
	updated time.Time
	head    string
}

// reviewRound reads the round out of a document name `Review <round>`
// (any case), or reports false for any other name.
func reviewRound(name string) (int, bool) {
	const prefix = "review "
	if len(name) <= len(prefix) || !strings.EqualFold(name[:len(prefix)], prefix) {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(name[len(prefix):]))
	if err != nil || n < 1 {
		return 0, false
	}
	return n, true
}

// reviewVerdict reads the verdict a review report starts with: `VERDICT:
// APPROVE` or `VERDICT: CHANGES`, ignoring case and leading space.
func reviewVerdict(head string) string {
	h := strings.ToUpper(strings.TrimSpace(head))
	switch {
	case strings.HasPrefix(h, "VERDICT: APPROVE"):
		return models.NowReviewApproved
	case strings.HasPrefix(h, "VERDICT: CHANGES"):
		return models.NowReviewChanges
	}
	return ""
}

// nowReviews sets Review on each ticket in agent_review from its latest
// `Review <round>` document (the highest round; the later save when two
// share one). A review saved before the ticket's latest entry into review
// belongs to an earlier round, so the review of this one is still running;
// so is one whose report starts with no verdict. Its Since must already be
// that entry.
func (s *Store) nowReviews(tickets []models.NowTicket, index map[string]int) error {
	var ids []string
	for _, t := range tickets {
		if t.Status == models.StatusAgentReview {
			ids = append(ids, t.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	placeholders, args := inList(ids)
	// Only the start of each report is read: the verdict is its first line.
	rows, err := s.db.Query(`SELECT ticket_id, name, updated_at, substr(content, 1, 64) FROM `+liveDocuments+`
		WHERE ticket_id IN (`+placeholders+`) AND format IN ('markdown', 'html')
		AND name LIKE 'review %'`, args...)
	if err != nil {
		return fmt.Errorf("loading review documents: %w", err)
	}
	defer rows.Close()
	latest := map[string]reviewDocument{}
	for rows.Next() {
		var id, name string
		var d reviewDocument
		if err := rows.Scan(&id, &name, &d.updated, &d.head); err != nil {
			return err
		}
		round, ok := reviewRound(name)
		if !ok {
			continue
		}
		d.round = round
		if cur, seen := latest[id]; !seen || d.round > cur.round || (d.round == cur.round && d.updated.After(cur.updated)) {
			latest[id] = d
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		t := &tickets[index[id]]
		t.Review = models.NowReviewRunning
		d, ok := latest[id]
		if !ok || d.updated.Before(t.Since) {
			continue
		}
		if v := reviewVerdict(d.head); v != "" {
			t.Review = v
		}
	}
	return nil
}

// nowLanded reads the tickets that are done and last moved to done within
// NowLandedWindow of now, newest first, at most NowLandedLimit, each with
// its landed commits.
func (s *Store) nowLanded(projectID string, now time.Time) ([]models.LandedTicket, error) {
	// A ticket's latest move to done is within the window exactly when any of
	// its moves to done is, so the window filters the rows and the latest
	// row kept per ticket is its DoneAt.
	rows, err := s.db.Query(`SELECT t.id, COALESCE(p.prefix, ''), t.number, t.title, c.created_at
		FROM ticket_status_changes c
		JOIN `+liveTickets+` t ON t.id = c.ticket_id
		LEFT JOIN `+liveProjects+` p ON p.id = t.project_id
		WHERE c.to_status = ? AND t.status = ? AND c.created_at >= ?
		AND (? = '' OR t.project_id = ?)
		ORDER BY c.created_at ASC, c.rowid ASC`,
		models.StatusDone, models.StatusDone, stamp(now.Add(-NowLandedWindow)), projectID, projectID)
	if err != nil {
		return nil, fmt.Errorf("loading landed tickets: %w", err)
	}
	var landed []models.LandedTicket
	index := map[string]int{}
	for rows.Next() {
		var t models.LandedTicket
		var number int
		if err := rows.Scan(&t.ID, &t.ProjectPrefix, &number, &t.Title, &t.DoneAt); err != nil {
			rows.Close()
			return nil, err
		}
		if i, ok := index[t.ID]; ok {
			landed[i].DoneAt = t.DoneAt
			continue
		}
		t.Key = models.Ticket{ProjectPrefix: t.ProjectPrefix, Number: number}.DisplayKey()
		t.Commits = []models.LandedCommit{}
		index[t.ID] = len(landed)
		landed = append(landed, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(landed) == 0 {
		return []models.LandedTicket{}, nil
	}

	sort.SliceStable(landed, func(a, b int) bool { return landed[a].DoneAt.After(landed[b].DoneAt) })
	if len(landed) > NowLandedLimit {
		landed = landed[:NowLandedLimit]
	}
	index = make(map[string]int, len(landed))
	ids := make([]string, len(landed))
	for i, t := range landed {
		index[t.ID] = i
		ids[i] = t.ID
	}
	placeholders, args := inList(ids)
	commitRows, err := s.db.Query(`SELECT ticket_id, sha, repo FROM ticket_landed_commits
		WHERE ticket_id IN (`+placeholders+`) ORDER BY ticket_id, position`, args...)
	if err != nil {
		return nil, fmt.Errorf("loading landed commits: %w", err)
	}
	defer commitRows.Close()
	for commitRows.Next() {
		var id string
		var c models.LandedCommit
		if err := commitRows.Scan(&id, &c.SHA, &c.Repo); err != nil {
			return nil, err
		}
		if i, ok := index[id]; ok {
			landed[i].Commits = append(landed[i].Commits, c)
		}
	}
	return landed, commitRows.Err()
}
