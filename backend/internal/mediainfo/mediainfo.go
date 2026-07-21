// Package mediainfo wraps ffprobe to extract authoritative technical metadata
// (codecs, resolution, HDR type, audio layout/Atmos, subtitle tracks) from
// media files, independent of any filename tags.
package mediainfo

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/rhymeswithlimo/northrou/backend/internal/model"
)

// Prober runs ffprobe against media files.
type Prober struct {
	ffprobePath string
	// deepDV enables a second, frame-level ffprobe to recover the Dolby Vision
	// profile when it is not present in the stream side-data. Off by default
	// because it reads a frame (slower); on for libraries with DV that only
	// exposes its configuration per-frame.
	deepDV bool
}

// New returns a Prober that shells out to the ffprobe at the given path.
func New(ffprobePath string, opts ...Option) *Prober {
	p := &Prober{ffprobePath: ffprobePath}
	for _, o := range opts {
		o(p)
	}
	return p
}

// Option configures a Prober.
type Option func(*Prober)

// WithDeepDolbyVision enables the frame-level Dolby Vision fallback probe.
func WithDeepDolbyVision(on bool) Option {
	return func(p *Prober) { p.deepDV = on }
}

// ffprobe JSON wire types (subset we consume).
type probeOutput struct {
	Format  probeFormat   `json:"format"`
	Streams []probeStream `json:"streams"`
}

type probeFormat struct {
	FormatName string `json:"format_name"`
	Duration   string `json:"duration"`
	Size       string `json:"size"`
	BitRate    string `json:"bit_rate"`
}

type probeStream struct {
	Index          int               `json:"index"`
	CodecType      string            `json:"codec_type"`
	CodecName      string            `json:"codec_name"`
	Profile        string            `json:"profile"`
	Width          int               `json:"width"`
	Height         int               `json:"height"`
	PixFmt         string            `json:"pix_fmt"`
	Channels       int               `json:"channels"`
	ChannelLayout  string            `json:"channel_layout"`
	ColorTransfer  string            `json:"color_transfer"`
	ColorPrimaries string            `json:"color_primaries"`
	BitRate        string            `json:"bit_rate"`
	Tags           map[string]string `json:"tags"`
	Disposition    map[string]int    `json:"disposition"`
	SideData       []sideData        `json:"side_data_list"`
}

// sideData is one ffprobe side-data entry. We read the type plus the Dolby
// Vision configuration fields (present on the "DOVI configuration record").
type sideData struct {
	Type       string `json:"side_data_type"`
	DVProfile  int    `json:"dv_profile"`
	DVBLCompat int    `json:"dv_bl_signal_compatibility_id"`
}

// imageCodecs are still-image codecs that appear as "video" streams but are
// actually embedded cover art / thumbnails, never the feature video.
var imageCodecs = map[string]bool{
	"mjpeg": true, "mjpg": true, "png": true, "bmp": true, "gif": true,
	"tiff": true, "webp": true,
}

// isCoverArt reports whether a video stream is embedded cover art rather than
// the real picture: either flagged attached_pic, or a still-image codec.
func isCoverArt(s probeStream) bool {
	return s.Disposition["attached_pic"] == 1 || imageCodecs[strings.ToLower(s.CodecName)]
}

// Probe runs ffprobe on path and returns normalized MediaFile technical fields.
// The caller sets ID/Path/Size/ModTime; this fills Container, Duration, Video,
// Audio, and Subtitles.
func (p *Prober) Probe(ctx context.Context, path string) (*model.MediaFile, error) {
	cmd := exec.CommandContext(ctx, p.ffprobePath,
		"-v", "error",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		path,
	)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("ffprobe %s: %s", path, strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("ffprobe %s: %w", path, err)
	}

	var po probeOutput
	if err := json.Unmarshal(out, &po); err != nil {
		return nil, fmt.Errorf("parse ffprobe json for %s: %w", path, err)
	}
	mf := assemble(po)
	mf.Path = path
	// Recover a Dolby Vision profile that only appears at the frame level.
	if p.deepDV && mf.Video.Codec == "hevc" && mf.Video.DVProfile == 0 {
		if profile, compat, ok := p.probeFrameDV(ctx, path); ok {
			mf.Video.DVProfile = profile
			mf.Video.DVBLCompat = compat
			mf.Video.HDR = model.HDRDolbyVision
		}
	}
	return mf, nil
}

// probeFrameDV reads the first video frame's side-data to recover a Dolby Vision
// configuration record absent from the stream-level probe. Cheap: one frame.
func (p *Prober) probeFrameDV(ctx context.Context, path string) (profile, compat int, ok bool) {
	cmd := exec.CommandContext(ctx, p.ffprobePath,
		"-v", "error", "-print_format", "json",
		"-select_streams", "v:0",
		"-read_intervals", "%+#1",
		"-show_frames", "-show_entries", "frame=side_data_list",
		path,
	)
	out, err := cmd.Output()
	if err != nil {
		return 0, 0, false
	}
	var fr struct {
		Frames []struct {
			SideData []sideData `json:"side_data_list"`
		} `json:"frames"`
	}
	if json.Unmarshal(out, &fr) != nil {
		return 0, 0, false
	}
	for _, f := range fr.Frames {
		for _, sd := range f.SideData {
			if sd.DVProfile > 0 {
				return sd.DVProfile, sd.DVBLCompat, true
			}
		}
	}
	return 0, 0, false
}

// assemble builds a MediaFile from parsed ffprobe output. Kept separate from the
// ffprobe exec so stream-selection logic (cover-art skipping, audio/subtitle
// classification) is unit-testable without a live ffprobe.
func assemble(po probeOutput) *model.MediaFile {
	mf := &model.MediaFile{
		Container: po.Format.FormatName,
		Duration:  parseFloat(po.Format.Duration),
	}

	var coverFallback *model.VideoStream
	for _, s := range po.Streams {
		switch s.CodecType {
		case "video":
			if s.Width <= 0 {
				continue
			}
			dvProfile, dvCompat := detectDolbyVision(s)
			vs := model.VideoStream{
				Index:      s.Index,
				Codec:      s.CodecName,
				Width:      s.Width,
				Height:     s.Height,
				HDR:        detectHDR(s),
				Profile:    s.Profile,
				BitRate:    parseInt(s.BitRate),
				PixFmt:     s.PixFmt,
				BitDepth:   bitDepthOf(s.PixFmt),
				DVProfile:  dvProfile,
				DVBLCompat: dvCompat,
			}
			// The first real (non-cover-art) video stream wins. Cover art is
			// kept only as a last-resort fallback for audio-with-artwork files.
			if isCoverArt(s) {
				if coverFallback == nil {
					c := vs
					coverFallback = &c
				}
				continue
			}
			if mf.Video.Codec == "" {
				mf.Video = vs
			}
		case "audio":
			title := s.Tags["title"]
			mf.Audio = append(mf.Audio, model.AudioStream{
				Index:         s.Index,
				Codec:         s.CodecName,
				Profile:       s.Profile,
				Channels:      s.Channels,
				ChannelLayout: s.ChannelLayout,
				Language:      s.Tags["language"],
				Title:         title,
				Atmos:         detectAtmos(s),
				Commentary:    isCommentary(title, s.Disposition),
				Default:       s.Disposition["default"] == 1,
				BitRate:       parseInt(s.BitRate),
			})
		case "subtitle":
			mf.Subtitles = append(mf.Subtitles, model.SubtitleStream{
				Index:    s.Index,
				Codec:    s.CodecName,
				Language: s.Tags["language"],
				Title:    s.Tags["title"],
				Forced:   s.Disposition["forced"] == 1,
				SDH:      isSDH(s.Tags["title"], s.Disposition),
				Default:  s.Disposition["default"] == 1,
			})
		}
	}
	// No real video stream (audio file with embedded artwork): use the cover.
	if mf.Video.Codec == "" && coverFallback != nil {
		mf.Video = *coverFallback
	}
	return mf
}

// bitDepthOf infers luma bit depth from an ffprobe pix_fmt string. Returns 0
// when unknown. Handles the common yuv/gbr planar formats (e.g. yuv420p10le).
func bitDepthOf(pixFmt string) int {
	f := strings.ToLower(pixFmt)
	switch {
	case f == "":
		return 0
	case strings.Contains(f, "p16"):
		return 16
	case strings.Contains(f, "p12"):
		return 12
	case strings.Contains(f, "p10"):
		return 10
	case strings.Contains(f, "p9"):
		return 9
	default:
		return 8
	}
}

// isCommentary reports whether an audio track is a commentary track, from its
// title tag or the ffprobe "comment" disposition.
func isCommentary(title string, disp map[string]int) bool {
	if disp["comment"] == 1 {
		return true
	}
	return strings.Contains(strings.ToLower(title), "commentary")
}

// isSDH reports whether a subtitle track is a hearing-impaired / SDH track,
// from its title tag or the ffprobe "hearing_impaired" disposition.
func isSDH(title string, disp map[string]int) bool {
	if disp["hearing_impaired"] == 1 {
		return true
	}
	t := strings.ToLower(title)
	return strings.Contains(t, "sdh") || strings.Contains(t, "hearing") ||
		strings.Contains(t, "(cc)") || strings.Contains(t, "[cc]")
}

// detectHDR classifies HDR from color metadata and side-data.
func detectHDR(s probeStream) model.HDRType {
	for _, sd := range s.SideData {
		t := strings.ToLower(sd.Type)
		switch {
		case strings.Contains(t, "dolby vision") || strings.Contains(t, "dovi"):
			return model.HDRDolbyVision
		case strings.Contains(t, "hdr dynamic") || strings.Contains(t, "hdr10+"):
			return model.HDR10Plus
		}
	}
	switch strings.ToLower(s.ColorTransfer) {
	case "smpte2084": // PQ
		return model.HDR10
	case "arib-std-b67": // HLG
		return model.HDRHLG
	}
	return model.HDRNone
}

// detectDolbyVision returns the DV profile and base-layer compatibility id from
// a stream's DOVI configuration record, or (0,0) when the stream is not DV.
func detectDolbyVision(s probeStream) (profile, compat int) {
	for _, sd := range s.SideData {
		if sd.DVProfile > 0 || strings.Contains(strings.ToLower(sd.Type), "dovi") {
			return sd.DVProfile, sd.DVBLCompat
		}
	}
	return 0, 0
}

// detectAtmos reports Dolby Atmos / object audio presence via the profile
// string ffprobe reports for TrueHD and E-AC3 JOC tracks.
func detectAtmos(s probeStream) bool {
	return strings.Contains(strings.ToLower(s.Profile), "atmos")
}

func parseFloat(s string) float64 {
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

func parseInt(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}
