package db

import (
	"errors"
	"reflect"
	"testing"

	"github.com/tcarac/taskboard/internal/models"
)

const (
	shaA1 = "6bafa19c0ffee0000000000000000000000000a1"
	shaA2 = "77aa0bc0ffee0000000000000000000000000a2a"
	shaB1 = "a198cc5deadbeef000000000000000000000b1b1"
	shaB2 = "ac5ecf6deadbeef000000000000000000000b2b2"
)

func seedTicketInRepos(t *testing.T, s *Store, projectID, title string, repos ...string) *models.Ticket {
	t.Helper()
	tk, err := s.CreateTicket(models.CreateTicketRequest{ProjectID: projectID, Title: title, Repos: repos})
	if err != nil {
		t.Fatalf("seeding ticket %q: %v", title, err)
	}
	return tk
}

func setDelivery(t *testing.T, s *Store, id string, d models.DeliveryUpdate) *models.Ticket {
	t.Helper()
	tk, err := s.UpdateTicket(id, models.UpdateTicketRequest{Delivery: &d})
	if err != nil {
		t.Fatalf("setting delivery: %v", err)
	}
	return tk
}

func commitsPtr(c ...models.LandedCommit) *[]models.LandedCommit {
	if c == nil {
		c = []models.LandedCommit{}
	}
	return &c
}

func wantDelivery(t *testing.T, s *Store, id string, want *models.Delivery) {
	t.Helper()
	got, err := s.GetTicket(id)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Delivery, want) {
		t.Fatalf("delivery = %+v, want %+v", got.Delivery, want)
	}
}

// Every field is stored and read back, commits in the order given, across
// two repos; a field left out stays as it was, and "" or [] clears.
func TestDeliveryFieldsSetReadAndClear(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Taskboard", "ACP")
	tk := seedTicketInRepos(t, s, p.ID, "Delivery", "acme/api", "acme/web")

	if got, _ := s.GetTicket(tk.ID); got.Delivery != nil {
		t.Fatalf("new ticket delivery = %+v, want none", got.Delivery)
	}

	updated := setDelivery(t, s, tk.ID, models.DeliveryUpdate{
		Branch:   strPtr(" acp-150-delivery-fields "),
		Worktree: strPtr("/w/acp-150"),
		PRURL:    strPtr("https://github.com/acme/api/pull/12"),
		LandedCommits: commitsPtr(
			models.LandedCommit{SHA: shaA1, Repo: "acme/api"},
			models.LandedCommit{SHA: shaB1, Repo: "acme/web"},
			models.LandedCommit{SHA: shaA2, Repo: "acme/api"},
			models.LandedCommit{SHA: shaB2, Repo: "acme/web"},
		),
	})
	full := &models.Delivery{
		Branch:   "acp-150-delivery-fields",
		Worktree: "/w/acp-150",
		PRURL:    "https://github.com/acme/api/pull/12",
		LandedCommits: []models.LandedCommit{
			{SHA: shaA1, Repo: "acme/api"},
			{SHA: shaB1, Repo: "acme/web"},
			{SHA: shaA2, Repo: "acme/api"},
			{SHA: shaB2, Repo: "acme/web"},
		},
	}
	if !reflect.DeepEqual(updated.Delivery, full) {
		t.Fatalf("UpdateTicket answered delivery %+v, want %+v", updated.Delivery, full)
	}
	wantDelivery(t, s, tk.ID, full)

	// An update without delivery, or with only one field, leaves the rest.
	if _, err := s.UpdateTicket(tk.ID, models.UpdateTicketRequest{Title: strPtr("Renamed")}); err != nil {
		t.Fatal(err)
	}
	wantDelivery(t, s, tk.ID, full)
	setDelivery(t, s, tk.ID, models.DeliveryUpdate{Branch: strPtr("other")})
	full.Branch = "other"
	wantDelivery(t, s, tk.ID, full)

	// "" clears a text field and an empty list clears the commits.
	setDelivery(t, s, tk.ID, models.DeliveryUpdate{PRURL: strPtr(""), LandedCommits: commitsPtr()})
	wantDelivery(t, s, tk.ID, &models.Delivery{Branch: "other", Worktree: "/w/acp-150"})

	// With everything cleared the ticket has no delivery at all.
	setDelivery(t, s, tk.ID, models.DeliveryUpdate{Branch: strPtr(""), Worktree: strPtr("  ")})
	wantDelivery(t, s, tk.ID, nil)
	var rows int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM ticket_delivery").Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("ticket_delivery rows = %d, %v; want none left", rows, err)
	}
}

// A commit is kept as lowercase hex; a repeat of the same commit in the same
// repo is kept once, while the same sha in another repo is its own commit.
// Without a repo, a commit takes the ticket's only repo.
func TestLandedCommitsAreNormalized(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Taskboard", "ACP")
	one := seedTicketInRepos(t, s, p.ID, "One repo", "acme/api")

	setDelivery(t, s, one.ID, models.DeliveryUpdate{LandedCommits: commitsPtr(
		models.LandedCommit{SHA: " 6BAFA19 "},
		models.LandedCommit{SHA: "6bafa19", Repo: "acme/api"},
		models.LandedCommit{SHA: "6bafa19", Repo: " acme/web "},
		models.LandedCommit{SHA: shaA2},
	)})
	wantDelivery(t, s, one.ID, &models.Delivery{LandedCommits: []models.LandedCommit{
		{SHA: "6bafa19", Repo: "acme/api"},
		{SHA: "6bafa19", Repo: "acme/web"},
		{SHA: shaA2, Repo: "acme/api"},
	}})

	// The default follows repos set in the same update.
	moved, err := s.UpdateTicket(one.ID, models.UpdateTicketRequest{
		Repos:    []string{"acme/web"},
		Delivery: &models.DeliveryUpdate{LandedCommits: commitsPtr(models.LandedCommit{SHA: shaB1})},
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []models.LandedCommit{{SHA: shaB1, Repo: "acme/web"}}; !reflect.DeepEqual(moved.Delivery.LandedCommits, want) {
		t.Fatalf("landed commits = %+v, want %+v", moved.Delivery.LandedCommits, want)
	}
}

// A bad sha, a missing repo that has nothing to default to, or a url that is
// not http(s) is the caller's mistake, and nothing in the request applies.
func TestDeliveryRejectsBadInput(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Taskboard", "ACP")
	two := seedTicketInRepos(t, s, p.ID, "Two repos", "acme/api", "acme/web")
	none := seedTicketInRepos(t, s, p.ID, "No repo")
	setDelivery(t, s, two.ID, models.DeliveryUpdate{Branch: strPtr("kept")})

	for _, tc := range []struct {
		name string
		id   string
		d    models.DeliveryUpdate
	}{
		{"short sha", two.ID, models.DeliveryUpdate{LandedCommits: commitsPtr(models.LandedCommit{SHA: "6bafa1", Repo: "acme/api"})}},
		{"not hex", two.ID, models.DeliveryUpdate{LandedCommits: commitsPtr(models.LandedCommit{SHA: "6bafa1g", Repo: "acme/api"})}},
		{"too long", two.ID, models.DeliveryUpdate{LandedCommits: commitsPtr(models.LandedCommit{SHA: shaA1 + shaA1, Repo: "acme/api"})}},
		{"no repo on two", two.ID, models.DeliveryUpdate{LandedCommits: commitsPtr(models.LandedCommit{SHA: shaA1})}},
		{"no repo on none", none.ID, models.DeliveryUpdate{LandedCommits: commitsPtr(models.LandedCommit{SHA: shaA1})}},
		{"pr not a url", two.ID, models.DeliveryUpdate{PRURL: strPtr("pull 12")}},
		{"pr not http", two.ID, models.DeliveryUpdate{PRURL: strPtr("ftp://github.com/acme/api/pull/12")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.d.Branch = strPtr("changed")
			_, err := s.UpdateTicket(tc.id, models.UpdateTicketRequest{Title: strPtr("changed"), Delivery: &tc.d})
			var invalid *ErrInvalidInput
			if !errors.As(err, &invalid) {
				t.Fatalf("err = %v, want an ErrInvalidInput", err)
			}
			got, _ := s.GetTicket(tc.id)
			if got.Title == "changed" || (got.Delivery != nil && got.Delivery.Branch == "changed") {
				t.Fatalf("a rejected update applied: title %q, delivery %+v", got.Title, got.Delivery)
			}
		})
	}
	wantDelivery(t, s, two.ID, &models.Delivery{Branch: "kept"})
}

func commitKeys(found []models.CommitTicket) []string {
	keys := []string{}
	for _, f := range found {
		keys = append(keys, f.Key)
	}
	return keys
}

// A full or short sha finds the ticket that landed it, whichever form was
// recorded, across tickets and two repos; repo narrows the search.
func TestFindTicketsByCommit(t *testing.T) {
	s := newTestStore(t)
	p := seedProject(t, s, "Taskboard", "ACP")
	api := seedTicketInRepos(t, s, p.ID, "API work", "acme/api")          // ACP-1
	both := seedTicketInRepos(t, s, p.ID, "Both", "acme/api", "acme/web") // ACP-2
	short := seedTicketInRepos(t, s, p.ID, "Recorded short", "acme/web")  // ACP-3
	seedTicketInRepos(t, s, p.ID, "Nothing landed", "acme/api")           // ACP-4

	setDelivery(t, s, api.ID, models.DeliveryUpdate{LandedCommits: commitsPtr(
		models.LandedCommit{SHA: shaA1}, models.LandedCommit{SHA: shaA2},
	)})
	setDelivery(t, s, both.ID, models.DeliveryUpdate{LandedCommits: commitsPtr(
		models.LandedCommit{SHA: shaB1, Repo: "acme/web"},
		models.LandedCommit{SHA: shaB2, Repo: "acme/api"},
		models.LandedCommit{SHA: shaB2, Repo: "acme/web"},
	)})
	setDelivery(t, s, short.ID, models.DeliveryUpdate{LandedCommits: commitsPtr(
		models.LandedCommit{SHA: "77aa0bc"},
	)})

	for _, tc := range []struct {
		name, sha, repo string
		want            []models.CommitTicket
	}{
		{"full sha", shaA1, "", []models.CommitTicket{
			{Key: "ACP-1", Commits: []models.LandedCommit{{SHA: shaA1, Repo: "acme/api"}}},
		}},
		{"short sha, any case", "6BAFA19", "", []models.CommitTicket{
			{Key: "ACP-1", Commits: []models.LandedCommit{{SHA: shaA1, Repo: "acme/api"}}},
		}},
		{"full sha finds one recorded short, and the full one", shaA2, "", []models.CommitTicket{
			{Key: "ACP-1", Commits: []models.LandedCommit{{SHA: shaA2, Repo: "acme/api"}}},
			{Key: "ACP-3", Commits: []models.LandedCommit{{SHA: "77aa0bc", Repo: "acme/web"}}},
		}},
		{"the same commit in two repos", "ac5ecf6", "", []models.CommitTicket{
			{Key: "ACP-2", Commits: []models.LandedCommit{{SHA: shaB2, Repo: "acme/api"}, {SHA: shaB2, Repo: "acme/web"}}},
		}},
		{"narrowed to a repo", "ac5ecf6", "acme/web", []models.CommitTicket{
			{Key: "ACP-2", Commits: []models.LandedCommit{{SHA: shaB2, Repo: "acme/web"}}},
		}},
		{"narrowed to a repo that does not have it", shaA1, "acme/web", []models.CommitTicket{}},
		{"no ticket landed it", "0123456789abcdef", "", []models.CommitTicket{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			found, err := s.FindTicketsByCommit(tc.sha, tc.repo)
			if err != nil {
				t.Fatal(err)
			}
			if found == nil {
				t.Fatal("found = nil, want a list (empty when nothing matches)")
			}
			if got, want := commitKeys(found), commitKeys(tc.want); !reflect.DeepEqual(got, want) {
				t.Fatalf("keys = %v, want %v", got, want)
			}
			for i := range found {
				if !reflect.DeepEqual(found[i].Commits, tc.want[i].Commits) {
					t.Fatalf("%s commits = %+v, want %+v", found[i].Key, found[i].Commits, tc.want[i].Commits)
				}
			}
		})
	}

	found, _ := s.FindTicketsByCommit(shaB1, "")
	if len(found) != 1 || found[0].ID != both.ID || found[0].Title != "Both" || found[0].Status != models.StatusTodo || found[0].ProjectPrefix != "ACP" {
		t.Fatalf("found = %+v, want ACP-2 with its id, title and status", found)
	}

	for _, bad := range []string{"", "6bafa1", "not-a-sha"} {
		var invalid *ErrInvalidInput
		if _, err := s.FindTicketsByCommit(bad, ""); !errors.As(err, &invalid) {
			t.Fatalf("FindTicketsByCommit(%q) err = %v, want an ErrInvalidInput", bad, err)
		}
	}

	// A deleted ticket takes its delivery with it.
	if err := s.DeleteTicket(api.ID); err != nil {
		t.Fatal(err)
	}
	if found, _ := s.FindTicketsByCommit(shaA1, ""); len(found) != 0 {
		t.Fatalf("after delete, found = %+v, want nothing", found)
	}
}
