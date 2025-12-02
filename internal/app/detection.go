package app

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/willibrandon/steep/internal/agent"
	"github.com/willibrandon/steep/internal/config"
	"github.com/willibrandon/steep/internal/logger"
)

// CheckAgentStatus checks if the steep-agent is running and returns status info.
// This is used to show agent status in the TUI status bar.
func CheckAgentStatus(cfg *config.Config) *AgentStatusInfo {
	result := &AgentStatusInfo{
		Running: false,
	}

	// Check if agent is running using the config's PID file path
	pidPath := fmt.Sprintf("%s/steep-agent.pid", cfg.Storage.GetDataPath())
	pid, err := agent.CheckPIDFile(pidPath)
	if err != nil {
		if err != agent.ErrStalePIDFile {
			logger.Debug("app: error checking agent PID file", "error", err)
		}
		return result
	}

	if pid <= 0 {
		return result
	}

	result.Running = true
	result.PID = pid

	// Try to get last collection time from agent_status table
	dbPath := fmt.Sprintf("%s/steep.db", cfg.Storage.GetDataPath())
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000&mode=ro")
	if err != nil {
		logger.Debug("app: failed to open steep database for agent status", "error", err)
		return result
	}
	defer db.Close()

	// Query agent_status table for last_collect
	statusStore := agent.NewAgentStatusStore(db)
	status, err := statusStore.Get()
	if err != nil {
		logger.Debug("app: failed to get agent status from database", "error", err)
		return result
	}

	if status != nil {
		result.Version = status.Version
		result.LastCollect = status.LastCollect
	}

	return result
}

// RefreshAgentStatus refreshes the agent status (can be called periodically).
func RefreshAgentStatus(cfg *config.Config) *AgentStatusInfo {
	return CheckAgentStatus(cfg)
}

// IsAgentHealthy checks if the agent data is fresh (within 2x collection interval).
func IsAgentHealthy(cfg *config.Config, status *AgentStatusInfo) bool {
	if !status.Running {
		return false
	}

	interval := cfg.Agent.Intervals.Activity
	if interval == 0 {
		interval = 2 * time.Second
	}

	maxStaleness := 2 * interval
	return time.Since(status.LastCollect) <= maxStaleness
}
