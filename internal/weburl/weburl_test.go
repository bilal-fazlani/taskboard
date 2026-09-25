package weburl

import (
	"testing"

	"github.com/tcarac/taskboard/internal/livebuild"
	"github.com/tcarac/taskboard/internal/models"
)

// setLiveBuild marks or unmarks this test binary as the live build for one test.
func setLiveBuild(t *testing.T, live bool) {
	t.Helper()
	prev := livebuild.Mark
	livebuild.Mark = ""
	if live {
		livebuild.Mark = "true"
	}
	t.Cleanup(func() { livebuild.Mark = prev })
}

func TestBaseFollowsTheBuild(t *testing.T) {
	t.Setenv(BaseEnv, "")

	setLiveBuild(t, false)
	if got, want := Base(), "http://localhost:3011"; got != want {
		t.Fatalf("development build base = %q, want %q", got, want)
	}

	setLiveBuild(t, true)
	if got, want := Base(), "http://localhost:3010"; got != want {
		t.Fatalf("live build base = %q, want %q", got, want)
	}
}

func TestBaseEnvOverrides(t *testing.T) {
	setLiveBuild(t, false)

	t.Setenv(BaseEnv, "https://board.example.com/")
	if got, want := Base(), "https://board.example.com"; got != want {
		t.Fatalf("base = %q, want the override without its trailing slash %q", got, want)
	}

	// Blank is no override, so a stray empty variable cannot produce "/?ticket=".
	t.Setenv(BaseEnv, "  ")
	if got, want := Base(), "http://localhost:3011"; got != want {
		t.Fatalf("base = %q, want the default %q", got, want)
	}
}

func TestTicketURL(t *testing.T) {
	if got, want := Ticket("http://localhost:3010", "ACP-25"), "http://localhost:3010/?ticket=ACP-25"; got != want {
		t.Fatalf("Ticket = %q, want %q", got, want)
	}
}

func TestFill(t *testing.T) {
	setLiveBuild(t, false)
	t.Setenv(BaseEnv, "")

	ticket := &models.Ticket{ID: "01AAA", Number: 25, ProjectPrefix: "ACP"}
	if got, want := Fill(ticket).URL, "http://localhost:3011/?ticket=ACP-25"; got != want {
		t.Fatalf("URL = %q, want %q", got, want)
	}
	if Fill(nil) != nil {
		t.Fatal("Fill(nil) should stay nil, so a lookup that found nothing still reads as not found")
	}
}

// A project with no prefix has no display key to speak of, and a bare number
// names no ticket in the web UI, so the link falls back to the id — the same
// fallback ticketRefFor makes on the web side.
func TestRefFallsBackToTheIDWithoutAPrefix(t *testing.T) {
	setLiveBuild(t, false)
	t.Setenv(BaseEnv, "")

	prefixless := models.Ticket{ID: "01AAA", Number: 3}
	if got, want := Ref(prefixless), "01AAA"; got != want {
		t.Fatalf("Ref = %q, want the id %q", got, want)
	}
	if got, want := Fill(&prefixless).URL, "http://localhost:3011/?ticket=01AAA"; got != want {
		t.Fatalf("URL = %q, want %q", got, want)
	}

	withPrefix := models.Ticket{ID: "01BBB", Number: 3, ProjectPrefix: "ACP"}
	if got, want := Ref(withPrefix), "ACP-3"; got != want {
		t.Fatalf("Ref = %q, want the display key %q", got, want)
	}
}

func TestFillAll(t *testing.T) {
	setLiveBuild(t, false)
	t.Setenv(BaseEnv, "")

	tickets := FillAll([]models.Ticket{
		{ID: "01AAA", Number: 7, ProjectPrefix: "ACP"},
		{ID: "01BBB", Number: 25, ProjectPrefix: "ACP"},
	})
	want := []string{"http://localhost:3011/?ticket=ACP-7", "http://localhost:3011/?ticket=ACP-25"}
	for i, t2 := range tickets {
		if t2.URL != want[i] {
			t.Fatalf("ticket %d URL = %q, want %q", i, t2.URL, want[i])
		}
	}
}

func TestTicketDocumentURL(t *testing.T) {
	got := TicketDocument("http://localhost:3011", "ACP-84", "Design spec.md")
	want := "http://localhost:3011/?ticket=ACP-84&doc=Design+spec.md"
	if got != want {
		t.Fatalf("TicketDocument = %q, want %q", got, want)
	}
}

func TestFillSetsDocumentURLs(t *testing.T) {
	t.Setenv(BaseEnv, "http://board.test")
	tk := &models.Ticket{ID: "01X", Number: 84, ProjectPrefix: "ACP", Documents: []models.DocumentMeta{
		{ID: "d1", Name: "Design spec", Format: models.DocumentFormatMarkdown},
	}}
	Fill(tk)
	if tk.Documents[0].URL != "http://board.test/?ticket=ACP-84&doc=Design+spec.md" {
		t.Fatalf("document URL = %q", tk.Documents[0].URL)
	}
}

func TestEpicDocumentURL(t *testing.T) {
	if got, want := Epic("http://localhost:3011", "ACP", "M4: Documents"),
		"http://localhost:3011/epics?project=ACP&epic=M4%3A+Documents"; got != want {
		t.Fatalf("Epic = %q, want %q", got, want)
	}
	got := EpicDocument("http://localhost:3011", "ACP", "M4: Documents", "Rollout plan.md")
	want := "http://localhost:3011/epics?project=ACP&epic=M4%3A+Documents&doc=Rollout+plan.md"
	if got != want {
		t.Fatalf("EpicDocument = %q, want %q", got, want)
	}
}
