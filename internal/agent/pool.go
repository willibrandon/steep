package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// InstanceConfig is the configuration for a PostgreSQL instance.
// This is an alias for convenience within the agent package.
type InstanceConfig struct {
	Name       string
	Connection string
}

// PoolManager manages PostgreSQL connection pools for multiple instances.
type PoolManager struct {
	pools   map[string]*pgxpool.Pool
	configs map[string]InstanceConfig
	mu      sync.RWMutex

	// Reconnection settings
	maxRetries    int
	retryInterval time.Duration

	// Instance store for status updates
	instanceStore *AgentInstanceStore
	logger        Logger
}

// NewPoolManager creates a new pool manager.
func NewPoolManager(instanceStore *AgentInstanceStore, logger Logger) *PoolManager {
	return &PoolManager{
		pools:         make(map[string]*pgxpool.Pool),
		configs:       make(map[string]InstanceConfig),
		maxRetries:    3,
		retryInterval: 5 * time.Second,
		instanceStore: instanceStore,
		logger:        logger,
	}
}

// Connect creates a connection pool for the given instance.
func (pm *PoolManager) Connect(ctx context.Context, instance InstanceConfig) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Close existing pool if any
	if existing, ok := pm.pools[instance.Name]; ok {
		existing.Close()
	}

	// Create pool configuration
	poolConfig, err := pgxpool.ParseConfig(instance.Connection)
	if err != nil {
		pm.updateInstanceStatus(instance.Name, InstanceStatusError, err.Error())
		return fmt.Errorf("invalid connection string for %s: %w", instance.Name, err)
	}

	// Set pool settings
	poolConfig.MaxConns = 5 // Keep pool small for agent
	poolConfig.MinConns = 1
	poolConfig.MaxConnLifetime = 30 * time.Minute
	poolConfig.MaxConnIdleTime = 10 * time.Minute
	poolConfig.HealthCheckPeriod = 30 * time.Second
	poolConfig.ConnConfig.RuntimeParams["application_name"] = "steep-internal"

	// Disable query logging for agent's connection to prevent feedback loop
	// Use PrepareConn (runs on EVERY acquire) not AfterConnect (only new connections)
	// This ensures logging is disabled even if a previous query enabled it
	poolConfig.PrepareConn = func(ctx context.Context, conn *pgx.Conn) (bool, error) {
		_, err := conn.Exec(ctx, "/* steep:internal */ SET log_statement = 'none'")
		if err == nil {
			_, err = conn.Exec(ctx, "/* steep:internal */ SET log_min_duration_statement = -1")
		}
		if err != nil {
			// Non-fatal: user might not be superuser
			pm.logger.Printf("Could not disable query logging (requires superuser): %v", err)
		}
		return true, nil // Connection is valid regardless of SET result
	}

	// Connect with timeout
	connectCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(connectCtx, poolConfig)
	if err != nil {
		pm.updateInstanceStatus(instance.Name, InstanceStatusError, err.Error())
		return fmt.Errorf("failed to connect to %s: %w", instance.Name, err)
	}

	// Verify connection
	if err := pool.Ping(connectCtx); err != nil {
		pool.Close()
		pm.updateInstanceStatus(instance.Name, InstanceStatusError, err.Error())
		return fmt.Errorf("failed to ping %s: %w", instance.Name, err)
	}

	pm.pools[instance.Name] = pool
	pm.configs[instance.Name] = instance
	pm.updateInstanceStatus(instance.Name, InstanceStatusConnected, "")

	pm.logger.Printf("Connected to instance: %s", instance.Name)
	return nil
}

// ConnectAll connects to all configured instances.
// Returns error only if all connections fail.
func (pm *PoolManager) ConnectAll(ctx context.Context, instances []InstanceConfig) error {
	var lastErr error
	successCount := 0

	for _, instance := range instances {
		if err := pm.Connect(ctx, instance); err != nil {
			pm.logger.Printf("Failed to connect to %s: %v", instance.Name, err)
			lastErr = err
		} else {
			successCount++
		}
	}

	if successCount == 0 && lastErr != nil {
		return fmt.Errorf("all PostgreSQL connections failed: %w", lastErr)
	}

	return nil
}

// Get returns the pool for the given instance name.
func (pm *PoolManager) Get(name string) (*pgxpool.Pool, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	pool, ok := pm.pools[name]
	return pool, ok
}

// GetDefault returns the default pool (first connected instance or "default").
func (pm *PoolManager) GetDefault() (*pgxpool.Pool, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	// Try "default" first
	if pool, ok := pm.pools["default"]; ok {
		return pool, true
	}

	// Return first available pool
	for _, pool := range pm.pools {
		return pool, true
	}

	return nil, false
}

// All returns all connected pools.
func (pm *PoolManager) All() map[string]*pgxpool.Pool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	result := make(map[string]*pgxpool.Pool, len(pm.pools))
	for name, pool := range pm.pools {
		result[name] = pool
	}
	return result
}

// Close closes all connection pools.
func (pm *PoolManager) Close() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	for name, pool := range pm.pools {
		pool.Close()
		pm.logger.Printf("Closed connection pool: %s", name)
	}
	pm.pools = make(map[string]*pgxpool.Pool)
}

// Reconnect attempts to reconnect a specific instance.
func (pm *PoolManager) Reconnect(ctx context.Context, name string) error {
	pm.mu.RLock()
	instanceConfig, ok := pm.configs[name]
	pm.mu.RUnlock()

	if !ok {
		return fmt.Errorf("unknown instance: %s", name)
	}

	pm.updateInstanceStatus(name, InstanceStatusDisconnected, "reconnecting")

	for attempt := 1; attempt <= pm.maxRetries; attempt++ {
		pm.logger.Printf("Reconnecting to %s (attempt %d/%d)", name, attempt, pm.maxRetries)

		if err := pm.Connect(ctx, instanceConfig); err == nil {
			return nil
		}

		if attempt < pm.maxRetries {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(pm.retryInterval * time.Duration(attempt)):
				// Exponential backoff
			}
		}
	}

	return fmt.Errorf("failed to reconnect to %s after %d attempts", name, pm.maxRetries)
}

// ReconnectAll attempts to reconnect all disconnected instances.
func (pm *PoolManager) ReconnectAll(ctx context.Context) {
	pm.mu.RLock()
	instances := make([]InstanceConfig, 0, len(pm.configs))
	for _, cfg := range pm.configs {
		instances = append(instances, cfg)
	}
	pm.mu.RUnlock()

	for _, instance := range instances {
		pm.mu.RLock()
		pool, ok := pm.pools[instance.Name]
		pm.mu.RUnlock()

		// Check if pool needs reconnection
		needsReconnect := !ok || pool == nil
		if !needsReconnect {
			// Check if pool is still healthy
			pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := pool.Ping(pingCtx)
			cancel()
			needsReconnect = err != nil
		}

		if needsReconnect {
			_ = pm.Reconnect(ctx, instance.Name)
		}
	}
}

// StartHealthCheck starts a background goroutine to periodically check pool health.
func (pm *PoolManager) StartHealthCheck(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pm.ReconnectAll(ctx)
			}
		}
	}()
}

// updateInstanceStatus updates the instance status in the store.
// IMPORTANT: Caller must NOT hold pm.mu lock when calling this method.
func (pm *PoolManager) updateInstanceStatus(name string, status InstanceStatus, errMsg string) {
	if pm.instanceStore == nil {
		return
	}

	// Upsert to ensure instance exists, then update status
	now := time.Now()
	instance := &AgentInstance{
		Name:             name,
		ConnectionString: "[configured]", // Don't store actual connection string
		Status:           status,
		ErrorMessage:     errMsg,
	}
	if status == InstanceStatusConnected {
		instance.LastSeen = &now
	}
	_ = pm.instanceStore.Upsert(instance)
}

// InstanceNames returns the names of all configured instances.
func (pm *PoolManager) InstanceNames() []string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	names := make([]string, 0, len(pm.configs))
	for name := range pm.configs {
		names = append(names, name)
	}
	return names
}

// Stats returns connection pool statistics for all instances.
func (pm *PoolManager) Stats() map[string]PoolStats {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	stats := make(map[string]PoolStats, len(pm.pools))
	for name, pool := range pm.pools {
		s := pool.Stat()
		stats[name] = PoolStats{
			TotalConns:    int(s.TotalConns()),
			AcquiredConns: int(s.AcquiredConns()),
			IdleConns:     int(s.IdleConns()),
			MaxConns:      int(s.MaxConns()),
		}
	}
	return stats
}

// PoolStats contains connection pool statistics.
type PoolStats struct {
	TotalConns    int
	AcquiredConns int
	IdleConns     int
	MaxConns      int
}
