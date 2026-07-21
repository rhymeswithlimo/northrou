package subtitles

import (
	"path/filepath"
	"os"
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
