package cli

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/tcarac/taskboard/internal/models"
)

const (
	cliSHA1 = "6bafa19c0ffee0000000000000000000000000a1"
	cliSHA2 = "a198cc5deadbeef000000000000000000000b1b1"
)

// ticket update sets the delivery fields, several commits across two repos
// included; ticket get shows them as text and JSON; a flag left out leaves
// its field alone and an empty value clears it.
func TestTicketUpdateSetsDelivery(t *testing.T) {
	setLiveBuild(t, false)
	sandboxHome(t)
	path := filepath.Join(t.TempDir(), "dev.db")
	run := func(args ...string) string {
		t.Helper()
		out, err := runCLI(t, append([]string{"--db", path}, args...)...)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		return out
	}
	delivery := func() *models.Delivery {
		t.Helper()
		var got models.Ticket
		out := run("ticket", "get", "BILL-1", "--json")
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("ticket get --json: %v\n%s", err, out)
		}
		return got.Delivery
	}

	run("project", "create", "Billing", "--prefix", "BILL")
	run("ticket", "create", "--project", "BILL", "--title", "Invoice", "--repo", "acme/api,acme/web")
	if d := delivery(); d != nil {
		t.Fatalf("new ticket delivery = %+v, want none", d)
	}

	run("ticket", "update", "BILL-1", "--branch", "bill-1", "--worktree", "/w/bill-1",
		"--pr-url", "https://github.com/acme/api/pull/9",
		"--landed-commit", "acme/api@"+cliSHA1+",acme/web@"+cliSHA2, "--landed-commit", "acme/web@77AA0BC")
	want := &models.Delivery{
		Branch: "bill-1", Worktree: "/w/bill-1", PRURL: "https://github.com/acme/api/pull/9",
		LandedCommits: []models.LandedCommit{
			{SHA: cliSHA1, Repo: "acme/api"},
			{SHA: cliSHA2, Repo: "acme/web"},
			{SHA: "77aa0bc", Repo: "acme/web"},
		},
	}
	if d := delivery(); !reflect.DeepEqual(d, want) {
		t.Fatalf("delivery = %+v, want %+v", d, want)
	}

	text := run("ticket", "get", "BILL-1")
	for _, line := range []string{
		"Branch: bill-1\n", "Worktree: /w/bill-1\n", "PR: https://github.com/acme/api/pull/9\n",
		"Landed commits:\n  acme/api@" + cliSHA1 + "\n  acme/web@" + cliSHA2 + "\n  acme/web@77aa0bc\n",
	} {
		if !strings.Contains(text, line) {
			t.Fatalf("ticket get does not show %q:\n%s", line, text)
		}
	}

	run("ticket", "update", "BILL-1", "--title", "Invoice v2")
	run("ticket", "update", "BILL-1", "--pr-url", "", "--landed-commit", "")
	want.PRURL, want.LandedCommits = "", nil
	if d := delivery(); !reflect.DeepEqual(d, want) {
		t.Fatalf("after clearing: delivery = %+v, want %+v", d, want)
	}

	// A bare sha needs a ticket with exactly one repo.
	if _, err := runCLI(t, "--db", path, "ticket", "update", "BILL-1", "--landed-commit", "0123abc"); err == nil || !strings.Contains(err.Error(), "has no repo") {
		t.Fatalf("bare sha on a two-repo ticket: err = %v, want a refusal", err)
	}
	run("ticket", "update", "BILL-1", "--repo", "acme/api", "--landed-commit", "0123ABC")
	if d := delivery(); !reflect.DeepEqual(d.LandedCommits, []models.LandedCommit{{SHA: "0123abc", Repo: "acme/api"}}) {
		t.Fatalf("bare sha on a one-repo ticket: %+v, want it in acme/api", d.LandedCommits)
	}
}

// ticket find-by-commit finds a ticket by a full or short sha, across two
// repos, as text or JSON.
func TestTicketFindByCommit(t *testing.T) {
	setLiveBuild(t, false)
	sandboxHome(t)
	path := filepath.Join(t.TempDir(), "dev.db")
	run := func(args ...string) string {
		t.Helper()
		out, err := runCLI(t, append([]string{"--db", path}, args...)...)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		return out
	}
	run("project", "create", "Billing", "--prefix", "BILL")
	run("ticket", "create", "--project", "BILL", "--title", "API", "--repo", "acme/api")
	run("ticket", "create", "--project", "BILL", "--title", "Web", "--repo", "acme/web")
	run("ticket", "update", "BILL-1", "--landed-commit", cliSHA1)
	run("ticket", "update", "BILL-2", "--landed-commit", "acme/web@"+cliSHA2+",acme/api@6bafa19")

	out := run("ticket", "find-by-commit", "6BAFA19")
	for _, want := range []string{
		"[BILL-1] API - todo\n", "  landed acme/api@" + cliSHA1 + "\n",
		"[BILL-2] Web - todo\n", "  landed acme/api@6bafa19\n", "?ticket=BILL-1",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("find-by-commit 6BAFA19 does not show %q:\n%s", want, out)
		}
	}

	var found []models.CommitTicket
	out = run("ticket", "find-by-commit", cliSHA2, "--json")
	if err := json.Unmarshal([]byte(out), &found); err != nil {
		t.Fatalf("find-by-commit --json: %v\n%s", err, out)
	}
	if len(found) != 1 || found[0].Key != "BILL-2" || !reflect.DeepEqual(found[0].Commits, []models.LandedCommit{{SHA: cliSHA2, Repo: "acme/web"}}) {
		t.Fatalf("find-by-commit full sha = %+v, want BILL-2 with its acme/web commit", found)
	}

	// The full sha also finds BILL-2's short record of it; --repo narrows.
	if out := run("ticket", "find-by-commit", cliSHA1); !strings.Contains(out, "BILL-1") || !strings.Contains(out, "BILL-2") {
		t.Fatalf("find-by-commit with the full sha:\n%s\nwant BILL-1 and BILL-2", out)
	}
	if out := run("ticket", "find-by-commit", cliSHA2, "--repo", "acme/api"); out != "No ticket landed "+cliSHA2+".\n" {
		t.Fatalf("find-by-commit --repo acme/api for an acme/web commit printed %q", out)
	}
	if out := run("ticket", "find-by-commit", "0000000"); out != "No ticket landed 0000000.\n" {
		t.Fatalf("no match printed %q", out)
	}
	if _, err := runCLI(t, "--db", path, "ticket", "find-by-commit", "zz"); err == nil || !strings.Contains(err.Error(), "not a commit sha") {
		t.Fatalf("bad sha: err = %v, want a refusal", err)
	}
}
