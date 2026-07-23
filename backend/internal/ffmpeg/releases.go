package ffmpeg

import "runtime"

// asset is a downloadable release archive.
type asset struct {
	URL  string
	Kind archiveKind
	// SHA256 is the hex checksum of the archive. When non-empty it is verified
	// after download (hard failure on mismatch, see manager.downloadAndExtract).
	//
	// It is currently empty for every asset, which downloads over HTTPS with a
	// logged warning. Do NOT naively pin a hash here against the URLs below:
	// they are rolling "latest" endpoints whose bytes change whenever upstream
	// rebuilds, so a static pin would hard-fail every download within days.
	// Pinning safely requires first switching to immutable versioned URLs, or
	// verifying against upstream's own published checksum at download time
	// (SHA256URL below). See the note above releases.
	SHA256 string
	// SHA256URL, when set, is fetched at download time and the archive is
	// verified against the checksum it contains. This is the rolling-URL-safe
	// path: the checksum tracks the same moving build as the archive. The body
	// may be a bare hex digest or an sha256sum-style "<hex>  <name>" listing (the
	// line whose name matches the archive's base name is used). Verification is
	// still hard-fail on mismatch. Populate per upstream that publishes a
	// SHA-256 alongside its build (BtbN, evermeet); sources that publish no clean
	// SHA-256 (johnvansickle) stay unpinned until an alternative is found.
	SHA256URL string
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
// These are rolling "latest"/"getrelease" endpoints: the same URL serves
// freshly rebuilt bytes over time (BtbN republishes its `latest` tag roughly
// daily). That is why asset.SHA256 is NOT statically pinned here - a pin
// against a moving target hard-fails once upstream rebuilds. Proper integrity
// verification needs immutable versioned URLs or download-time checks against
// upstream's own published checksums; tracked as a known limitation, not done.
// Extraction matches binaries by base name so exact internal archive layout
// does not matter.
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
