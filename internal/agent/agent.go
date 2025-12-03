package agent

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/willibrandon/steep/internal/agent/collectors"
	"github.com/willibrandon/steep/internal/config"
	"github.com/willibrandon/steep/internal/storage/sqlite"

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

	// Pool manager for PostgreSQL connections
	poolManager *PoolManager

	// Collector coordinator
	coordinator *CollectorCoordinator

	// Retention manager for automatic data pruning
	retentionManager *RetentionManager

	// Alerter for background alerting via webhooks
	alerter *Alerter

	// SQLite stores for data persistence
	replicationStore *sqlite.ReplicationStore
	queryStore       *sqlite.QueryStatsStore

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
		pidFile: getPIDFilePath(cfg),
	}

	// Compute config hash for drift detection
	a.configHash = computeConfigHash(cfg)

	return a, nil
}

// Start initializes the agent and begins data collection.
func (a *Agent) Start() error {
	a.logger.Println("Starting steep-agent...")

	// Ensure data directory exists
	dataDir := a.config.Storage.GetDataPath()
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("failed to create data directory %s: %w", dataDir, err)
	}

	dbPath := getDBPath(a.config)

	// T077: Check disk space before starting
	if err := CheckMinDiskSpace(dbPath); err != nil {
		if diskErr, ok := err.(*DiskFullError); ok {
			a.logger.Printf("WARNING: Low disk space - %d bytes available (minimum: %d bytes)",
				diskErr.AvailableBytes, diskErr.RequiredBytes)
			// Continue with warning - don't crash
		}
	}

	// T078: Check database integrity on startup
	if _, err := os.Stat(dbPath); err == nil {
		if err := CheckDatabaseIntegrity(dbPath); err != nil {
			if _, ok := err.(*CorruptionError); ok {
				a.logger.Printf("ERROR: Database corruption detected: %v", err)
				a.logger.Println("Attempting to recreate database...")

				backupPath, recreateErr := RecreateDatabase(dbPath)
				if recreateErr != nil {
					return fmt.Errorf("database corrupted and could not be recreated: %w", recreateErr)
				}

				if backupPath != "" {
					a.logger.Printf("Corrupted database backed up to: %s", backupPath)
				}
				a.logger.Println("Database will be recreated with fresh schema")
			} else {
				a.logger.Printf("Warning: Could not verify database integrity: %v", err)
			}
		}
	}

	// Open SQLite database
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return fmt.Errorf("failed to open database %s: %w", dbPath, err)
	}
	a.db = db

	// T076: Check schema version and run migrations
	schemaManager := NewSchemaManager(db, a.debug)
	if err := schemaManager.CheckAndMigrate(); err != nil {
		return fmt.Errorf("schema migration failed for %s: %w", dbPath, err)
	}
	if a.debug {
		a.logger.Printf("Schema version: %d", CurrentSchemaVersion)
	}

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

	// Initialize SQLite wrapper for stores that need it
	sqliteDB := sqlite.WrapConn(db)

	// Initialize the SQLite schema (creates metrics_history, query_stats, etc.)
	if err := sqliteDB.InitSchema(); err != nil {
		return fmt.Errorf("failed to init SQLite schema: %w", err)
	}

	a.replicationStore = sqlite.NewReplicationStore(sqliteDB)
	a.queryStore = sqlite.NewQueryStatsStore(sqliteDB)

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

	// Initialize pool manager and connect to PostgreSQL instances
	a.poolManager = NewPoolManager(a.instanceStore, a.logger)

	instances := a.getInstanceConfigs()
	if err := a.poolManager.ConnectAll(a.ctx, instances); err != nil {
		return fmt.Errorf("failed to connect to PostgreSQL: %w", err)
	}

	// Start health check goroutine
	a.poolManager.StartHealthCheck(a.ctx, 30*time.Second)

	// Start collectors
	if err := a.startCollectors(); err != nil {
		return fmt.Errorf("failed to start collectors: %w", err)
	}

	// Start retention manager for automatic data pruning
	a.retentionManager = NewRetentionManager(a.db, &a.config.Agent.Retention, a.logger, a.debug)
	a.retentionManager.Start()

	// Start alerter for background alerting via webhooks (US7)
	if a.config.Agent.Alerts.Enabled {
		a.startAlerter()
	}

	return nil
}

// startAlerter initializes and starts the alerter for background alerting.
func (a *Agent) startAlerter() {
	// Get default pool for alerter
	defaultPool, ok := a.poolManager.GetDefault()
	if !ok {
		a.logger.Println("Alerter: no default pool available, skipping")
		return
	}

	// Get instance name (default or first configured)
	instanceName := "default"
	if len(a.config.Agent.Instances) > 0 {
		instanceName = a.config.Agent.Instances[0].Name
	}

	alerterCfg := AlerterConfig{
		Enabled:            a.config.Agent.Alerts.Enabled,
		WebhookURL:         a.config.Agent.Alerts.WebhookURL,
		EvaluationInterval: 5 * time.Second, // Fixed 5s evaluation interval
		Rules:              a.config.Alerts.Rules,
	}

	a.alerter = NewAlerter(alerterCfg, defaultPool, instanceName, a.logger, a.debug)
	a.alerter.Start()
}

// getInstanceConfigs returns instance configurations from config or defaults.
func (a *Agent) getInstanceConfigs() []InstanceConfig {
	if len(a.config.Agent.Instances) > 0 {
		instances := make([]InstanceConfig, len(a.config.Agent.Instances))
		for i, inst := range a.config.Agent.Instances {
			instances[i] = InstanceConfig{
				Name:       inst.Name,
				Connection: inst.Connection,
			}
		}
		return instances
	}

	// Build default connection from main config
	connStr := fmt.Sprintf("host=%s port=%d dbname=%s user=%s sslmode=%s",
		a.config.Connection.Host,
		a.config.Connection.Port,
		a.config.Connection.Database,
		a.config.Connection.User,
		a.config.Connection.SSLMode,
	)

	return []InstanceConfig{{
		Name:       "default",
		Connection: connStr,
	}}
}

// startCollectors initializes and starts all data collectors.
// T051: Iterates over all configured instances, creating a set of collectors per instance.
func (a *Agent) startCollectors() error {
	// Get all connected pools
	allPools := a.poolManager.All()
	if len(allPools) == 0 {
		return fmt.Errorf("no PostgreSQL connection available")
	}

	intervals := a.config.Agent.Intervals

	// Create collector coordinator (uses first pool for coordinator-level operations)
	defaultPool, _ := a.poolManager.GetDefault()
	a.coordinator = NewCollectorCoordinator(defaultPool, a.db, &a.config.Agent, a.statusStore, a.logger)

	// Register collectors for EACH connected instance
	// This enables multi-instance monitoring where data from each instance
	// is tagged with the instance_name in SQLite tables.
	for instanceName, pool := range allPools {
		a.logger.Printf("Registering collectors for instance: %s", instanceName)

		// Register all collector types for this instance
		a.coordinator.RegisterCollector(
			collectors.NewActivityCollector(pool, a.db, intervals.Activity, instanceName),
		)
		a.coordinator.RegisterCollector(
			collectors.NewQueriesCollector(pool, a.db, a.queryStore, intervals.Queries, instanceName),
		)
		a.coordinator.RegisterCollector(
			collectors.NewReplicationCollector(pool, a.db, a.replicationStore, intervals.Replication, instanceName),
		)
		a.coordinator.RegisterCollector(
			collectors.NewLocksCollector(pool, a.db, intervals.Locks, instanceName),
		)
		a.coordinator.RegisterCollector(
			collectors.NewMetricsCollector(pool, a.db, intervals.Metrics, instanceName),
		)
	}

	a.logger.Printf("Total collectors registered: %d (across %d instances)", len(allPools)*5, len(allPools))

	// Start all collectors
	return a.coordinator.Start(a.ctx)
}

// Stop gracefully shuts down the agent.
func (a *Agent) Stop() error {
	a.logger.Println("Stopping steep-agent...")

	// Signal all goroutines to stop
	a.cancel()

	// Stop alerter
	if a.alerter != nil {
		a.alerter.Stop()
	}

	// Stop retention manager
	if a.retentionManager != nil {
		a.retentionManager.Stop()
	}

	// Stop collector coordinator
	if a.coordinator != nil {
		a.coordinator.Stop()
	}

	// Close PostgreSQL connections
	if a.poolManager != nil {
		a.poolManager.Close()
	}

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

// getDBPath returns the path to the SQLite database using the config.
func getDBPath(cfg *config.Config) string {
	return fmt.Sprintf("%s/steep.db", cfg.Storage.GetDataPath())
}

// getPIDFilePath returns the path to the PID file using the config storage path.
func getPIDFilePath(cfg *config.Config) string {
	return fmt.Sprintf("%s/steep-agent.pid", cfg.Storage.GetDataPath())
}

// computeConfigHash generates a SHA256 hash of the agent configuration for drift detection.
// Uses the centralized config.ComputeAgentConfigHash function for consistency.
func computeConfigHash(cfg *config.Config) string {
	return config.ComputeAgentConfigHash(&cfg.Agent)
}
