package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/kardianos/service"
	"github.com/willibrandon/steep/internal/config"
)

// Exit codes per cli-interface.md
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
	agent      *Agent
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
		cfg, err = config.LoadConfigFromPath(p.configPath)
	} else {
		cfg, err = config.LoadConfig()
	}
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	p.cfg = cfg

	// Create agent
	a, err := New(cfg, p.debug)
	if err != nil {
		return fmt.Errorf("failed to create agent: %w", err)
	}
	p.agent = a

	// Start agent in goroutine
	go func() {
		if err := p.agent.Start(); err != nil {
			// Log error but don't crash - service manager will restart
			fmt.Fprintf(os.Stderr, "Agent start error: %v\n", err)
		}
	}()

	return nil
}

// Stop is called when the service stops.
// Per kardianos/service, this should complete gracefully.
func (p *program) Stop(s service.Service) error {
	if p.agent != nil {
		return p.agent.Stop()
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
		Name:        "steep-agent",
		DisplayName: "Steep PostgreSQL Monitor Agent",
		Description: "Background daemon that continuously collects PostgreSQL monitoring data for the Steep TUI.",
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
			"KeepAlive":   true,
			"RunAtLoad":   true,
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

// Start starts the installed service.
func Start() error {
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

	if err := svc.Start(); err != nil {
		return fmt.Errorf("failed to start service: %w", err)
	}

	return nil
}

// Stop stops the running service.
func Stop() error {
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

// Status represents the service status.
type Status struct {
	State       string             `json:"state"`
	PID         int                `json:"pid,omitempty"`
	Uptime      string             `json:"uptime,omitempty"`
	LastCollect string             `json:"last_collect,omitempty"`
	Instances   []ServiceInstanceStatus `json:"instances,omitempty"`
	Errors      []string           `json:"errors,omitempty"`
	Version     string             `json:"version,omitempty"`
	ConfigHash  string             `json:"config_hash,omitempty"`
}

// ServiceInstanceStatus represents a monitored instance status for service status output.
type ServiceInstanceStatus struct {
	Name     string `json:"name"`
	Status   string `json:"status"`
	LastSeen string `json:"last_seen,omitempty"`
	Error    string `json:"error,omitempty"`
}

// GetStatus retrieves the service status.
func GetStatus(cfg *config.Config) (*Status, error) {
	svc, err := NewService(ServiceConfig{})
	if err != nil {
		return nil, fmt.Errorf("failed to create service: %w", err)
	}

	svcStatus, err := svc.Status()
	if err != nil {
		return &Status{State: "not_installed"}, nil
	}

	status := &Status{
		Errors: []string{},
	}

	switch svcStatus {
	case service.StatusRunning:
		status.State = "running"
	case service.StatusStopped:
		status.State = "stopped"
	default:
		status.State = "unknown"
	}

	// If running, query SQLite for additional info
	if svcStatus == service.StatusRunning && cfg != nil {
		dbPath := filepath.Join(cfg.Storage.GetDataPath(), "steep.db")
		if agentStatus, err := readAgentStatus(dbPath); err == nil {
			status.PID = agentStatus.PID
			status.Version = agentStatus.Version
			status.ConfigHash = agentStatus.ConfigHash
			if !agentStatus.LastCollect.IsZero() {
				status.LastCollect = agentStatus.LastCollect.Format("2006-01-02T15:04:05Z07:00")
			}
			if !agentStatus.StartTime.IsZero() {
				status.Uptime = formatUptime(agentStatus.StartTime)
			}
			if agentStatus.ErrorCount > 0 && agentStatus.LastError != "" {
				status.Errors = append(status.Errors, agentStatus.LastError)
			}
		}

		// Query instances
		if instances, err := readAgentInstances(dbPath); err == nil {
			for _, inst := range instances {
				is := ServiceInstanceStatus{
					Name:   inst.Name,
					Status: inst.Status,
				}
				if !inst.LastSeen.IsZero() {
					is.LastSeen = formatTimeSince(inst.LastSeen)
				}
				if inst.ErrorMessage != "" {
					is.Error = inst.ErrorMessage
				}
				status.Instances = append(status.Instances, is)
			}
		}
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
	plistPath := filepath.Join(homeDir, "Library", "LaunchAgents", "steep-agent.plist")
	_, err = os.Stat(plistPath)
	return err == nil
}

// isSystemServiceInstalled checks if the service plist exists in system LaunchDaemons.
func isSystemServiceInstalled() bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	_, err := os.Stat("/Library/LaunchDaemons/steep-agent.plist")
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
