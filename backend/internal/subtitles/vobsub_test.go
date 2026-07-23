package subtitles

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseVobTime(t *testing.T) {
	d, ok := parseVobTime("01:02:03:500")
	if !ok || d != time.Hour+2*time.Minute+3*time.Second+500*time.Millisecond {
		t.Errorf("parseVobTime = %v ok=%v", d, ok)
	}
	if _, ok := parseVobTime("garbage"); ok {
		t.Error("expected failure on garbage")
	}
}

func TestParsePalette(t *testing.T) {
	pal := parsePalette(" 000000, ff0000, 00ff00, 0000ff")
	if pal[1].R != 0xFF || pal[1].G != 0 || pal[1].B != 0 {
		t.Errorf("palette[1] = %+v, want red", pal[1])
	}
	if pal[2].G != 0xFF {
		t.Errorf("palette[2] = %+v, want green", pal[2])
	}
}

func TestParseTimestampLine(t *testing.T) {
	e, ok := parseTimestampLine("timestamp: 00:00:05:000, filepos: 000000800")
	if !ok {
		t.Fatal("failed to parse")
	}
	if e.start != 5*time.Second {
		t.Errorf("start = %v, want 5s", e.start)
	}
	if e.filepos != 0x800 {
		t.Errorf("filepos = %d, want 0x800", e.filepos)
	}
}

func TestParseIdx(t *testing.T) {
	dir := t.TempDir()
	idxPath := filepath.Join(dir, "t.idx")
	content := "# comment\n" +
		"palette: 000000, ffffff, 808080, 404040\n" +
		"timestamp: 00:00:01:000, filepos: 000000000\n" +
		"timestamp: 00:00:04:500, filepos: 000000200\n"
	if err := os.WriteFile(idxPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := parseIdx(idxPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(idx.entries))
	}
	if idx.entries[1].start != 4500*time.Millisecond || idx.entries[1].filepos != 0x200 {
		t.Errorf("entry[1] = %+v", idx.entries[1])
	}
	if idx.palette[1].R != 0xFF {
		t.Errorf("palette not parsed: %+v", idx.palette[1])
	}
}

func TestNibbleReader(t *testing.T) {
	n := newNibbleReader([]byte{0xAB, 0xCD}, 0)
	got := []byte{n.read(), n.read(), n.read(), n.read()}
	want := []byte{0xA, 0xB, 0xC, 0xD}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("nibble %d = %x, want %x", i, got[i], want[i])
		}
	}
	if !n.eof() {
		t.Error("expected eof")
	}
}

func TestReadRLE(t *testing.T) {
	// 1-nibble run: 0b1101 = length 3, color 1.
	n := newNibbleReader([]byte{0xD0}, 0)
	l, c := readRLE(n)
	if l != 3 || c != 1 {
		t.Errorf("1-nibble RLE = (%d,%d), want (3,1)", l, c)
	}
	// 2-nibble run: nibbles 0x1,0x5 -> 0x15 = 0b00010101 -> length 5, color 1.
	n = newNibbleReader([]byte{0x15}, 0)
	l, c = readRLE(n)
	if l != 5 || c != 1 {
		t.Errorf("2-nibble RLE = (%d,%d), want (5,1)", l, c)
	}
}

// TestReadSPUTruncatedHeaderNoPanic guards the off-by-one that crashed the whole
// server: a .sub ending in a truncated private_stream_1 (0xBD) header where the
// header-data-length byte sits exactly at len(sub) must not panic.
func TestReadSPUTruncatedHeaderNoPanic(t *testing.T) {
	// pack header (0x000001BA + 10) then a truncated 0xBD packet whose
	// payloadStart+2 index lands exactly at len(sub).
	pack := append([]byte{0x00, 0x00, 0x01, 0xBA}, make([]byte, 10)...)
	// 0xBD start code + 2-byte PES length, then only 2 more bytes so
	// payloadStart(=+6) + 2 == len(sub).
	bd := []byte{0x00, 0x00, 0x01, 0xBD, 0x00, 0x10, 0x00, 0x00}
	sub := append(pack, bd...)
	// Must return (nil) rather than panic on the out-of-range index.
	if got := readSPU(sub, int64(len(pack))); got != nil {
		t.Fatalf("expected nil for truncated SPU, got %d bytes", len(got))
	}
}

// TestPGSOversizedDimensionsRejected guards against the unbounded-allocation
// OOM / 32-bit overflow: ODS/PCS dimensions beyond the cap are refused instead
// of driving a multi-gigabyte make().
func TestPGSOversizedDimensionsRejected(t *testing.T) {
	// ODS with width=height=65535.
	ods := make([]byte, 11)
	ods[7], ods[8] = 0xFF, 0xFF  // width
	ods[9], ods[10] = 0xFF, 0xFF // height
	if _, _, ok := parseODS(ods); ok {
		t.Fatal("parseODS must reject 65535x65535")
	}
	// PCS with width=height=65535.
	pcs := make([]byte, 11)
	pcs[0], pcs[1] = 0xFF, 0xFF
	pcs[2], pcs[3] = 0xFF, 0xFF
	if w, h, _ := parsePCS(pcs); w != 0 || h != 0 {
		t.Fatalf("parsePCS must reject 65535x65535, got %dx%d", w, h)
	}
}
