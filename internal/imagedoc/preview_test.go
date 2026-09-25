package imagedoc

import (
	"bytes"
	"image"
	"math/rand/v2"
	"testing"

	"github.com/tcarac/taskboard/internal/models"
)

// noise is the hardest picture to compress: every pixel random.
func noise(w, h int, transparent bool) *image.NRGBA {
	rng := rand.New(rand.NewPCG(1, 2))
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = uint8(rng.Uint32())
	}
	if !transparent {
		for i := 3; i < len(img.Pix); i += 4 {
			img.Pix[i] = 255
		}
	}
	return img
}

func TestNeedsPreview(t *testing.T) {
	for _, c := range []struct {
		w, h, size int
		want       bool
	}{
		{1568, 1000, 3 << 20, false},
		{1000, 1568, PreviewTrigger, false},
		{1569, 10, 1000, true},
		{10, 4000, 1000, true},
		{800, 600, PreviewTrigger + 1, true},
	} {
		if got := NeedsPreview(c.w, c.h, c.size); got != c.want {
			t.Errorf("NeedsPreview(%d, %d, %d) = %v", c.w, c.h, c.size, got)
		}
	}
}

func decodePreview(t *testing.T, p *Preview) image.Image {
	t.Helper()
	if len(p.Data) > PreviewBudget {
		t.Fatalf("preview is %d bytes, over the %d budget", len(p.Data), PreviewBudget)
	}
	img, format, err := image.Decode(bytes.NewReader(p.Data))
	if err != nil {
		t.Fatal(err)
	}
	if "image/"+format != p.ContentType || img.Bounds().Dx() != p.Width || img.Bounds().Dy() != p.Height {
		t.Fatalf("preview says %s %d×%d, decodes as %s %v", p.ContentType, p.Width, p.Height, format, img.Bounds())
	}
	return img
}

func TestMakePreviewScalesLargeImages(t *testing.T) {
	stored := encodeJPEG(t, pattern(3000, 2000))
	p, err := MakePreview(models.DocumentFormatJPEG, stored)
	if err != nil {
		t.Fatal(err)
	}
	decodePreview(t, p)
	if p.Width != 1568 || p.Height != 1045 || p.ContentType != "image/jpeg" {
		t.Errorf("preview %s %d×%d, want JPEG 1568×1045", p.ContentType, p.Width, p.Height)
	}

	// The orientation is applied: the file keeps it in EXIF, which the
	// re-encoded preview has not.
	turned, err := Prepare(models.DocumentFormatJPEG, jpegWithMetadataOf(t, pattern(2400, 1200), 6))
	if err != nil {
		t.Fatal(err)
	}
	p, err = MakePreview(models.DocumentFormatJPEG, turned.Data)
	if err != nil {
		t.Fatal(err)
	}
	img := decodePreview(t, p)
	if p.Width != 784 || p.Height != 1568 {
		t.Errorf("turned preview %d×%d, want 784×1568", p.Width, p.Height)
	}
	// Turned right: the stored top left (no blue) shows at the top right,
	// and the stored top right (full blue) at the bottom right.
	src := pattern(2400, 1200)
	if !near(img.At(p.Width-20, 20), src.At(30, 30)) || !near(img.At(p.Width-20, p.Height-20), src.At(2370, 30)) {
		t.Errorf("turned preview right corners = %v, %v; want the stored top corners %v, %v",
			img.At(p.Width-20, 20), img.At(p.Width-20, p.Height-20), src.At(30, 30), src.At(2370, 30))
	}

	// Transparency stays, as PNG.
	var clear image.NRGBA = *image.NewNRGBA(image.Rect(0, 0, 2000, 400))
	p, err = MakePreview(models.DocumentFormatPNG, encodePNG(t, &clear))
	if err != nil {
		t.Fatal(err)
	}
	decodePreview(t, p)
	if p.ContentType != "image/png" || p.Width != 1568 {
		t.Errorf("transparent preview %s %d×%d", p.ContentType, p.Width, p.Height)
	}
}

// TestMakePreviewWorstCases: pure noise at the size a preview allows, opaque
// and transparent, still comes in under the budget.
func TestMakePreviewWorstCases(t *testing.T) {
	for _, transparent := range []bool{false, true} {
		stored := encodePNG(t, noise(1568, 1568, transparent))
		if !NeedsPreview(1568, 1568, len(stored)) {
			t.Fatalf("a %d-byte noisy PNG does not need a preview", len(stored))
		}
		p, err := MakePreview(models.DocumentFormatPNG, stored)
		if err != nil {
			t.Fatal(err)
		}
		decodePreview(t, p)
		t.Logf("transparent %v: %d-byte PNG → %s %d×%d, %d bytes", transparent, len(stored), p.ContentType, p.Width, p.Height, len(p.Data))
	}
}

func TestMakePreviewOfAnimations(t *testing.T) {
	for format, file := range map[string][]byte{
		models.DocumentFormatGIF:  gifWithMetadata(t),
		models.DocumentFormatWebP: readTestdata(t, "animated.webp"),
	} {
		prepared, err := Prepare(format, file)
		if err != nil {
			t.Fatal(err)
		}
		p, err := MakePreview(format, prepared.Data)
		if err != nil {
			t.Fatalf("%s: %v", format, err)
		}
		decodePreview(t, p)
		if p.Width != prepared.Width || p.Height != prepared.Height {
			t.Errorf("%s: preview %d×%d, image %d×%d", format, p.Width, p.Height, prepared.Width, prepared.Height)
		}
	}
}
