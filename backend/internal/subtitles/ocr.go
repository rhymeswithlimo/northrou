package subtitles

import (
	"context"
	"image/png"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rhymeswithlimo/northrou/backend/internal/ffmpeg"
	"github.com/rhymeswithlimo/northrou/backend/internal/language"
)

// ocrWorker consumes OCR jobs: extract the PGS track to a .sup file, decode it
// into timed images, OCR each with Tesseract, and write a WebVTT file.
func (e *Extractor) ocrWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case j := <-e.jobs:
			e.waitWhilePlaying(ctx) // yield to active playback before OCR
			e.processOCR(ctx, j)
		}
	}
}

func (e *Extractor) processOCR(ctx context.Context, j ocrJob) {
	// Backstop: subtitle bitstreams are untrusted, and a decoder panic here would
	// otherwise take down the whole process (this runs in a background worker
	// goroutine, outside any HTTP Recoverer). Contain it to this one track. The
	// specific known panics (VobSub truncation, PGS oversized dims) are also
	// fixed at the source, but this guards against the next one.
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("OCR worker recovered from panic", "track", j.trackID, "panic", rec)
			_ = e.db.SetSubtitleVTT(ctx, j.trackID, "", "failed")
		}
	}()

	_ = e.db.SetSubtitleVTT(ctx, j.trackID, "", "processing")

	work, err := os.MkdirTemp("", "northrou-ocr-*")
	if err != nil {
		e.failOCR(ctx, j.trackID, "tempdir", err)
		return
	}
	defer os.RemoveAll(work)

	subs, err := e.imagesForJob(ctx, work, j)
	if err != nil {
		e.failOCR(ctx, j.trackID, "decode", err)
		return
	}

	lang := language.Tesseract(j.language)
	var cues []Cue
	for i, s := range subs {
		imgPath := filepath.Join(work, "cue"+strconv.Itoa(i)+".png")
		if err := writePNG(imgPath, s); err != nil {
			continue
		}
		text, err := e.tesseractOCR(ctx, imgPath, lang)
		if err != nil {
			slog.Debug("tesseract failed", "err", err)
			continue
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		cues = append(cues, Cue{Start: s.Start, End: s.End, Text: text})
	}

	if len(cues) == 0 {
		_ = e.db.SetSubtitleVTT(ctx, j.trackID, "", "failed")
		return
	}
	out := e.vttPath(j.trackID)
	if err := WriteVTT(out, cues); err != nil {
		e.failOCR(ctx, j.trackID, "write vtt", err)
		return
	}
	_ = e.db.SetSubtitleVTT(ctx, j.trackID, out, "done")
	slog.Info("OCR complete", "track", j.trackID, "cues", len(cues))
}

// imagesForJob produces the timed subtitle images for an OCR job, decoding
// whichever source format the job carries.
func (e *Extractor) imagesForJob(ctx context.Context, work string, j ocrJob) ([]pgsSub, error) {
	switch j.kind {
	case ocrVobSubExtern:
		return ParseVobSub(j.idxPath, j.subPath)
	case ocrVobSubEmbed:
		idx, sub, err := e.extractVobSub(ctx, work, j.filePath, j.streamIndex)
		if err != nil {
			return nil, err
		}
		return ParseVobSub(idx, sub)
	default: // PGS
		sup := filepath.Join(work, "track.sup")
		cmd := exec.CommandContext(ctx, e.ffmpegPath,
			"-y", "-i", j.filePath,
			"-map", "0:"+strconv.Itoa(j.streamIndex),
			"-c:s", "copy", "-f", "sup", sup,
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			slog.Debug("pgs extract failed", "err", lastLine(out))
			return nil, err
		}
		return ParseSUP(sup)
	}
}

// extractVobSub uses ffmpeg to demux an embedded dvd_subtitle stream to a
// .idx/.sub pair, returning both paths.
func (e *Extractor) extractVobSub(ctx context.Context, work, filePath string, streamIndex int) (idx, sub string, err error) {
	idx = filepath.Join(work, "track.idx")
	sub = filepath.Join(work, "track.sub")
	cmd := exec.CommandContext(ctx, e.ffmpegPath,
		"-y", "-i", filePath,
		"-map", "0:"+strconv.Itoa(streamIndex),
		"-c:s", "copy", idx,
	)
	if out, cerr := cmd.CombinedOutput(); cerr != nil {
		slog.Debug("vobsub extract failed", "err", lastLine(out))
		return "", "", cerr
	}
	return idx, sub, nil
}

// tesseractOCR runs tesseract on an image and returns recognized text.
func (e *Extractor) tesseractOCR(ctx context.Context, imgPath, lang string) (string, error) {
	// tesseract <img> stdout -l <lang> --psm 6  (assume a block of text)
	cmd := exec.CommandContext(ctx, e.tesseract, imgPath, "stdout", "-l", lang, "--psm", "6")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func (e *Extractor) failOCR(ctx context.Context, trackID int64, stage string, err error) {
	slog.Debug("OCR job failed", "track", trackID, "stage", stage, "err", err)
	_ = e.db.SetSubtitleVTT(ctx, trackID, "", "failed")
}

func writePNG(path string, s pgsSub) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, s.Img)
}

// DetectTesseract returns the path to a usable tesseract binary, or "" if none
// is available. It checks PATH and the managed bin dir.
func DetectTesseract(managedBinDir string) string {
	if p, err := exec.LookPath("tesseract"); err == nil {
		return p
	}
	candidate := filepath.Join(managedBinDir, ffmpeg.ExecName("tesseract"))
	if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
		return candidate
	}
	return ""
}
