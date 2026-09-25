package models

import (
	"fmt"
	"math"
	"time"
)

// Document formats. The format is chosen when a document is created and never
// changes; the name carries no extension, and the UI shows one based on this.
// Markdown and HTML documents hold text; the others are images, whose bytes
// live apart from the document's details (see the store).
const (
	DocumentFormatMarkdown = "markdown"
	DocumentFormatHTML     = "html"
	DocumentFormatPNG      = "png"
	DocumentFormatJPEG     = "jpeg"
	DocumentFormatGIF      = "gif"
	DocumentFormatWebP     = "webp"
)

// ImageFormats are the formats that are images, in the order messages list
// them.
var ImageFormats = []string{DocumentFormatPNG, DocumentFormatJPEG, DocumentFormatGIF, DocumentFormatWebP}

// IsImageFormat reports whether format is one of the image formats.
func IsImageFormat(format string) bool {
	switch format {
	case DocumentFormatPNG, DocumentFormatJPEG, DocumentFormatGIF, DocumentFormatWebP:
		return true
	}
	return false
}

// ImageFormatName is how an image format is named to people: "PNG",
// "JPEG", "GIF", "WebP". Any other format comes back as it is.
func ImageFormatName(format string) string {
	switch format {
	case DocumentFormatPNG:
		return "PNG"
	case DocumentFormatJPEG:
		return "JPEG"
	case DocumentFormatGIF:
		return "GIF"
	case DocumentFormatWebP:
		return "WebP"
	}
	return format
}

// ImageMediaType is the Content-Type an image format is served with, and ""
// for a format that is not an image.
func ImageMediaType(format string) string {
	if IsImageFormat(format) {
		return "image/" + format
	}
	return ""
}

// MaxDocumentBytes is the largest content a document may hold, in bytes of
// UTF-8. A larger save is refused, never truncated.
const MaxDocumentBytes = 8 << 20

// DocumentExtension is the extension a format is shown and downloaded with.
// A JPEG shows as ".jpg", however it was named when it was attached.
func DocumentExtension(format string) string {
	switch format {
	case DocumentFormatHTML:
		return ".html"
	case DocumentFormatPNG:
		return ".png"
	case DocumentFormatJPEG:
		return ".jpg"
	case DocumentFormatGIF:
		return ".gif"
	case DocumentFormatWebP:
		return ".webp"
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
// or an epic's details carry. Exactly one of TicketID and EpicID is set. Size is the content's length in bytes (an image's: its
// file's). Revision counts content saves, starting at 1; a rename leaves it
// alone. Width and Height are set for images only: the size in pixels the
// picture shows at, with its orientation applied.
type DocumentMeta struct {
	ID        string    `json:"id"`
	TicketID  string    `json:"ticketId,omitempty"`
	EpicID    string    `json:"epicId,omitempty"`
	Name      string    `json:"name"`
	Format    string    `json:"format"`
	Size      int       `json:"size"`
	Width     int       `json:"width,omitempty"`
	Height    int       `json:"height,omitempty"`
	Revision  int       `json:"revision"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`

	// URL opens the document in the web UI. The CLI and MCP fill it in, like
	// Ticket.URL; the HTTP API leaves it empty.
	URL string `json:"url,omitempty"`
}

// Document is a document with its content. An image's Content is always
// empty: its bytes are read on their own (the store's GetDocumentImage).
type Document struct {
	DocumentMeta
	Content string `json:"content"`

	// Set only by a rename of an image: the places in its owner's text
	// whose references were rewritten to the new name, and those where a
	// reference could not be rewritten and now shows a missing image.
	ReferencesUpdated []ImagePlace `json:"referencesUpdated,omitempty"`
	ReferencesLeft    []ImagePlace `json:"referencesLeft,omitempty"`
}

type CreateDocumentRequest struct {
	// Exactly one of TicketID and EpicID is set: the owning ticket's or
	// epic's id. Callers resolve display keys and epic names first.
	TicketID string `json:"ticketId,omitempty"`
	EpicID   string `json:"epicId,omitempty"`
	Name     string `json:"name"`
	// Format defaults to markdown when empty.
	Format  string `json:"format,omitempty"`
	Content string `json:"content"`
}

// CreateImageRequest attaches an image: Data is the file as uploaded, which
// the store checks against Format and stores with its metadata removed.
type CreateImageRequest struct {
	TicketID string
	EpicID   string
	Name     string
	Format   string
	Data     []byte
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
