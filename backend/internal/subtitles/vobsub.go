package subtitles

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"os"
	"strconv"
	"strings"
	"time"
)

// VobSub (DVD) subtitles are a .idx text index plus a .sub MPEG program stream
// of subpicture units (SPUs). Each SPU is a 2-bit RLE bitmap with a control
// sequence carrying display timing, palette, and area. We decode each SPU into a
// dark-on-light grayscale image (like the PGS path) for Tesseract to OCR.

// vobsubIndex is a parsed .idx: the global palette and the timestamped byte
// offsets into the .sub where each subtitle's SPU begins.
type vobsubIndex struct {
	palette [16]color.RGBA
	entries []vobsubEntry
}

type vobsubEntry struct {
	start   time.Duration
	filepos int64
}

// ParseVobSub decodes a .idx/.sub pair into timed images ready for OCR.
func ParseVobSub(idxPath, subPath string) ([]pgsSub, error) {
	idx, err := parseIdx(idxPath)
	if err != nil {
		return nil, err
	}
	sub, err := os.ReadFile(subPath)
	if err != nil {
		return nil, err
	}
	var out []pgsSub
	for i, e := range idx.entries {
		if e.filepos < 0 || e.filepos >= int64(len(sub)) {
			continue
		}
		spu := readSPU(sub, e.filepos)
		if spu == nil {
			continue
		}
		img, dur, ok := decodeSPU(spu, idx.palette)
		if !ok {
			continue
		}
		end := e.start + dur
		if i+1 < len(idx.entries) && (dur == 0 || end > idx.entries[i+1].start) {
			end = idx.entries[i+1].start // clamp to the next cue when timing is absent
		}
		if end <= e.start {
			end = e.start + 3*time.Second
		}
		out = append(out, pgsSub{Start: e.start, End: end, Img: img})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no decodable vobsub cues")
	}
	return out, nil
}

// parseIdx reads the palette and timestamp/filepos entries from a .idx file.
func parseIdx(path string) (*vobsubIndex, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	idx := &vobsubIndex{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(line, "palette:"):
			idx.palette = parsePalette(strings.TrimPrefix(line, "palette:"))
		case strings.HasPrefix(line, "timestamp:"):
			if e, ok := parseTimestampLine(line); ok {
				idx.entries = append(idx.entries, e)
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(idx.entries) == 0 {
		return nil, fmt.Errorf("no timestamp entries in idx")
	}
	return idx, nil
}

// parsePalette parses "RRGGBB, RRGGBB, ..." (up to 16 entries) into RGBA colors.
func parsePalette(s string) [16]color.RGBA {
	var pal [16]color.RGBA
	for i, tok := range strings.Split(s, ",") {
		if i >= 16 {
			break
		}
		v, err := strconv.ParseUint(strings.TrimSpace(tok), 16, 32)
		if err != nil {
			continue
		}
		pal[i] = color.RGBA{R: byte(v >> 16), G: byte(v >> 8), B: byte(v), A: 0xFF}
	}
	return pal
}

// parseTimestampLine parses "timestamp: HH:MM:SS:mmm, filepos: 000000000".
func parseTimestampLine(line string) (vobsubEntry, bool) {
	parts := strings.Split(line, ",")
	if len(parts) != 2 {
		return vobsubEntry{}, false
	}
	ts := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(parts[0]), "timestamp:"))
	fp := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(parts[1]), "filepos:"))
	dur, ok := parseVobTime(ts)
	if !ok {
		return vobsubEntry{}, false
	}
	pos, err := strconv.ParseInt(fp, 16, 64)
	if err != nil {
		return vobsubEntry{}, false
	}
	return vobsubEntry{start: dur, filepos: pos}, true
}

// parseVobTime parses "HH:MM:SS:mmm" into a Duration.
func parseVobTime(s string) (time.Duration, bool) {
	var h, m, sec, ms int
	if _, err := fmt.Sscanf(s, "%d:%d:%d:%d", &h, &m, &sec, &ms); err != nil {
		return 0, false
	}
	return time.Duration(h)*time.Hour + time.Duration(m)*time.Minute +
		time.Duration(sec)*time.Second + time.Duration(ms)*time.Millisecond, true
}

// readSPU extracts one SPU's bytes starting at filepos in the .sub, unwrapping
// the MPEG program-stream packets (pack headers and private_stream_1 PES) and
// concatenating the subpicture payload until the SPU is complete.
func readSPU(sub []byte, filepos int64) []byte {
	var spu []byte
	pos := int(filepos)
	spuLen := -1
	for pos+4 <= len(sub) {
		// Expect a start code 0x000001xx.
		if sub[pos] != 0x00 || sub[pos+1] != 0x00 || sub[pos+2] != 0x01 {
			return nil
		}
		code := sub[pos+3]
		switch code {
		case 0xBA: // pack header: 0x000001BA + 10 bytes (MPEG-2)
			if pos+14 > len(sub) {
				return nil
			}
			stuffing := int(sub[pos+13] & 0x07)
			pos += 14 + stuffing
		case 0xBD: // private_stream_1 (subpictures)
			if pos+6 > len(sub) {
				return nil
			}
			pesLen := int(binary.BigEndian.Uint16(sub[pos+4 : pos+6]))
			payloadStart := pos + 6
			if payloadStart+2 > len(sub) {
				return nil
			}
			hdrDataLen := int(sub[payloadStart+2])
			dataStart := payloadStart + 3 + hdrDataLen
			// First byte of the substream payload is the stream id (0x20..0x3F);
			// skip it, the rest is SPU data.
			dataStart++
			dataEnd := pos + 6 + pesLen
			if dataStart >= dataEnd || dataEnd > len(sub) {
				return nil
			}
			spu = append(spu, sub[dataStart:dataEnd]...)
			if spuLen < 0 && len(spu) >= 2 {
				spuLen = int(binary.BigEndian.Uint16(spu[0:2]))
			}
			if spuLen >= 0 && len(spu) >= spuLen {
				return spu[:spuLen]
			}
			pos = dataEnd
		default: // other PES (padding, etc.): skip by its length
			if pos+6 > len(sub) {
				return nil
			}
			l := int(binary.BigEndian.Uint16(sub[pos+4 : pos+6]))
			pos += 6 + l
		}
	}
	if spuLen >= 0 && len(spu) >= spuLen {
		return spu[:spuLen]
	}
	return nil
}

// decodeSPU decodes an SPU into an image plus its display duration.
func decodeSPU(spu []byte, palette [16]color.RGBA) (image.Image, time.Duration, bool) {
	if len(spu) < 4 {
		return nil, 0, false
	}
	ctrlOffset := int(binary.BigEndian.Uint16(spu[2:4]))
	if ctrlOffset < 4 || ctrlOffset >= len(spu) {
		return nil, 0, false
	}

	var (
		x1, y1, x2, y2 int
		field1, field2 int
		palIdx         = [4]byte{}
		alpha          = [4]byte{}
		startDate, endDate time.Duration
		haveArea, havePix  bool
	)

	// Walk the control sequences.
	pos := ctrlOffset
	for pos+4 <= len(spu) {
		date := int(binary.BigEndian.Uint16(spu[pos : pos+2]))
		next := int(binary.BigEndian.Uint16(spu[pos+2 : pos+4]))
		// DVD control date is in 1/90000*1024 s units (~1.024ms per unit).
		when := time.Duration(date) * 1024 * time.Second / 90000
		p := pos + 4
	cmds:
		for p < len(spu) {
			switch spu[p] {
			case 0x00: // force display
				p++
			case 0x01: // start display
				startDate = when
				p++
			case 0x02: // stop display
				endDate = when
				p++
			case 0x03: // palette (4 x 4-bit indices)
				if p+3 > len(spu) {
					break cmds
				}
				palIdx[3] = spu[p+1] >> 4
				palIdx[2] = spu[p+1] & 0x0F
				palIdx[1] = spu[p+2] >> 4
				palIdx[0] = spu[p+2] & 0x0F
				p += 3
			case 0x04: // contrast/alpha (4 x 4-bit)
				if p+3 > len(spu) {
					break cmds
				}
				alpha[3] = spu[p+1] >> 4
				alpha[2] = spu[p+1] & 0x0F
				alpha[1] = spu[p+2] >> 4
				alpha[0] = spu[p+2] & 0x0F
				p += 3
			case 0x05: // display area (x1,x2,y1,y2 as 12-bit each)
				if p+7 > len(spu) {
					break cmds
				}
				x1 = int(spu[p+1])<<4 | int(spu[p+2])>>4
				x2 = int(spu[p+2]&0x0F)<<8 | int(spu[p+3])
				y1 = int(spu[p+4])<<4 | int(spu[p+5])>>4
				y2 = int(spu[p+5]&0x0F)<<8 | int(spu[p+6])
				haveArea = true
				p += 7
			case 0x06: // pixel data offsets for the two interlaced fields
				if p+5 > len(spu) {
					break cmds
				}
				field1 = int(binary.BigEndian.Uint16(spu[p+1 : p+3]))
				field2 = int(binary.BigEndian.Uint16(spu[p+3 : p+5]))
				havePix = true
				p += 5
			case 0xFF: // end of control sequence
				p++
				break cmds
			default:
				p++
			}
		}
		if next == pos || next < ctrlOffset || next >= len(spu) {
			break
		}
		pos = next
	}

	if !haveArea || !havePix {
		return nil, 0, false
	}
	w, h := x2-x1+1, y2-y1+1
	if w <= 0 || h <= 0 || w > 4096 || h > 4096 {
		return nil, 0, false
	}
	img := renderVobSub(spu, w, h, field1, field2, palIdx, alpha, palette)
	dur := endDate - startDate
	return img, dur, true
}

// renderVobSub decodes the two interlaced RLE fields into a dark-on-light
// grayscale image for OCR (transparent/background stays white).
func renderVobSub(spu []byte, w, h, field1, field2 int, palIdx, alpha [4]byte, palette [16]color.RGBA) image.Image {
	img := image.NewGray(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 0xFF // white background
	}
	// Even rows from field1, odd rows from field2.
	decodeField(img, spu, field1, w, h, 0, palIdx, alpha, palette)
	decodeField(img, spu, field2, w, h, 1, palIdx, alpha, palette)
	return img
}

// decodeField decodes one interlaced RLE field, writing rows y = startRow, +2...
func decodeField(img *image.Gray, spu []byte, offset, w, h, startRow int, palIdx, alpha [4]byte, palette [16]color.RGBA) {
	nib := newNibbleReader(spu, offset)
	x, y := 0, startRow
	for y < h && !nib.eof() {
		runLen, colorIdx := readRLE(nib)
		if runLen == 0 { // rest of line
			runLen = w - x
		}
		for i := 0; i < runLen && x < w; i++ {
			if alpha[colorIdx] != 0 { // opaque pixel = ink
				lum := luminance(palette[palIdx[colorIdx]&0x0F])
				img.SetGray(x, y, color.Gray{Y: lum})
			}
			x++
		}
		if x >= w {
			x = 0
			y += 2
			nib.align()
		}
	}
}

// readRLE reads one VobSub run: 1-4 nibbles encode (length<<2 | color).
func readRLE(n *nibbleReader) (runLen int, colorIdx byte) {
	v := n.read()
	if v >= 0x4 { // 1 nibble: length 1-3
		return int(v >> 2), byte(v & 0x3)
	}
	v = v<<4 | n.read()
	if v >= 0x10 { // 2 nibbles
		return int(v >> 2), byte(v & 0x3)
	}
	v = v<<4 | n.read()
	if v >= 0x40 { // 3 nibbles
		return int(v >> 2), byte(v & 0x3)
	}
	v = v<<4 | n.read()
	return int(v >> 2), byte(v & 0x3) // 4 nibbles; length 0 = to end of line
}

// nibbleReader reads 4-bit nibbles from a byte slice starting at a byte offset.
type nibbleReader struct {
	data []byte
	pos  int // nibble index
}

func newNibbleReader(data []byte, byteOffset int) *nibbleReader {
	return &nibbleReader{data: data, pos: byteOffset * 2}
}

func (n *nibbleReader) read() byte {
	if n.eof() {
		return 0
	}
	b := n.data[n.pos/2]
	n.pos++
	if n.pos%2 == 1 {
		return b >> 4
	}
	return b & 0x0F
}

func (n *nibbleReader) align() {
	if n.pos%2 == 1 {
		n.pos++
	}
}

func (n *nibbleReader) eof() bool { return n.pos/2 >= len(n.data) }

// luminance maps a palette color to an 8-bit grey, inverted so ink is dark.
func luminance(c color.RGBA) byte {
	y := (299*int(c.R) + 587*int(c.G) + 114*int(c.B)) / 1000
	return byte(255 - y)
}
