package imagedoc

import (
	"bytes"
	"errors"
)

var errJPEG = errors.New("malformed JPEG")

// JPEG markers the stripper tells apart.
const (
	markerSOI   = 0xD8
	markerEOI   = 0xD9
	markerSOS   = 0xDA
	markerTEM   = 0x01
	markerRST0  = 0xD0
	markerRST7  = 0xD7
	markerAPP0  = 0xE0
	markerAPP1  = 0xE1
	markerAPP2  = 0xE2
	markerAPP14 = 0xEE
	markerAPPF  = 0xEF
	markerCOM   = 0xFE
)

// stripJPEG copies a JPEG's segments, leaving out its metadata:
//
//   - APP0 keeps a JFIF header only, with any embedded thumbnail removed
//     (a JFIF thumbnail can show the picture before it was cropped); JFXX and
//     other APP0 segments go.
//   - APP1 EXIF becomes a minimal block holding the orientation, or goes
//     when the orientation is 1; XMP and other APP1 segments go.
//   - APP2 keeps the ICC colour profile only; MPF (the extra pictures some
//     cameras append) and FlashPix go.
//   - APP14 keeps an "Adobe" segment, which says how the colours are coded
//     and is needed to show the picture right.
//   - Every other APPn (IPTC in APP13 among them) and every COM go.
//
// The frame, tables and scans are copied byte for byte, and anything after
// the end-of-image marker (MPF pictures, maker trailers) is dropped.
func stripJPEG(b []byte) ([]byte, int, error) {
	if len(b) < 4 || b[0] != 0xFF || b[1] != markerSOI {
		return nil, 0, errJPEG
	}
	out := make([]byte, 0, len(b))
	out = append(out, 0xFF, markerSOI)
	orientation := 1
	i := 2
	for {
		if i >= len(b) {
			// Ended without EOI: keep what is there and let the decoder
			// judge it.
			return out, orientation, nil
		}
		if b[i] != 0xFF {
			return nil, 0, errJPEG
		}
		for i < len(b) && b[i] == 0xFF {
			i++ // fill bytes
		}
		if i >= len(b) {
			return nil, 0, errJPEG
		}
		marker := b[i]
		i++
		switch {
		case marker == markerEOI:
			return append(out, 0xFF, markerEOI), orientation, nil
		case marker == markerTEM || (marker >= markerRST0 && marker <= markerRST7):
			out = append(out, 0xFF, marker)
			continue
		case marker == 0x00 || marker == markerSOI:
			return nil, 0, errJPEG
		}
		if i+2 > len(b) {
			return nil, 0, errJPEG
		}
		n := int(b[i])<<8 | int(b[i+1])
		if n < 2 || i+n > len(b) {
			return nil, 0, errJPEG
		}
		segment := b[i : i+n] // length bytes and payload
		payload := segment[2:]
		i += n

		switch {
		case marker == markerAPP0:
			if jfif, ok := jfifWithoutThumbnail(payload); ok {
				out = appendSegment(out, markerAPP0, jfif)
			}
		case marker == markerAPP1:
			if bytes.HasPrefix(payload, exifHeader) {
				if o := tiffOrientation(payload); o > 1 {
					orientation = o
					out = appendSegment(out, markerAPP1, append(append([]byte{}, exifHeader...), orientationTIFF(o)...))
				}
			}
		case marker == markerAPP2:
			if bytes.HasPrefix(payload, []byte("ICC_PROFILE\x00")) {
				out = append(out, 0xFF, marker)
				out = append(out, segment...)
			}
		case marker == markerAPP14:
			if bytes.HasPrefix(payload, []byte("Adobe")) {
				out = append(out, 0xFF, marker)
				out = append(out, segment...)
			}
		case marker > markerAPP2 && marker <= markerAPPF, marker == markerCOM:
			// metadata: left out
		default:
			out = append(out, 0xFF, marker)
			out = append(out, segment...)
		}

		if marker == markerSOS {
			// Entropy-coded data runs to the next marker that is not a
			// stuffed 0xFF00 or a restart marker.
			j := i
			for j+1 < len(b) {
				if b[j] == 0xFF {
					next := b[j+1]
					if next != 0x00 && (next < markerRST0 || next > markerRST7) {
						break
					}
				}
				j++
			}
			if j+1 >= len(b) {
				j = len(b)
			}
			out = append(out, b[i:j]...)
			i = j
		}
	}
}

// appendSegment writes a marker segment with payload.
func appendSegment(out []byte, marker byte, payload []byte) []byte {
	n := len(payload) + 2
	out = append(out, 0xFF, marker, byte(n>>8), byte(n))
	return append(out, payload...)
}

// jfifWithoutThumbnail returns a JFIF APP0 payload with its thumbnail
// removed, and false for any other APP0.
func jfifWithoutThumbnail(p []byte) ([]byte, bool) {
	// "JFIF\0", version (2), units (1), densities (2+2), thumbnail size (1+1)
	const header = 14
	if len(p) < header || !bytes.HasPrefix(p, []byte("JFIF\x00")) {
		return nil, false
	}
	jfif := append([]byte{}, p[:header]...)
	jfif[12], jfif[13] = 0, 0
	return jfif, true
}
