package agent

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/willibrandon/steep/internal/config"

	_ "github.com/mattn/go-sqlite3"
)

// Version is set by ldflags during build
var Version = "dev"

// Agent is the main steep-agent daemon that collects PostgreSQL monitoring data.
type Agent struct {
	config *config.Config
	db     *sql.DB

	statusStore   *AgentStatusStore
	instanceStore *AgentInstanceStore

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	pidFile    string
	configHash string

	// Logger for agent operations
	logger *log.Logger
	debug  bool
}

// New creates a new Agent instance with the given configuration.
func New(cfg *config.Config, debug bool) (*Agent, error) {
	ctx, cancel := context.WithCancel(context.Background())

	a := &Agent{
		config:  cfg,
		ctx:     ctx,
		cancel:  cancel,
		debug:   debug,
		logger:  log.New(os.Stdout, "[steep-agent] ", log.LstdFlags),
		pidFile: getPIDFilePath(),
	}

	// Compute config hash for drift detection
	a.configHash = computeConfigHash(cfg)

	return a, nil
}

// Start initializes the agent and begins data collection.
func (a *Agent) Start() error {
	a.logger.Println("Starting steep-agent...")

	// Open SQLite database
	dbPath := getDBPath()
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	a.db = db

	// Initialize stores
	a.statusStore = NewAgentStatusStore(db)
	a.instanceStore = NewAgentInstanceStore(db)

	// Initialize schemas
	if err := a.statusStore.InitSchema(); err != nil {
		return fmt.Errorf("failed to init agent_status schema: %w", err)
	}
	if err := a.instanceStore.InitSchema(); err != nil {
		return fmt.Errorf("failed to init agent_instances schema: %w", err)
	}

	// Write PID file
	if err := WritePIDFile(a.pidFile); err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}

	// Record agent status
	status := &AgentStatus{
		PID:         os.Getpid(),
		StartTime:   time.Now(),
		LastCollect: time.Now(),
		Version:     Version,
		ConfigHash:  a.configHash,
		ErrorCount:  0,
		LastError:   "",
	}
	if err := a.statusStore.Upsert(status); err != nil {
		return fmt.Errorf("failed to write agent status: %w", err)
	}

	a.logger.Printf("Agent started (PID: %d, version: %s)", os.Getpid(), Version)
	if a.debug {
		a.logger.Printf("Debug mode enabled")
		a.logger.Printf("Config hash: %s", a.configHash)
		a.logger.Printf("Database: %s", dbPath)
	}

	// TODO: Start collectors (implemented in US1)

	return nil
}

// Stop gracefully shuts down the agent.
func (a *Agent) Stop() error {
	a.logger.Println("Stopping steep-agent...")

	// Signal all goroutines to stop
	a.cancel()

	// Wait for in-flight operations with timeout
	done := make(chan struct{})
	go func() {
		a.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		a.logger.Println("All collectors stopped gracefully")
	case <-time.After(5 * time.Second):
		a.logger.Println("Shutdown timeout, forcing exit")
	}

	// WAL checkpoint for durability
	if a.db != nil {
		_, err := a.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
		if err != nil && a.debug {
			a.logger.Printf("WAL checkpoint failed: %v", err)
		}
	}

	// Delete agent status row (clean shutdown indicator)
	if a.statusStore != nil {
		_ = a.statusStore.Delete()
	}

	// Remove PID file
	if err := RemovePIDFile(a.pidFile); err != nil && a.debug {
		a.logger.Printf("Failed to remove PID file: %v", err)
	}

	// Close database
	if a.db != nil {
		if err := a.db.Close(); err != nil && a.debug {
			a.logger.Printf("Failed to close database: %v", err)
		}
	}

	a.logger.Println("Agent stopped")
	return nil
}

// Shutdown is an alias for Stop (for kardianos/service compatibility).
func (a *Agent) Shutdown() error {
	return a.Stop()
}

// Wait blocks until the agent is stopped.
func (a *Agent) Wait() {
	<-a.ctx.Done()
}

// Context returns the agent's context.
func (a *Agent) Context() context.Context {
	return a.ctx
}

// Config returns the agent's configuration.
func (a *Agent) Config() *config.Config {
	return a.config
}

// DB returns the agent's database connection.
func (a *Agent) DB() *sql.DB {
	return a.db
}

// getDBPath returns the path to the SQLite database.
func getDBPath() string {
	// Use the same database as the TUI
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "steep.db"
	}
	return fmt.Sprintf("%s/.config/steep/steep.db", homeDir)
}

// getPIDFilePath returns the path to the PID file.
func getPIDFilePath() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "steep-agent.pid"
	}
	return fmt.Sprintf("%s/.config/steep/steep-agent.pid", homeDir)
}

// computeConfigHash generates a hash of the agent configuration for drift detection.
func computeConfigHash(cfg *config.Config) string {
	// Simple hash based on key config values
	// In production, use a proper hash function
	h := fmt.Sprintf("%v-%v-%v-%v",
		cfg.Agent.Enabled,
		cfg.Agent.Intervals,
		cfg.Agent.Retention,
		len(cfg.Agent.Instances),
	)
	return h
}
