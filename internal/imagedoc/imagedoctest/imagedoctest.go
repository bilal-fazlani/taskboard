// Package imagedoctest makes image files for tests: pictures in each format
// carrying the metadata a camera or photo software writes, which the store
// must remove. Only tests import it.
package imagedoctest

import (
	"bytes"
	"embed"
	"encoding/binary"
	"image"
	"image/color"
	"image/color/palette"
	"image/gif"
	"image/jpeg"
	"image/png"
)

// fixtures are WebP files made with libwebp's cwebp and img2webp, since Go
// has no WebP encoder, and a progressive JPEG made with ImageMagick (with a
// "SECRET-COMMENT" comment), since Go writes baseline JPEGs only.
//
//go:embed fixtures
var fixtures embed.FS

// Fixture returns one of the fixture files: lossy.webp (64×48, VP8),
// lossless-alpha.webp (64×48, VP8L with transparency), lossy-alpha.webp
// (64×48, VP8X with ALPH and VP8), animated.webp (40×30, two frames) and
// progressive.jpg (64×48).
func Fixture(name string) []byte {
	b, err := fixtures.ReadFile("fixtures/" + name)
	if err != nil {
		panic(err)
	}
	return b
}

// Secrets are the markers the metadata of these files holds. None may
// survive stripping.
var Secrets = []string{
	"SECRET-GPS-DATUM", "SECRET-CAMERA", "SECRET-XMP", "SECRET-IPTC", "SECRET-COMMENT",
	"SECRET-TEXT", "SECRET-TRAILER", "SECRET-PRIVATE", "SECRET-THUMB",
}

// GPSLatitude is 51° 30' 26" as three big-endian RATIONALs, as GPSEXIF
// writes it.
var GPSLatitude = []byte{0, 0, 0, 51, 0, 0, 0, 1, 0, 0, 0, 30, 0, 0, 0, 1, 0, 0, 0, 26, 0, 0, 0, 1}

// HasSecrets reports whether b holds any of the metadata markers or the GPS
// latitude.
func HasSecrets(b []byte) (string, bool) {
	for _, s := range Secrets {
		if bytes.Contains(b, []byte(s)) {
			return s, true
		}
	}
	if bytes.Contains(b, GPSLatitude) {
		return "GPS latitude", true
	}
	return "", false
}

// GPSEXIF builds a big-endian TIFF EXIF block like a phone camera writes:
// IFD0 with Make, Orientation (when orientation > 0) and a pointer to a GPS
// IFD holding the latitude and a map datum.
func GPSEXIF(orientation int) []byte {
	be := binary.BigEndian
	entry := func(tag, typ uint16, count uint32, value []byte) []byte {
		e := make([]byte, 12)
		be.PutUint16(e[0:], tag)
		be.PutUint16(e[2:], typ)
		be.PutUint32(e[4:], count)
		copy(e[8:], value)
		return e
	}
	camera := []byte("SECRET-CAMERA\x00")
	datum := []byte("SECRET-GPS-DATUM\x00")

	n := 2
	if orientation > 0 {
		n = 3
	}
	ifd0Size := 2 + 12*n + 4
	cameraOff := 8 + ifd0Size
	gpsOff := cameraOff + len(camera)
	gpsSize := 2 + 12*3 + 4
	latOff := gpsOff + gpsSize
	datumOff := latOff + len(GPSLatitude)

	u32 := func(v int) []byte { b := make([]byte, 4); be.PutUint32(b, uint32(v)); return b }
	out := []byte{'M', 'M', 0, 42, 0, 0, 0, 8}
	out = be.AppendUint16(out, uint16(n))
	out = append(out, entry(0x010F, 2, uint32(len(camera)), u32(cameraOff))...)
	if orientation > 0 {
		out = append(out, entry(0x0112, 3, 1, []byte{0, byte(orientation)})...)
	}
	out = append(out, entry(0x8825, 4, 1, u32(gpsOff))...)
	out = be.AppendUint32(out, 0)
	out = append(out, camera...)
	out = be.AppendUint16(out, 3)
	out = append(out, entry(0x0001, 2, 2, []byte("N\x00"))...)
	out = append(out, entry(0x0002, 5, 3, u32(latOff))...)
	out = append(out, entry(0x0012, 2, uint32(len(datum)), u32(datumOff))...)
	out = be.AppendUint32(out, 0)
	out = append(out, GPSLatitude...)
	out = append(out, datum...)
	return out
}

// Pattern is an opaque picture with a different colour in each quadrant, so
// a turn shows in where the colours land.
func Pattern(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.NRGBA{uint8(x * 255 / w), uint8(y * 255 / h), 128, 255}
			switch {
			case x < w/2 && y < h/2:
				c.B = 0 // top left
			case x >= w/2 && y < h/2:
				c.B = 255 // top right
			}
			img.Set(x, y, c)
		}
	}
	return img
}

// PNG encodes Pattern(w, h).
func PNG(w, h int) []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, Pattern(w, h)); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// JPEGWithGPS encodes Pattern(w, h) as a JPEG with an EXIF block holding
// GPS, a camera make and the orientation (none when 0), and a comment.
func JPEGWithGPS(w, h, orientation int) []byte {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, Pattern(w, h), &jpeg.Options{Quality: 90}); err != nil {
		panic(err)
	}
	plain := buf.Bytes()
	exif := append([]byte("Exif\x00\x00"), GPSEXIF(orientation)...)
	var b []byte
	b = append(b, 0xFF, 0xD8)
	b = appendJPEGSegment(b, 0xE1, exif)
	b = appendJPEGSegment(b, 0xFE, []byte("SECRET-COMMENT"))
	return append(b, plain[2:]...)
}

func appendJPEGSegment(out []byte, marker byte, payload []byte) []byte {
	n := len(payload) + 2
	out = append(out, 0xFF, marker, byte(n>>8), byte(n))
	return append(out, payload...)
}

// AnimatedGIF is a two-frame, looping 20×10 GIF with a comment.
func AnimatedGIF() []byte {
	frames := []*image.Paletted{
		image.NewPaletted(image.Rect(0, 0, 20, 10), palette.Plan9),
		image.NewPaletted(image.Rect(5, 2, 15, 8), palette.Plan9),
	}
	for y := 0; y < 10; y++ {
		for x := 0; x < 20; x++ {
			frames[0].Set(x, y, color.RGBA{uint8(x * 12), uint8(y * 25), 60, 255})
			frames[1].Set(x, y, color.RGBA{200, uint8(x * 12), uint8(y * 25), 255})
		}
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, &gif.GIF{Image: frames, Delay: []int{10, 20}, LoopCount: 0}); err != nil {
		panic(err)
	}
	plain := buf.Bytes()
	i := bytes.Index(plain, []byte{0x21, 0xF9})
	var b []byte
	b = append(b, plain[:i]...)
	b = append(b, 0x21, 0xFE, 14)
	b = append(b, "SECRET-COMMENT"...)
	b = append(b, 0)
	return append(b, plain[i:]...)
}

// WebPWithGPS is lossy.webp made an extended WebP carrying EXIF with GPS
// and the orientation, and XMP.
func WebPWithGPS(orientation int) []byte {
	plain := Fixture("lossy.webp")
	vp8 := plain[12:] // the VP8 chunk, header included
	header := []byte{0x08 | 0x04, 0, 0, 0, 63, 0, 0, 47, 0, 0}
	var body []byte
	body = appendRIFFChunk(body, "VP8X", header)
	body = append(body, vp8...)
	body = appendRIFFChunk(body, "EXIF", GPSEXIF(orientation))
	body = appendRIFFChunk(body, "XMP ", []byte("<x:xmpmeta>SECRET-XMP</x:xmpmeta>"))
	out := []byte("RIFF")
	out = binary.LittleEndian.AppendUint32(out, uint32(4+len(body)))
	out = append(out, "WEBP"...)
	return append(out, body...)
}

func appendRIFFChunk(out []byte, id string, data []byte) []byte {
	out = append(out, id...)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(data)))
	out = append(out, data...)
	if len(data)&1 == 1 {
		out = append(out, 0)
	}
	return out
}
