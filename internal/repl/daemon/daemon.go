package daemon

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/willibrandon/steep/internal/repl/config"
	"github.com/willibrandon/steep/internal/repl/db"
	replgrpc "github.com/willibrandon/steep/internal/repl/grpc"
	"github.com/willibrandon/steep/internal/repl/health"
	replinit "github.com/willibrandon/steep/internal/repl/init"
	"github.com/willibrandon/steep/internal/repl/ipc"
)

// Version is set by ldflags during build.
var Version = "dev"

// State represents the daemon's current operational state.
type State string

const (
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateStopping State = "stopping"
	StateStopped  State = "stopped"
)

// Daemon is the main steep-repl daemon that coordinates bidirectional replication.
type Daemon struct {
	config *config.Config

	// Runtime state
	state     State
	stateMu   sync.RWMutex
	startTime time.Time

	// Context for graceful shutdown
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Logging
	logger *log.Logger
	debug  bool

	// PostgreSQL connection
	pool        *db.Pool
	auditWriter *db.AuditWriter

	// IPC server for TUI communication
	ipcServer *ipc.Server

	// gRPC server for node-to-node communication
	grpcServer *replgrpc.Server

	// HTTP health server for load balancer integration
	httpServer *health.Server

	// InitManager for node initialization operations
	initManager *replinit.Manager
}

// New creates a new Daemon instance with the given configuration.
func New(cfg *config.Config, debug bool) (*Daemon, error) {
	ctx, cancel := context.WithCancel(context.Background())

	d := &Daemon{
		config: cfg,
		state:  StateStopped,
		ctx:    ctx,
		cancel: cancel,
		debug:  debug,
		logger: log.New(os.Stdout, "[steep-repl] ", log.LstdFlags),
	}

	return d, nil
}

// Start initializes the daemon and begins operation.
func (d *Daemon) Start() error {
	d.setState(StateStarting)
	d.startTime = time.Now()
	d.logger.Println("Starting steep-repl daemon...")

	// Validate configuration
	if err := d.config.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	d.logger.Printf("Node ID: %s", d.config.NodeID)
	d.logger.Printf("Node Name: %s", d.config.NodeName)

	// Initialize PostgreSQL connection pool
	d.logger.Printf("Connecting to PostgreSQL at %s:%d...", d.config.PostgreSQL.Host, d.config.PostgreSQL.Port)
	pool, err := db.NewPool(d.ctx, d.config)
	if err != nil {
		return fmt.Errorf("failed to connect to PostgreSQL: %w", err)
	}
	d.pool = pool
	d.logger.Printf("Connected to PostgreSQL %s", pool.VersionString())

	// Initialize audit writer
	d.auditWriter = db.NewAuditWriter(pool)

	// Auto-register this node in steep_repl.nodes
	if err := d.registerSelf(); err != nil {
		return fmt.Errorf("failed to register node: %w", err)
	}
	d.logger.Printf("Registered node %s (%s)", d.config.NodeID, d.config.NodeName)

	// Log daemon.started event
	if err := d.auditWriter.LogDaemonStarted(d.ctx, d.config.NodeID, d.config.NodeName, Version); err != nil {
		d.logger.Printf("Warning: failed to log daemon.started event: %v", err)
		// Non-fatal - continue startup
	}

	// Start IPC server if enabled
	if d.config.IPC.Enabled {
		socketPath := d.config.IPC.Path
		if socketPath == "" {
			socketPath = ipc.DefaultSocketPath()
		}

		ipcServer, err := ipc.NewServer(socketPath, d.logger, d.debug)
		if err != nil {
			return fmt.Errorf("failed to create IPC server: %w", err)
		}

		// Register handlers
		provider := &daemonIPCProvider{d: d}
		handlers := ipc.NewHandlers(provider)
		handlers.RegisterAll(ipcServer)

		if err := ipcServer.Start(d.ctx); err != nil {
			return fmt.Errorf("failed to start IPC server: %w", err)
		}
		d.ipcServer = ipcServer
	} else {
		d.logger.Println("IPC server disabled")
	}

	// Initialize the InitManager for node initialization operations
	// This must be done before starting the gRPC server so InitService can be registered
	slogger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	d.initManager = replinit.NewManager(pool.Pool(), &d.config.Initialization, &d.config.PostgreSQL, d.auditWriter, slogger)
	d.logger.Println("InitManager initialized")

	// Start gRPC server for node-to-node communication
	grpcConfig := replgrpc.ServerConfig{
		Port:     d.config.GRPC.Port,
		CertFile: d.config.GRPC.TLS.CertFile,
		KeyFile:  d.config.GRPC.TLS.KeyFile,
		CAFile:   d.config.GRPC.TLS.CAFile,
	}

	grpcProvider := &daemonGRPCProvider{d: d}
	grpcServer, err := replgrpc.NewServer(grpcConfig, grpcProvider, d.logger, d.debug)
	if err != nil {
		return fmt.Errorf("failed to create gRPC server: %w", err)
	}

	// Register InitServer with gRPC server before starting
	initServer := replgrpc.NewInitServer(d.initManager, d.logger, d.debug)
	grpcServer.SetInitServer(initServer)

	if err := grpcServer.Start(d.ctx); err != nil {
		return fmt.Errorf("failed to start gRPC server: %w", err)
	}
	d.grpcServer = grpcServer

	// Start HTTP health server if enabled
	if d.config.HTTP.Enabled {
		httpConfig := health.ServerConfig{
			Port: d.config.HTTP.Port,
			Bind: d.config.HTTP.Bind,
		}

		httpProvider := &daemonHTTPProvider{d: d}
		httpServer := health.NewServer(httpConfig, httpProvider, d.logger, d.debug)

		if err := httpServer.Start(d.ctx); err != nil {
			return fmt.Errorf("failed to start HTTP server: %w", err)
		}
		d.httpServer = httpServer
	} else {
		d.logger.Println("HTTP health server disabled")
	}

	d.setState(StateRunning)
	d.logger.Println("steep-repl daemon started successfully")

	return nil
}

// Stop gracefully shuts down the daemon.
func (d *Daemon) Stop() error {
	d.setState(StateStopping)
	d.logger.Println("Stopping steep-repl daemon...")

	// Log daemon.stopped event before closing pool
	if d.auditWriter != nil && d.pool != nil && d.pool.IsConnected() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := d.auditWriter.LogDaemonStopped(ctx, d.config.NodeID, d.Uptime()); err != nil {
			d.logger.Printf("Warning: failed to log daemon.stopped event: %v", err)
		}
		cancel()
	}

	// Signal all goroutines to stop
	d.cancel()

	// Wait for all goroutines with timeout
	done := make(chan struct{})
	go func() {
		d.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		d.logger.Println("All components stopped gracefully")
	case <-time.After(30 * time.Second):
		d.logger.Println("Shutdown timeout - forcing stop")
	}

	// Stop InitManager
	if d.initManager != nil {
		if err := d.initManager.Close(); err != nil {
			d.logger.Printf("Warning: failed to close InitManager: %v", err)
		}
		d.initManager = nil
	}

	// Stop gRPC server
	if d.grpcServer != nil {
		if err := d.grpcServer.Stop(); err != nil {
			d.logger.Printf("Warning: failed to stop gRPC server: %v", err)
		}
		d.grpcServer = nil
	}

	// Stop IPC server
	if d.ipcServer != nil {
		if err := d.ipcServer.Stop(); err != nil {
			d.logger.Printf("Warning: failed to stop IPC server: %v", err)
		}
		d.ipcServer = nil
	}

	// Close PostgreSQL connection pool
	if d.pool != nil {
		d.pool.Close()
		d.pool = nil
	}

	// Stop HTTP health server
	if d.httpServer != nil {
		if err := d.httpServer.Stop(); err != nil {
			d.logger.Printf("Warning: failed to stop HTTP server: %v", err)
		}
		d.httpServer = nil
	}

	d.setState(StateStopped)
	d.logger.Println("steep-repl daemon stopped")

	return nil
}

// Wait blocks until the daemon is stopped.
func (d *Daemon) Wait() {
	<-d.ctx.Done()
}

// State returns the current daemon state.
func (d *Daemon) State() State {
	d.stateMu.RLock()
	defer d.stateMu.RUnlock()
	return d.state
}

// setState updates the daemon state thread-safely.
func (d *Daemon) setState(state State) {
	d.stateMu.Lock()
	defer d.stateMu.Unlock()
	d.state = state
}

// Config returns the daemon configuration.
func (d *Daemon) Config() *config.Config {
	return d.config
}

// Pool returns the PostgreSQL connection pool.
func (d *Daemon) Pool() *db.Pool {
	return d.pool
}

// AuditWriter returns the audit log writer.
func (d *Daemon) AuditWriter() *db.AuditWriter {
	return d.auditWriter
}

// InitManager returns the node initialization manager.
func (d *Daemon) InitManager() *replinit.Manager {
	return d.initManager
}

// Uptime returns how long the daemon has been running.
func (d *Daemon) Uptime() time.Duration {
	if d.startTime.IsZero() {
		return 0
	}
	return time.Since(d.startTime)
}

// registerSelf registers this daemon's node in steep_repl.nodes.
// Uses UPSERT to handle both initial registration and reconnection.
func (d *Daemon) registerSelf() error {
	query := `
		INSERT INTO steep_repl.nodes (node_id, node_name, host, port, priority, status, last_seen)
		VALUES ($1, $2, $3, $4, $5, 'healthy', NOW())
		ON CONFLICT (node_id) DO UPDATE SET
			node_name = EXCLUDED.node_name,
			host = EXCLUDED.host,
			port = EXCLUDED.port,
			status = 'healthy',
			last_seen = NOW()
	`

	// Use the PostgreSQL host/port from config as this node's reachable address
	return d.pool.Exec(d.ctx, query,
		d.config.NodeID,
		d.config.NodeName,
		d.config.PostgreSQL.Host,
		d.config.PostgreSQL.Port,
		50, // Default priority
	)
}

// Status returns a summary of the daemon's current status.
func (d *Daemon) Status() *Status {
	status := &Status{
		State:     d.State(),
		NodeID:    d.config.NodeID,
		NodeName:  d.config.NodeName,
		Uptime:    d.Uptime(),
		StartTime: d.startTime,
		Version:   Version,
		// Component status
		PostgreSQL: d.getPostgreSQLStatus(),
		GRPC:       d.getGRPCStatus(),
		IPC:        d.getIPCStatus(),
		HTTP:       d.getHTTPStatus(),
	}
	return status
}

// getGRPCStatus returns the current gRPC server status.
func (d *Daemon) getGRPCStatus() ComponentStatus {
	if d.grpcServer == nil {
		return ComponentStatus{Status: "not_initialized"}
	}
	return ComponentStatus{
		Status: "listening",
		Port:   d.config.GRPC.Port,
	}
}

// getIPCStatus returns the current IPC server status.
func (d *Daemon) getIPCStatus() ComponentStatus {
	if !d.config.IPC.Enabled {
		return ComponentStatus{Status: "disabled"}
	}
	if d.ipcServer == nil {
		return ComponentStatus{Status: "not_initialized"}
	}
	return ComponentStatus{Status: "listening"}
}

// getHTTPStatus returns the current HTTP health server status.
func (d *Daemon) getHTTPStatus() ComponentStatus {
	if !d.config.HTTP.Enabled {
		return ComponentStatus{Status: "disabled"}
	}
	if d.httpServer == nil {
		return ComponentStatus{Status: "not_initialized"}
	}
	return ComponentStatus{
		Status: "listening",
		Port:   d.httpServer.Port(),
	}
}

// getPostgreSQLStatus returns the current PostgreSQL connection status.
func (d *Daemon) getPostgreSQLStatus() ComponentStatus {
	if d.pool == nil {
		return ComponentStatus{Status: "not_initialized"}
	}

	poolStatus := d.pool.Status()
	cs := ComponentStatus{
		Port: poolStatus.Port,
	}

	if poolStatus.Connected {
		cs.Status = "connected"
		cs.Version = poolStatus.Version
	} else {
		cs.Status = "disconnected"
		if poolStatus.Error != "" {
			cs.Error = poolStatus.Error
		}
	}

	return cs
}

// Status holds the daemon's current status information.
type Status struct {
	State      State           `json:"state"`
	NodeID     string          `json:"node_id"`
	NodeName   string          `json:"node_name"`
	Uptime     time.Duration   `json:"uptime"`
	StartTime  time.Time       `json:"start_time"`
	Version    string          `json:"version"`
	PostgreSQL ComponentStatus `json:"postgresql"`
	GRPC       ComponentStatus `json:"grpc"`
	IPC        ComponentStatus `json:"ipc"`
	HTTP       ComponentStatus `json:"http"`
}

// ComponentStatus holds the status of a daemon component.
type ComponentStatus struct {
	Status  string `json:"status"` // "connected", "listening", "not_initialized", "error"
	Port    int    `json:"port,omitempty"`
	Version string `json:"version,omitempty"`
	Error   string `json:"error,omitempty"`
}

// daemonIPCProvider implements ipc.DaemonProvider to avoid import cycles.
type daemonIPCProvider struct {
	d *Daemon
}

func (p *daemonIPCProvider) GetStatus() ipc.DaemonStatus {
	status := p.d.Status()
	result := ipc.DaemonStatus{
		State:     string(status.State),
		NodeID:    status.NodeID,
		NodeName:  status.NodeName,
		Uptime:    status.Uptime,
		StartTime: status.StartTime,
		Version:   status.Version,
	}
	result.PostgreSQL.Status = status.PostgreSQL.Status
	result.PostgreSQL.Version = status.PostgreSQL.Version
	result.PostgreSQL.Port = status.PostgreSQL.Port
	result.GRPC.Status = status.GRPC.Status
	result.GRPC.Port = status.GRPC.Port
	result.IPC.Status = status.IPC.Status
	result.HTTP.Status = status.HTTP.Status
	result.HTTP.Port = status.HTTP.Port
	return result
}

func (p *daemonIPCProvider) GetConfig() ipc.DaemonConfig {
	cfg := p.d.Config()
	result := ipc.DaemonConfig{
		NodeID:   cfg.NodeID,
		NodeName: cfg.NodeName,
	}
	result.PostgreSQL.Host = cfg.PostgreSQL.Host
	result.PostgreSQL.Port = cfg.PostgreSQL.Port
	result.GRPC.Port = cfg.GRPC.Port
	return result
}

func (p *daemonIPCProvider) GetPool() *db.Pool {
	return p.d.Pool()
}

func (p *daemonIPCProvider) GetAuditWriter() *db.AuditWriter {
	return p.d.AuditWriter()
}

func (p *daemonIPCProvider) HealthCheckPool(ctx context.Context) error {
	pool := p.d.Pool()
	if pool == nil {
		return fmt.Errorf("pool not initialized")
	}
	return pool.HealthCheck(ctx)
}

// daemonGRPCProvider implements replgrpc.DaemonProvider to avoid import cycles.
type daemonGRPCProvider struct {
	d *Daemon
}

func (p *daemonGRPCProvider) GetNodeID() string {
	return p.d.config.NodeID
}

func (p *daemonGRPCProvider) GetNodeName() string {
	return p.d.config.NodeName
}

func (p *daemonGRPCProvider) GetVersion() string {
	return Version
}

func (p *daemonGRPCProvider) GetStartTime() time.Time {
	return p.d.startTime
}

func (p *daemonGRPCProvider) GetPool() *db.Pool {
	return p.d.Pool()
}

func (p *daemonGRPCProvider) IsPostgreSQLConnected() bool {
	pool := p.d.Pool()
	return pool != nil && pool.IsConnected()
}

func (p *daemonGRPCProvider) GetPostgreSQLVersion() string {
	pool := p.d.Pool()
	if pool == nil {
		return ""
	}
	return pool.VersionString()
}

// daemonHTTPProvider implements health.HealthProvider to avoid import cycles.
type daemonHTTPProvider struct {
	d *Daemon
}

func (p *daemonHTTPProvider) GetNodeID() string {
	return p.d.config.NodeID
}

func (p *daemonHTTPProvider) GetNodeName() string {
	return p.d.config.NodeName
}

func (p *daemonHTTPProvider) GetVersion() string {
	return Version
}

func (p *daemonHTTPProvider) GetUptime() time.Duration {
	return p.d.Uptime()
}

func (p *daemonHTTPProvider) IsPostgreSQLConnected() bool {
	pool := p.d.Pool()
	return pool != nil && pool.IsConnected()
}

func (p *daemonHTTPProvider) GetPostgreSQLStatus() string {
	pool := p.d.Pool()
	if pool == nil {
		return "not initialized"
	}
	return pool.VersionString()
}

func (p *daemonHTTPProvider) IsGRPCRunning() bool {
	return p.d.grpcServer != nil
}

func (p *daemonHTTPProvider) GetGRPCPort() int {
	return p.d.config.GRPC.Port
}

func (p *daemonHTTPProvider) IsIPCRunning() bool {
	return p.d.ipcServer != nil
}
