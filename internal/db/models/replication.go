// Package models contains data structures for database entities.
package models

import (
	"fmt"
	"strings"
	"time"
)

// =============================================================================
// Enums and Constants
// =============================================================================

// ReplicationSyncState represents the synchronization mode of a replica.
type ReplicationSyncState int

const (
	// SyncStateAsync indicates asynchronous replication
	SyncStateAsync ReplicationSyncState = iota
	// SyncStateSync indicates synchronous replication
	SyncStateSync
	// SyncStatePotential indicates a potential synchronous standby
	SyncStatePotential
	// SyncStateQuorum indicates quorum-based synchronous replication
	SyncStateQuorum
)

// String returns the string representation of the sync state.
func (s ReplicationSyncState) String() string {
	switch s {
	case SyncStateAsync:
		return "async"
	case SyncStateSync:
		return "sync"
	case SyncStatePotential:
		return "potential"
	case SyncStateQuorum:
		return "quorum"
	default:
		return "unknown"
	}
}

// ParseSyncState converts a PostgreSQL sync_state string to ReplicationSyncState.
func ParseSyncState(s string) ReplicationSyncState {
	switch strings.ToLower(s) {
	case "sync":
		return SyncStateSync
	case "potential":
		return SyncStatePotential
	case "quorum":
		return SyncStateQuorum
	default:
		return SyncStateAsync
	}
}

// LagSeverity indicates the severity level of replication lag.
type LagSeverity int

const (
	// LagSeverityHealthy indicates lag < 1MB (green)
	LagSeverityHealthy LagSeverity = iota
	// LagSeverityWarning indicates lag 1-10MB (yellow)
	LagSeverityWarning
	// LagSeverityCritical indicates lag > 10MB (red)
	LagSeverityCritical
)

// String returns the string representation of the lag severity.
func (s LagSeverity) String() string {
	switch s {
	case LagSeverityHealthy:
		return "healthy"
	case LagSeverityWarning:
		return "warning"
	case LagSeverityCritical:
		return "critical"
	default:
		return "unknown"
	}
}

// SlotType represents the type of replication slot.
type SlotType int

const (
	// SlotTypePhysical indicates a physical replication slot
	SlotTypePhysical SlotType = iota
	// SlotTypeLogical indicates a logical replication slot
	SlotTypeLogical
)

// String returns the string representation of the slot type.
func (t SlotType) String() string {
	switch t {
	case SlotTypePhysical:
		return "physical"
	case SlotTypeLogical:
		return "logical"
	default:
		return "unknown"
	}
}

// ParseSlotType converts a PostgreSQL slot_type string to SlotType.
func ParseSlotType(s string) SlotType {
	if strings.ToLower(s) == "logical" {
		return SlotTypeLogical
	}
	return SlotTypePhysical
}

// =============================================================================
// T004: Replica Model
// =============================================================================

// Replica represents a streaming replication standby server connected to the primary.
// Source: pg_stat_replication (PostgreSQL 10+)
type Replica struct {
	// ApplicationName is the replica identifier (application_name)
	ApplicationName string
	// ClientAddr is the IP address of the replica
	ClientAddr string
	// State is the connection state (streaming, catchup, startup, backup)
	State string
	// SyncState is the synchronization mode
	SyncState ReplicationSyncState
	// SentLSN is the last WAL position sent
	SentLSN string
	// WriteLSN is the last WAL position written to disk
	WriteLSN string
	// FlushLSN is the last WAL position flushed to disk
	FlushLSN string
	// ReplayLSN is the last WAL position replayed
	ReplayLSN string
	// ByteLag is pg_wal_lsn_diff(sent_lsn, replay_lsn)
	ByteLag int64
	// WriteLag is the time lag for write
	WriteLag time.Duration
	// FlushLag is the time lag for flush
	FlushLag time.Duration
	// ReplayLag is the time lag for replay
	ReplayLag time.Duration
	// BackendStart is the connection start time
	BackendStart time.Time
	// Upstream is the name of upstream server for cascading replication
	Upstream string
}

// GetLagSeverity returns the severity level based on byte lag.
func (r *Replica) GetLagSeverity() LagSeverity {
	const (
		oneMB = 1024 * 1024
		tenMB = 10 * oneMB
	)
	switch {
	case r.ByteLag < oneMB:
		return LagSeverityHealthy
	case r.ByteLag < tenMB:
		return LagSeverityWarning
	default:
		return LagSeverityCritical
	}
}

// FormatByteLag returns human-readable lag (e.g., "1.2 MB").
func (r *Replica) FormatByteLag() string {
	return FormatBytes(r.ByteLag)
}

// FormatReplayLag returns human-readable time lag.
func (r *Replica) FormatReplayLag() string {
	if r.ReplayLag == 0 {
		return "-"
	}
	if r.ReplayLag < time.Second {
		return fmt.Sprintf("%dms", r.ReplayLag.Milliseconds())
	}
	if r.ReplayLag < time.Minute {
		return fmt.Sprintf("%.1fs", r.ReplayLag.Seconds())
	}
	return fmt.Sprintf("%.1fm", r.ReplayLag.Minutes())
}

// =============================================================================
// T005: ReplicationSlot Model
// =============================================================================

// ReplicationSlot represents a physical or logical replication slot.
// Source: pg_replication_slots (PostgreSQL 9.4+, wal_status 13+)
type ReplicationSlot struct {
	// SlotName is the unique slot identifier
	SlotName string
	// SlotType is physical or logical
	SlotType SlotType
	// Database is the database name (logical only)
	Database string
	// Active is true if the slot is in use
	Active bool
	// ActivePID is the PID using the slot
	ActivePID int
	// RestartLSN is the oldest WAL needed
	RestartLSN string
	// ConfirmedFlushLSN is the last confirmed position (logical slots)
	ConfirmedFlushLSN string
	// RetainedBytes is the WAL retained by this slot
	RetainedBytes int64
	// WALStatus is reserved, extended, unreserved, lost (PG13+)
	WALStatus string
	// SafeWALSize is remaining WAL before slot removal (PG13+)
	SafeWALSize int64
	// LastInactiveTime tracks when slot became inactive (for orphan detection)
	LastInactiveTime *time.Time
}

// IsOrphaned returns true if the slot has been inactive beyond the threshold.
func (s *ReplicationSlot) IsOrphaned(threshold time.Duration) bool {
	if s.Active {
		return false
	}
	if s.LastInactiveTime == nil {
		return false
	}
	return time.Since(*s.LastInactiveTime) > threshold
}

// RetentionWarning returns true if retained WAL exceeds the threshold ratio.
func (s *ReplicationSlot) RetentionWarning(availableSpace int64) bool {
	if availableSpace <= 0 {
		return false
	}
	return float64(s.RetainedBytes)/float64(availableSpace) > 0.8
}

// FormatRetainedBytes returns human-readable size.
func (s *ReplicationSlot) FormatRetainedBytes() string {
	return FormatBytes(s.RetainedBytes)
}

// =============================================================================
// T006: Publication Model
// =============================================================================

// Publication represents a logical replication publication on the primary.
// Source: pg_publication, pg_publication_tables (PostgreSQL 10+)
type Publication struct {
	// Name is the publication name
	Name string
	// AllTables is true if the publication publishes all tables
	AllTables bool
	// Insert is true if INSERT operations are published
	Insert bool
	// Update is true if UPDATE operations are published
	Update bool
	// Delete is true if DELETE operations are published
	Delete bool
	// Truncate is true if TRUNCATE operations are published (PG11+)
	Truncate bool
	// Tables is the list of published table names
	Tables []string
	// TableCount is the number of tables in publication
	TableCount int
	// SubscriberCount is the number of active subscribers
	SubscriberCount int
}

// OperationFlags returns a formatted string of enabled operations (e.g., "I U D").
func (p *Publication) OperationFlags() string {
	var flags []string
	if p.Insert {
		flags = append(flags, "I")
	}
	if p.Update {
		flags = append(flags, "U")
	}
	if p.Delete {
		flags = append(flags, "D")
	}
	if p.Truncate {
		flags = append(flags, "T")
	}
	if len(flags) == 0 {
		return "-"
	}
	return strings.Join(flags, " ")
}

// =============================================================================
// T007: Subscription Model
// =============================================================================

// Subscription represents a logical replication subscription on the subscriber.
// Source: pg_subscription, pg_stat_subscription (PostgreSQL 10+, stats 12+)
type Subscription struct {
	// Name is the subscription name
	Name string
	// Enabled is true if the subscription is active
	Enabled bool
	// ConnInfo is the connection string to the publisher
	ConnInfo string
	// Publications is the array of publication names
	Publications []string
	// ReceivedLSN is the last LSN received
	ReceivedLSN string
	// LatestEndLSN is the latest end LSN from publisher
	LatestEndLSN string
	// ByteLag is the lag in bytes
	ByteLag int64
	// LastMsgSendTime is the last message time from publisher
	LastMsgSendTime time.Time
	// LastMsgReceiptTime is the last receipt time
	LastMsgReceiptTime time.Time
}

// GetLagSeverity returns severity based on byte lag (same thresholds as Replica).
func (s *Subscription) GetLagSeverity() LagSeverity {
	const (
		oneMB = 1024 * 1024
		tenMB = 10 * oneMB
	)
	switch {
	case s.ByteLag < oneMB:
		return LagSeverityHealthy
	case s.ByteLag < tenMB:
		return LagSeverityWarning
	default:
		return LagSeverityCritical
	}
}

// IsStale returns true if no messages have been received recently.
func (s *Subscription) IsStale(threshold time.Duration) bool {
	if s.LastMsgReceiptTime.IsZero() {
		return true
	}
	return time.Since(s.LastMsgReceiptTime) > threshold
}

// FormatByteLag returns human-readable lag.
func (s *Subscription) FormatByteLag() string {
	return FormatBytes(s.ByteLag)
}

// =============================================================================
// T008: LagHistoryEntry Model
// =============================================================================

// LagHistoryEntry is a time-series record for persistent lag trend analysis.
// Storage: SQLite (replication_lag_history table)
type LagHistoryEntry struct {
	// ID is the auto-increment primary key
	ID int64
	// Timestamp is the measurement time
	Timestamp time.Time
	// ReplicaName is the application name of the replica
	ReplicaName string
	// SentLSN is the sent LSN at time of measurement
	SentLSN string
	// WriteLSN is the write LSN at measurement
	WriteLSN string
	// FlushLSN is the flush LSN at measurement
	FlushLSN string
	// ReplayLSN is the replay LSN at measurement
	ReplayLSN string
	// ByteLag is the calculated byte lag
	ByteLag int64
	// TimeLagMs is the replay lag in milliseconds
	TimeLagMs int64
	// SyncState is the sync state at measurement
	SyncState string
	// Direction is future field for multi-master (outbound/inbound)
	Direction string
	// ConflictCount is future field for multi-master conflicts
	ConflictCount int
}

// =============================================================================
// T009: ReplicationData Aggregate
// =============================================================================

// ReplicationData is the combined data structure for a single monitor refresh cycle.
type ReplicationData struct {
	// IsPrimary indicates if connected to primary server
	IsPrimary bool
	// Replicas is the list of streaming replication standbys
	Replicas []Replica
	// Slots is the list of replication slots
	Slots []ReplicationSlot
	// Publications is the list of logical replication publications
	Publications []Publication
	// Subscriptions is the list of logical replication subscriptions
	Subscriptions []Subscription
	// LagHistory is the in-memory ring buffer keyed by replica name
	LagHistory map[string][]float64
	// RefreshTime is when the data was fetched
	RefreshTime time.Time
	// QueryDuration is how long the queries took
	QueryDuration time.Duration
	// WALReceiverStatus is populated when connected to a standby
	WALReceiverStatus *WALReceiverStatus
	// Config is the replication configuration readiness check result
	Config *ReplicationConfig
}

// WALReceiverStatus represents the status from pg_stat_wal_receiver on a standby.
type WALReceiverStatus struct {
	// Status is the WAL receiver status
	Status string
	// ReceivedLSN is the last LSN received
	ReceivedLSN string
	// LagBytes is the lag in bytes from primary
	LagBytes int64
	// SenderHost is the primary server host
	SenderHost string
	// SenderPort is the primary server port
	SenderPort int
	// SlotName is the slot name being used
	SlotName string
	// ConnInfo is the connection info to primary
	ConnInfo string
}

// NewReplicationData creates an initialized ReplicationData structure.
func NewReplicationData() *ReplicationData {
	return &ReplicationData{
		Replicas:      make([]Replica, 0),
		Slots:         make([]ReplicationSlot, 0),
		Publications:  make([]Publication, 0),
		Subscriptions: make([]Subscription, 0),
		LagHistory:    make(map[string][]float64),
		RefreshTime:   time.Now(),
	}
}

// HasReplicas returns true if there are streaming replicas.
func (d *ReplicationData) HasReplicas() bool {
	return len(d.Replicas) > 0
}

// HasSlots returns true if there are replication slots.
func (d *ReplicationData) HasSlots() bool {
	return len(d.Slots) > 0
}

// HasLogicalReplication returns true if publications or subscriptions exist.
func (d *ReplicationData) HasLogicalReplication() bool {
	return len(d.Publications) > 0 || len(d.Subscriptions) > 0
}

// GetMaxLag returns the maximum byte lag across all replicas.
func (d *ReplicationData) GetMaxLag() int64 {
	var maxLag int64
	for _, r := range d.Replicas {
		if r.ByteLag > maxLag {
			maxLag = r.ByteLag
		}
	}
	return maxLag
}

// GetOverallSeverity returns the worst lag severity across all replicas.
func (d *ReplicationData) GetOverallSeverity() LagSeverity {
	severity := LagSeverityHealthy
	for _, r := range d.Replicas {
		s := r.GetLagSeverity()
		if s > severity {
			severity = s
		}
	}
	return severity
}

// =============================================================================
// T010: ReplicationConfig Model
// =============================================================================

// ConfigParam represents a single configuration parameter check.
type ConfigParam struct {
	// Name is the parameter name
	Name string
	// CurrentValue is the current setting
	CurrentValue string
	// RequiredValue is the minimum required value for replication
	RequiredValue string
	// Unit is the unit of measurement (e.g., "MB", "connections")
	Unit string
	// IsValid is true if the current value meets requirements
	IsValid bool
	// NeedsRestart is true if changing requires server restart
	NeedsRestart bool
	// Context is the setting context (sighup, postmaster, etc.)
	Context string
}

// ReplicationConfig contains all configuration parameters for readiness check.
type ReplicationConfig struct {
	// WALLevel checks wal_level setting
	WALLevel ConfigParam
	// MaxWALSenders checks max_wal_senders
	MaxWALSenders ConfigParam
	// MaxReplicationSlots checks max_replication_slots
	MaxReplicationSlots ConfigParam
	// WALKeepSize checks wal_keep_size (or wal_keep_segments for older versions)
	WALKeepSize ConfigParam
	// HotStandby checks hot_standby
	HotStandby ConfigParam
	// ArchiveMode checks archive_mode
	ArchiveMode ConfigParam
}

// IsReady returns true if all required parameters are properly configured.
func (c *ReplicationConfig) IsReady() bool {
	return c.WALLevel.IsValid &&
		c.MaxWALSenders.IsValid &&
		c.MaxReplicationSlots.IsValid
}

// RequiresRestart returns true if any invalid parameters need restart to change.
func (c *ReplicationConfig) RequiresRestart() bool {
	params := []ConfigParam{
		c.WALLevel,
		c.MaxWALSenders,
		c.MaxReplicationSlots,
		c.WALKeepSize,
		c.HotStandby,
		c.ArchiveMode,
	}
	for _, p := range params {
		if !p.IsValid && p.NeedsRestart {
			return true
		}
	}
	return false
}

// GetIssues returns a list of misconfigured parameters.
func (c *ReplicationConfig) GetIssues() []ConfigParam {
	var issues []ConfigParam
	params := []ConfigParam{
		c.WALLevel,
		c.MaxWALSenders,
		c.MaxReplicationSlots,
		c.WALKeepSize,
		c.HotStandby,
		c.ArchiveMode,
	}
	for _, p := range params {
		if !p.IsValid {
			issues = append(issues, p)
		}
	}
	return issues
}

// AllParams returns all configuration parameters as a slice.
func (c *ReplicationConfig) AllParams() []ConfigParam {
	return []ConfigParam{
		c.WALLevel,
		c.MaxWALSenders,
		c.MaxReplicationSlots,
		c.WALKeepSize,
		c.HotStandby,
		c.ArchiveMode,
	}
}
