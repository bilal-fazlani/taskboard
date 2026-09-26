package mcp

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/tcarac/taskboard/internal/db"
	"github.com/tcarac/taskboard/internal/models"
)

// documentPathDescription explains the path argument of create_document and
// update_document. MCP runs only as a local stdio process, so the file is
// read on the caller's own machine, as its user; the HTTP API has no such
// argument, since it can be reached from elsewhere.
const documentPathDescription = "An absolute path to a local file, read by this MCP server's own process on your " +
	"machine. Prefer it to content and data for a file you have locally (a screenshot, a mock, a long write-up): " +
	"those put the whole file through your context, path does not. The format comes from the extension " +
	"(.md, .html, .htm, .png, .jpg, .jpeg, .gif, .webp), with the same checks and 8 MB limit as content and data."

// readDocumentPath reads the file the path argument names. The path must be
// absolute, since a relative one would be taken from wherever the MCP
// process happens to run. The file is looked at before it is opened: a
// missing file, a directory or anything else that is not a regular file
// (a pipe or a device, which could block or never end) and a file over
// models.MaxDocumentBytes are refused without reading it; reading then stops
// one byte past the limit, so a file that grew in between is never read in
// full either. Every refusal names the path.
func readDocumentPath(path string) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("path must be absolute: %q is not", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, pathError(path, err)
	}
	if err := checkDocumentFileInfo(path, info); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, pathError(path, err)
	}
	defer f.Close()
	// The file the path names may have changed since the look above.
	info, err = f.Stat()
	if err != nil {
		return nil, pathError(path, err)
	}
	if err := checkDocumentFileInfo(path, info); err != nil {
		return nil, err
	}
	return readAtMostDocument(path, f)
}

// checkDocumentFileInfo refuses what the path argument must never read: a
// directory, anything else that isn't a regular file, and a file over the
// limit.
func checkDocumentFileInfo(path string, info fs.FileInfo) error {
	switch {
	case info.IsDir():
		return fmt.Errorf("%s is a directory, not a file", path)
	case !info.Mode().IsRegular():
		return fmt.Errorf("%s is not a regular file", path)
	case info.Size() > models.MaxDocumentBytes:
		return fmt.Errorf("%s is %s. The limit is 8 MB.", path, models.FormatSize(int(info.Size())))
	}
	return nil
}

// readAtMostDocument reads r up to one byte past the limit and refuses it
// when it gets there, so it never reads more than that.
func readAtMostDocument(path string, r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, models.MaxDocumentBytes+1))
	if err != nil {
		return nil, pathError(path, err)
	}
	if len(data) > models.MaxDocumentBytes {
		return nil, fmt.Errorf("%s is over 8 MB. The limit is 8 MB.", path)
	}
	return data, nil
}

// documentPathText is a file read from path as a markdown or HTML document's
// content. It must be text: content that arrives as a JSON string always is,
// but a file could hold anything, and an extensionless screenshot would
// otherwise become a markdown document full of binary bytes.
func documentPathText(path string, file []byte) (string, error) {
	if !utf8.Valid(file) {
		return "", fmt.Errorf("%s is not text (it isn't valid UTF-8), so it can't be a markdown or HTML document; "+
			"for an image, give the file its extension or pass format", path)
	}
	return string(file), nil
}

// pathError words a failure to look at or read the file, naming its path.
func pathError(path string, err error) error {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("no file at %s", path)
	case errors.Is(err, fs.ErrPermission):
		return fmt.Errorf("cannot read %s: permission denied", path)
	}
	var pe *fs.PathError
	if errors.As(err, &pe) {
		err = pe.Err // the path is named once, below
	}
	return fmt.Errorf("cannot read %s: %v", path, err)
}

// documentPathFormat is the format create_document gives a file read from
// path, as `doc add --file` decides it: format (or the extension on an
// image's name) when given, else the path's extension, else markdown. A .svg
// file is taken as "svg" so the store refuses it in its own words rather
// than attaching it as text.
func documentPathFormat(path, format string) string {
	format = db.DocumentFileFormat(path, format)
	if format == "" && strings.EqualFold(filepath.Ext(path), ".svg") {
		return "svg"
	}
	return format
}

// checkPathFitsDocument refuses a file whose extension names a format other
// than the document's own, since update_document never changes a format:
// without it a picture could be saved as a markdown document's text.
func checkPathFitsDocument(path string, d *models.Document) error {
	format := documentPathFormat(path, "")
	if format == "" || format == d.Format {
		return nil
	}
	return fmt.Errorf("%s ends in %s, but this document is %s; a document's format never changes",
		path, filepath.Ext(path), formatName(d.Format))
}

// formatName is how the path errors name a format.
func formatName(format string) string {
	switch format {
	case models.DocumentFormatMarkdown:
		return "markdown"
	case models.DocumentFormatHTML:
		return "HTML"
	}
	return models.ImageFormatName(format)
}
