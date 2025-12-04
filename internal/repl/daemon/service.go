package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/kardianos/service"
	"github.com/willibrandon/steep/internal/repl/config"
)

// Exit codes for CLI commands
const (
	ExitSuccess          = 0
	ExitPermissionDenied = 1
	ExitServiceExists    = 2
	ExitConfigError      = 3
	ExitServiceNotFound  = 1
	ExitAlreadyRunning   = 2
	ExitStartFailed      = 3
	ExitNotRunning       = 1
	ExitStopFailed       = 2
	ExitRestartFailed    = 2
	ExitStopped          = 2
	ExitUnhealthy        = 3
)

// ServiceConfig holds configuration for creating the service.
type ServiceConfig struct {
	ConfigPath string
	UserMode   bool
	Debug      bool
}

// program implements the service.Program interface for kardianos/service.
type program struct {
	daemon     *Daemon
	cfg        *config.Config
	configPath string
	debug      bool
	exit       chan struct{}
}

// Start is called when the service starts.
// Per kardianos/service, this must return quickly - start work in goroutine.
func (p *program) Start(s service.Service) error {
	// Load configuration
	var cfg *config.Config
	var err error

	if p.configPath != "" {
		cfg, err = config.LoadFromPath(p.configPath)
	} else {
		cfg, err = config.Load()
	}
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	p.cfg = cfg

	// Create daemon
	d, err := New(cfg, p.debug)
	if err != nil {
		return fmt.Errorf("failed to create daemon: %w", err)
	}
	p.daemon = d

	// Start daemon in goroutine
	go func() {
		if err := p.daemon.Start(); err != nil {
			// Log error but don't crash - service manager will restart
			fmt.Fprintf(os.Stderr, "Daemon start error: %v\n", err)
		}
	}()

	return nil
}

// Stop is called when the service stops.
// Per kardianos/service, this should complete gracefully.
func (p *program) Stop(s service.Service) error {
	if p.daemon != nil {
		return p.daemon.Stop()
	}
	return nil
}

// NewService creates a new service instance.
func NewService(svcConfig ServiceConfig) (service.Service, error) {
	prg := &program{
		configPath: svcConfig.ConfigPath,
		debug:      svcConfig.Debug,
		exit:       make(chan struct{}),
	}

	cfg := &service.Config{
		Name:        "steep-repl",
		DisplayName: "Steep Replication Daemon",
		Description: "Background daemon that coordinates bidirectional replication across PostgreSQL 18 instances.",
	}

	// Auto-detect if this is a user service by checking if plist exists in LaunchAgents
	userMode := svcConfig.UserMode
	if !userMode {
		userMode = isUserServiceInstalled()
	}

	// Set service options based on user mode
	if userMode {
		cfg.Option = service.KeyValue{
			"UserService": true,
		}
	}

	// Platform-specific configuration
	switch runtime.GOOS {
	case "darwin":
		// macOS launchd configuration
		cfg.Option = mergeOptions(cfg.Option, service.KeyValue{
			"KeepAlive":      true,
			"RunAtLoad":      true,
			"LaunchOnlyOnce": false,
		})
	case "linux":
		// systemd configuration
		cfg.Option = mergeOptions(cfg.Option, service.KeyValue{
			"Restart": "on-failure",
		})
	case "windows":
		// Windows service recovery configuration
		cfg.Option = mergeOptions(cfg.Option, service.KeyValue{
			"OnFailure":              "restart",
			"OnFailureDelayDuration": "5s",
			"OnFailureResetPeriod":   10,
		})
	}

	// Add arguments for service execution
	if svcConfig.ConfigPath != "" {
		cfg.Arguments = []string{"run", "--config", svcConfig.ConfigPath}
	} else {
		cfg.Arguments = []string{"run"}
	}

	if svcConfig.Debug {
		cfg.Arguments = append(cfg.Arguments, "--debug")
	}

	return service.New(prg, cfg)
}

// mergeOptions merges two KeyValue maps.
func mergeOptions(base, additional service.KeyValue) service.KeyValue {
	if base == nil {
		base = service.KeyValue{}
	}
	for k, v := range additional {
		base[k] = v
	}
	return base
}

// Install installs the service.
func Install(svcConfig ServiceConfig) error {
	svc, err := NewService(svcConfig)
	if err != nil {
		return fmt.Errorf("failed to create service: %w", err)
	}

	// Check if already installed
	status, err := svc.Status()
	if err == nil && status != service.StatusUnknown {
		return fmt.Errorf("service already installed")
	}

	if err := svc.Install(); err != nil {
		// Check for permission error
		if os.IsPermission(err) {
			return &PermissionError{Err: err}
		}
		return fmt.Errorf("failed to install service: %w", err)
	}

	return nil
}

// Uninstall removes the service.
func Uninstall() error {
	svc, err := NewService(ServiceConfig{})
	if err != nil {
		return fmt.Errorf("failed to create service: %w", err)
	}

	// Check if installed
	status, err := svc.Status()
	if err != nil || status == service.StatusUnknown {
		return fmt.Errorf("service not installed")
	}

	// Stop if running
	if status == service.StatusRunning {
		_ = svc.Stop()
	}

	if err := svc.Uninstall(); err != nil {
		if os.IsPermission(err) {
			return &PermissionError{Err: err}
		}
		return fmt.Errorf("failed to uninstall service: %w", err)
	}

	return nil
}

// StartService starts the installed service.
func StartService() error {
	svc, err := NewService(ServiceConfig{})
	if err != nil {
		return fmt.Errorf("failed to create service: %w", err)
	}

	status, err := svc.Status()
	if err != nil {
		return fmt.Errorf("service not installed")
	}

	if status == service.StatusRunning {
		return fmt.Errorf("service already running")
	}

	// Try to start the service
	if err := svc.Start(); err != nil {
		// On macOS, if launchd throttled the service due to repeated failures,
		// try to recover by unloading and reloading
		if runtime.GOOS == "darwin" {
			if recoverErr := recoverLaunchdService(); recoverErr == nil {
				// Retry start after recovery
				if retryErr := svc.Start(); retryErr != nil {
					return fmt.Errorf("failed to start service after recovery: %w", retryErr)
				}
			} else {
				return fmt.Errorf("failed to start service: %w", err)
			}
		} else {
			return fmt.Errorf("failed to start service: %w", err)
		}
	}

	// Verify the service actually started
	time.Sleep(500 * time.Millisecond)
	status, err = svc.Status()
	if err != nil || status != service.StatusRunning {
		return fmt.Errorf("service failed to start (check logs)")
	}

	return nil
}

// recoverLaunchdService attempts to recover a throttled launchd service.
func recoverLaunchdService() error {
	plistPath := "/Library/LaunchDaemons/steep-repl.plist"
	if isUserServiceInstalled() {
		home, _ := os.UserHomeDir()
		plistPath = filepath.Join(home, "Library", "LaunchAgents", "steep-repl.plist")
	}

	// Try bootout first (modern launchctl)
	domain := "system"
	if isUserServiceInstalled() {
		domain = fmt.Sprintf("gui/%d", os.Getuid())
	}

	// Bootout to clear throttle state
	exec.Command("launchctl", "bootout", domain+"/steep-repl").Run()
	time.Sleep(100 * time.Millisecond)

	// Bootstrap to reload
	cmd := exec.Command("launchctl", "bootstrap", domain, plistPath)
	return cmd.Run()
}

// StopService stops the running service.
func StopService() error {
	svc, err := NewService(ServiceConfig{})
	if err != nil {
		return fmt.Errorf("failed to create service: %w", err)
	}

	status, err := svc.Status()
	if err != nil {
		return fmt.Errorf("service not installed")
	}

	if status != service.StatusRunning {
		return fmt.Errorf("service not running")
	}

	if err := svc.Stop(); err != nil {
		return fmt.Errorf("failed to stop service: %w", err)
	}

	return nil
}

// Restart restarts the service.
func Restart() error {
	svc, err := NewService(ServiceConfig{})
	if err != nil {
		return fmt.Errorf("failed to create service: %w", err)
	}

	status, err := svc.Status()
	if err != nil {
		return fmt.Errorf("service not installed")
	}

	if err := svc.Restart(); err != nil {
		return fmt.Errorf("failed to restart service: %w", err)
	}
	_ = status // suppress unused warning

	return nil
}

// ServiceStatus represents the service status for CLI output.
type ServiceStatus struct {
	State      string          `json:"state"`
	NodeID     string          `json:"node_id,omitempty"`
	NodeName   string          `json:"node_name,omitempty"`
	PID        int             `json:"pid,omitempty"`
	Uptime     string          `json:"uptime,omitempty"`
	StartTime  time.Time       `json:"start_time,omitempty"`
	Version    string          `json:"version,omitempty"`
	PostgreSQL ComponentStatus `json:"postgresql"`
	GRPC       ComponentStatus `json:"grpc"`
	IPC        ComponentStatus `json:"ipc"`
	HTTP       ComponentStatus `json:"http"`
}

// GetStatus retrieves the service status.
func GetStatus() (*ServiceStatus, error) {
	svc, err := NewService(ServiceConfig{})
	if err != nil {
		return nil, fmt.Errorf("failed to create service: %w", err)
	}

	svcStatus, err := svc.Status()
	if err != nil {
		return &ServiceStatus{State: "not_installed"}, nil
	}

	status := &ServiceStatus{
		PostgreSQL: ComponentStatus{Status: "not_initialized"},
		GRPC:       ComponentStatus{Status: "not_initialized"},
		IPC:        ComponentStatus{Status: "not_initialized"},
		HTTP:       ComponentStatus{Status: "not_initialized"},
	}

	switch svcStatus {
	case service.StatusRunning:
		status.State = "running"
	case service.StatusStopped:
		status.State = "stopped"
	default:
		status.State = "unknown"
	}

	// If running, read status from PID file or IPC
	if svcStatus == service.StatusRunning {
		pid, err := ReadPIDFile(DefaultPIDFilePath())
		if err == nil {
			status.PID = pid
		}
		status.Version = Version
		// TODO: Query daemon via IPC for detailed component status
	}

	return status, nil
}

// PermissionError indicates an operation requires elevated privileges.
type PermissionError struct {
	Err error
}

func (e *PermissionError) Error() string {
	if runtime.GOOS == "windows" {
		return "administrator privileges required"
	}
	return "permission denied (try with sudo)"
}

func (e *PermissionError) Unwrap() error {
	return e.Err
}

// isUserServiceInstalled checks if the service plist exists in user's LaunchAgents.
func isUserServiceInstalled() bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	plistPath := filepath.Join(homeDir, "Library", "LaunchAgents", "steep-repl.plist")
	_, err = os.Stat(plistPath)
	return err == nil
}

// isSystemServiceInstalled checks if the service plist exists in system LaunchDaemons.
func isSystemServiceInstalled() bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	_, err := os.Stat("/Library/LaunchDaemons/steep-repl.plist")
	return err == nil
}

// IsRunningAsRoot returns true if the process is running with root privileges.
func IsRunningAsRoot() bool {
	return os.Geteuid() == 0
}

// RequiresSudo returns true if the installed service requires sudo to manage.
func RequiresSudo() bool {
	return isSystemServiceInstalled() && !IsRunningAsRoot()
}
