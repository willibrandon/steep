package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/willibrandon/steep/internal/config"
	"github.com/willibrandon/steep/internal/db"
	"github.com/willibrandon/steep/internal/db/queries"
	"github.com/willibrandon/steep/internal/logger"
	"github.com/willibrandon/steep/internal/monitors"
	querymonitor "github.com/willibrandon/steep/internal/monitors/queries"
	"github.com/willibrandon/steep/internal/storage/sqlite"
	"github.com/willibrandon/steep/internal/ui"
	locksview "github.com/willibrandon/steep/internal/ui/views/locks"
	logsview "github.com/willibrandon/steep/internal/ui/views/logs"
	queriesview "github.com/willibrandon/steep/internal/ui/views/queries"
)

// connectToDatabase creates a command to connect to the database
func connectToDatabase(cfg *config.Config) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		pool, err := db.NewConnectionPool(ctx, cfg)
		if err != nil {
			return ConnectionFailedMsg{Err: err}
		}

		// Get server version
		version, err := db.GetServerVersion(ctx, pool)
		if err != nil {
			version = "Unknown"
		}

		return DatabaseConnectedMsg{
			Pool:    pool,
			Version: version,
		}
	}
}

// tickStatusBar creates a command to update the status bar timestamp
func tickStatusBar() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return StatusBarTickMsg{Timestamp: t}
	})
}

// attemptReconnection creates a command to attempt database reconnection
func attemptReconnection(cfg *config.Config, state *db.ReconnectionState) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		// Send attempt message
		attemptMsg := ReconnectAttemptMsg{
			Attempt:     state.Attempt + 1,
			MaxAttempts: state.MaxAttempts,
			NextDelay:   state.CalculateNextDelay(),
		}

		// Attempt reconnection
		pool, err := db.AttemptReconnection(ctx, cfg, state)
		if err != nil {
			// Check if we've exhausted all attempts
			if !state.HasAttemptsRemaining() {
				return ReconnectFailedMsg{
					Err: fmt.Errorf("all reconnection attempts failed: %w", err),
				}
			}
			// Return attempt message to trigger next attempt
			return attemptMsg
		}

		// Get server version
		version, err := db.GetServerVersion(ctx, pool)
		if err != nil {
			version = "Unknown"
		}

		return ReconnectSuccessMsg{
			Pool:    pool,
			Version: version,
		}
	}
}

// fetchActivityData creates a command to fetch activity data
func fetchActivityData(monitor *monitors.ActivityMonitor) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		return monitor.FetchOnce(ctx)
	}
}

// fetchStatsData creates a command to fetch stats data
func fetchStatsData(monitor *monitors.StatsMonitor) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		return monitor.FetchOnce(ctx)
	}
}

// cancelQuery creates a command to cancel a running query
func cancelQuery(pool *pgxpool.Pool, pid int) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		success, err := queries.CancelQuery(ctx, pool, pid)
		return ui.CancelQueryResultMsg{
			PID:     pid,
			Success: success,
			Error:   err,
		}
	}
}

// terminateConnection creates a command to terminate a connection
func terminateConnection(pool *pgxpool.Pool, pid int) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		success, err := queries.TerminateConnection(ctx, pool, pid)
		return ui.TerminateConnectionResultMsg{
			PID:     pid,
			Success: success,
			Error:   err,
		}
	}
}

// killLockingProcess creates a command to terminate a blocking process
func killLockingProcess(pool *pgxpool.Pool, pid int) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		success, err := queries.TerminateBackend(ctx, pool, pid)
		return ui.KillQueryResultMsg{
			PID:     pid,
			Success: success,
			Error:   err,
		}
	}
}

// resetQueryStats creates a command to reset query statistics
func resetQueryStats(store *sqlite.QueryStatsStore) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		err := store.Reset(ctx)
		return queriesview.ResetQueryStatsResultMsg{
			Success: err == nil,
			Error:   err,
		}
	}
}

// resetQueryLogPositions creates a command to reset query log positions
func resetQueryLogPositions(store *sqlite.QueryStatsStore, monitor *querymonitor.Monitor, program *tea.Program) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		logger.Info("resetQueryLogPositions: starting")
		err := store.ResetLogPositions(ctx)

		if err != nil {
			return queriesview.ResetQueryLogPositionsResultMsg{
				Success: false,
				Error:   err,
			}
		}

		// Send success message immediately to show toast before scanning
		if program != nil {
			program.Send(queriesview.ResetQueryLogPositionsResultMsg{
				Success: true,
				Error:   nil,
			})
		}

		// Now parse logs with progress reporting
		if monitor != nil {
			logger.Info("resetQueryLogPositions: calling ParseWithProgress")
			monitor.ParseWithProgress(ctx, func(current, total int) {
				if program != nil {
					program.Send(queriesview.QueryScanProgressMsg{
						CurrentFile: current,
						TotalFiles:  total,
					})
				}
			})
		}

		logger.Info("resetQueryLogPositions: complete")
		// Return nil since we already sent the result message
		return nil
	}
}

// checkLoggingStatus creates a command to check PostgreSQL logging status
func checkLoggingStatus(monitor *querymonitor.Monitor) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		status, err := monitor.CheckLoggingStatus(ctx)
		if err != nil {
			return queriesview.LoggingStatusMsg{
				Error: err,
			}
		}
		return queriesview.LoggingStatusMsg{
			Enabled: status.Enabled,
		}
	}
}

// enableLogging creates a command to enable query logging
func enableLogging(monitor *querymonitor.Monitor) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		err := monitor.EnableLogging(ctx)
		return queriesview.EnableLoggingResultMsg{
			Success: err == nil,
			Error:   err,
		}
	}
}

// checkLogsLoggingStatus creates a command to check PostgreSQL logging status for logs view
func checkLogsLoggingStatus(pool *pgxpool.Pool) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		// Check logging_collector - this determines if PostgreSQL writes to log files
		var loggingCollector string
		err := pool.QueryRow(ctx, "SHOW logging_collector").Scan(&loggingCollector)
		if err != nil {
			return logsview.LoggingStatusMsg{
				Error: err,
			}
		}

		enabled := loggingCollector == "on"

		// Get log directory and filename
		var dataDir, logDir, logFilename string
		if err := pool.QueryRow(ctx, "SHOW data_directory").Scan(&dataDir); err != nil {
			return logsview.LoggingStatusMsg{Error: err}
		}
		if err := pool.QueryRow(ctx, "SHOW log_directory").Scan(&logDir); err != nil {
			return logsview.LoggingStatusMsg{Error: err}
		}
		if err := pool.QueryRow(ctx, "SHOW log_filename").Scan(&logFilename); err != nil {
			return logsview.LoggingStatusMsg{Error: err}
		}

		// Resolve absolute log directory path
		if logDir != "" && logDir[0] != '/' {
			logDir = dataDir + "/" + logDir
		}

		// Convert log_filename pattern to glob pattern
		logPattern := logFilename
		placeholders := []string{"%Y", "%m", "%d", "%H", "%M", "%S", "%a", "%b", "%j", "%W", "%y", "%I", "%p", "%e", "%c", "%n"}
		for _, ph := range placeholders {
			logPattern = strings.ReplaceAll(logPattern, ph, "*")
		}
		for strings.Contains(logPattern, "**") {
			logPattern = strings.ReplaceAll(logPattern, "**", "*")
		}

		// Detect format from log pattern
		format := monitors.DetectFormatFromFilename(logPattern)
		return logsview.LoggingStatusMsg{
			Enabled:    enabled,
			LogDir:     logDir,
			LogPattern: logPattern,
			LogFormat:  format,
		}
	}
}

// enableLogsLogging creates a command to enable logging_collector for logs view
func enableLogsLogging(pool *pgxpool.Pool) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		// Enable logging_collector via ALTER SYSTEM
		_, err := pool.Exec(ctx, "ALTER SYSTEM SET logging_collector = on")
		if err != nil {
			return logsview.EnableLoggingResultMsg{
				Success: false,
				Error:   fmt.Errorf("failed to enable logging_collector: %w", err),
			}
		}

		return logsview.EnableLoggingResultMsg{
			Success: true,
			Error:   nil,
		}
	}
}

// fetchQueryStats creates a command to fetch query statistics
func fetchQueryStats(store *sqlite.QueryStatsStore, monitor *querymonitor.Monitor, sortCol queriesview.SortColumn, sortAsc bool, filter string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		// Map view sort column to store sort field
		var storeSort sqlite.SortField
		switch sortCol {
		case queriesview.SortByTotalTime:
			storeSort = sqlite.SortByTotalTime
		case queriesview.SortByCalls:
			storeSort = sqlite.SortByCalls
		case queriesview.SortByMeanTime:
			storeSort = sqlite.SortByMeanTime
		case queriesview.SortByRows:
			storeSort = sqlite.SortByRows
		default:
			storeSort = sqlite.SortByTotalTime
		}

		var stats []sqlite.QueryStats
		var err error

		if filter != "" {
			stats, err = store.SearchQueries(ctx, filter, storeSort, sortAsc, 100)
		} else {
			stats, err = store.GetTopQueries(ctx, storeSort, sortAsc, 100)
		}

		// Get data source from monitor
		var dataSource queriesview.DataSourceType
		if monitor != nil {
			switch monitor.DataSource() {
			case querymonitor.DataSourceLogParsing:
				dataSource = queriesview.DataSourceLogParsing
			default:
				dataSource = queriesview.DataSourceSampling
			}
		}

		return queriesview.QueriesDataMsg{
			Stats:      stats,
			FetchedAt:  time.Now(),
			Error:      err,
			DataSource: dataSource,
		}
	}
}

// executeExplain creates a command to run EXPLAIN for a query
func executeExplain(monitor *querymonitor.Monitor, query string, fingerprint uint64, analyze bool) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		plan, err := monitor.GetExplainPlan(ctx, query, analyze)
		return queriesview.ExplainResultMsg{
			Query:       query,
			Plan:        plan,
			Fingerprint: fingerprint,
			Error:       err,
			Analyze:     analyze,
		}
	}
}

// fetchLocksData creates a command to fetch lock data
func fetchLocksData(monitor *monitors.LocksMonitor) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		return monitor.FetchOnce(ctx)
	}
}

// fetchReplicationData creates a command to fetch replication data
func fetchReplicationData(monitor *monitors.ReplicationMonitor) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		return monitor.FetchOnce(ctx)
	}
}

// fetchDeadlockHistory creates a command to fetch deadlock history
func fetchDeadlockHistory(monitor *monitors.DeadlockMonitor, program *tea.Program) tea.Cmd {
	if monitor == nil {
		return func() tea.Msg {
			return ui.DeadlockHistoryMsg{Enabled: false}
		}
	}
	return func() tea.Msg {
		ctx := context.Background()
		logger.Info("fetchDeadlockHistory: starting parse")
		// Parse any new entries with progress reporting
		monitor.ParseOnceWithProgress(ctx, func(current, total int) {
			if program != nil {
				program.Send(ui.DeadlockScanProgressMsg{
					CurrentFile: current,
					TotalFiles:  total,
				})
			}
		})
		logger.Info("fetchDeadlockHistory: parse complete, getting recent deadlocks")
		// Get recent deadlocks (last 30 days, limit 100)
		deadlocks, err := monitor.GetRecentDeadlocks(ctx, 30, 100)
		logger.Info("fetchDeadlockHistory: complete", "count", len(deadlocks), "error", err)
		return ui.DeadlockHistoryMsg{
			Deadlocks: deadlocks,
			Enabled:   monitor.IsEnabled(),
			Error:     err,
		}
	}
}

// fetchDeadlockDetail creates a command to fetch a single deadlock event
func fetchDeadlockDetail(store *sqlite.DeadlockStore, eventID int64) tea.Cmd {
	if store == nil {
		return nil
	}
	return func() tea.Msg {
		ctx := context.Background()
		event, err := store.GetEvent(ctx, eventID)
		return ui.DeadlockDetailMsg{
			Event: event,
			Error: err,
		}
	}
}

// enableLoggingCollector creates a command to enable logging_collector
func enableLoggingCollector(pool *pgxpool.Pool) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		err := queries.EnableLoggingCollector(ctx, pool)
		return locksview.EnableLoggingCollectorResultMsg{
			Success: err == nil,
			Error:   err,
		}
	}
}

// resetDeadlockHistory creates a command to reset deadlock history
func resetDeadlockHistory(store *sqlite.DeadlockStore) tea.Cmd {
	return func() tea.Msg {
		logger.Info("resetDeadlockHistory: starting")
		ctx := context.Background()
		err := store.Reset(ctx)
		logger.Info("resetDeadlockHistory: complete", "error", err)
		return ui.ResetDeadlocksResultMsg{
			Success: err == nil,
			Error:   err,
		}
	}
}

// resetLogPositions creates a command to reset log positions
func resetLogPositions(store *sqlite.DeadlockStore, monitor *monitors.DeadlockMonitor) tea.Cmd {
	return func() tea.Msg {
		logger.Info("resetLogPositions: starting")
		ctx := context.Background()
		err := store.ResetLogPositions(ctx)
		if err == nil && monitor != nil {
			// Also reset in-memory positions
			monitor.ResetPositions()
		}
		logger.Info("resetLogPositions: complete", "error", err)
		return ui.ResetLogPositionsResultMsg{
			Success: err == nil,
			Error:   err,
		}
	}
}

// fetchConfigData creates a command to fetch configuration data
func fetchConfigData(monitor *monitors.ConfigMonitor) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		return monitor.FetchOnce(ctx)
	}
}

// setConfigParameter creates a command to set a configuration parameter
func setConfigParameter(pool *pgxpool.Pool, parameter, value, paramContext string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		err := queries.AlterSystemSet(ctx, pool, parameter, value)
		return ui.SetConfigResultMsg{
			Parameter: parameter,
			Value:     value,
			Context:   paramContext,
			Success:   err == nil,
			Error:     err,
		}
	}
}

// resetConfigParameter creates a command to reset a configuration parameter
func resetConfigParameter(pool *pgxpool.Pool, parameter, paramContext string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		err := queries.AlterSystemReset(ctx, pool, parameter)
		return ui.ResetConfigResultMsg{
			Parameter: parameter,
			Context:   paramContext,
			Success:   err == nil,
			Error:     err,
		}
	}
}

// reloadConfig creates a command to reload PostgreSQL configuration
func reloadConfig(pool *pgxpool.Pool) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		success, err := queries.ReloadConfig(ctx, pool)
		return ui.ReloadConfigResultMsg{
			Success: success && err == nil,
			Error:   err,
		}
	}
}
