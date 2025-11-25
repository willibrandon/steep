// Package queries provides database query functions for PostgreSQL monitoring.
package queries

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/willibrandon/steep/internal/db/models"
)

// =============================================================================
// T017: IsPrimary - Server role detection
// =============================================================================

// IsPrimary returns true if connected to a primary server, false if standby.
func IsPrimary(ctx context.Context, pool *pgxpool.Pool) (bool, error) {
	var isInRecovery bool
	err := pool.QueryRow(ctx, "SELECT pg_is_in_recovery()").Scan(&isInRecovery)
	if err != nil {
		return false, fmt.Errorf("check recovery status: %w", err)
	}
	return !isInRecovery, nil
}

// =============================================================================
// T011: GetReplicas - pg_stat_replication query
// =============================================================================

// GetReplicas retrieves streaming replication standby information from pg_stat_replication.
// Returns empty slice if no replicas are connected.
// Note: This excludes logical replication subscribers (they appear in pg_stat_replication but use logical slots).
func GetReplicas(ctx context.Context, pool *pgxpool.Pool) ([]models.Replica, error) {
	query := `
		SELECT
			COALESCE(r.application_name, '') AS application_name,
			COALESCE(r.client_addr::text, '') AS client_addr,
			COALESCE(r.state, '') AS state,
			COALESCE(r.sync_state, 'async') AS sync_state,
			COALESCE(r.sent_lsn::text, '') AS sent_lsn,
			COALESCE(r.write_lsn::text, '') AS write_lsn,
			COALESCE(r.flush_lsn::text, '') AS flush_lsn,
			COALESCE(r.replay_lsn::text, '') AS replay_lsn,
			COALESCE(pg_wal_lsn_diff(r.sent_lsn, r.replay_lsn), 0) AS byte_lag,
			COALESCE(EXTRACT(EPOCH FROM r.write_lag), 0) AS write_lag_seconds,
			COALESCE(EXTRACT(EPOCH FROM r.flush_lag), 0) AS flush_lag_seconds,
			COALESCE(EXTRACT(EPOCH FROM r.replay_lag), 0) AS replay_lag_seconds,
			r.backend_start
		FROM pg_stat_replication r
		LEFT JOIN pg_replication_slots s ON r.application_name = s.slot_name
		WHERE s.slot_type IS NULL OR s.slot_type != 'logical'
		ORDER BY r.application_name
	`

	rows, err := pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query pg_stat_replication: %w", err)
	}
	defer rows.Close()

	var replicas []models.Replica
	for rows.Next() {
		var r models.Replica
		var syncStateStr string
		var writeLagSec, flushLagSec, replayLagSec float64

		err := rows.Scan(
			&r.ApplicationName,
			&r.ClientAddr,
			&r.State,
			&syncStateStr,
			&r.SentLSN,
			&r.WriteLSN,
			&r.FlushLSN,
			&r.ReplayLSN,
			&r.ByteLag,
			&writeLagSec,
			&flushLagSec,
			&replayLagSec,
			&r.BackendStart,
		)
		if err != nil {
			return nil, fmt.Errorf("scan replica row: %w", err)
		}

		r.SyncState = models.ParseSyncState(syncStateStr)
		r.WriteLag = time.Duration(writeLagSec * float64(time.Second))
		r.FlushLag = time.Duration(flushLagSec * float64(time.Second))
		r.ReplayLag = time.Duration(replayLagSec * float64(time.Second))

		replicas = append(replicas, r)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate replicas: %w", err)
	}

	return replicas, nil
}

// =============================================================================
// T012: GetSlots - pg_replication_slots query
// =============================================================================

// GetSlots retrieves replication slot information from pg_replication_slots.
// Handles version differences (PG13+ for wal_status/safe_wal_size).
func GetSlots(ctx context.Context, pool *pgxpool.Pool) ([]models.ReplicationSlot, error) {
	// Check PostgreSQL version for wal_status column (PG13+)
	// SHOW returns text, so scan to string first then parse
	var pgVersionStr string
	err := pool.QueryRow(ctx, "SHOW server_version_num").Scan(&pgVersionStr)
	if err != nil {
		return nil, fmt.Errorf("get server version: %w", err)
	}
	var pgVersion int
	fmt.Sscanf(pgVersionStr, "%d", &pgVersion)

	var query string
	if pgVersion >= 130000 {
		// PostgreSQL 13+ with wal_status and safe_wal_size
		query = `
			SELECT
				slot_name,
				slot_type,
				COALESCE(database, '') AS database,
				active,
				COALESCE(active_pid, 0) AS active_pid,
				COALESCE(restart_lsn::text, '') AS restart_lsn,
				COALESCE(confirmed_flush_lsn::text, '') AS confirmed_flush_lsn,
				COALESCE(pg_wal_lsn_diff(pg_current_wal_lsn(), restart_lsn), 0) AS retained_bytes,
				COALESCE(wal_status, '') AS wal_status,
				COALESCE(safe_wal_size, 0) AS safe_wal_size
			FROM pg_replication_slots
			ORDER BY slot_name
		`
	} else {
		// PostgreSQL < 13 without wal_status
		query = `
			SELECT
				slot_name,
				slot_type,
				COALESCE(database, '') AS database,
				active,
				COALESCE(active_pid, 0) AS active_pid,
				COALESCE(restart_lsn::text, '') AS restart_lsn,
				COALESCE(confirmed_flush_lsn::text, '') AS confirmed_flush_lsn,
				COALESCE(pg_wal_lsn_diff(pg_current_wal_lsn(), restart_lsn), 0) AS retained_bytes,
				'' AS wal_status,
				0::bigint AS safe_wal_size
			FROM pg_replication_slots
			ORDER BY slot_name
		`
	}

	rows, err := pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query pg_replication_slots: %w", err)
	}
	defer rows.Close()

	var slots []models.ReplicationSlot
	for rows.Next() {
		var s models.ReplicationSlot
		var slotTypeStr string

		err := rows.Scan(
			&s.SlotName,
			&slotTypeStr,
			&s.Database,
			&s.Active,
			&s.ActivePID,
			&s.RestartLSN,
			&s.ConfirmedFlushLSN,
			&s.RetainedBytes,
			&s.WALStatus,
			&s.SafeWALSize,
		)
		if err != nil {
			return nil, fmt.Errorf("scan slot row: %w", err)
		}

		s.SlotType = models.ParseSlotType(slotTypeStr)
		slots = append(slots, s)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate slots: %w", err)
	}

	return slots, nil
}

// =============================================================================
// T013: GetPublications - pg_publication query
// =============================================================================

// GetPublications retrieves logical replication publications.
// Returns empty slice if no publications exist.
func GetPublications(ctx context.Context, pool *pgxpool.Pool) ([]models.Publication, error) {
	query := `
		SELECT
			p.pubname,
			p.puballtables,
			p.pubinsert,
			p.pubupdate,
			p.pubdelete,
			COALESCE(p.pubtruncate, false) AS pubtruncate,
			COUNT(pt.tablename) AS table_count
		FROM pg_publication p
		LEFT JOIN pg_publication_tables pt ON p.pubname = pt.pubname
		GROUP BY p.pubname, p.puballtables, p.pubinsert, p.pubupdate, p.pubdelete, p.pubtruncate
		ORDER BY p.pubname
	`

	rows, err := pool.Query(ctx, query)
	if err != nil {
		// If relation doesn't exist (pre-PG10), return empty
		if strings.Contains(err.Error(), "does not exist") {
			return []models.Publication{}, nil
		}
		return nil, fmt.Errorf("query pg_publication: %w", err)
	}
	defer rows.Close()

	var publications []models.Publication
	for rows.Next() {
		var p models.Publication
		err := rows.Scan(
			&p.Name,
			&p.AllTables,
			&p.Insert,
			&p.Update,
			&p.Delete,
			&p.Truncate,
			&p.TableCount,
		)
		if err != nil {
			return nil, fmt.Errorf("scan publication row: %w", err)
		}
		publications = append(publications, p)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate publications: %w", err)
	}

	// Get tables for each publication
	for i := range publications {
		tables, err := getPublicationTables(ctx, pool, publications[i].Name)
		if err != nil {
			return nil, err
		}
		publications[i].Tables = tables
	}

	return publications, nil
}

// getPublicationTables retrieves table names for a publication.
func getPublicationTables(ctx context.Context, pool *pgxpool.Pool, pubName string) ([]string, error) {
	query := `
		SELECT schemaname || '.' || tablename
		FROM pg_publication_tables
		WHERE pubname = $1
		ORDER BY schemaname, tablename
	`

	rows, err := pool.Query(ctx, query, pubName)
	if err != nil {
		return nil, fmt.Errorf("query publication tables for %s: %w", pubName, err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			return nil, fmt.Errorf("scan table name: %w", err)
		}
		tables = append(tables, tableName)
	}

	return tables, rows.Err()
}

// =============================================================================
// T014: GetSubscriptions - pg_subscription query
// =============================================================================

// GetSubscriptions retrieves logical replication subscriptions.
// Handles version differences (PG12+ for pg_stat_subscription).
func GetSubscriptions(ctx context.Context, pool *pgxpool.Pool) ([]models.Subscription, error) {
	// Check PostgreSQL version for pg_stat_subscription (PG12+)
	var pgVersion int
	err := pool.QueryRow(ctx, "SHOW server_version_num").Scan(&pgVersion)
	if err != nil {
		return nil, fmt.Errorf("get server version: %w", err)
	}

	var query string
	if pgVersion >= 120000 {
		// PostgreSQL 12+ with pg_stat_subscription
		query = `
			SELECT
				s.subname,
				s.subenabled,
				COALESCE(s.subconninfo, '') AS subconninfo,
				s.subpublications,
				COALESCE(ss.received_lsn::text, '') AS received_lsn,
				COALESCE(ss.latest_end_lsn::text, '') AS latest_end_lsn,
				COALESCE(pg_wal_lsn_diff(ss.latest_end_lsn, ss.received_lsn), 0) AS byte_lag,
				COALESCE(ss.last_msg_send_time, '1970-01-01'::timestamp) AS last_msg_send_time,
				COALESCE(ss.last_msg_receipt_time, '1970-01-01'::timestamp) AS last_msg_receipt_time
			FROM pg_subscription s
			LEFT JOIN pg_stat_subscription ss ON s.oid = ss.subid
			WHERE ss.relid IS NULL
			ORDER BY s.subname
		`
	} else {
		// PostgreSQL 10-11 without pg_stat_subscription
		query = `
			SELECT
				subname,
				subenabled,
				COALESCE(subconninfo, '') AS subconninfo,
				subpublications,
				'' AS received_lsn,
				'' AS latest_end_lsn,
				0::bigint AS byte_lag,
				'1970-01-01'::timestamp AS last_msg_send_time,
				'1970-01-01'::timestamp AS last_msg_receipt_time
			FROM pg_subscription
			ORDER BY subname
		`
	}

	rows, err := pool.Query(ctx, query)
	if err != nil {
		// If relation doesn't exist (pre-PG10), return empty
		if strings.Contains(err.Error(), "does not exist") {
			return []models.Subscription{}, nil
		}
		return nil, fmt.Errorf("query pg_subscription: %w", err)
	}
	defer rows.Close()

	var subscriptions []models.Subscription
	for rows.Next() {
		var s models.Subscription
		var publications []string

		err := rows.Scan(
			&s.Name,
			&s.Enabled,
			&s.ConnInfo,
			&publications,
			&s.ReceivedLSN,
			&s.LatestEndLSN,
			&s.ByteLag,
			&s.LastMsgSendTime,
			&s.LastMsgReceiptTime,
		)
		if err != nil {
			return nil, fmt.Errorf("scan subscription row: %w", err)
		}

		s.Publications = publications
		subscriptions = append(subscriptions, s)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate subscriptions: %w", err)
	}

	return subscriptions, nil
}

// =============================================================================
// T015: GetReplicationConfig - pg_settings query
// =============================================================================

// GetReplicationConfig retrieves configuration parameters for replication readiness check.
func GetReplicationConfig(ctx context.Context, pool *pgxpool.Pool) (*models.ReplicationConfig, error) {
	query := `
		SELECT name, setting, COALESCE(unit, '') AS unit, context
		FROM pg_settings
		WHERE name IN (
			'wal_level',
			'max_wal_senders',
			'max_replication_slots',
			'wal_keep_size',
			'hot_standby',
			'archive_mode'
		)
		ORDER BY name
	`

	rows, err := pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query pg_settings: %w", err)
	}
	defer rows.Close()

	config := &models.ReplicationConfig{}
	paramMap := make(map[string]*models.ConfigParam)
	paramMap["wal_level"] = &config.WALLevel
	paramMap["max_wal_senders"] = &config.MaxWALSenders
	paramMap["max_replication_slots"] = &config.MaxReplicationSlots
	paramMap["wal_keep_size"] = &config.WALKeepSize
	paramMap["hot_standby"] = &config.HotStandby
	paramMap["archive_mode"] = &config.ArchiveMode

	for rows.Next() {
		var name, setting, unit, context string
		err := rows.Scan(&name, &setting, &unit, &context)
		if err != nil {
			return nil, fmt.Errorf("scan setting row: %w", err)
		}

		if param, ok := paramMap[name]; ok {
			param.Name = name
			param.CurrentValue = setting
			param.Unit = unit
			param.Context = context
			param.NeedsRestart = (context == "postmaster")

			// Set required values and validate
			switch name {
			case "wal_level":
				param.RequiredValue = "replica or logical"
				param.IsValid = setting == "replica" || setting == "logical"
			case "max_wal_senders":
				param.RequiredValue = "> 0"
				param.IsValid = setting != "0"
			case "max_replication_slots":
				param.RequiredValue = "> 0"
				param.IsValid = setting != "0"
			case "wal_keep_size":
				param.RequiredValue = "recommended > 0"
				param.IsValid = true // Not strictly required
			case "hot_standby":
				param.RequiredValue = "on (for standby queries)"
				param.IsValid = true // Not strictly required for primary
			case "archive_mode":
				param.RequiredValue = "on (for PITR)"
				param.IsValid = true // Not strictly required
			}
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate settings: %w", err)
	}

	return config, nil
}

// =============================================================================
// T016: GetWALReceiverStatus - pg_stat_wal_receiver query
// =============================================================================

// GetWALReceiverStatus retrieves WAL receiver status when connected to a standby.
// Returns nil if not on a standby or no WAL receiver is active.
func GetWALReceiverStatus(ctx context.Context, pool *pgxpool.Pool) (*models.WALReceiverStatus, error) {
	// First check if we're on a standby
	isPrimary, err := IsPrimary(ctx, pool)
	if err != nil {
		return nil, err
	}
	if isPrimary {
		return nil, nil // Not a standby, no WAL receiver
	}

	// Check PostgreSQL version for sender_host/port columns (PG12+)
	var pgVersion int
	err = pool.QueryRow(ctx, "SHOW server_version_num").Scan(&pgVersion)
	if err != nil {
		return nil, fmt.Errorf("get server version: %w", err)
	}

	var query string
	if pgVersion >= 120000 {
		query = `
			SELECT
				COALESCE(status, '') AS status,
				COALESCE(received_lsn::text, '') AS received_lsn,
				COALESCE(sender_host, '') AS sender_host,
				COALESCE(sender_port, 0) AS sender_port,
				COALESCE(slot_name, '') AS slot_name,
				COALESCE(conninfo, '') AS conninfo
			FROM pg_stat_wal_receiver
			LIMIT 1
		`
	} else {
		query = `
			SELECT
				COALESCE(status, '') AS status,
				COALESCE(received_lsn::text, '') AS received_lsn,
				'' AS sender_host,
				0 AS sender_port,
				COALESCE(slot_name, '') AS slot_name,
				COALESCE(conninfo, '') AS conninfo
			FROM pg_stat_wal_receiver
			LIMIT 1
		`
	}

	var status models.WALReceiverStatus
	err = pool.QueryRow(ctx, query).Scan(
		&status.Status,
		&status.ReceivedLSN,
		&status.SenderHost,
		&status.SenderPort,
		&status.SlotName,
		&status.ConnInfo,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil // No active WAL receiver
		}
		return nil, fmt.Errorf("query pg_stat_wal_receiver: %w", err)
	}

	return &status, nil
}

// =============================================================================
// Additional helper queries for setup wizards
// =============================================================================

// GetReplicationUsers retrieves users with REPLICATION privilege.
func GetReplicationUsers(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	query := `
		SELECT rolname
		FROM pg_roles
		WHERE rolreplication = true AND rolcanlogin = true
		ORDER BY rolname
	`

	rows, err := pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query replication users: %w", err)
	}
	defer rows.Close()

	var users []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, name)
	}

	return users, rows.Err()
}

// CreateReplicationUser creates a new replication user.
func CreateReplicationUser(ctx context.Context, pool *pgxpool.Pool, username, password string) error {
	// Use format string since we can't parameterize role names
	query := fmt.Sprintf(
		"CREATE USER %s WITH REPLICATION LOGIN PASSWORD %s",
		pgx.Identifier{username}.Sanitize(),
		quoteLiteral(password),
	)

	_, err := pool.Exec(ctx, query)
	if err != nil {
		return fmt.Errorf("create replication user: %w", err)
	}

	return nil
}

// DropReplicationSlot drops a replication slot.
func DropReplicationSlot(ctx context.Context, pool *pgxpool.Pool, slotName string) error {
	_, err := pool.Exec(ctx, "SELECT pg_drop_replication_slot($1)", slotName)
	if err != nil {
		return fmt.Errorf("drop replication slot %s: %w", slotName, err)
	}
	return nil
}

// quoteLiteral safely quotes a string literal for SQL.
func quoteLiteral(s string) string {
	// Escape single quotes by doubling them
	escaped := strings.ReplaceAll(s, "'", "''")
	return "'" + escaped + "'"
}
