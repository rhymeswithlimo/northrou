package ffmpeg

import "runtime"

// asset is a downloadable release archive.
type asset struct {
	URL  string
	Kind archiveKind
	// SHA256 is the hex checksum of the archive. When non-empty it is
	// verified after download. These MUST be pinned per Northrou release; an
	// empty value downloads with a logged warning (dev convenience only).
	SHA256 string
}

// release describes where to obtain static ffmpeg/ffprobe for one platform.
// Either Bundle (one archive containing both binaries) or the FFmpeg/FFprobe
// pair (separate archives) is set.
type release struct {
	Bundle  *asset
	FFmpeg  *asset
	FFprobe *asset
}

// releases maps "GOOS/GOARCH" to its static-build source.
//
// Sources:
//   - Linux amd64/arm64:  BtbN/FFmpeg-Builds (GPL static, binaries under bin/)
//   - Linux arm (v7):     johnvansickle armhf static
//   - Windows amd64:      BtbN win64 GPL static
//   - macOS amd64/arm64:  evermeet.cx (ffmpeg + ffprobe packaged separately)
//
// URLs point at stable "latest" endpoints; checksums are pinned at release
// time. Extraction matches binaries by base name so exact internal archive
// layout does not matter.
var releases = map[string]release{
	"linux/amd64": {Bundle: &asset{
		URL:  "https://github.com/BtbN/FFmpeg-Builds/releases/download/latest/ffmpeg-master-latest-linux64-gpl.tar.xz",
		Kind: kindTarXz,
	}},
	"linux/arm64": {Bundle: &asset{
		URL:  "https://github.com/BtbN/FFmpeg-Builds/releases/download/latest/ffmpeg-master-latest-linuxarm64-gpl.tar.xz",
		Kind: kindTarXz,
	}},
	"linux/arm": {Bundle: &asset{
		URL:  "https://johnvansickle.com/ffmpeg/releases/ffmpeg-release-armhf-static.tar.xz",
		Kind: kindTarXz,
	}},
	"windows/amd64": {Bundle: &asset{
		URL:  "https://github.com/BtbN/FFmpeg-Builds/releases/download/latest/ffmpeg-master-latest-win64-gpl.zip",
		Kind: kindZip,
	}},
	"darwin/amd64": {
		FFmpeg:  &asset{URL: "https://evermeet.cx/ffmpeg/getrelease/ffmpeg/zip", Kind: kindZip},
		FFprobe: &asset{URL: "https://evermeet.cx/ffmpeg/getrelease/ffprobe/zip", Kind: kindZip},
	},
	// evermeet ships x86_64 builds; on Apple Silicon they run under Rosetta 2.
	// Native arm64 users may install ffmpeg via Homebrew and set
	// transcode.prefer_system_ffmpeg = true.
	"darwin/arm64": {
		FFmpeg:  &asset{URL: "https://evermeet.cx/ffmpeg/getrelease/ffmpeg/zip", Kind: kindZip},
		FFprobe: &asset{URL: "https://evermeet.cx/ffmpeg/getrelease/ffprobe/zip", Kind: kindZip},
	},
}

// releaseFor returns the release descriptor for the running platform.
func releaseFor() (release, bool) {
	r, ok := releases[runtime.GOOS+"/"+runtime.GOARCH]
	return r, ok
}
