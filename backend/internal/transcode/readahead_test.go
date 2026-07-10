package transcode

import (
	"bytes"
	"crypto/rand"
	"io"
	"testing"
	"time"
)

func TestReadAhead_DeliversAllBytesInOrder(t *testing.T) {
	src := make([]byte, 1<<20+123) // not a chunk multiple
	if _, err := rand.Read(src); err != nil {
		t.Fatal(err)
	}
	ra := newReadAhead(bytes.NewReader(src), 4096, 8)
	defer ra.Close()

	got, err := io.ReadAll(ra)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, src) {
		t.Fatalf("read-ahead corrupted stream: got %d bytes, want %d", len(got), len(src))
	}
}

func TestReadAhead_CloseStopsPumpEarly(t *testing.T) {
	// A slow/abandoned consumer must not leak the pump goroutine. Use an
	// infinite source and a tiny buffer so the pump blocks on send, then Close.
	ra := newReadAhead(infiniteReader{}, 1024, 2)

	// Read one chunk, then stop consuming so the channel fills and the pump
	// blocks trying to send.
	buf := make([]byte, 512)
	if _, err := ra.Read(buf); err != nil {
		t.Fatalf("first read: %v", err)
	}
	time.Sleep(10 * time.Millisecond) // let the pump fill the channel and block

	done := make(chan struct{})
	go func() {
		ra.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Close did not return; pump goroutine likely leaked")
	}

	// Close is idempotent.
	ra.Close()
}

// infiniteReader always returns data and never errors.
type infiniteReader struct{}

func (infiniteReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0xAB
	}
	return len(p), nil
}
