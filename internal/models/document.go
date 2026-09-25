package models

import (
	"fmt"
	"math"
	"time"
)

// Document formats. The format is chosen when a document is created and never
// changes; the name carries no extension, and the UI shows one based on this.
const (
	DocumentFormatMarkdown = "markdown"
	DocumentFormatHTML     = "html"
)

// MaxDocumentBytes is the largest content a document may hold, in bytes of
// UTF-8. A larger save is refused, never truncated.
const MaxDocumentBytes = 8 << 20

// DocumentExtension is the extension a format is shown and downloaded with.
func DocumentExtension(format string) string {
	if format == DocumentFormatHTML {
		return ".html"
	}
	return ".md"
}

// DocumentDisplayName is how a document is shown, linked and downloaded:
// its name plus its format's extension, "Design spec.md".
func DocumentDisplayName(name, format string) string {
	return name + DocumentExtension(format)
}

// FormatSize renders a byte count the way the UI and the CLI show it. Sizes
// round up, so a document just over the limit never reads as the limit.
func FormatSize(bytes int) string {
	switch {
	case bytes < 1024:
		return fmt.Sprintf("%d B", bytes)
	case bytes < 1<<20:
		return fmt.Sprintf("%d KB", int(math.Ceil(float64(bytes)/1024)))
	default:
		return fmt.Sprintf("%.1f MB", math.Ceil(float64(bytes)*10/(1<<20))/10)
	}
}

// DocumentMeta is a document without its content: what lists and a ticket's
// details carry. Size is the content's length in bytes. Revision counts
// content saves, starting at 1; a rename leaves it alone.
type DocumentMeta struct {
	ID        string    `json:"id"`
	TicketID  string    `json:"ticketId,omitempty"`
	Name      string    `json:"name"`
	Format    string    `json:"format"`
	Size      int       `json:"size"`
	Revision  int       `json:"revision"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`

	// URL opens the document in the web UI. The CLI and MCP fill it in, like
	// Ticket.URL; the HTTP API leaves it empty.
	URL string `json:"url,omitempty"`
}

// Document is a document with its content.
type Document struct {
	DocumentMeta
	Content string `json:"content"`
}

type CreateDocumentRequest struct {
	// TicketID is the owning ticket's id; callers resolve display keys first.
	TicketID string `json:"ticketId"`
	Name     string `json:"name"`
	// Format defaults to markdown when empty.
	Format  string `json:"format,omitempty"`
	Content string `json:"content"`
}

// UpdateDocumentRequest changes only the fields that are non-nil. Content
// replaces the whole document.
type UpdateDocumentRequest struct {
	Name    *string `json:"name,omitempty"`
	Content *string `json:"content,omitempty"`
	// ExpectedRevision, when set with Content, is the revision the caller
	// started editing from. If the document has moved past it, the save is
	// refused rather than overwriting someone else's. The web UI sends it;
	// agents do not. A rename alone ignores it.
	ExpectedRevision *int `json:"expectedRevision,omitempty"`
}
