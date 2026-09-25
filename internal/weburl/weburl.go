// Package weburl builds the links that open a ticket in the web UI, so the
// CLI and the MCP server can hand an agent a URL it can print or follow.
//
// The canonical link is the home view with a ticket query parameter, on the
// port this build serves from — http://localhost:3010/?ticket=ACP-25 for the
// installed build, :3011 for a development one. Every view reads the
// parameter, and the home view is the one a bare link should land on.
package weburl

import (
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/tcarac/taskboard/internal/livebuild"
	"github.com/tcarac/taskboard/internal/models"
)

const (
	// LivePort is the default for the live build installed by `make install`.
	LivePort = 3010
	// DevPort is the default for every other build, so it never collides with
	// the live server.
	DevPort = 3011
)

// BaseEnv names the environment variable that overrides the base URL, for a
// board reached on another port or through a tunnel. Without it the base is
// the default port of this build, which is where `taskboard start` serves.
const BaseEnv = "TASKBOARD_URL"

// DefaultPort keeps development builds off the live server's port.
func DefaultPort() int {
	if livebuild.Enabled() {
		return LivePort
	}
	return DevPort
}

// Base is the web UI's origin: TASKBOARD_URL when it is set, otherwise
// localhost on this build's default port. It carries no trailing slash.
//
// The CLI and the MCP server talk to the database directly rather than to a
// running server, so they cannot observe a `start --port` that differs from
// the default; TASKBOARD_URL is how that board says where it is.
func Base() string {
	if override := strings.TrimSpace(os.Getenv(BaseEnv)); override != "" {
		return strings.TrimRight(override, "/")
	}
	return "http://localhost:" + strconv.Itoa(DefaultPort())
}

// Ticket is the URL that opens the ticket named by ref, a display key such as
// "ACP-25" or an id.
func Ticket(base, ref string) string {
	return base + "/?ticket=" + url.QueryEscape(ref)
}

// TicketDocument is the URL that opens a ticket with one of its documents on
// top, named by its display name ("Design spec.md") as the web UI's `doc`
// parameter expects.
func TicketDocument(base, ticketRef, displayName string) string {
	return Ticket(base, ticketRef) + "&doc=" + url.QueryEscape(displayName)
}

// Epic is the URL that opens an epic's modal on the Epics view. Epic names
// are unique within a project, and the view shows one project, so the
// project's prefix and the epic's name are enough.
func Epic(base, projectPrefix, epicName string) string {
	return base + "/epics?project=" + url.QueryEscape(projectPrefix) + "&epic=" + url.QueryEscape(epicName)
}

// EpicDocument is the URL that opens an epic with one of its documents on
// top, named by its display name as the web UI's `doc` parameter expects.
func EpicDocument(base, projectPrefix, epicName, displayName string) string {
	return Epic(base, projectPrefix, epicName) + "&doc=" + url.QueryEscape(displayName)
}

// DocumentDownload is the URL that downloads a document's file from the
// board's HTTP API, an image at full size and without its metadata.
func DocumentDownload(base, documentID string) string {
	return base + "/api/documents/" + url.PathEscape(documentID) + "/download"
}

// Ref is how a ticket names itself in a URL: its display key, or its id when
// its project has no prefix, since the bare number a prefixless DisplayKey
// gives back matches no ticket in the web UI. This mirrors ticketRefFor in
// web/src/lib/ticketParam.ts, which the two must agree on.
func Ref(t models.Ticket) string {
	if t.ProjectPrefix == "" {
		return t.ID
	}
	return t.DisplayKey()
}

// Fill sets a ticket's URL and its documents' URLs, and hands the ticket back so it can be returned
// inline. A nil ticket (a lookup that found nothing) passes through untouched.
func Fill(t *models.Ticket) *models.Ticket {
	if t == nil {
		return nil
	}
	base := Base()
	ref := Ref(*t)
	t.URL = Ticket(base, ref)
	for i := range t.Documents {
		d := &t.Documents[i]
		d.URL = TicketDocument(base, ref, models.DocumentDisplayName(d.Name, d.Format))
	}
	return t
}

// FillAll sets the URL on every ticket in a list, in place.
func FillAll(tickets []models.Ticket) []models.Ticket {
	base := Base()
	for i := range tickets {
		tickets[i].URL = Ticket(base, Ref(tickets[i]))
	}
	return tickets
}
