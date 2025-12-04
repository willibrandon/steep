package daemon

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/willibrandon/steep/internal/repl/config"
	"github.com/willibrandon/steep/internal/repl/db"
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

	// Component references (will be added in later phases)
	// ipcServer *ipc.Server       // T051-T063: IPC server
	// grpcServer *grpc.Server     // T064-T074: gRPC server
	// httpServer *health.Server   // T075-T081: HTTP health server
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

	// Log daemon.started event
	if err := d.auditWriter.LogDaemonStarted(d.ctx, d.config.NodeID, d.config.NodeName, Version); err != nil {
		d.logger.Printf("Warning: failed to log daemon.started event: %v", err)
		// Non-fatal - continue startup
	}

	// TODO (T051-T063): Start IPC server
	// TODO (T064-T074): Start gRPC server
	// TODO (T075-T081): Start HTTP health server

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

	// Close PostgreSQL connection pool
	if d.pool != nil {
		d.pool.Close()
		d.pool = nil
	}

	// TODO (T051-T063): Stop IPC server
	// TODO (T064-T074): Stop gRPC server
	// TODO (T075-T081): Stop HTTP health server

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

// Uptime returns how long the daemon has been running.
func (d *Daemon) Uptime() time.Duration {
	if d.startTime.IsZero() {
		return 0
	}
	return time.Since(d.startTime)
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
		GRPC:       ComponentStatus{Status: "not_initialized"},
		IPC:        ComponentStatus{Status: "not_initialized"},
		HTTP:       ComponentStatus{Status: "not_initialized"},
	}
	return status
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
