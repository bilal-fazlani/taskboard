package imagedoc

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"

	"github.com/tcarac/taskboard/internal/models"
)

// Agents are handed a scaled-down copy of a large image rather than the file
// (Bilal 2026-09-25): model APIs refuse images over a few megabytes, and
// some MCP clients cap a tool result near 1 MB.
const (
	// PreviewSide is the long side a preview fits, the largest a model
	// looks at without scaling it down itself.
	PreviewSide = 1568
	// PreviewTrigger is the file size over which an image is previewed
	// even when it already fits PreviewSide.
	PreviewTrigger = 3584 << 10 // 3.5 MB
	// PreviewBudget is the most a preview may take, so it stays under 1 MB
	// once base64-encoded.
	PreviewBudget = 750_000
)

// NeedsPreview reports whether an image of these details goes to an agent
// as a scaled-down copy: over PreviewSide on its long side, or over
// PreviewTrigger in bytes.
func NeedsPreview(width, height, size int) bool {
	return max(width, height) > PreviewSide || size > PreviewTrigger
}

// Preview is a scaled-down copy of an image, for an agent to look at.
type Preview struct {
	Data          []byte
	ContentType   string
	Width, Height int
}

// MakePreview decodes a stored image (its first frame when animated), turns
// it to its orientation and scales it to fit PreviewSide, encoded within
// PreviewBudget: JPEG when opaque, PNG when it has transparency, like the
// thumbnails. When that is still too large it steps down: lower JPEG
// quality, then smaller sizes, and in the end JPEG on white for a
// transparent picture. The stored file is never touched.
func MakePreview(format string, data []byte) (*Preview, error) {
	if !models.IsImageFormat(format) {
		return nil, errors.New("imagedoc: not an image format")
	}
	// The stored file is already stripped; stripping again only reads the
	// orientation it kept.
	_, orientation, err := strip(format, data)
	if err != nil {
		return nil, err
	}
	cfg, err := decodeConfig(format, data)
	if err != nil {
		return nil, err
	}
	return withDecodeSlot(func() (*Preview, error) {
		img, err := decodeFirstFrame(format, data, cfg)
		if err != nil {
			return nil, err
		}
		for _, side := range []int{PreviewSide, 1280, 1024, 768, 512} {
			pic := orient(scaleToFit(img, side), orientation)
			if !pic.Opaque() {
				if p, err := encodeWithin(pic, "image/png", 0); p != nil || err != nil {
					return p, err
				}
				pic = onWhite(pic)
			}
			for _, quality := range []int{85, 70, 55} {
				if p, err := encodeWithin(pic, "image/jpeg", quality); p != nil || err != nil {
					return p, err
				}
			}
		}
		return nil, errors.New("imagedoc: no preview fits the budget")
	})
}

// encodeWithin encodes pic, and returns nil (with no error) when the result
// is over PreviewBudget.
func encodeWithin(pic *image.RGBA, contentType string, quality int) (*Preview, error) {
	var buf bytes.Buffer
	var err error
	if contentType == "image/png" {
		err = png.Encode(&buf, pic)
	} else {
		err = jpeg.Encode(&buf, pic, &jpeg.Options{Quality: quality})
	}
	if err != nil {
		return nil, err
	}
	if buf.Len() > PreviewBudget {
		return nil, nil
	}
	b := pic.Bounds()
	return &Preview{Data: buf.Bytes(), ContentType: contentType, Width: b.Dx(), Height: b.Dy()}, nil
}

// onWhite lays a picture with transparency on white, for JPEG.
func onWhite(pic *image.RGBA) *image.RGBA {
	out := image.NewRGBA(pic.Bounds())
	draw.Draw(out, out.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(out, out.Bounds(), pic, pic.Bounds().Min, draw.Over)
	return out
}
