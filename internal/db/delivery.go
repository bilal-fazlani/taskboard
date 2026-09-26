package db

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/tcarac/taskboard/internal/models"
)

// shaPattern is a short or full commit sha as the store keeps it: lowercase
// hex, from git's default short length (7) up to a full SHA-256 sha (64).
var shaPattern = regexp.MustCompile(`^[0-9a-f]{7,64}$`)

// normalizeSHA trims and lowercases a sha and checks it is one.
func normalizeSHA(raw string) (string, error) {
	sha := strings.ToLower(strings.TrimSpace(raw))
	if !shaPattern.MatchString(sha) {
		return "", invalidInput("%q is not a commit sha: pass 7 to 64 hex characters", raw)
	}
	return sha, nil
}

// normalizePRURL trims a pull request url and checks it is an http or https
// url with a host; "" (a clear) passes.
func normalizePRURL(raw string) (string, error) {
	u := strings.TrimSpace(raw)
	if u == "" {
		return "", nil
	}
	parsed, err := url.Parse(u)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", invalidInput("prUrl %q is not a pull request url: pass an http or https url", raw)
	}
	return u, nil
}

// normalizeLandedCommits checks and tidies a landed commits list: each sha
// normalized, each repo trimmed, a commit without a repo given the ticket's
// only repo, and a repeated (repo, sha) kept once, at its first place.
// ticketRepos are the ticket's repos as they will be after the update.
func normalizeLandedCommits(commits []models.LandedCommit, ticketRepos []string) ([]models.LandedCommit, error) {
	out := make([]models.LandedCommit, 0, len(commits))
	seen := map[models.LandedCommit]bool{}
	for _, c := range commits {
		sha, err := normalizeSHA(c.SHA)
		if err != nil {
			return nil, err
		}
		repo := strings.TrimSpace(c.Repo)
		if repo == "" {
			if len(ticketRepos) != 1 {
				return nil, invalidInput("landed commit %s has no repo: pass its repo, since the ticket has %d repos rather than one to default to", sha, len(ticketRepos))
			}
			repo = ticketRepos[0]
		}
		lc := models.LandedCommit{SHA: sha, Repo: repo}
		if !seen[lc] {
			seen[lc] = true
			out = append(out, lc)
		}
	}
	return out, nil
}

// normalizedDelivery is a DeliveryUpdate checked and tidied, ready to write.
type normalizedDelivery struct {
	branch, worktree, prURL *string
	commits                 *[]models.LandedCommit
}

// normalizeDeliveryUpdate checks every field of u before anything is
// written, so a bad sha or url applies nothing.
func normalizeDeliveryUpdate(u *models.DeliveryUpdate, ticketRepos []string) (*normalizedDelivery, error) {
	n := &normalizedDelivery{}
	if u.Branch != nil {
		b := strings.TrimSpace(*u.Branch)
		n.branch = &b
	}
	if u.Worktree != nil {
		w := strings.TrimSpace(*u.Worktree)
		n.worktree = &w
	}
	if u.PRURL != nil {
		p, err := normalizePRURL(*u.PRURL)
		if err != nil {
			return nil, err
		}
		n.prURL = &p
	}
	if u.LandedCommits != nil {
		c, err := normalizeLandedCommits(*u.LandedCommits, ticketRepos)
		if err != nil {
			return nil, err
		}
		n.commits = &c
	}
	return n, nil
}

// writeDelivery applies a normalized update inside the caller's transaction:
// only the fields it sets, leaving the rest as they are. A ticket whose text
// fields all end up empty keeps no ticket_delivery row.
func writeDelivery(q dbtx, ticketID string, n *normalizedDelivery) error {
	if n.branch != nil || n.worktree != nil || n.prURL != nil {
		if _, err := q.Exec("INSERT OR IGNORE INTO ticket_delivery (ticket_id) VALUES (?)", ticketID); err != nil {
			return fmt.Errorf("adding delivery: %w", err)
		}
		for _, f := range []struct {
			column string
			value  *string
		}{{"branch", n.branch}, {"worktree", n.worktree}, {"pr_url", n.prURL}} {
			if f.value == nil {
				continue
			}
			if _, err := q.Exec("UPDATE ticket_delivery SET "+f.column+" = ? WHERE ticket_id = ?", *f.value, ticketID); err != nil {
				return fmt.Errorf("setting %s: %w", f.column, err)
			}
		}
		if _, err := q.Exec(
			"DELETE FROM ticket_delivery WHERE ticket_id = ? AND branch = '' AND worktree = '' AND pr_url = ''", ticketID,
		); err != nil {
			return fmt.Errorf("tidying delivery: %w", err)
		}
	}
	if n.commits != nil {
		if _, err := q.Exec("DELETE FROM ticket_landed_commits WHERE ticket_id = ?", ticketID); err != nil {
			return fmt.Errorf("clearing landed commits: %w", err)
		}
		for i, c := range *n.commits {
			if _, err := q.Exec(
				"INSERT INTO ticket_landed_commits (ticket_id, repo, sha, position) VALUES (?, ?, ?, ?)",
				ticketID, c.Repo, c.SHA, i,
			); err != nil {
				return fmt.Errorf("adding landed commit: %w", err)
			}
		}
	}
	return nil
}

// ticketReposIn reads a ticket's repos through q, so a transaction sees its
// own writes.
func ticketReposIn(q dbtx, ticketID string) ([]string, error) {
	rows, err := q.Query("SELECT repo FROM ticket_repos WHERE ticket_id = ? ORDER BY repo", ticketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var repos []string
	for rows.Next() {
		var repo string
		if err := rows.Scan(&repo); err != nil {
			return nil, err
		}
		repos = append(repos, repo)
	}
	return repos, rows.Err()
}

// getTicketDelivery reads a ticket's delivery fields, or nil when none is
// set.
func getTicketDelivery(q dbtx, ticketID string) (*models.Delivery, error) {
	var d models.Delivery
	err := q.QueryRow(
		"SELECT branch, worktree, pr_url FROM ticket_delivery WHERE ticket_id = ?", ticketID,
	).Scan(&d.Branch, &d.Worktree, &d.PRURL)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("reading delivery: %w", err)
	}
	rows, err := q.Query(
		"SELECT sha, repo FROM ticket_landed_commits WHERE ticket_id = ? ORDER BY position", ticketID)
	if err != nil {
		return nil, fmt.Errorf("reading landed commits: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var c models.LandedCommit
		if err := rows.Scan(&c.SHA, &c.Repo); err != nil {
			return nil, err
		}
		d.LandedCommits = append(d.LandedCommits, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if d.IsEmpty() {
		return nil, nil
	}
	return &d, nil
}

// FindTicketsByCommit finds the tickets that landed a commit, by its full or
// short sha: a landed commit matches when either sha starts with the other,
// so a short sha finds a full one and a full sha finds one recorded short.
// repo, when not empty, keeps only commits in that repo (matched exactly, as
// ticket repos are). Tickets come in key order, each with its matching
// commits in the order they were given; no match is an empty list, not an
// error. A sha that is not 7 to 64 hex characters is an ErrInvalidInput.
func (s *Store) FindTicketsByCommit(sha, repo string) ([]models.CommitTicket, error) {
	sha, err := normalizeSHA(sha)
	if err != nil {
		return nil, err
	}
	repo = strings.TrimSpace(repo)
	rows, err := s.db.Query(`SELECT t.id, COALESCE(p.prefix, ''), t.number, t.title, t.status, c.sha, c.repo
		FROM ticket_landed_commits c
		JOIN tickets t ON t.id = c.ticket_id
		LEFT JOIN projects p ON p.id = t.project_id
		WHERE (substr(c.sha, 1, length(?1)) = ?1 OR substr(?1, 1, length(c.sha)) = c.sha)
		  AND (?2 = '' OR c.repo = ?2)
		ORDER BY p.prefix, t.number, c.position`, sha, repo)
	if err != nil {
		return nil, fmt.Errorf("finding tickets by commit: %w", err)
	}
	defer rows.Close()

	found := []models.CommitTicket{}
	for rows.Next() {
		var ct models.CommitTicket
		var number int
		var c models.LandedCommit
		if err := rows.Scan(&ct.ID, &ct.ProjectPrefix, &number, &ct.Title, &ct.Status, &c.SHA, &c.Repo); err != nil {
			return nil, err
		}
		if n := len(found); n > 0 && found[n-1].ID == ct.ID {
			found[n-1].Commits = append(found[n-1].Commits, c)
			continue
		}
		ct.Key = models.Ticket{ProjectPrefix: ct.ProjectPrefix, Number: number}.DisplayKey()
		ct.Commits = []models.LandedCommit{c}
		found = append(found, ct)
	}
	return found, rows.Err()
}
