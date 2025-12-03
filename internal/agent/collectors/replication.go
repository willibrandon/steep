package collectors

import (
	"context"
	"database/sql"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/willibrandon/steep/internal/db/models"
	"github.com/willibrandon/steep/internal/db/queries"
	"github.com/willibrandon/steep/internal/storage/sqlite"
)

// ReplicationCollector collects replication status at regular intervals.
type ReplicationCollector struct {
	pool         *pgxpool.Pool
	sqliteDB     *sql.DB
	store        *sqlite.ReplicationStore
	interval     time.Duration
	instanceName string
}

// NewReplicationCollector creates a new replication collector.
func NewReplicationCollector(pool *pgxpool.Pool, sqliteDB *sql.DB, store *sqlite.ReplicationStore, interval time.Duration, instanceName string) *ReplicationCollector {
	return &ReplicationCollector{
		pool:         pool,
		sqliteDB:     sqliteDB,
		store:        store,
		interval:     interval,
		instanceName: instanceName,
	}
}

// Name returns the collector name.
func (c *ReplicationCollector) Name() string {
	return "replication"
}

// Interval returns the collection interval.
func (c *ReplicationCollector) Interval() time.Duration {
	return c.interval
}

// Collect fetches replication status and persists lag history.
func (c *ReplicationCollector) Collect(ctx context.Context) error {
	// Check if this is a primary server
	isPrimary, err := queries.IsPrimary(ctx, c.pool)
	if err != nil {
		return err
	}

	if isPrimary {
		return c.collectPrimaryStats(ctx)
	}

	return c.collectStandbyStats(ctx)
}

// collectPrimaryStats collects replication stats from a primary server.
func (c *ReplicationCollector) collectPrimaryStats(ctx context.Context) error {
	replicas, err := queries.GetReplicas(ctx, c.pool)
	if err != nil {
		return err
	}

	// Store lag history for each replica
	for _, r := range replicas {
		entry := models.LagHistoryEntry{
			Timestamp:   time.Now(),
			ReplicaName: r.ApplicationName,
			SentLSN:     r.SentLSN,
			WriteLSN:    r.WriteLSN,
			FlushLSN:    r.FlushLSN,
			ReplayLSN:   r.ReplayLSN,
			ByteLag:     r.ByteLag,
			TimeLagMs:   r.ReplayLag.Milliseconds(),
			SyncState:   r.SyncState.String(),
			Direction:   "outbound",
		}

		if c.store != nil {
			_ = c.store.SaveLagEntry(ctx, entry)
		}
	}

	return nil
}

// collectStandbyStats collects replication stats from a standby server.
func (c *ReplicationCollector) collectStandbyStats(ctx context.Context) error {
	status, err := queries.GetWALReceiverStatus(ctx, c.pool)
	if err != nil {
		return err
	}

	if status == nil {
		return nil
	}

	// Store WAL receiver status
	if c.store != nil {
		entry := models.LagHistoryEntry{
			Timestamp:   time.Now(),
			ReplicaName: "self",
			ByteLag:     status.LagBytes,
			TimeLagMs:   0,
			Direction:   "inbound",
		}
		_ = c.store.SaveLagEntry(ctx, entry)
	}

	return nil
}

// Ensure ReplicationStore is compatible (compile-time check)
var _ interface {
	SaveLagEntry(ctx context.Context, entry models.LagHistoryEntry) error
} = (*sqlite.ReplicationStore)(nil)
