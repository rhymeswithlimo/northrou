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
	_ = e.db.SetSubtitleVTT(ctx, j.trackID, "", "processing")

	work, err := os.MkdirTemp("", "northrou-pgs-*")
	if err != nil {
		e.failOCR(ctx, j.trackID, "tempdir", err)
		return
	}
	defer os.RemoveAll(work)

	sup := filepath.Join(work, "track.sup")
	// Extract the PGS stream unchanged into a .sup container.
	cmd := exec.CommandContext(ctx, e.ffmpegPath,
		"-y", "-i", j.filePath,
		"-map", "0:"+strconv.Itoa(j.streamIndex),
		"-c:s", "copy", "-f", "sup", sup,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		e.failOCR(ctx, j.trackID, "extract sup", err)
		slog.Debug("pgs extract failed", "err", lastLine(out))
		return
	}

	subs, err := ParseSUP(sup)
	if err != nil {
		e.failOCR(ctx, j.trackID, "parse sup", err)
		return
	}

	lang := ocrLang(j.language)
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

// ocrLang maps an MKV/ISO language code to a Tesseract language, defaulting to
// English.
func ocrLang(lang string) string {
	lang = strings.ToLower(strings.TrimSpace(lang))
	switch lang {
	case "", "und":
		return "eng"
	case "en":
		return "eng"
	case "es":
		return "spa"
	case "fr":
		return "fra"
	case "de":
		return "deu"
	case "it":
		return "ita"
	case "pt":
		return "por"
	case "ja":
		return "jpn"
	case "zh":
		return "chi_sim"
	default:
		return lang // already ISO 639-2 (eng, spa, ...) in most MKVs
	}
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
