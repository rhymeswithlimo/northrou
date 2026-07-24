// Package service installs, removes, and runs Northrou as a native OS service
// (systemd on Linux, launchd on macOS, the Windows Service Control Manager) via
// a single cross-platform abstraction.
package service

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"
	"strings"

	"github.com/kardianos/service"
	"github.com/rhymeswithlimo/northrou/backend/internal/app"
)

const serviceName = "northrou"

// program adapts Northrou's app lifecycle to the kardianos service interface.
type program struct {
	configPath string
	cancel     context.CancelFunc
	done       chan struct{}
	openSetup  bool
}

// New constructs the OS service handle plus its program. configPath is passed
// through to the daemon so the service and interactive runs share config.
func New(configPath string) (service.Service, *program, error) {
	cfg := &service.Config{
		Name:        serviceName,
		DisplayName: "Northrou Media Server",
		Description: "Self-hosted media server for your physical collection.",
		Arguments:   []string{"serve", "--config", configPath},
		// Windows only (kardianos ignores these keys elsewhere): an abrupt
		// process exit (e.g. self-update replacing the binary and exiting to
		// pick it up) registers with the SCM as a failure, so it needs an
		// explicit recovery action to come back. systemd (Restart=always) and
		// launchd (KeepAlive) already restart on any exit by default. Using
		// plain string keys here, not the service.OnFailure* constants, since
		// those are only declared in kardianos's Windows-only build file.
		Option: service.KeyValue{
			"OnFailure":              "restart",
			"OnFailureDelayDuration": "5s",
		},
	}
	prog := &program{configPath: configPath, done: make(chan struct{})}
	svc, err := service.New(prog, cfg)
	if err != nil {
		return nil, nil, err
	}
	return svc, prog, nil
}

// Interactive reports whether the process is running in a terminal (true) as
// opposed to under a service manager (false).
func Interactive() bool {
	return service.Interactive()
}

// Start is called by the service manager; it launches the daemon in the
// background and returns immediately.
func (p *program) Start(s service.Service) error {
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	go p.run(ctx)
	return nil
}

func (p *program) run(ctx context.Context) {
	defer close(p.done)
	a, err := app.New(p.configPath)
	if err != nil {
		slog.Error("service failed to start", "err", err)
		return
	}
	defer a.Close()
	// Auto-update relies on the service manager restarting the process after
	// it applies an update and exits; only turn it on for this managed path,
	// never for a foreground `northrou serve`.
	a.RunAsService = true
	if err := a.Run(ctx); err != nil {
		slog.Error("service run error", "err", err)
	}
}

// Stop is called by the service manager on shutdown.
func (p *program) Stop(s service.Service) error {
	if p.cancel != nil {
		p.cancel()
	}
	<-p.done
	return nil
}

// Install registers the service with the OS and starts it.
func Install(configPath string) error {
	svc, _, err := New(configPath)
	if err != nil {
		return err
	}
	if err := svc.Install(); err != nil {
		return fmt.Errorf("install service: %w", err)
	}
	if err := svc.Start(); err != nil {
		return fmt.Errorf("start service: %w", err)
	}
	return nil
}

// Start starts the installed service.
func Start(configPath string) error {
	svc, _, err := New(configPath)
	if err != nil {
		return err
	}
	if err := svc.Start(); err != nil {
		return fmt.Errorf("start service: %w", err)
	}
	return nil
}

// Stop stops the installed service.
func Stop(configPath string) error {
	svc, _, err := New(configPath)
	if err != nil {
		return err
	}
	if err := svc.Stop(); err != nil {
		return fmt.Errorf("stop service: %w", err)
	}
	return nil
}

// Restart restarts the installed service.
func Restart(configPath string) error {
	svc, _, err := New(configPath)
	if err != nil {
		return err
	}
	if err := svc.Restart(); err != nil {
		return fmt.Errorf("restart service: %w", err)
	}
	return nil
}

// Status describes the installed service's state in operator terms.
type Status int

const (
	StatusUnknown Status = iota
	StatusNotInstalled
	StatusRunning
	StatusStopped
)

func (s Status) String() string {
	switch s {
	case StatusNotInstalled:
		return "not installed"
	case StatusRunning:
		return "running"
	case StatusStopped:
		return "stopped"
	}
	return "unknown"
}

// GetStatus reports whether the service is installed and running. An error
// means the state could not be determined (e.g. no permission to ask the
// service manager), which is distinct from a clean "not installed".
func GetStatus(configPath string) (Status, error) {
	svc, _, err := New(configPath)
	if err != nil {
		return StatusUnknown, err
	}
	st, err := svc.Status()
	if err != nil {
		if errors.Is(err, service.ErrNotInstalled) {
			return StatusNotInstalled, nil
		}
		return StatusUnknown, err
	}
	switch st {
	case service.StatusRunning:
		return StatusRunning, nil
	case service.StatusStopped:
		return StatusStopped, nil
	}
	return StatusUnknown, nil
}

// logindConfPath is systemd-logind's config file; a var so tests can point it
// elsewhere without touching the real system file.
const logindConfPath = "/etc/systemd/logind.conf"

// LidSwitchWarning returns a message when this machine is configured to
// suspend on lid close, empty otherwise (including on non-Linux, or if the
// setting can't be determined). Northrou is often installed on a laptop
// pressed into service as a home server; a closed lid then either actually
// suspends it (streaming and scans stop cold) or, if suspend.target happens
// to be masked, sends systemd-logind into a tight suspend-retry loop that
// starves the CPU instead. Neither is fixed here: editing /etc/systemd/
// logind.conf is a system-wide, hard-to-notice change to make on someone's
// behalf, so this only surfaces it and lets the operator decide.
func LidSwitchWarning() string {
	f, err := os.Open(logindConfPath)
	if err != nil {
		return ""
	}
	defer f.Close()
	return lidSwitchWarning(runtime.GOOS, f)
}

// lidSwitchWarning is the testable core: it takes goos and an already-open
// reader over a logind.conf-formatted file, so behavior can be checked with a
// table test on any platform (see CLAUDE.md's goos-parameter convention).
//
// A home server is effectively always on AC power, so the setting that
// governs it is HandleLidSwitchExternalPower - and per logind.conf(5), that
// key defaults to whatever HandleLidSwitch is set to when left unset itself.
func lidSwitchWarning(goos string, conf io.Reader) string {
	if goos != "linux" {
		return ""
	}
	lidSwitch := "suspend" // systemd's documented default when unset
	extPower := ""         // "" = not explicitly set; falls back to lidSwitch
	sc := bufio.NewScanner(conf)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		val := strings.ToLower(strings.TrimSpace(v))
		switch {
		case strings.EqualFold(strings.TrimSpace(k), "HandleLidSwitchExternalPower"):
			extPower = val
		case strings.EqualFold(strings.TrimSpace(k), "HandleLidSwitch"):
			lidSwitch = val
		}
	}
	if sc.Err() != nil {
		return ""
	}
	handling := extPower
	if handling == "" {
		handling = lidSwitch
	}
	if handling == "" || handling == "ignore" {
		return ""
	}
	return fmt.Sprintf("this machine is configured to %q on lid close (systemd-logind's "+
		"HandleLidSwitchExternalPower, falling back to HandleLidSwitch). If it's running "+
		"Northrou as a server, closing the lid will interrupt streams and in-progress "+
		"scans. To keep it running headless:\n"+
		"  sudo sed -i 's/^#\\?HandleLidSwitch=.*/HandleLidSwitch=ignore/; "+
		"s/^#\\?HandleLidSwitchExternalPower=.*/HandleLidSwitchExternalPower=ignore/' "+logindConfPath+"\n"+
		"  sudo systemctl restart systemd-logind", handling)
}

// Uninstall stops and removes the service.
func Uninstall(configPath string) error {
	svc, _, err := New(configPath)
	if err != nil {
		return err
	}
	_ = svc.Stop()
	if err := svc.Uninstall(); err != nil {
		return fmt.Errorf("uninstall service: %w", err)
	}
	return nil
}

// RunManaged runs the daemon under the service manager's control (blocks,
// dispatching Start/Stop). Call this from `serve` when Interactive() is false.
func RunManaged(configPath string) error {
	svc, _, err := New(configPath)
	if err != nil {
		return err
	}
	return svc.Run()
}
