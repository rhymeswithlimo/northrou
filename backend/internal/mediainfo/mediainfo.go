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
}

// New returns a Prober that shells out to the ffprobe at the given path.
func New(ffprobePath string) *Prober {
	return &Prober{ffprobePath: ffprobePath}
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
	Index         int               `json:"index"`
	CodecType     string            `json:"codec_type"`
	CodecName     string            `json:"codec_name"`
	Profile       string            `json:"profile"`
	Width         int               `json:"width"`
	Height        int               `json:"height"`
	Channels      int               `json:"channels"`
	ChannelLayout string            `json:"channel_layout"`
	ColorTransfer string            `json:"color_transfer"`
	ColorPrimaries string           `json:"color_primaries"`
	BitRate       string            `json:"bit_rate"`
	Tags          map[string]string `json:"tags"`
	Disposition   map[string]int    `json:"disposition"`
	SideData      []struct {
		Type string `json:"side_data_type"`
	} `json:"side_data_list"`
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

	mf := &model.MediaFile{
		Path:      path,
		Container: po.Format.FormatName,
		Duration:  parseFloat(po.Format.Duration),
	}

	for _, s := range po.Streams {
		switch s.CodecType {
		case "video":
			// First real video stream wins (skip attached cover art).
			if mf.Video.Codec == "" && s.Width > 0 {
				mf.Video = model.VideoStream{
					Index:   s.Index,
					Codec:   s.CodecName,
					Width:   s.Width,
					Height:  s.Height,
					HDR:     detectHDR(s),
					Profile: s.Profile,
					BitRate: parseInt(s.BitRate),
				}
			}
		case "audio":
			mf.Audio = append(mf.Audio, model.AudioStream{
				Index:         s.Index,
				Codec:         s.CodecName,
				Profile:       s.Profile,
				Channels:      s.Channels,
				ChannelLayout: s.ChannelLayout,
				Language:      s.Tags["language"],
				Atmos:         detectAtmos(s),
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
				Default:  s.Disposition["default"] == 1,
			})
		}
	}
	return mf, nil
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
