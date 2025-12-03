package agent

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// agentStatusRow represents a row from agent_status table.
type agentStatusRow struct {
	PID         int
	StartTime   time.Time
	LastCollect time.Time
	Version     string
	ConfigHash  string
	ErrorCount  int
	LastError   string
}

// readAgentStatus reads the agent status from SQLite.
func readAgentStatus(dbPath string) (*agentStatusRow, error) {
	db, err := sql.Open("sqlite3", dbPath+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer db.Close()

	var status agentStatusRow
	var startTimeStr, lastCollectStr sql.NullString

	err = db.QueryRow(`
		SELECT pid, start_time, last_collect, version, COALESCE(config_hash, ''),
		       error_count, COALESCE(last_error, '')
		FROM agent_status WHERE id = 1
	`).Scan(&status.PID, &startTimeStr, &lastCollectStr, &status.Version,
		&status.ConfigHash, &status.ErrorCount, &status.LastError)
	if err != nil {
		return nil, err
	}

	if startTimeStr.Valid {
		status.StartTime, _ = time.Parse(time.RFC3339, startTimeStr.String)
		if status.StartTime.IsZero() {
			status.StartTime, _ = time.Parse("2006-01-02 15:04:05.999999999-07:00", startTimeStr.String)
		}
	}
	if lastCollectStr.Valid {
		status.LastCollect, _ = time.Parse(time.RFC3339, lastCollectStr.String)
		if status.LastCollect.IsZero() {
			status.LastCollect, _ = time.Parse("2006-01-02 15:04:05.999999999-07:00", lastCollectStr.String)
		}
	}

	return &status, nil
}

// agentInstanceRow represents a row from agent_instances table.
type agentInstanceRow struct {
	Name         string
	Status       string
	LastSeen     time.Time
	ErrorMessage string
}

// readAgentInstances reads all agent instances from SQLite.
func readAgentInstances(dbPath string) ([]agentInstanceRow, error) {
	db, err := sql.Open("sqlite3", dbPath+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(`
		SELECT name, status, last_seen, COALESCE(error_message, '')
		FROM agent_instances
		ORDER BY name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var instances []agentInstanceRow
	for rows.Next() {
		var inst agentInstanceRow
		var lastSeenStr sql.NullString

		if err := rows.Scan(&inst.Name, &inst.Status, &lastSeenStr, &inst.ErrorMessage); err != nil {
			continue
		}

		if lastSeenStr.Valid {
			inst.LastSeen, _ = time.Parse(time.RFC3339, lastSeenStr.String)
			if inst.LastSeen.IsZero() {
				inst.LastSeen, _ = time.Parse("2006-01-02 15:04:05.999999999-07:00", lastSeenStr.String)
			}
		}

		instances = append(instances, inst)
	}

	return instances, nil
}

// formatUptime formats the duration since start time as a human-readable string.
func formatUptime(startTime time.Time) string {
	d := time.Since(startTime)

	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60

	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm %ds", days, hours, minutes, seconds)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm %ds", hours, minutes, seconds)
	}
	if minutes > 0 {
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	}
	return fmt.Sprintf("%ds", seconds)
}

// formatTimeSince formats the time since a given time as a human-readable string.
func formatTimeSince(t time.Time) string {
	d := time.Since(t)

	if d < time.Minute {
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours())/24)
}
