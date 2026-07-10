package transcode

import "io"

// Read-ahead buffering for forward-only progressive streams.
//
// ffmpeg writes its output into a small OS pipe (~64 KiB). When the client
// drains slowly (a momentary network or disk hiccup), that pipe fills and
// ffmpeg blocks, so playback can underrun. A readAhead goroutine eagerly pulls
// from the pipe into a bounded in-memory buffer, decoupling the producer from a
// briefly-slow consumer. The buffer is hard-capped, and Phase 1's admission
// gate bounds how many progressive streams run at once, so total RAM is bounded
// no matter how many viewers connect. This is a worst-case target: little RAM
// on the box, so the cap matters more than the size.
const (
	readAheadChunkBytes = 64 << 10 // per-read chunk
	readAheadChunks     = 64       // ~4 MiB of buffered read-ahead per stream
)

// readAhead wraps src with a background pump that reads into a bounded channel
// of chunks. Callers read from it like any io.Reader and must call Close when
// done so the pump goroutine cannot leak if the consumer stops early.
type readAhead struct {
	ch   chan []byte
	stop chan struct{}
	cur  []byte
}

// newReadAhead starts the pump. chunks*chunkBytes is the read-ahead cap.
func newReadAhead(src io.Reader, chunkBytes, chunks int) *readAhead {
	r := &readAhead{
		ch:   make(chan []byte, chunks),
		stop: make(chan struct{}),
	}
	go func() {
		defer close(r.ch)
		for {
			b := make([]byte, chunkBytes)
			n, err := src.Read(b)
			if n > 0 {
				select {
				case r.ch <- b[:n]:
				case <-r.stop:
					return
				}
			}
			if err != nil {
				return // EOF or read error ends the stream
			}
		}
	}()
	return r
}

// Read returns buffered bytes, blocking until the pump produces more or the
// stream ends.
func (r *readAhead) Read(p []byte) (int, error) {
	if len(r.cur) == 0 {
		b, ok := <-r.ch
		if !ok {
			return 0, io.EOF
		}
		r.cur = b
	}
	n := copy(p, r.cur)
	r.cur = r.cur[n:]
	return n, nil
}

// Close signals the pump to stop. Safe to call once; idempotent via the guard.
func (r *readAhead) Close() {
	select {
	case <-r.stop:
	default:
		close(r.stop)
	}
}
