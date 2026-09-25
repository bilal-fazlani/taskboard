package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tcarac/taskboard/internal/imagedoc/imagedoctest"
)

// cliImageBoard makes a throwaway board with project DOC, epic Launch and
// ticket DOC-1, and returns its database path.
func cliImageBoard(t *testing.T) string {
	t.Helper()
	setLiveBuild(t, false)
	sandboxHome(t)
	path := filepath.Join(t.TempDir(), "dev.db")
	for _, args := range [][]string{
		{"project", "create", "Docs", "--prefix", "DOC"},
		{"epic", "create", "DOC", "Launch"},
		{"ticket", "create", "--project", "DOC", "--title", "Has images"},
	} {
		if _, err := runCLI(t, append([]string{"--db", path}, args...)...); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func writeTemp(t *testing.T, name string, data []byte) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(file, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestDocCommandsForImages(t *testing.T) {
	path := cliImageBoard(t)
	photo := writeTemp(t, "IMG_0042.JPEG", imagedoctest.JPEGWithGPS(60, 40, 6))

	added := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "doc", "add", "DOC-1", "--file", photo); err != nil {
			t.Fatalf("doc add: %v", err)
		}
	})
	if !strings.Contains(added, "Added document IMG_0042.jpg") || !strings.Contains(added, "doc=IMG_0042.jpg") {
		t.Fatalf("doc add printed %q", added)
	}
	id := lastParenthesized(t, added)

	listed := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "doc", "list", "DOC-1"); err != nil {
			t.Fatalf("doc list: %v", err)
		}
	})
	if !strings.Contains(listed, "IMG_0042.jpg") || !strings.Contains(listed, "40×60") {
		t.Fatalf("doc list printed %q", listed)
	}

	// show prints details, never the bytes.
	shown := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "doc", "show", "img_0042.jpeg", "--ticket", "DOC-1"); err != nil {
			t.Fatalf("doc show: %v", err)
		}
	})
	if !strings.Contains(shown, "IMG_0042.jpg ("+id+")") || !strings.Contains(shown, "JPEG image, 40×60 pixels") ||
		!strings.Contains(shown, "revision 1") || !strings.Contains(shown, "doc=IMG_0042.jpg") || strings.Contains(shown, "\xff\xd8") {
		t.Fatalf("doc show printed %q", shown)
	}

	// download writes the stored file: the metadata is gone.
	out := filepath.Join(t.TempDir(), "photo.jpg")
	downloaded := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "doc", "download", id, "--output", out); err != nil {
			t.Fatalf("doc download: %v", err)
		}
	})
	if !strings.Contains(downloaded, "Downloaded IMG_0042.jpg to "+out) {
		t.Fatalf("doc download printed %q", downloaded)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if secret, found := imagedoctest.HasSecrets(data); found || !bytes.HasPrefix(data, []byte{0xFF, 0xD8}) {
		t.Fatalf("downloaded file holds %s", secret)
	}
	if _, err := runCLI(t, "--db", path, "doc", "download", id, "--output", out); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("overwriting without --force: %v", err)
	}
	if _, err := runCLI(t, "--db", path, "doc", "download", id, "--output", out, "--force"); err != nil {
		t.Fatalf("overwriting with --force: %v", err)
	}
	// By default the file takes the display name, in the current directory.
	dir := t.TempDir()
	t.Chdir(dir)
	captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "doc", "download", "IMG_0042", "--ticket", "DOC-1"); err != nil {
			t.Fatalf("doc download to the display name: %v", err)
		}
	})
	if got, err := os.ReadFile(filepath.Join(dir, "IMG_0042.jpg")); err != nil || !bytes.Equal(got, data) {
		t.Fatalf("download by display name: %v", err)
	}
	// To standard output.
	piped := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "doc", "download", id, "-o", "-"); err != nil {
			t.Fatalf("doc download to stdout: %v", err)
		}
	})
	if piped != string(data) {
		t.Fatal("stdout download differs from the stored file")
	}

	// write replaces the picture: same name, new revision.
	replacement := writeTemp(t, "anything.jpg", imagedoctest.JPEGWithGPS(30, 20, 0))
	saved := captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "doc", "write", id, "--file", replacement); err != nil {
			t.Fatalf("doc write: %v", err)
		}
	})
	if !strings.Contains(saved, "Saved IMG_0042.jpg") {
		t.Fatalf("doc write printed %q", saved)
	}
	shown = captureStdout(t, func() {
		runCLI(t, "--db", path, "doc", "show", id)
	})
	if !strings.Contains(shown, "30×20 pixels") || !strings.Contains(shown, "revision 2") {
		t.Fatalf("after write, doc show printed %q", shown)
	}
	// The format is fixed.
	if _, err := runCLI(t, "--db", path, "doc", "write", id, "--file", writeTemp(t, "x.png", imagedoctest.PNG(8, 8))); err == nil ||
		err.Error() != "This isn't a JPEG image: its content is PNG." {
		t.Fatalf("write a PNG over a JPEG: %v", err)
	}

	// Each format, on a ticket and on an epic, from a file or standard input.
	for _, tc := range []struct {
		args  []string
		input []byte
		want  string
	}{
		{[]string{"DOC-1", "--file", writeTemp(t, "Login screen.png", imagedoctest.PNG(40, 30))}, nil, "Added document Login screen.png"},
		{[]string{"DOC-1", "--file", writeTemp(t, "spinner.gif", imagedoctest.AnimatedGIF())}, nil, "Added document spinner.gif"},
		{[]string{"--epic", "Launch", "--project", "DOC", "--file", writeTemp(t, "mock.webp", imagedoctest.WebPWithGPS(1))}, nil, "Added document mock.webp"},
		{[]string{"DOC-1", "--file", "-", "--name", "Piped", "--format", "png"}, imagedoctest.PNG(8, 8), "Added document Piped.png"},
		{[]string{"DOC-1", "--file", writeTemp(t, "IMG_1.png", imagedoctest.PNG(8, 8)), "--name", "Home page"}, nil, "Added document Home page.png"},
	} {
		printed := captureStdout(t, func() {
			if _, err := runCLIWithInput(t, string(tc.input), append([]string{"--db", path, "doc", "add"}, tc.args...)...); err != nil {
				t.Fatalf("doc add %v: %v", tc.args, err)
			}
		})
		if !strings.Contains(printed, tc.want) {
			t.Fatalf("doc add %v printed %q", tc.args, printed)
		}
	}
}

func TestDocDownloadWritesTextDocuments(t *testing.T) {
	path := cliImageBoard(t)
	if _, err := runCLIWithInput(t, "# Plan\n", "--db", path, "doc", "add", "DOC-1", "--name", "Plan", "--file", "-"); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "plan.md")
	captureStdout(t, func() {
		if _, err := runCLI(t, "--db", path, "doc", "download", "plan.md", "--ticket", "DOC-1", "-o", out); err != nil {
			t.Fatalf("doc download: %v", err)
		}
	})
	if got, err := os.ReadFile(out); err != nil || string(got) != "# Plan\n" {
		t.Fatalf("downloaded %q, %v", got, err)
	}
}

func TestDocCommandImageRefusals(t *testing.T) {
	path := cliImageBoard(t)
	for _, tc := range []struct {
		file string
		data []byte
		want string
	}{
		{"photo.png", imagedoctest.JPEGWithGPS(8, 8, 0), "This isn't a PNG image: its content is JPEG."},
		{"logo.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), "SVG images can't be attached. Use PNG, JPEG, GIF or WebP."},
		{"logo.png", []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), "This isn't a PNG image: it's an SVG, and SVG images can't be attached."},
		{"big.png", append(imagedoctest.PNG(8, 8), make([]byte, 8<<20)...), "This image is 8.1 MB. The limit is 8 MB."},
	} {
		if _, err := runCLI(t, "--db", path, "doc", "add", "DOC-1", "--file", writeTemp(t, tc.file, tc.data)); err == nil || err.Error() != tc.want {
			t.Errorf("doc add %s: %v, want %q", tc.file, err, tc.want)
		}
	}
	listed := captureStdout(t, func() { runCLI(t, "--db", path, "doc", "list", "DOC-1") })
	if !strings.Contains(listed, "No documents.") {
		t.Errorf("refused files were attached: %q", listed)
	}
}
