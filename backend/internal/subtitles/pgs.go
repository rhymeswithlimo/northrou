package subtitles

import (
	"encoding/binary"
	"image"
	"image/color"
	"io"
	"os"
	"time"
)

// PGS (Presentation Graphic Stream) .sup parsing. The format is a sequence of
// segments, each: "PG" magic, 32-bit PTS (90kHz), 32-bit DTS, 8-bit type,
// 16-bit length, payload. A display set ends with an END segment.
//
// Segment types:
const (
	segPCS = 0x16 // Presentation Composition
	segWDS = 0x17 // Window Definition
	segPDS = 0x14 // Palette Definition
	segODS = 0x15 // Object Definition (RLE bitmap)
	segEND = 0x80 // End of display set
)

// pgsSub is a decoded subtitle image with its on-screen time range.
type pgsSub struct {
	Start time.Duration
	End   time.Duration
	Img   image.Image
}

// object is a decoded (indexed) bitmap.
type object struct {
	width, height int
	pixels        []byte // palette indices, row-major
}

// ParseSUP parses a PGS .sup file into timed subtitle images.
func ParseSUP(path string) ([]pgsSub, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parseSUP(f)
}

func parseSUP(r io.Reader) ([]pgsSub, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	var subs []pgsSub
	palette := map[byte]color.RGBA{}
	objects := map[uint16]*object{}

	var curPTS uint32
	var compObjects []compObject
	var pcsW, pcsH int
	pendingIdx := -1 // index into subs awaiting an end time

	pos := 0
	for pos+13 <= len(data) {
		if data[pos] != 'P' || data[pos+1] != 'G' {
			break // not a valid segment header; stop gracefully
		}
		pts := binary.BigEndian.Uint32(data[pos+2 : pos+6])
		segType := data[pos+10]
		segLen := int(binary.BigEndian.Uint16(data[pos+11 : pos+13]))
		payloadStart := pos + 13
		if payloadStart+segLen > len(data) {
			break
		}
		payload := data[payloadStart : payloadStart+segLen]

		switch segType {
		case segPCS:
			curPTS = pts
			pcsW, pcsH, compObjects = parsePCS(payload)
		case segPDS:
			parsePDS(payload, palette)
		case segODS:
			if id, obj, ok := parseODS(payload); ok {
				objects[id] = obj
			}
		case segEND:
			if len(compObjects) == 0 {
				// Empty composition => clear: close the pending cue.
				if pendingIdx >= 0 {
					subs[pendingIdx].End = ptsToDuration(curPTS)
					pendingIdx = -1
				}
			} else {
				img := renderComposition(pcsW, pcsH, compObjects, objects, palette)
				if img != nil {
					subs = append(subs, pgsSub{Start: ptsToDuration(curPTS), End: ptsToDuration(curPTS) + 4*time.Second, Img: img})
					pendingIdx = len(subs) - 1
				}
			}
		}
		pos = payloadStart + segLen
	}
	return subs, nil
}

type compObject struct {
	objectID uint16
	x, y     int
}

func parsePCS(p []byte) (w, h int, objs []compObject) {
	if len(p) < 11 {
		return 0, 0, nil
	}
	w = int(binary.BigEndian.Uint16(p[0:2]))
	h = int(binary.BigEndian.Uint16(p[2:4]))
	if w <= 0 || h <= 0 || w > maxSubDim || h > maxSubDim {
		return 0, 0, nil
	}
	count := int(p[10])
	off := 11
	for i := 0; i < count; i++ {
		if off+8 > len(p) {
			break
		}
		objID := binary.BigEndian.Uint16(p[off : off+2])
		flags := p[off+3]
		x := int(binary.BigEndian.Uint16(p[off+4 : off+6]))
		y := int(binary.BigEndian.Uint16(p[off+6 : off+8]))
		off += 8
		if flags&0x80 != 0 { // cropped: skip 8 crop bytes
			off += 8
		}
		objs = append(objs, compObject{objectID: objID, x: x, y: y})
	}
	return w, h, objs
}

func parsePDS(p []byte, palette map[byte]color.RGBA) {
	// 1 byte palette id, 1 byte version, then 5-byte entries.
	for i := 2; i+5 <= len(p); i += 5 {
		id := p[i]
		y := p[i+1]
		cr := p[i+2]
		cb := p[i+3]
		a := p[i+4]
		r, g, b := ycrcbToRGB(y, cr, cb)
		palette[id] = color.RGBA{R: r, G: g, B: b, A: a}
	}
}

// maxSubDim caps a decoded subtitle bitmap's width/height. PGS dimensions come
// from untrusted 16-bit fields (up to 65535); left unchecked, width*height
// reaches ~4.29e9, forcing a ~4 GB allocation (OOM) on 64-bit or overflowing
// int to negative on 32-bit builds (linux/arm) so `make` panics. 4096 comfortably
// covers 4K UHD subtitle planes and matches the VobSub decoder's own cap.
const maxSubDim = 4096

func parseODS(p []byte) (uint16, *object, bool) {
	if len(p) < 11 {
		return 0, nil, false
	}
	id := binary.BigEndian.Uint16(p[0:2])
	// p[2] version, p[3] sequence flag, p[4:7] data length (3 bytes)
	width := int(binary.BigEndian.Uint16(p[7:9]))
	height := int(binary.BigEndian.Uint16(p[9:11]))
	if width <= 0 || height <= 0 || width > maxSubDim || height > maxSubDim {
		return 0, nil, false
	}
	rle := p[11:]
	pixels := decodeRLE(rle, width, height)
	return id, &object{width: width, height: height, pixels: pixels}, true
}

// decodeRLE decodes PGS run-length-encoded object data into palette indices.
func decodeRLE(data []byte, width, height int) []byte {
	out := make([]byte, width*height)
	x, y := 0, 0
	i := 0
	put := func(color byte, run int) {
		for k := 0; k < run; k++ {
			if x < width && y < height {
				out[y*width+x] = color
			}
			x++
		}
	}
	for i < len(data) {
		b := data[i]
		i++
		if b != 0 {
			put(b, 1)
			continue
		}
		if i >= len(data) {
			break
		}
		b2 := data[i]
		i++
		if b2 == 0 {
			// end of line
			x = 0
			y++
			continue
		}
		var run int
		var col byte
		switch b2 & 0xC0 {
		case 0x00:
			run = int(b2 & 0x3F)
		case 0x40:
			if i >= len(data) {
				return out
			}
			run = int(b2&0x3F)<<8 | int(data[i])
			i++
		case 0x80:
			run = int(b2 & 0x3F)
			if i >= len(data) {
				return out
			}
			col = data[i]
			i++
		default: // 0xC0
			if i+1 >= len(data) {
				return out
			}
			run = int(b2&0x3F)<<8 | int(data[i])
			i++
			col = data[i]
			i++
		}
		put(col, run)
	}
	return out
}

// renderComposition composites decoded objects onto a canvas and returns a
// grayscale image with dark text on a light background (ideal for OCR). Only
// the bounding box of visible pixels is kept.
func renderComposition(w, h int, comps []compObject, objects map[uint16]*object, palette map[byte]color.RGBA) image.Image {
	if w == 0 || h == 0 {
		return nil
	}
	canvas := image.NewGray(image.Rect(0, 0, w, h))
	// Fill white (background after inversion).
	for i := range canvas.Pix {
		canvas.Pix[i] = 255
	}
	minX, minY, maxX, maxY := w, h, 0, 0
	any := false

	for _, c := range comps {
		obj := objects[c.objectID]
		if obj == nil {
			continue
		}
		for oy := 0; oy < obj.height; oy++ {
			for ox := 0; ox < obj.width; ox++ {
				idx := obj.pixels[oy*obj.width+ox]
				col, ok := palette[idx]
				if !ok || col.A == 0 {
					continue
				}
				px, py := c.x+ox, c.y+oy
				if px < 0 || py < 0 || px >= w || py >= h {
					continue
				}
				// Composite over black, then invert -> dark on light.
				luma := (uint16(col.R)*30 + uint16(col.G)*59 + uint16(col.B)*11) / 100
				overBlack := uint8(uint16(luma) * uint16(col.A) / 255)
				canvas.SetGray(px, py, color.Gray{Y: 255 - overBlack})
				any = true
				if px < minX {
					minX = px
				}
				if py < minY {
					minY = py
				}
				if px > maxX {
					maxX = px
				}
				if py > maxY {
					maxY = py
				}
			}
		}
	}
	if !any {
		return nil
	}
	// Crop to bounding box with a small margin.
	pad := 8
	minX, minY = max0(minX-pad), max0(minY-pad)
	maxX, maxY = minInt(maxX+pad, w-1), minInt(maxY+pad, h-1)
	return canvas.SubImage(image.Rect(minX, minY, maxX+1, maxY+1))
}

func ycrcbToRGB(y, cr, cb byte) (r, g, b uint8) {
	yf := float64(y)
	crf := float64(cr) - 128
	cbf := float64(cb) - 128
	rf := yf + 1.402*crf
	gf := yf - 0.344136*cbf - 0.714136*crf
	bf := yf + 1.772*cbf
	return clamp8(rf), clamp8(gf), clamp8(bf)
}

func clamp8(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

func ptsToDuration(pts uint32) time.Duration {
	return time.Duration(pts) * time.Second / 90000
}

func max0(v int) int {
	if v < 0 {
		return 0
	}
	return v
}
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
