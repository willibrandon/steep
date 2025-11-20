package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/willibrandon/steep/internal/config"
	"github.com/willibrandon/steep/internal/logger"
)

// ReconnectionState tracks automatic reconnection attempts
type ReconnectionState struct {
	Attempt     int           // Current attempt number (1-based)
	LastAttempt time.Time     // Timestamp of last attempt
	NextDelay   time.Duration // Delay until next attempt
	MaxAttempts int           // Maximum attempts before giving up
}

// NewReconnectionState creates a new reconnection state
func NewReconnectionState(maxAttempts int) *ReconnectionState {
	return &ReconnectionState{
		Attempt:     0,
		MaxAttempts: maxAttempts,
		NextDelay:   time.Second, // Start with 1 second
	}
}

// CalculateNextDelay calculates the next delay using exponential backoff
// Sequence: 1s, 2s, 4s, 8s, 16s, capped at 30s
func (r *ReconnectionState) CalculateNextDelay() time.Duration {
	// Exponential backoff: 2^attempt seconds
	delay := time.Duration(1<<uint(r.Attempt)) * time.Second

	// Cap at 30 seconds
	if delay > 30*time.Second {
		delay = 30 * time.Second
	}

	return delay
}

// NextAttempt prepares for the next reconnection attempt
func (r *ReconnectionState) NextAttempt() bool {
	r.Attempt++
	r.LastAttempt = time.Now()
	r.NextDelay = r.CalculateNextDelay()

	logger.Debug("Preparing reconnection attempt",
		"attempt", r.Attempt,
		"max_attempts", r.MaxAttempts,
		"next_delay", r.NextDelay,
	)

	return r.Attempt <= r.MaxAttempts
}

// Reset resets the reconnection state after successful connection
func (r *ReconnectionState) Reset() {
	logger.Debug("Resetting reconnection state after successful connection")
	r.Attempt = 0
	r.NextDelay = time.Second
}

// HasAttemptsRemaining returns true if more attempts are available
func (r *ReconnectionState) HasAttemptsRemaining() bool {
	return r.Attempt < r.MaxAttempts
}

// AttemptReconnection attempts to reconnect to the database
func AttemptReconnection(ctx context.Context, cfg *config.Config, state *ReconnectionState) (*pgxpool.Pool, error) {
	logger.Info("Attempting database reconnection",
		"attempt", state.Attempt+1,
		"max_attempts", state.MaxAttempts,
	)

	if !state.NextAttempt() {
		logger.Error("Maximum reconnection attempts exceeded",
			"max_attempts", state.MaxAttempts,
		)
		return nil, fmt.Errorf("maximum reconnection attempts (%d) exceeded", state.MaxAttempts)
	}

	// Wait for the calculated delay
	if state.Attempt > 1 {
		logger.Debug("Waiting before reconnection attempt",
			"delay", state.NextDelay,
		)
		time.Sleep(state.NextDelay)
	}

	// Attempt to create new connection pool
	pool, err := NewConnectionPool(ctx, cfg)
	if err != nil {
		logger.Warn("Reconnection attempt failed",
			"attempt", state.Attempt,
			"error", err,
		)
		return nil, fmt.Errorf("reconnection attempt %d failed: %w", state.Attempt, err)
	}

	// Reset state on success
	logger.Info("Database reconnection successful",
		"attempt", state.Attempt,
	)
	state.Reset()
	return pool, nil
}
