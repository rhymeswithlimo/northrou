package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/rhymeswithlimo/northrou/backend/internal/buildinfo"
	"github.com/rhymeswithlimo/northrou/backend/internal/config"
	"github.com/rhymeswithlimo/northrou/backend/internal/service"
	"github.com/rhymeswithlimo/northrou/backend/internal/setup"
	"github.com/spf13/cobra"
)

// newStatusCmd answers "what is my server doing, and what should I do next?"
// in one place: service state, addresses, setup progress, remote access,
// library counts, and - when something is missing - the exact command that
// fixes it.
func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show what this server is doing and what to do next",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
			defer cancel()
			fmt.Print(statusReport(ctx, flagConfigPath))
			return nil
		},
	}
}

// statusReport assembles the whole status output. Every probe is best effort:
// a stopped server or an unreadable config narrows the report, it never
// aborts it.
func statusReport(ctx context.Context, configPath string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Northrou %s\n", orDash(buildinfo.Version))

	// Config: without it there is no port to probe, so defaults fill in.
	cfg, err := config.Load(configPath)
	configured := err == nil
	if !configured {
		cfg = config.Default()
	}

	// Service state, from the OS service manager.
	svcState := "unknown"
	if st, err := service.GetStatus(configPath); err == nil {
		svcState = st.String()
	}

	// The running daemon, if any, over its local API.
	port := cfg.Server.Port
	probe := probeServer(ctx, port)

	running := probe.healthy
	switch {
	case running && svcState == "running":
		fmt.Fprintf(&b, "  Service:  running\n")
	case running:
		// Answering on the port but not via the service manager: a foreground
		// `northrou serve` or `northrou setup`.
		fmt.Fprintf(&b, "  Service:  %s (but a server is running in a terminal)\n", svcState)
	default:
		fmt.Fprintf(&b, "  Service:  %s\n", svcState)
	}

	name := cfg.DisplayName()
	if probe.serverName != "" {
		name = probe.serverName
	}
	fmt.Fprintf(&b, "  Server:   %s\n", name)
	if running {
		fmt.Fprintf(&b, "  Address:  http://localhost:%d/", port)
		for _, ip := range setup.LocalIPv4s() {
			fmt.Fprintf(&b, "  http://%s:%d/", ip, port)
		}
		b.WriteString("\n")
	}

	// Setup / remote / library, best effort.
	setupDone := configured && cfg.Remote.ConnectionCode != ""
	if running {
		setupDone = !probe.needsSetup
	}
	if setupDone {
		switch {
		case cfg.Remote.Enabled && cfg.Remote.ConnectionCode != "":
			fmt.Fprintf(&b, "  Remote:   on — connection code %s\n", cfg.Remote.ConnectionCode)
		case cfg.Remote.Enabled:
			b.WriteString("  Remote:   on — no connection code yet (run 'northrou setup')\n")
		default:
			b.WriteString("  Remote:   off (home network only)\n")
		}
	}

	if probe.libraryKnown {
		scan := ""
		if probe.scanRunning {
			scan = " — scanning now"
		} else if probe.unmatched > 0 {
			scan = fmt.Sprintf(" — %d unmatched file(s), fix in Settings", probe.unmatched)
		}
		fmt.Fprintf(&b, "  Library:  %d movie(s), %d show(s)%s\n", probe.movies, probe.shows, scan)
	}
	if running {
		ff := "ready"
		if !probe.ffmpegReady {
			ff = "still downloading (streaming needs it)"
		}
		fmt.Fprintf(&b, "  ffmpeg:   %s\n", ff)
		fmt.Fprintf(&b, "  Streams:  %d active\n", probe.activeStreams)
	}

	// Next steps, most fundamental first. One is usually enough to print.
	var next []string
	switch {
	case !setupDone:
		next = append(next, "Run 'northrou setup' to set up this server.")
	case !running && svcState == service.StatusNotInstalled.String():
		next = append(next, "Start it: 'sudo northrou install' (background service) or 'northrou serve' (foreground).")
	case !running && svcState == "stopped":
		next = append(next, "Start it: 'sudo northrou start'.")
	case running && len(cfg.Media.MovieDirs)+len(cfg.Media.ShowDirs) == 0 && probe.movies+probe.shows == 0:
		next = append(next, "No media folders yet: run 'northrou setup' or add them in 'northrou admin' → Library.")
	}
	for _, n := range next {
		fmt.Fprintf(&b, "\n%s\n", n)
	}
	return b.String()
}

// serverProbe is what the local API told us about the running daemon.
type serverProbe struct {
	healthy      bool
	needsSetup   bool
	serverName   string
	ffmpegReady  bool
	activeStreams int
	scanRunning  bool
	unmatched    int
	movies       int
	shows        int
	libraryKnown bool
}

// probeServer queries the daemon's local API: health, setup status, then - via
// a local pair - the admin surface. Every step tolerates failure; the returned
// struct just knows less.
func probeServer(ctx context.Context, port int) serverProbe {
	var p serverProbe
	base := fmt.Sprintf("http://localhost:%d", port)
	client := &http.Client{Timeout: 3 * time.Second}

	get := func(path, token string, out any) error {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		return json.NewDecoder(resp.Body).Decode(out)
	}

	var health struct {
		Status string `json:"status"`
	}
	if get("/api/health", "", &health) != nil {
		return p
	}
	p.healthy = true

	var st struct {
		NeedsSetup bool   `json:"needs_setup"`
		ServerName string `json:"server_name"`
	}
	if get("/api/setup/status", "", &st) == nil {
		p.needsSetup = st.NeedsSetup
		p.serverName = st.ServerName
	}
	if p.needsSetup {
		return p
	}

	// Pair locally (trusted, no code) for the admin reads. Ephemeral, so a
	// status check never shows up as a paired "device".
	var login struct {
		AccessToken string `json:"access_token"`
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/auth/pair",
		bytes.NewReader([]byte(`{"ephemeral":true,"device_name":"Northrou CLI"}`)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return p
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&login) != nil {
		return p
	}
	tok := login.AccessToken

	var hw struct {
		FFmpegReady      bool `json:"ffmpeg_ready"`
		ActiveTranscodes int  `json:"active_transcodes"`
	}
	if get("/api/admin/hardware", tok, &hw) == nil {
		p.ffmpegReady = hw.FFmpegReady
	}
	var streams struct {
		Count int `json:"count"`
	}
	if get("/api/admin/streams", tok, &streams) == nil {
		p.activeStreams = streams.Count
	}
	var scan struct {
		Running   bool `json:"running"`
		Unmatched int  `json:"unmatched"`
	}
	if get("/api/admin/scan", tok, &scan) == nil {
		p.scanRunning = scan.Running
		p.unmatched = scan.Unmatched
	}
	var movies, shows []json.RawMessage
	if get("/api/movies", tok, &movies) == nil && get("/api/shows", tok, &shows) == nil {
		p.movies, p.shows = len(movies), len(shows)
		p.libraryKnown = true
	}
	return p
}

func orDash(s string) string {
	if s == "" {
		return "(dev)"
	}
	return s
}
