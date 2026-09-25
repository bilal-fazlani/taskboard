// Package imagedoc checks and prepares the images attached as documents.
// Prepare makes sure a file really is the format it is named as, by its
// content, removes its metadata without re-encoding the picture, and makes a
// small thumbnail.
//
// Metadata goes at the container level: the segments, chunks and extension
// blocks that hold it are left out and every other byte is copied as it was,
// so the picture decodes to exactly the same pixels. One EXIF tag stays:
// Orientation, when it says the picture is turned (anything but 1). Removing
// it would show a phone photo on its side, and applying it would mean
// re-encoding, so the file keeps a minimal EXIF block holding that tag alone.
// Width, Height and the thumbnail are as the picture is shown, with the
// orientation applied.
package imagedoc

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"strings"

	"github.com/tcarac/taskboard/internal/models"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/webp"
)

// MaxPixels bounds the pictures Prepare decodes, so a small file that claims
// an enormous size cannot exhaust memory. 50 megapixels holds the largest
// phone camera photos (48 MP).
const MaxPixels = 50_000_000

// ThumbnailSize is the box a thumbnail fits in, in pixels.
const ThumbnailSize = 320

// Refusal is a file Prepare will not take. Its message is written for people
// and is the whole error, like the store's own rule messages.
type Refusal struct{ Msg string }

func (e *Refusal) Error() string { return e.Msg }

func refuse(format string, args ...any) error {
	return &Refusal{Msg: fmt.Sprintf(format, args...)}
}

// Prepared is an image ready to store.
type Prepared struct {
	// Data is the file with its metadata removed: what is stored and
	// downloaded.
	Data []byte
	// Width and Height are in pixels, as shown (orientation applied).
	Width, Height int
	// Thumbnail fits in ThumbnailSize×ThumbnailSize and is never larger than
	// the picture. It is a JPEG when the picture is opaque and a PNG when it
	// has transparency. ThumbnailType is its Content-Type.
	Thumbnail     []byte
	ThumbnailType string
}

// Sniff names the image format data holds by its first bytes: one of the
// image formats, "svg" for an SVG (or other XML) file, or "".
func Sniff(data []byte) string {
	switch {
	case bytes.HasPrefix(data, pngSignature):
		return models.DocumentFormatPNG
	case bytes.HasPrefix(data, []byte{0xFF, 0xD8, 0xFF}):
		return models.DocumentFormatJPEG
	case bytes.HasPrefix(data, []byte("GIF87a")), bytes.HasPrefix(data, []byte("GIF89a")):
		return models.DocumentFormatGIF
	case len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return models.DocumentFormatWebP
	}
	head := data
	if len(head) > 1024 {
		head = head[:1024]
	}
	text := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(string(head), "\xef\xbb\xbf")))
	if strings.HasPrefix(text, "<?xml") || strings.HasPrefix(text, "<svg") || strings.Contains(text, "<svg") {
		return "svg"
	}
	return ""
}

// Prepare checks that data is an image of format, removes its metadata and
// makes its thumbnail. Every file it will not take is a *Refusal.
func Prepare(format string, data []byte) (*Prepared, error) {
	if !models.IsImageFormat(format) {
		return nil, fmt.Errorf("imagedoc: %q is not an image format", format)
	}
	name := models.ImageFormatName(format)
	switch sniffed := Sniff(data); {
	case sniffed == format:
	case sniffed == "svg":
		return nil, refuse("This isn't a %s image: it's an SVG, and SVG images can't be attached.", name)
	case sniffed != "":
		return nil, refuse("This isn't a %s image: its content is %s.", name, models.ImageFormatName(sniffed))
	default:
		return nil, refuse("This isn't a %s image: its content is not PNG, JPEG, GIF or WebP.", name)
	}

	damaged := refuse("This %s image can't be read. It may be damaged.", name)
	stripped, orientation, err := strip(format, data)
	if err != nil {
		return nil, damaged
	}

	cfg, err := decodeConfig(format, stripped)
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, damaged
	}
	if cfg.Width*cfg.Height > MaxPixels {
		return nil, refuse("This image is %d×%d pixels. The limit is 50 megapixels.", cfg.Width, cfg.Height)
	}
	img, err := decodeFirstFrame(format, stripped, cfg)
	if err != nil {
		return nil, damaged
	}

	thumb, thumbType, err := thumbnail(img, orientation)
	if err != nil {
		return nil, fmt.Errorf("making thumbnail: %w", err)
	}
	width, height := cfg.Width, cfg.Height
	if orientation >= 5 {
		width, height = height, width
	}
	return &Prepared{Data: stripped, Width: width, Height: height, Thumbnail: thumb, ThumbnailType: thumbType}, nil
}

// strip removes a file's metadata; see the package comment. orientation is
// the EXIF orientation the file keeps, 1 when it has none.
func strip(format string, data []byte) (out []byte, orientation int, err error) {
	switch format {
	case models.DocumentFormatPNG:
		return stripPNG(data)
	case models.DocumentFormatJPEG:
		return stripJPEG(data)
	case models.DocumentFormatGIF:
		out, err := stripGIF(data)
		return out, 1, err
	case models.DocumentFormatWebP:
		return stripWebP(data)
	}
	return nil, 0, errors.New("unknown format")
}

// Strip removes a file's metadata as Prepare does, without the other checks.
// Tests compare its output with the original.
func Strip(format string, data []byte) ([]byte, error) {
	out, _, err := strip(format, data)
	return out, err
}

func decodeConfig(format string, data []byte) (image.Config, error) {
	r := bytes.NewReader(data)
	switch format {
	case models.DocumentFormatPNG:
		return png.DecodeConfig(r)
	case models.DocumentFormatJPEG:
		return jpeg.DecodeConfig(r)
	case models.DocumentFormatGIF:
		return gif.DecodeConfig(r)
	case models.DocumentFormatWebP:
		return webp.DecodeConfig(r)
	}
	return image.Config{}, errors.New("unknown format")
}

// decodeFirstFrame decodes the picture, or the first frame of an animation
// laid on its canvas.
func decodeFirstFrame(format string, data []byte, cfg image.Config) (image.Image, error) {
	r := bytes.NewReader(data)
	switch format {
	case models.DocumentFormatPNG:
		return png.Decode(r)
	case models.DocumentFormatJPEG:
		return jpeg.Decode(r)
	case models.DocumentFormatGIF:
		// gif.Decode stops after the first frame, which may cover only part
		// of the canvas.
		frame, err := gif.Decode(r)
		if err != nil {
			return nil, err
		}
		return onCanvas(frame, image.Point{}, cfg), nil
	case models.DocumentFormatWebP:
		frame, offset, err := DecodeWebPFrame(data)
		if err != nil {
			return nil, err
		}
		return onCanvas(frame, offset, cfg), nil
	}
	return nil, errors.New("unknown format")
}

// onCanvas lays a frame on a transparent canvas of cfg's size, at offset,
// unless it already covers the canvas.
func onCanvas(frame image.Image, offset image.Point, cfg image.Config) image.Image {
	canvas := image.Rect(0, 0, cfg.Width, cfg.Height)
	b := frame.Bounds()
	if b.Add(offset) == canvas {
		return frame
	}
	dst := image.NewNRGBA(canvas)
	draw.Draw(dst, b.Add(offset), frame, b.Min, draw.Src)
	return dst
}

// thumbnail scales img to fit ThumbnailSize (never up), turns it to its
// orientation and encodes it.
func thumbnail(img image.Image, orientation int) ([]byte, string, error) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	tw, th := w, h
	if w > ThumbnailSize || h > ThumbnailSize {
		if w >= h {
			tw, th = ThumbnailSize, max(1, (h*ThumbnailSize+w/2)/w)
		} else {
			tw, th = max(1, (w*ThumbnailSize+h/2)/h), ThumbnailSize
		}
	}
	scaled := image.NewRGBA(image.Rect(0, 0, tw, th))
	if tw == w && th == h {
		draw.Draw(scaled, scaled.Bounds(), img, b.Min, draw.Src)
	} else {
		// A box pass first, when the picture is much larger, keeps the
		// resampling below fast; CatmullRom then scales the rest.
		src := boxShrink(img, tw, th)
		xdraw.CatmullRom.Scale(scaled, scaled.Bounds(), src, src.Bounds(), xdraw.Src, nil)
	}
	oriented := orient(scaled, orientation)

	var buf bytes.Buffer
	if oriented.Opaque() {
		if err := jpeg.Encode(&buf, oriented, &jpeg.Options{Quality: 85}); err != nil {
			return nil, "", err
		}
		return buf.Bytes(), "image/jpeg", nil
	}
	if err := png.Encode(&buf, oriented); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), "image/png", nil
}

// boxShrink averages img down by a whole factor, to no less than twice the
// target size, so the final resampling reads few pixels. It returns img as it
// is when that factor would be 1.
func boxShrink(img image.Image, tw, th int) image.Image {
	b := img.Bounds()
	f := min(b.Dx()/(2*tw), b.Dy()/(2*th))
	if f < 2 {
		return img
	}
	src := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(src, src.Bounds(), img, b.Min, draw.Src)
	w, h := b.Dx()/f, b.Dy()/f
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	n := uint32(f * f)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var r, g, bl, a uint32
			for yy := y * f; yy < (y+1)*f; yy++ {
				row := src.Pix[yy*src.Stride+x*f*4 : yy*src.Stride+(x+1)*f*4]
				for i := 0; i < len(row); i += 4 {
					r += uint32(row[i])
					g += uint32(row[i+1])
					bl += uint32(row[i+2])
					a += uint32(row[i+3])
				}
			}
			o := y*dst.Stride + x*4
			dst.Pix[o], dst.Pix[o+1], dst.Pix[o+2], dst.Pix[o+3] = uint8(r/n), uint8(g/n), uint8(bl/n), uint8(a/n)
		}
	}
	return dst
}

// orient turns img the way an EXIF orientation says the picture is shown.
func orient(img *image.RGBA, orientation int) *image.RGBA {
	if orientation < 2 || orientation > 8 {
		return img
	}
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	dw, dh := w, h
	if orientation >= 5 {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < dh; y++ {
		for x := 0; x < dw; x++ {
			var sx, sy int
			switch orientation {
			case 2: // mirrored
				sx, sy = w-1-x, y
			case 3: // upside down
				sx, sy = w-1-x, h-1-y
			case 4: // mirrored upside down
				sx, sy = x, h-1-y
			case 5: // mirrored, turned left
				sx, sy = y, x
			case 6: // turned right (90° clockwise to show)
				sx, sy = y, h-1-x
			case 7: // mirrored, turned right
				sx, sy = w-1-y, h-1-x
			case 8: // turned left (90° anticlockwise to show)
				sx, sy = w-1-y, x
			}
			copy(dst.Pix[y*dst.Stride+x*4:y*dst.Stride+x*4+4], img.Pix[sy*img.Stride+sx*4:sy*img.Stride+sx*4+4])
		}
	}
	return dst
}
