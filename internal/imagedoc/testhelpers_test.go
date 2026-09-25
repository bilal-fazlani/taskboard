package imagedoc

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"testing"

	"github.com/tcarac/taskboard/internal/imagedoc/imagedoctest"
)

var (
	gpsEXIF     = imagedoctest.GPSEXIF
	gpsLatitude = imagedoctest.GPSLatitude
	pattern     = imagedoctest.Pattern
)

func assertNoSecrets(t *testing.T, b []byte) {
	t.Helper()
	for _, s := range imagedoctest.Secrets {
		if bytes.Contains(b, []byte(s)) {
			t.Errorf("stripped file still holds %q", s)
		}
	}
}

// assertHasGPS makes sure a test file really carries what stripping must
// remove, so a pass means something.
func assertHasGPS(t *testing.T, b []byte) {
	t.Helper()
	for _, s := range []string{"SECRET-GPS-DATUM", "SECRET-CAMERA", "SECRET-XMP", "SECRET-TRAILER"} {
		if !bytes.Contains(b, []byte(s)) {
			t.Fatalf("the test file lacks %q", s)
		}
	}
	if !bytes.Contains(b, gpsLatitude) {
		t.Fatal("the test file lacks its GPS latitude")
	}
}

// ifd0Tags lists the tags in the first IFD of a TIFF EXIF block.
func ifd0Tags(t *testing.T, tiff []byte) []uint16 {
	t.Helper()
	tiff = bytes.TrimPrefix(tiff, exifHeader)
	var order binary.ByteOrder = binary.BigEndian
	if string(tiff[:2]) == "II" {
		order = binary.LittleEndian
	}
	off := int(order.Uint32(tiff[4:]))
	n := int(order.Uint16(tiff[off:]))
	var tags []uint16
	for i := 0; i < n; i++ {
		tags = append(tags, order.Uint16(tiff[off+2+12*i:]))
	}
	return tags
}

// samePixels fails unless a and b decode to the same colour at every pixel.
func samePixels(t *testing.T, a, b image.Image) {
	t.Helper()
	if a.Bounds() != b.Bounds() {
		t.Fatalf("bounds differ: %v and %v", a.Bounds(), b.Bounds())
	}
	r := a.Bounds()
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if color.NRGBA64Model.Convert(a.At(x, y)) != color.NRGBA64Model.Convert(b.At(x, y)) {
				t.Fatalf("pixel (%d,%d) differs: %v and %v", x, y, a.At(x, y), b.At(x, y))
			}
		}
	}
}

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	return imagedoctest.Fixture(name)
}
