package subtitles

import (
	"bytes"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

// bom markers we strip before handing text to ffmpeg.
var (
	bomUTF8    = []byte{0xEF, 0xBB, 0xBF}
	bomUTF16LE = []byte{0xFF, 0xFE}
	bomUTF16BE = []byte{0xFE, 0xFF}
)

// toUTF8 normalizes subtitle bytes to BOM-less UTF-8. Text subtitles in the
// wild are frequently Windows-1252/Latin-1 (scene and YIFY releases especially);
// feeding those to ffmpeg as if they were UTF-8 mojibakes accented characters.
// We detect UTF-8/UTF-16 by BOM or validity and otherwise assume Windows-1252,
// a superset of Latin-1 that covers the vast majority of Western releases.
func toUTF8(data []byte) []byte {
	switch {
	case bytes.HasPrefix(data, bomUTF8):
		return data[len(bomUTF8):]
	case bytes.HasPrefix(data, bomUTF16LE):
		return decodeUTF16(data[2:], false)
	case bytes.HasPrefix(data, bomUTF16BE):
		return decodeUTF16(data[2:], true)
	}
	if utf8.Valid(data) {
		return data
	}
	// Fall back to Windows-1252. charmap decoding never fails (every byte maps).
	out, err := charmap.Windows1252.NewDecoder().Bytes(data)
	if err != nil {
		return data
	}
	return out
}

func decodeUTF16(b []byte, bigEndian bool) []byte {
	// Assemble 16-bit code units and decode via utf16.Decode so surrogate PAIRS
	// (non-BMP characters, e.g. emoji in a subtitle) are combined into one rune
	// instead of being emitted as two broken halves.
	u16 := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		if bigEndian {
			u16 = append(u16, uint16(b[i])<<8|uint16(b[i+1]))
		} else {
			u16 = append(u16, uint16(b[i+1])<<8|uint16(b[i]))
		}
	}
	return []byte(string(utf16.Decode(u16)))
}
