package subtitles

import (
	"testing"
	"time"
)

func TestDecodeRLE(t *testing.T) {
	// A 4x2 image:
	//   row 0: color 5, then run of 3 zeros (0x00 0x03)
	//   row 1: run of 2 of color 7 (0x00 0x82 0x07), then 2 zeros to fill
	// PGS RLE: non-zero byte => 1 px of that color; 0x00 then code byte.
	// Encoding row0: [5][00 03] -> px(5), then 3 px of color0. end-of-line [00 00]
	// Encoding row1: [00 82 07] -> 2 px of color 7; [00 42 02] -> run 2 color0; [00 00]
	data := []byte{
		5, 0x00, 0x03, 0x00, 0x00, // row 0
		0x00, 0x82, 0x07, 0x00, 0x42, 0x02, 0x00, 0x00, // row 1
	}
	px := decodeRLE(data, 4, 2)
	want := []byte{
		5, 0, 0, 0,
		7, 7, 0, 0,
	}
	for i := range want {
		if px[i] != want[i] {
			t.Fatalf("pixel %d = %d, want %d (full=%v)", i, px[i], want[i], px)
		}
	}
}

func TestRLERunLengthTwoByte(t *testing.T) {
	// 0x00 0x40|0x01 0x00 => run of 256 of color 0. Width 300 => 256 zeros then rest.
	data := []byte{0x00, 0x41, 0x00} // (0x41&0x3f)<<8 | 0x00 = 0x100 = 256
	px := decodeRLE(data, 300, 1)
	for i := 0; i < 256; i++ {
		if px[i] != 0 {
			t.Fatalf("expected 0 at %d", i)
		}
	}
}

func TestYCrCbToRGB(t *testing.T) {
	// Pure white: Y=235-ish maps near white; use full-range approx Y=255.
	r, g, b := ycrcbToRGB(255, 128, 128)
	if r < 250 || g < 250 || b < 250 {
		t.Errorf("expected near-white, got %d,%d,%d", r, g, b)
	}
	// Neutral gray.
	r, g, b = ycrcbToRGB(128, 128, 128)
	if r != 128 || g != 128 || b != 128 {
		t.Errorf("expected 128,128,128, got %d,%d,%d", r, g, b)
	}
}

func TestPTSToDuration(t *testing.T) {
	// 90000 ticks = 1 second.
	if d := ptsToDuration(90000); d != time.Second {
		t.Errorf("expected 1s, got %v", d)
	}
	if d := ptsToDuration(45000); d != 500*time.Millisecond {
		t.Errorf("expected 500ms, got %v", d)
	}
}

func TestVTTTimestamp(t *testing.T) {
	got := vttTimestamp(3661*time.Second + 250*time.Millisecond)
	if got != "01:01:01.250" {
		t.Errorf("got %q", got)
	}
}
