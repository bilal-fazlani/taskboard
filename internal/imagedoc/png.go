package imagedoc

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
)

var pngSignature = []byte("\x89PNG\r\n\x1a\n")

var errPNG = errors.New("malformed PNG")

// pngKeep are the ancillary chunks a PNG keeps: the ones that change how its
// pixels are shown (transparency, colour space and profile, gamma, HDR
// levels, background, pixel density, significant bits, histogram) and APNG
// animation. Everything else goes: tEXt,
// zTXt and iTXt (which carry XMP and other text), tIME, eXIf (but see
// stripPNG), and private chunks.
// pngCritical are the critical chunks PNG defines. A file with any other
// critical chunk (an uppercase first letter) is refused: a browser will not
// show it, and it could carry anything.
var pngCritical = map[string]bool{"IHDR": true, "PLTE": true, "IDAT": true, "IEND": true}

var pngKeep = map[string]bool{
	"tRNS": true, "cHRM": true, "gAMA": true, "iCCP": true, "sBIT": true, "sRGB": true,
	"cICP": true, "mDCv": true, "cLLi": true, "bKGD": true, "hIST": true, "pHYs": true,
	"acTL": true, "fcTL": true, "fdAT": true,
}

// stripPNG copies a PNG's chunks as they are, leaving out the metadata ones
// (see pngKeep). An eXIf chunk becomes a minimal one holding the
// orientation, or goes when the orientation is 1. Anything after IEND is
// dropped. An unknown critical chunk makes the file malformed.
func stripPNG(b []byte) ([]byte, int, error) {
	if len(b) < len(pngSignature) || string(b[:len(pngSignature)]) != string(pngSignature) {
		return nil, 0, errPNG
	}
	out := make([]byte, 0, len(b))
	out = append(out, pngSignature...)
	orientation := 1
	i := len(pngSignature)
	for {
		if i+8 > len(b) {
			return nil, 0, errPNG
		}
		n := int(binary.BigEndian.Uint32(b[i:]))
		typ := string(b[i+4 : i+8])
		end := i + 12 + n
		if n < 0 || end > len(b) || end < i {
			return nil, 0, errPNG
		}
		chunk := b[i:end]
		data := b[i+8 : i+8+n]
		i = end

		if typ[0] >= 'A' && typ[0] <= 'Z' && !pngCritical[typ] {
			return nil, 0, errPNG
		}
		switch {
		case typ == "eXIf":
			if o := tiffOrientation(data); o > 1 {
				orientation = o
				out = appendPNGChunk(out, "eXIf", orientationTIFF(o))
			}
		case pngCritical[typ] || pngKeep[typ]:
			out = append(out, chunk...)
		}
		if typ == "IEND" {
			return out, orientation, nil
		}
	}
}

func appendPNGChunk(out []byte, typ string, data []byte) []byte {
	out = binary.BigEndian.AppendUint32(out, uint32(len(data)))
	start := len(out)
	out = append(out, typ...)
	out = append(out, data...)
	return binary.BigEndian.AppendUint32(out, crc32.ChecksumIEEE(out[start:]))
}
