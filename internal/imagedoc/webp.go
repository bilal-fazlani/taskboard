package imagedoc

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"

	"golang.org/x/image/webp"
)

var errWebP = errors.New("malformed WebP")

// VP8X flags.
const (
	vp8xAnimation = 1 << 1
	vp8xXMP       = 1 << 2
	vp8xEXIF      = 1 << 3
	vp8xAlpha     = 1 << 4
)

// webpKeep are the chunks a WebP keeps: the picture itself (VP8, VP8L and
// its ALPH transparency), the extended header, animation, and the ICC colour
// profile. EXIF is handled on its own (see stripWebP); XMP and unknown
// chunks go.
var webpKeep = map[string]bool{
	"VP8 ": true, "VP8L": true, "VP8X": true, "ALPH": true, "ANIM": true, "ANMF": true, "ICCP": true,
}

type riffChunk struct {
	id   string
	data []byte
}

// readRIFF splits a WebP file into its top-level chunks. Anything after the
// size the RIFF header gives is ignored.
func readRIFF(b []byte) ([]riffChunk, error) {
	if len(b) < 12 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WEBP" {
		return nil, errWebP
	}
	size := int(binary.LittleEndian.Uint32(b[4:]))
	if size < 4 || size > len(b)-8 {
		return nil, errWebP
	}
	return readChunks(b[12 : 8+size])
}

// readChunks splits a run of RIFF chunks, each padded to an even length.
func readChunks(b []byte) ([]riffChunk, error) {
	var chunks []riffChunk
	for i := 0; i < len(b); {
		if i+8 > len(b) {
			return nil, errWebP
		}
		n := int(binary.LittleEndian.Uint32(b[i+4:]))
		if n < 0 || n > len(b)-i-8 {
			return nil, errWebP
		}
		chunks = append(chunks, riffChunk{id: string(b[i : i+4]), data: b[i+8 : i+8+n]})
		i += 8 + n + n&1
	}
	return chunks, nil
}

// writeRIFF builds a WebP file from chunks.
func writeRIFF(chunks []riffChunk) []byte {
	size := 4
	for _, c := range chunks {
		size += 8 + len(c.data) + len(c.data)&1
	}
	out := make([]byte, 0, size+8)
	out = append(out, "RIFF"...)
	out = binary.LittleEndian.AppendUint32(out, uint32(size))
	out = append(out, "WEBP"...)
	for _, c := range chunks {
		out = append(out, c.id...)
		out = binary.LittleEndian.AppendUint32(out, uint32(len(c.data)))
		out = append(out, c.data...)
		if len(c.data)&1 == 1 {
			out = append(out, 0)
		}
	}
	return out
}

// stripWebP rebuilds a WebP from the chunks webpKeep names, copied as they
// are. An EXIF chunk becomes a minimal one holding the orientation, or goes
// when the orientation is 1 (a simple WebP, without VP8X, cannot carry one).
// The VP8X flags are brought in line with what is left.
func stripWebP(b []byte) ([]byte, int, error) {
	chunks, err := readRIFF(b)
	if err != nil {
		return nil, 0, err
	}
	extended := len(chunks) > 0 && chunks[0].id == "VP8X"
	if extended && len(chunks[0].data) < 10 {
		return nil, 0, errWebP
	}
	orientation := 1
	kept := make([]riffChunk, 0, len(chunks))
	for _, c := range chunks {
		switch {
		case c.id == "EXIF":
			if o := tiffOrientation(c.data); o > 1 && extended {
				orientation = o
				kept = append(kept, riffChunk{id: "EXIF", data: orientationTIFF(o)})
			}
		case webpKeep[c.id]:
			kept = append(kept, c)
		}
	}
	if extended {
		header := append([]byte{}, kept[0].data...)
		header[0] &^= vp8xXMP | vp8xEXIF
		if orientation > 1 {
			header[0] |= vp8xEXIF
		}
		kept[0].data = header
	}
	return writeRIFF(kept), orientation, nil
}

// DecodeWebPFrame decodes a WebP's picture, or its first frame when it is
// animated, with the frame's offset on the canvas. It takes the picture's
// own chunks into a minimal file for x/image/webp, which reads neither
// animations nor an extended file whose VP8L picture has the alpha flag set.
func DecodeWebPFrame(b []byte) (image.Image, image.Point, error) {
	chunks, err := readRIFF(b)
	if err != nil {
		return nil, image.Point{}, err
	}
	var offset image.Point
	frame := chunks
	if len(chunks) > 0 && chunks[0].id == "VP8X" {
		if len(chunks[0].data) < 10 {
			return nil, image.Point{}, errWebP
		}
		if chunks[0].data[0]&vp8xAnimation != 0 {
			frame = nil
			for _, c := range chunks {
				if c.id != "ANMF" {
					continue
				}
				if len(c.data) < 16 {
					return nil, image.Point{}, errWebP
				}
				offset = image.Pt(2*int(le24(c.data[0:])), 2*int(le24(c.data[3:])))
				if frame, err = readChunks(c.data[16:]); err != nil {
					return nil, image.Point{}, err
				}
				break
			}
		}
	}

	var alph, pic *riffChunk
	for i := range frame {
		switch frame[i].id {
		case "ALPH":
			if alph == nil {
				alph = &frame[i]
			}
		case "VP8 ", "VP8L":
			if pic == nil {
				pic = &frame[i]
			}
		}
	}
	if pic == nil {
		return nil, image.Point{}, errWebP
	}
	minimal := []riffChunk{*pic}
	if pic.id == "VP8 " && alph != nil {
		// ALPH needs a VP8X header giving the picture's size.
		cfg, err := webp.DecodeConfig(bytes.NewReader(writeRIFF([]riffChunk{*pic})))
		if err != nil {
			return nil, image.Point{}, err
		}
		header := make([]byte, 10)
		header[0] = vp8xAlpha
		putLE24(header[4:], uint32(cfg.Width-1))
		putLE24(header[7:], uint32(cfg.Height-1))
		minimal = []riffChunk{{id: "VP8X", data: header}, *alph, *pic}
	}
	img, err := webp.Decode(bytes.NewReader(writeRIFF(minimal)))
	return img, offset, err
}

func le24(b []byte) uint32 { return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 }

func putLE24(b []byte, v uint32) { b[0], b[1], b[2] = byte(v), byte(v>>8), byte(v>>16) }
