package imagedoc

import (
	"errors"
)

var errGIF = errors.New("malformed GIF")

// gifKeepApps are the application extensions a GIF keeps: the looping
// settings of an animation (Netscape's, and the older ANIMEXTS one) and an
// ICC colour profile. Others, XMP among them, go.
var gifKeepApps = map[string]bool{
	"NETSCAPE2.0": true,
	"ANIMEXTS1.0": true,
	"ICCRGBG1012": true,
}

// stripGIF copies a GIF's blocks as they are, leaving out comment extensions
// and the application extensions gifKeepApps does not name. Graphic control
// extensions (frame timing and transparency), plain text extensions (part
// of the picture, in the GIF specification) and every frame are kept.
// Anything after the trailer is dropped, and a missing trailer is added.
func stripGIF(b []byte) ([]byte, error) {
	if len(b) < 13 {
		return nil, errGIF
	}
	i := 13 // header and logical screen descriptor
	if b[10]&0x80 != 0 {
		i += 3 << ((b[10] & 0x07) + 1) // global colour table
	}
	if i > len(b) {
		return nil, errGIF
	}
	out := make([]byte, 0, len(b))
	out = append(out, b[:i]...)
	for {
		if i >= len(b) {
			return append(out, 0x3B), nil
		}
		switch b[i] {
		case 0x3B: // trailer
			return append(out, 0x3B), nil
		case 0x21: // extension
			if i+2 > len(b) {
				return nil, errGIF
			}
			label := b[i+1]
			end, err := skipSubBlocks(b, i+2)
			if err != nil {
				return nil, err
			}
			keep := label == 0xF9 || label == 0x01
			if label == 0xFF && i+3+11 <= len(b) && b[i+2] == 11 {
				keep = gifKeepApps[string(b[i+3:i+3+11])]
			}
			if keep {
				out = append(out, b[i:end]...)
			}
			i = end
		case 0x2C: // image descriptor
			j := i + 10
			if j > len(b) {
				return nil, errGIF
			}
			if b[i+9]&0x80 != 0 {
				j += 3 << ((b[i+9] & 0x07) + 1) // local colour table
			}
			j++ // LZW minimum code size
			if j > len(b) {
				return nil, errGIF
			}
			end, err := skipSubBlocks(b, j)
			if err != nil {
				return nil, err
			}
			out = append(out, b[i:end]...)
			i = end
		default:
			return nil, errGIF
		}
	}
}

// skipSubBlocks returns the offset just past the data sub-blocks starting at
// i, including their zero-length terminator.
func skipSubBlocks(b []byte, i int) (int, error) {
	for {
		if i >= len(b) {
			return 0, errGIF
		}
		n := int(b[i])
		i++
		if n == 0 {
			return i, nil
		}
		i += n
	}
}
