package imagedoc

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/color/palette"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/tcarac/taskboard/internal/models"
)

// jpegSegment is a marker segment with payload.
func jpegSegment(marker byte, payload string) []byte {
	return appendSegment(nil, marker, []byte(payload))
}

// jpegWithMetadata encodes a picture and fills it with what a camera and
// photo software put in a JPEG, before and after the picture.
func jpegWithMetadata(t *testing.T, orientation int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, pattern(48, 32), &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	plain := buf.Bytes()
	var b []byte
	b = append(b, 0xFF, 0xD8)
	// JFIF with a 2×2 thumbnail: 12 bytes of RGB.
	b = append(b, jpegSegment(0xE0, "JFIF\x00\x01\x02\x00\x00\x48\x00\x48\x02\x02SECRET-THUMB")...)
	b = append(b, appendSegment(nil, 0xE1, append(append([]byte{}, exifHeader...), gpsEXIF(orientation)...))...)
	b = append(b, jpegSegment(0xE1, "http://ns.adobe.com/xap/1.0/\x00<x:xmpmeta>SECRET-XMP</x:xmpmeta>")...)
	b = append(b, jpegSegment(0xE2, "ICC_PROFILE\x00\x01\x01fake-profile")...)
	b = append(b, jpegSegment(0xE2, "MPF\x00SECRET-PRIVATE")...)
	b = append(b, jpegSegment(0xED, "Photoshop 3.0\x008BIM\x04\x04SECRET-IPTC")...)
	b = append(b, jpegSegment(0xEE, "Adobe\x00\x64\x00\x00\x00\x00\x01")...)
	b = append(b, jpegSegment(0xFE, "SECRET-COMMENT")...)
	b = append(b, plain[2:]...)
	b = append(b, "SECRET-TRAILER"...)
	return b
}

func TestStripJPEG(t *testing.T) {
	orig := jpegWithMetadata(t, 6)
	assertHasGPS(t, orig)
	out, orientation, err := stripJPEG(orig)
	if err != nil {
		t.Fatal(err)
	}
	assertNoSecrets(t, out)
	if bytes.Contains(out, gpsLatitude) {
		t.Error("the GPS latitude survived")
	}
	for _, kept := range []string{"ICC_PROFILE\x00\x01\x01fake-profile", "Adobe\x00\x64"} {
		if !bytes.Contains(out, []byte(kept)) {
			t.Errorf("lost %q, which showing the picture needs", kept)
		}
	}
	if orientation != 6 {
		t.Errorf("orientation = %d, want 6", orientation)
	}
	i := bytes.Index(out, exifHeader)
	if i < 0 {
		t.Fatal("no EXIF left to hold the orientation")
	}
	if tags := ifd0Tags(t, out[i:]); len(tags) != 1 || tags[0] != 0x0112 {
		t.Errorf("EXIF keeps tags %x, want only Orientation (112)", tags)
	}
	if !bytes.HasSuffix(out, []byte{0xFF, 0xD9}) {
		t.Error("output does not end at EOI")
	}
	// The JFIF header stays, with no thumbnail.
	if !bytes.Contains(out, []byte("JFIF\x00\x01\x02\x00\x00\x48\x00\x48\x00\x00")) {
		t.Error("JFIF header lost or still announcing a thumbnail")
	}

	a, err := jpeg.Decode(bytes.NewReader(orig))
	if err != nil {
		t.Fatal(err)
	}
	b, err := jpeg.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	samePixels(t, a, b)
}

func TestStripJPEGDropsEXIFWhenUpright(t *testing.T) {
	out, orientation, err := stripJPEG(jpegWithMetadata(t, 1))
	if err != nil {
		t.Fatal(err)
	}
	if orientation != 1 || bytes.Contains(out, exifHeader) {
		t.Errorf("orientation %d, EXIF kept %v; want 1 and none", orientation, bytes.Contains(out, exifHeader))
	}
	out, orientation, err = stripJPEG(jpegWithMetadata(t, 0))
	if err != nil {
		t.Fatal(err)
	}
	if orientation != 1 || bytes.Contains(out, exifHeader) {
		t.Errorf("with no orientation tag: orientation %d, EXIF kept %v", orientation, bytes.Contains(out, exifHeader))
	}
}

func TestStripProgressiveJPEG(t *testing.T) {
	orig := readTestdata(t, "progressive.jpg")
	out, _, err := stripJPEG(orig)
	if err != nil {
		t.Fatal(err)
	}
	assertNoSecrets(t, out)
	if len(out) != len(orig)-len(jpegSegment(0xFE, "SECRET-COMMENT")) {
		t.Errorf("stripped %d bytes, want just the comment segment", len(orig)-len(out))
	}
	a, _ := jpeg.Decode(bytes.NewReader(orig))
	b, err := jpeg.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	samePixels(t, a, b)
}

func pngChunk(typ, data string) []byte {
	return appendPNGChunk(nil, typ, []byte(data))
}

func pngWithMetadata(t *testing.T, img image.Image, orientation int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	plain := buf.Bytes()
	ihdrEnd := 8 + 12 + 13
	var b []byte
	b = append(b, plain[:ihdrEnd]...)
	b = append(b, pngChunk("gAMA", "\x00\x00\xb1\x8f")...)
	b = append(b, pngChunk("tEXt", "Comment\x00SECRET-TEXT")...)
	b = append(b, pngChunk("zTXt", "Author\x00\x00SECRET-TEXT")...)
	b = append(b, pngChunk("iTXt", "XML:com.adobe.xmp\x00\x00\x00\x00\x00<x:xmpmeta>SECRET-XMP</x:xmpmeta>")...)
	b = append(b, pngChunk("tIME", "\x07\xea\x09\x19\x0c\x00\x00")...)
	b = append(b, appendPNGChunk(nil, "eXIf", gpsEXIF(orientation))...)
	b = append(b, pngChunk("prVt", "SECRET-PRIVATE")...)
	b = append(b, plain[ihdrEnd:]...)
	b = append(b, "SECRET-TRAILER"...)
	return b
}

func TestStripPNG(t *testing.T) {
	orig := pngWithMetadata(t, pattern(40, 30), 8)
	assertHasGPS(t, orig)
	out, orientation, err := stripPNG(orig)
	if err != nil {
		t.Fatal(err)
	}
	assertNoSecrets(t, out)
	if bytes.Contains(out, gpsLatitude) {
		t.Error("the GPS latitude survived")
	}
	if !bytes.Contains(out, pngChunk("gAMA", "\x00\x00\xb1\x8f")) {
		t.Error("gAMA, which changes how the picture shows, was removed")
	}
	if orientation != 8 {
		t.Errorf("orientation = %d, want 8", orientation)
	}
	i := bytes.Index(out, []byte("eXIf"))
	if i < 0 {
		t.Fatal("no eXIf left to hold the orientation")
	}
	n := int(binary.BigEndian.Uint32(out[i-4:]))
	exif := out[i+4 : i+4+n]
	if tags := ifd0Tags(t, exif); len(tags) != 1 || tags[0] != 0x0112 {
		t.Errorf("eXIf keeps tags %x, want only Orientation", tags)
	}
	if crc := binary.BigEndian.Uint32(out[i+4+n:]); crc != crc32.ChecksumIEEE(out[i:i+4+n]) {
		t.Error("the new eXIf chunk's CRC is wrong")
	}

	a, err := png.Decode(bytes.NewReader(orig))
	if err != nil {
		t.Fatal(err)
	}
	b, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	samePixels(t, a, b)

	out, orientation, err = stripPNG(pngWithMetadata(t, pattern(40, 30), 1))
	if err != nil || orientation != 1 || bytes.Contains(out, []byte("eXIf")) {
		t.Errorf("upright: orientation %d, eXIf kept %v, err %v", orientation, bytes.Contains(out, []byte("eXIf")), err)
	}
}

func gifWithMetadata(t *testing.T) []byte {
	t.Helper()
	frames := []*image.Paletted{image.NewPaletted(image.Rect(0, 0, 20, 10), palette.Plan9), image.NewPaletted(image.Rect(5, 2, 15, 8), palette.Plan9)}
	for y := 0; y < 10; y++ {
		for x := 0; x < 20; x++ {
			frames[0].Set(x, y, color.RGBA{uint8(x * 12), uint8(y * 25), 60, 255})
			frames[1].Set(x, y, color.RGBA{200, uint8(x * 12), uint8(y * 25), 255})
		}
	}
	var buf bytes.Buffer
	err := gif.EncodeAll(&buf, &gif.GIF{Image: frames, Delay: []int{10, 20}, LoopCount: 0})
	if err != nil {
		t.Fatal(err)
	}
	plain := buf.Bytes()
	// Metadata goes before the first frame's graphic control extension:
	// after the header, screen descriptor, global table and NETSCAPE block.
	i := bytes.Index(plain, []byte{0x21, 0xF9})
	var b []byte
	b = append(b, plain[:i]...)
	b = append(b, 0x21, 0xFE, 14)
	b = append(b, "SECRET-COMMENT"...)
	b = append(b, 0)
	b = append(b, 0x21, 0xFF, 11)
	b = append(b, "XMP DataXMP"...)
	b = append(b, 10)
	b = append(b, "SECRET-XMP"...)
	b = append(b, 0)
	b = append(b, plain[i:]...)
	b = append(b, "SECRET-TRAILER"...)
	return b
}

func TestStripGIF(t *testing.T) {
	orig := gifWithMetadata(t)
	for _, s := range []string{"SECRET-COMMENT", "SECRET-XMP", "SECRET-TRAILER"} {
		if !bytes.Contains(orig, []byte(s)) {
			t.Fatalf("the test file lacks %q", s)
		}
	}
	out, err := stripGIF(orig)
	if err != nil {
		t.Fatal(err)
	}
	assertNoSecrets(t, out)
	if !bytes.Contains(out, []byte("NETSCAPE2.0")) {
		t.Error("the looping block was removed")
	}
	a, err := gif.DecodeAll(bytes.NewReader(orig))
	if err != nil {
		t.Fatal(err)
	}
	b, err := gif.DecodeAll(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Image) != 2 || len(b.Image) != 2 || b.LoopCount != a.LoopCount || b.Delay[1] != 20 {
		t.Fatalf("animation changed: %d frames, loop %d, delays %v", len(b.Image), b.LoopCount, b.Delay)
	}
	for i := range a.Image {
		samePixels(t, a.Image[i], b.Image[i])
	}
}

// withWebPMetadata turns a WebP test file into an extended one carrying
// EXIF (with GPS and the orientation), XMP and a private chunk, as camera
// software writes them.
func withWebPMetadata(t *testing.T, file []byte, orientation int) []byte {
	t.Helper()
	chunks, err := readRIFF(file)
	if err != nil {
		t.Fatal(err)
	}
	if chunks[0].id != "VP8X" {
		img, _, err := DecodeWebPFrame(file)
		if err != nil {
			t.Fatal(err)
		}
		header := make([]byte, 10)
		if chunks[0].id == "VP8L" && !isOpaque(img) {
			header[0] |= vp8xAlpha
		}
		putLE24(header[4:], uint32(img.Bounds().Dx()-1))
		putLE24(header[7:], uint32(img.Bounds().Dy()-1))
		chunks = append([]riffChunk{{id: "VP8X", data: header}}, chunks...)
	}
	header := append([]byte{}, chunks[0].data...)
	header[0] |= vp8xEXIF | vp8xXMP
	chunks[0].data = header
	chunks = append(chunks,
		riffChunk{id: "EXIF", data: gpsEXIF(orientation)},
		riffChunk{id: "XMP ", data: []byte("<x:xmpmeta>SECRET-XMP</x:xmpmeta>")},
		riffChunk{id: "prVt", data: []byte("SECRET-PRIVATE")},
	)
	return append(writeRIFF(chunks), "SECRET-TRAILER"...)
}

func isOpaque(img image.Image) bool {
	o, ok := img.(interface{ Opaque() bool })
	return ok && o.Opaque()
}

func TestStripWebP(t *testing.T) {
	for _, name := range []string{"lossy.webp", "lossless-alpha.webp", "lossy-alpha.webp", "animated.webp"} {
		t.Run(name, func(t *testing.T) {
			plain := readTestdata(t, name)
			orig := withWebPMetadata(t, plain, 3)
			assertHasGPS(t, orig)
			out, orientation, err := stripWebP(orig)
			if err != nil {
				t.Fatal(err)
			}
			assertNoSecrets(t, out)
			if bytes.Contains(out, gpsLatitude) {
				t.Error("the GPS latitude survived")
			}
			if orientation != 3 {
				t.Errorf("orientation = %d, want 3", orientation)
			}
			chunks, err := readRIFF(out)
			if err != nil {
				t.Fatal(err)
			}
			if flags := chunks[0].data[0]; flags&vp8xXMP != 0 || flags&vp8xEXIF == 0 {
				t.Errorf("VP8X flags %08b: want EXIF set and XMP clear", flags)
			}
			for _, c := range chunks {
				if c.id == "EXIF" {
					if tags := ifd0Tags(t, c.data); len(tags) != 1 || tags[0] != 0x0112 {
						t.Errorf("EXIF keeps tags %x", tags)
					}
				}
			}
			// Every picture chunk is copied byte for byte.
			origChunks, _ := readRIFF(orig)
			for _, c := range origChunks {
				if webpKeep[c.id] && c.id != "VP8X" && !bytes.Contains(out, c.data) {
					t.Errorf("chunk %q changed", c.id)
				}
			}

			a, _, err := DecodeWebPFrame(plain)
			if err != nil {
				t.Fatal(err)
			}
			b, _, err := DecodeWebPFrame(out)
			if err != nil {
				t.Fatal(err)
			}
			samePixels(t, a, b)

			upright, orientation, err := stripWebP(withWebPMetadata(t, plain, 1))
			if err != nil || orientation != 1 || bytes.Contains(upright, []byte("EXIF")) {
				t.Errorf("upright: orientation %d, EXIF kept %v, err %v", orientation, bytes.Contains(upright, []byte("EXIF")), err)
			}
			if chunks, _ := readRIFF(upright); chunks[0].data[0]&vp8xEXIF != 0 {
				t.Error("VP8X still announces EXIF")
			}
		})
	}
}

func TestStripSimpleWebPUnchanged(t *testing.T) {
	plain := readTestdata(t, "lossy.webp")
	out, err := Strip(models.DocumentFormatWebP, plain)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, plain) {
		t.Error("a WebP with no metadata changed")
	}
}

func TestStripRefusesMalformedFiles(t *testing.T) {
	cases := map[string][]byte{
		models.DocumentFormatPNG:  append([]byte{}, pngSignature...),
		models.DocumentFormatJPEG: {0xFF, 0xD8, 0xFF, 0xE1, 0xFF, 0xFF},
		models.DocumentFormatGIF:  []byte("GIF89a\x01\x00\x01\x00\x80\x00\x00"),
		models.DocumentFormatWebP: []byte("RIFF\xff\x00\x00\x00WEBPVP8 "),
	}
	for format, b := range cases {
		if _, err := Strip(format, b); err == nil {
			t.Errorf("%s: a truncated file was taken", format)
		}
	}
}
