package imagedoc

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tcarac/taskboard/internal/models"
)

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func encodeJPEG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func wantRefusal(t *testing.T, err error, want string) {
	t.Helper()
	var r *Refusal
	if !errors.As(err, &r) {
		t.Fatalf("err = %v, want the refusal %q", err, want)
	}
	if r.Msg != want {
		t.Fatalf("refusal = %q, want %q", r.Msg, want)
	}
}

func TestPrepareChecksContent(t *testing.T) {
	pngFile := encodePNG(t, pattern(8, 8))
	svg := []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)

	_, err := Prepare(models.DocumentFormatJPEG, pngFile)
	wantRefusal(t, err, "This isn't a JPEG image: its content is PNG.")
	_, err = Prepare(models.DocumentFormatPNG, svg)
	wantRefusal(t, err, "This isn't a PNG image: it's an SVG, and SVG images can't be attached.")
	_, err = Prepare(models.DocumentFormatWebP, []byte("<svg width='1'/>"))
	wantRefusal(t, err, "This isn't a WebP image: it's an SVG, and SVG images can't be attached.")
	_, err = Prepare(models.DocumentFormatGIF, []byte("<html><body>hello</body></html>"))
	wantRefusal(t, err, "This isn't a GIF image: its content is not PNG, JPEG, GIF or WebP.")
	_, err = Prepare(models.DocumentFormatPNG, nil)
	wantRefusal(t, err, "This isn't a PNG image: its content is not PNG, JPEG, GIF or WebP.")
	_, err = Prepare(models.DocumentFormatPNG, pngFile[:len(pngFile)/2])
	wantRefusal(t, err, "This PNG image can't be read. It may be damaged.")

	// A private critical chunk (here one holding a script) makes the file
	// damaged: browsers refuse unknown critical chunks, and it must not be
	// stored.
	ihdrEnd := 8 + 12 + 13
	withHTML := append(append([]byte{}, pngFile[:ihdrEnd]...), appendPNGChunk(nil, "HTML", []byte("<script>alert(1)</script>"))...)
	withHTML = append(withHTML, pngFile[ihdrEnd:]...)
	_, err = Prepare(models.DocumentFormatPNG, withHTML)
	wantRefusal(t, err, "This PNG image can't be read. It may be damaged.")
	if _, err := Strip(models.DocumentFormatPNG, withHTML); err == nil {
		t.Error("Strip kept a private critical chunk")
	}

	// A header claiming a huge picture is refused before anything is
	// decoded.
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], 10000)
	binary.BigEndian.PutUint32(ihdr[4:], 6000)
	ihdr[8], ihdr[9] = 8, 6
	huge := append(append([]byte{}, pngSignature...), appendPNGChunk(nil, "IHDR", ihdr)...)
	huge = appendPNGChunk(huge, "IDAT", []byte{0x78, 0x9c})
	huge = appendPNGChunk(huge, "IEND", nil)
	_, err = Prepare(models.DocumentFormatPNG, huge)
	wantRefusal(t, err, "This image is 10000×6000 pixels. The limit is 50 megapixels.")

	if _, err := Prepare("svg", svg); err == nil || errors.As(err, new(*Refusal)) {
		t.Errorf("an unknown format should be a programming error, got %v", err)
	}
}

func decodeThumb(t *testing.T, p *Prepared) image.Image {
	t.Helper()
	img, _, err := image.Decode(bytes.NewReader(p.Thumbnail))
	if err != nil {
		t.Fatalf("thumbnail does not decode: %v", err)
	}
	return img
}

func TestPrepareEachFormat(t *testing.T) {
	var gifBuf bytes.Buffer
	if err := gif.Encode(&gifBuf, pattern(30, 20), nil); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		format        string
		data          []byte
		width, height int
		thumbType     string
	}{
		{models.DocumentFormatPNG, pngWithMetadata(t, pattern(40, 30), 1), 40, 30, "image/jpeg"},
		{models.DocumentFormatJPEG, jpegWithMetadata(t, 1), 48, 32, "image/jpeg"},
		{models.DocumentFormatGIF, gifWithMetadata(t), 20, 10, "image/jpeg"},
		{models.DocumentFormatGIF, gifBuf.Bytes(), 30, 20, "image/jpeg"},
		{models.DocumentFormatWebP, withWebPMetadata(t, readTestdata(t, "lossy.webp"), 1), 64, 48, "image/jpeg"},
		{models.DocumentFormatWebP, readTestdata(t, "lossless-alpha.webp"), 64, 48, "image/png"},
		{models.DocumentFormatWebP, withWebPMetadata(t, readTestdata(t, "lossy-alpha.webp"), 1), 64, 48, "image/png"},
		{models.DocumentFormatWebP, readTestdata(t, "animated.webp"), 40, 30, "image/jpeg"},
		{models.DocumentFormatPNG, encodePNG(t, image.NewNRGBA(image.Rect(0, 0, 5, 5))), 5, 5, "image/png"},
	}
	for _, c := range cases {
		p, err := Prepare(c.format, c.data)
		if err != nil {
			t.Fatalf("%s: %v", c.format, err)
		}
		assertNoSecrets(t, p.Data)
		if p.Width != c.width || p.Height != c.height {
			t.Errorf("%s: %d×%d, want %d×%d", c.format, p.Width, p.Height, c.width, c.height)
		}
		if p.ThumbnailType != c.thumbType {
			t.Errorf("%s: thumbnail is %s, want %s", c.format, p.ThumbnailType, c.thumbType)
		}
		// Small pictures keep their size in the thumbnail.
		if b := decodeThumb(t, p).Bounds(); b.Dx() != c.width || b.Dy() != c.height {
			t.Errorf("%s: thumbnail %v, want %d×%d", c.format, b, c.width, c.height)
		}
	}
}

func TestPrepareThumbnailFits(t *testing.T) {
	for _, size := range []image.Point{{1000, 500}, {300, 900}, {2000, 2000}, {4000, 3}} {
		p, err := Prepare(models.DocumentFormatJPEG, encodeJPEG(t, pattern(size.X, size.Y)))
		if err != nil {
			t.Fatal(err)
		}
		b := decodeThumb(t, p).Bounds()
		if max(b.Dx(), b.Dy()) != ThumbnailSize || b.Dx() > ThumbnailSize || b.Dy() > ThumbnailSize {
			t.Errorf("%v: thumbnail is %v", size, b)
		}
		if b.Dy() < 1 {
			t.Errorf("%v: thumbnail has no height", size)
		}
		if p.Width != size.X || p.Height != size.Y {
			t.Errorf("%v: size %d×%d", size, p.Width, p.Height)
		}
	}
}

// near reports whether two colours are within JPEG's error of each other.
func near(a, b color.Color) bool {
	ar, ag, ab, _ := a.RGBA()
	br, bg, bb, _ := b.RGBA()
	d := func(x, y uint32) bool { return x-y < 0x1800 || y-x < 0x1800 }
	return d(ar, br) && d(ag, bg) && d(ab, bb)
}

func TestPrepareAppliesOrientation(t *testing.T) {
	// Turned right: stored 80×40, shown 40×80, with the stored top left
	// (no blue) showing at the top right.
	src := pattern(80, 40)
	p, err := Prepare(models.DocumentFormatJPEG, jpegWithMetadataOf(t, src, 6))
	if err != nil {
		t.Fatal(err)
	}
	if p.Width != 40 || p.Height != 80 {
		t.Fatalf("shown size %d×%d, want 40×80", p.Width, p.Height)
	}
	thumb := decodeThumb(t, p)
	if b := thumb.Bounds(); b.Dx() != 40 || b.Dy() != 80 {
		t.Fatalf("thumbnail %v, want 40×80", b)
	}
	if !near(thumb.At(35, 5), src.At(5, 5)) {
		t.Errorf("thumbnail top right %v, want the stored top left %v", thumb.At(35, 5), src.At(5, 5))
	}
	if !near(thumb.At(35, 75), src.At(75, 5)) {
		t.Errorf("thumbnail bottom right %v, want the stored top right %v", thumb.At(35, 75), src.At(75, 5))
	}
}

func TestOrientEveryWay(t *testing.T) {
	// A 3×2 picture numbered 0..5 row by row, turned each way.
	src := image.NewRGBA(image.Rect(0, 0, 3, 2))
	for i := 0; i < 6; i++ {
		src.Pix[i*4] = uint8(i)
	}
	want := map[int][][]uint8{
		1: {{0, 1, 2}, {3, 4, 5}},
		2: {{2, 1, 0}, {5, 4, 3}},
		3: {{5, 4, 3}, {2, 1, 0}},
		4: {{3, 4, 5}, {0, 1, 2}},
		5: {{0, 3}, {1, 4}, {2, 5}},
		6: {{3, 0}, {4, 1}, {5, 2}},
		7: {{5, 2}, {4, 1}, {3, 0}},
		8: {{2, 5}, {1, 4}, {0, 3}},
	}
	for o, rows := range want {
		got := orient(src, o)
		for y, row := range rows {
			for x, v := range row {
				if g := got.Pix[y*got.Stride+x*4]; g != v {
					t.Errorf("orientation %d: (%d,%d) = %d, want %d", o, x, y, g, v)
				}
			}
		}
	}
}

// jpegWithMetadataOf is jpegWithMetadata for a given picture.
func jpegWithMetadataOf(t *testing.T, img image.Image, orientation int) []byte {
	t.Helper()
	plain := encodeJPEG(t, img)
	var b []byte
	b = append(b, 0xFF, 0xD8)
	b = append(b, appendSegment(nil, 0xE1, append(append([]byte{}, exifHeader...), gpsEXIF(orientation)...))...)
	return append(b, plain[2:]...)
}

func TestDecodesRunAtMostTwoAtATime(t *testing.T) {
	var running, peak atomic.Int32
	testHookDecoding = func() {
		n := running.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		running.Add(-1)
	}
	t.Cleanup(func() { testHookDecoding = nil })

	file := encodePNG(t, pattern(64, 48))
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := Prepare(models.DocumentFormatPNG, file); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if p := peak.Load(); p != MaxConcurrentDecodes {
		t.Errorf("%d decodes ran at once, want at most (and, with 8 waiting, exactly) %d", p, MaxConcurrentDecodes)
	}
}
