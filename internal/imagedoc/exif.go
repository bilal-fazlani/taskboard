package imagedoc

import (
	"bytes"
	"encoding/binary"
)

// exifHeader starts the EXIF block in a JPEG's APP1 segment, and sometimes in
// a WebP's EXIF chunk.
var exifHeader = []byte("Exif\x00\x00")

// tiffOrientation reads the Orientation tag (0x0112) from the first IFD of a
// TIFF-structured EXIF block. It returns 1, "shown as stored", when there is
// no such tag, or anything about it is out of place.
func tiffOrientation(t []byte) int {
	t = bytes.TrimPrefix(t, exifHeader)
	if len(t) < 8 {
		return 1
	}
	var order binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 1
	}
	if order.Uint16(t[2:]) != 42 {
		return 1
	}
	off := int(order.Uint32(t[4:]))
	if off < 8 || off+2 > len(t) {
		return 1
	}
	n := int(order.Uint16(t[off:]))
	for i := 0; i < n; i++ {
		e := off + 2 + 12*i
		if e+12 > len(t) {
			return 1
		}
		if order.Uint16(t[e:]) != 0x0112 {
			continue
		}
		// SHORT, one value, held in the first two bytes of the value field.
		if order.Uint16(t[e+2:]) != 3 || order.Uint32(t[e+4:]) != 1 {
			return 1
		}
		if v := int(order.Uint16(t[e+8:])); v >= 1 && v <= 8 {
			return v
		}
		return 1
	}
	return 1
}

// orientationTIFF is the smallest EXIF block that says orientation: a
// big-endian TIFF header and one IFD with the Orientation tag alone.
func orientationTIFF(orientation int) []byte {
	return []byte{
		'M', 'M', 0x00, 0x2A, // byte order, TIFF magic
		0x00, 0x00, 0x00, 0x08, // first IFD at offset 8
		0x00, 0x01, // one entry
		0x01, 0x12, // Orientation
		0x00, 0x03, // SHORT
		0x00, 0x00, 0x00, 0x01, // one value
		0x00, byte(orientation), 0x00, 0x00, // the value, padded
		0x00, 0x00, 0x00, 0x00, // no next IFD
	}
}
