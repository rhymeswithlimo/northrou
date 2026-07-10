// Package service installs, removes, and runs Northrou as a native OS service
// (systemd on Linux, launchd on macOS, the Windows Service Control Manager) via
// a single cross-platform abstraction.
package service

import (
	"context"
	"fmt"
	"log/slog"

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
		Arguments:   []string{"serve", "--no-browser", "--config", configPath},
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
