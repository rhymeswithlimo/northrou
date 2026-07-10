package transcode

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// hlsSegmentSeconds is the target HLS segment length. Short segments cut
// startup latency: playback can begin after the first segment is encoded, and
// on a weak CPU a shorter first segment means a shorter wait. We also force a
// keyframe at each boundary (see hlsArgs) so segments are actually this length
// instead of being dictated by the encoder's default ~10s GOP, which would make
// the first segment (and startup) take a full GOP to appear. Fine-grained
// segments also give smoother seeking within the already-encoded range.
const hlsSegmentSeconds = 2

// hlsSession is a running ffmpeg HLS transcode writing segments to a work dir.
type hlsSession struct {
	id         string
	dir        string
	cmd        *exec.Cmd
	cancel     context.CancelFunc
	release    func() // frees the admission budget slot; set by ensureHLS
	lastAccess time.Time
	mu         sync.Mutex
}

// touch updates the last-access time (for idle reaping).
func (h *hlsSession) touch() {
	h.mu.Lock()
	h.lastAccess = time.Now()
	h.mu.Unlock()
}

func (h *hlsSession) idle() time.Duration {
	h.mu.Lock()
	defer h.mu.Unlock()
	return time.Since(h.lastAccess)
}

func (h *hlsSession) stop() {
	if h.cancel != nil {
		h.cancel()
	}
	if h.release != nil {
		h.release()
	}
	_ = os.RemoveAll(h.dir)
}

// startHLS launches an ffmpeg HLS transcode for the decision and returns once
// the playlist file exists (so the first client request succeeds).
func (s *Streamer) startHLS(parent context.Context, dec Decision, inputPath, sessionID string) (*hlsSession, error) {
	dir := filepath.Join(s.hlsDir, sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(parent)

	rung := topRung(dec, s.maxBitrateKbps)
	args := s.hlsArgs(dec, inputPath, dir, rung)
	cmd := exec.CommandContext(ctx, s.ffmpegPath, args...)
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start ffmpeg hls: %w", err)
	}
	slog.Info("HLS transcode started", "session", sessionID, "backend", dec.HWBackend,
		"height", rung.Height, "bitrate_kbps", rung.BitrateKbps, "tonemap", dec.Tonemap)

	sess := &hlsSession{id: sessionID, dir: dir, cmd: cmd, cancel: cancel, lastAccess: time.Now()}
	go func() {
		_ = cmd.Wait()
	}()

	// Wait (briefly) for the playlist to appear.
	playlist := filepath.Join(dir, "stream.m3u8")
	if err := waitForFile(ctx, playlist, 30*time.Second); err != nil {
		sess.stop()
		return nil, fmt.Errorf("hls playlist did not appear: %w", err)
	}
	return sess, nil
}

// hlsArgs builds the ffmpeg command for a single-rendition HLS transcode to
// H.264 at the target rung.
func (s *Streamer) hlsArgs(dec Decision, inputPath, dir string, rung Rung) []string {
	args := []string{"-hide_banner", "-loglevel", "error"}
	args = append(args, inputArgs(dec.HWBackend)...)
	args = append(args, "-i", inputPath)
	args = append(args, "-map", "0:v:0", "-map", "0:a:0")

	// Video encode.
	args = append(args, "-c:v", videoEncoder(dec.HWBackend))
	if vf := videoFilter(dec, rung.Height); vf != "" {
		args = append(args, "-vf", vf)
	}
	args = append(args, "-b:v", strconv.Itoa(rung.BitrateKbps)+"k")
	if dec.HWBackend == "software" || dec.HWBackend == "" {
		args = append(args, "-preset", "veryfast")
	}
	// Force a keyframe at each segment boundary so segments are actually
	// hlsSegmentSeconds long (and independently seekable) rather than following
	// the encoder's default GOP, which would delay the first segment.
	args = append(args, "-force_key_frames",
		fmt.Sprintf("expr:gte(t,n_forced*%d)", hlsSegmentSeconds))

	// Audio.
	if dec.TranscodeAudio {
		args = append(args, audioArgs(dec)...)
	} else {
		args = append(args, "-c:a", "aac", "-b:a", "192k") // HLS-safe default
	}

	// HLS output (VOD-style, keep all segments so the client can seek).
	args = append(args,
		"-f", "hls",
		"-hls_time", strconv.Itoa(hlsSegmentSeconds),
		"-hls_playlist_type", "event",
		"-hls_list_size", "0",
		"-hls_flags", "independent_segments",
		"-hls_segment_filename", filepath.Join(dir, "seg_%d.ts"),
		filepath.Join(dir, "stream.m3u8"),
	)
	return args
}

// topRung selects the single rendition height/bitrate for a transcode session:
// the highest ladder rung within source and cap limits.
func topRung(dec Decision, maxBitrateKbps int) Rung {
	// Source height is not carried on Decision; the streamer passes an
	// already-capped ladder. Use the ladder's top rung.
	rungs := LadderRungs(sourceHeightHint(dec), maxBitrateKbps)
	return rungs[0]
}

// sourceHeightHint returns a height used to size the ladder. The streamer sets
// this via the decision's video info when available; default 1080.
func sourceHeightHint(dec Decision) int {
	if dec.sourceHeight > 0 {
		return dec.sourceHeight
	}
	return 1080
}

// waitForFile polls until path exists or the timeout elapses.
func waitForFile(ctx context.Context, path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	t := time.NewTicker(100 * time.Millisecond)
	defer t.Stop()
	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			if time.Now().After(deadline) {
				return fmt.Errorf("timeout")
			}
		}
	}
}
