package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/willibrandon/steep/internal/alerts"
)

// AlertStore provides SQLite persistence for alert events.
type AlertStore struct {
	db *DB
}

// NewAlertStore creates a new AlertStore.
func NewAlertStore(db *DB) *AlertStore {
	return &AlertStore{db: db}
}

// SaveEvent persists a state transition event.
func (s *AlertStore) SaveEvent(ctx context.Context, event *alerts.Event) error {
	query := `
		INSERT INTO alert_events (rule_name, prev_state, new_state, metric_value, threshold_value, triggered_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`

	result, err := s.db.conn.ExecContext(ctx, query,
		event.RuleName,
		event.PrevState.String(),
		event.NewState.String(),
		event.MetricValue,
		event.ThresholdValue,
		event.TriggeredAt,
	)
	if err != nil {
		return err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	event.ID = id

	return nil
}

// GetHistory returns recent alert events.
// If limit is 0, returns all events within retention period.
func (s *AlertStore) GetHistory(ctx context.Context, limit int) ([]alerts.Event, error) {
	query := `
		SELECT id, rule_name, prev_state, new_state, metric_value, threshold_value,
		       triggered_at, acknowledged_at, acknowledged_by
		FROM alert_events
		ORDER BY triggered_at DESC
	`

	if limit > 0 {
		query += " LIMIT ?"
	}

	var rows *sql.Rows
	var err error
	if limit > 0 {
		rows, err = s.db.conn.QueryContext(ctx, query, limit)
	} else {
		rows, err = s.db.conn.QueryContext(ctx, query)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanEvents(rows)
}

// GetHistoryForRule returns events for a specific rule.
func (s *AlertStore) GetHistoryForRule(ctx context.Context, ruleName string, limit int) ([]alerts.Event, error) {
	query := `
		SELECT id, rule_name, prev_state, new_state, metric_value, threshold_value,
		       triggered_at, acknowledged_at, acknowledged_by
		FROM alert_events
		WHERE rule_name = ?
		ORDER BY triggered_at DESC
	`

	if limit > 0 {
		query += " LIMIT ?"
	}

	var rows *sql.Rows
	var err error
	if limit > 0 {
		rows, err = s.db.conn.QueryContext(ctx, query, ruleName, limit)
	} else {
		rows, err = s.db.conn.QueryContext(ctx, query, ruleName)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanEvents(rows)
}

// GetHistoryByState returns events filtered by state.
func (s *AlertStore) GetHistoryByState(ctx context.Context, state alerts.AlertState, limit int) ([]alerts.Event, error) {
	query := `
		SELECT id, rule_name, prev_state, new_state, metric_value, threshold_value,
		       triggered_at, acknowledged_at, acknowledged_by
		FROM alert_events
		WHERE new_state = ?
		ORDER BY triggered_at DESC
	`

	if limit > 0 {
		query += " LIMIT ?"
	}

	var rows *sql.Rows
	var err error
	if limit > 0 {
		rows, err = s.db.conn.QueryContext(ctx, query, state.String(), limit)
	} else {
		rows, err = s.db.conn.QueryContext(ctx, query, state.String())
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanEvents(rows)
}

// Acknowledge marks an event as acknowledged.
func (s *AlertStore) Acknowledge(ctx context.Context, eventID int64, by string) error {
	query := `
		UPDATE alert_events
		SET acknowledged_at = ?, acknowledged_by = ?
		WHERE id = ?
	`

	_, err := s.db.conn.ExecContext(ctx, query, time.Now(), by, eventID)
	return err
}

// Prune removes events older than retention period.
// Returns number of deleted events.
func (s *AlertStore) Prune(ctx context.Context, retention time.Duration) (int64, error) {
	cutoff := time.Now().Add(-retention)

	query := `DELETE FROM alert_events WHERE triggered_at < ?`

	result, err := s.db.conn.ExecContext(ctx, query, cutoff)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected()
}

// scanEvents scans rows into a slice of Event.
func scanEvents(rows *sql.Rows) ([]alerts.Event, error) {
	var events []alerts.Event

	for rows.Next() {
		var e alerts.Event
		var prevState, newState string
		var acknowledgedAt sql.NullTime
		var acknowledgedBy sql.NullString

		err := rows.Scan(
			&e.ID,
			&e.RuleName,
			&prevState,
			&newState,
			&e.MetricValue,
			&e.ThresholdValue,
			&e.TriggeredAt,
			&acknowledgedAt,
			&acknowledgedBy,
		)
		if err != nil {
			return nil, err
		}

		e.PrevState = alerts.AlertState(prevState)
		e.NewState = alerts.AlertState(newState)

		if acknowledgedAt.Valid {
			t := acknowledgedAt.Time
			e.AcknowledgedAt = &t
		}
		if acknowledgedBy.Valid {
			e.AcknowledgedBy = acknowledgedBy.String
		}

		events = append(events, e)
	}

	return events, rows.Err()
}
