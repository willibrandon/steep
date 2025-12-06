package models

import (
	"fmt"
	"time"
)

// NodeStatus represents the health status of a node in the replication cluster.
type NodeStatus string

const (
	NodeStatusUnknown     NodeStatus = "unknown"
	NodeStatusHealthy     NodeStatus = "healthy"
	NodeStatusDegraded    NodeStatus = "degraded"
	NodeStatusUnreachable NodeStatus = "unreachable"
	NodeStatusOffline     NodeStatus = "offline"
)

// InitState represents the initialization state of a node.
type InitState string

const (
	// InitStateUninitialized means the node is registered but has no data.
	InitStateUninitialized InitState = "uninitialized"
	// InitStatePreparing means the node is preparing for initialization (creating slots, validating schemas).
	InitStatePreparing InitState = "preparing"
	// InitStateCopying means snapshot/backup restore is in progress.
	InitStateCopying InitState = "copying"
	// InitStateCatchingUp means WAL changes are being applied since snapshot.
	InitStateCatchingUp InitState = "catching_up"
	// InitStateSynchronized means normal replication is active.
	InitStateSynchronized InitState = "synchronized"
	// InitStateDiverged means the node has been detected as out of sync.
	InitStateDiverged InitState = "diverged"
	// InitStateFailed means initialization failed and requires intervention.
	InitStateFailed InitState = "failed"
	// InitStateReinitializing means recovery/reinitialization is in progress.
	InitStateReinitializing InitState = "reinitializing"
)

// AllInitStates returns all valid initialization states.
func AllInitStates() []InitState {
	return []InitState{
		InitStateUninitialized,
		InitStatePreparing,
		InitStateCopying,
		InitStateCatchingUp,
		InitStateSynchronized,
		InitStateDiverged,
		InitStateFailed,
		InitStateReinitializing,
	}
}

// IsValid returns true if the init state is a recognized value.
func (s InitState) IsValid() bool {
	for _, valid := range AllInitStates() {
		if s == valid {
			return true
		}
	}
	return false
}

// CanTransitionTo checks if a transition from the current state to the target state is valid.
func (s InitState) CanTransitionTo(target InitState) bool {
	transitions := map[InitState][]InitState{
		InitStateUninitialized:  {InitStatePreparing, InitStateFailed},
		InitStatePreparing:      {InitStateCopying, InitStateCatchingUp, InitStateFailed}, // CatchingUp for manual init (skips copying)
		InitStateCopying:        {InitStateCatchingUp, InitStateFailed},
		InitStateCatchingUp:     {InitStateSynchronized, InitStateFailed},
		InitStateSynchronized:   {InitStateDiverged},
		InitStateDiverged:       {InitStateReinitializing, InitStateFailed},
		InitStateFailed:         {InitStateUninitialized, InitStatePreparing, InitStateReinitializing},
		InitStateReinitializing: {InitStateCopying, InitStateSynchronized, InitStateFailed},
	}

	allowed, ok := transitions[s]
	if !ok {
		return false
	}
	for _, a := range allowed {
		if a == target {
			return true
		}
	}
	return false
}

// ValidateTransition returns an error if the transition is not allowed.
func (s InitState) ValidateTransition(target InitState) error {
	if !s.CanTransitionTo(target) {
		return fmt.Errorf("invalid state transition from %s to %s", s, target)
	}
	return nil
}

// String returns the string representation of the init state.
func (s InitState) String() string {
	return string(s)
}

// AllNodeStatuses returns all valid node status values.
func AllNodeStatuses() []NodeStatus {
	return []NodeStatus{
		NodeStatusUnknown,
		NodeStatusHealthy,
		NodeStatusDegraded,
		NodeStatusUnreachable,
		NodeStatusOffline,
	}
}

// IsValid returns true if the status is a recognized value.
func (s NodeStatus) IsValid() bool {
	for _, valid := range AllNodeStatuses() {
		if s == valid {
			return true
		}
	}
	return false
}

// Node represents a PostgreSQL database instance participating in bidirectional replication.
type Node struct {
	NodeID        string     `db:"node_id" json:"node_id"`
	NodeName      string     `db:"node_name" json:"node_name"`
	Host          string     `db:"host" json:"host"`
	Port          int        `db:"port" json:"port"`
	Priority      int        `db:"priority" json:"priority"`
	IsCoordinator bool       `db:"is_coordinator" json:"is_coordinator"`
	LastSeen      *time.Time `db:"last_seen" json:"last_seen,omitempty"`
	Status        NodeStatus `db:"status" json:"status"`

	// Initialization state fields
	InitState       InitState  `db:"init_state" json:"init_state"`
	InitSourceNode  string     `db:"init_source_node" json:"init_source_node,omitempty"`
	InitStartedAt   *time.Time `db:"init_started_at" json:"init_started_at,omitempty"`
	InitCompletedAt *time.Time `db:"init_completed_at" json:"init_completed_at,omitempty"`
}

// Validate checks that the node has valid field values.
func (n *Node) Validate() error {
	if n.NodeID == "" {
		return ErrNodeIDRequired
	}
	if n.NodeName == "" {
		return ErrNodeNameRequired
	}
	if n.Host == "" {
		return ErrHostRequired
	}
	if n.Port < 1 || n.Port > 65535 {
		return ErrInvalidPort
	}
	if n.Priority < 1 || n.Priority > 100 {
		return ErrInvalidPriority
	}
	if !n.Status.IsValid() {
		return ErrInvalidStatus
	}
	return nil
}
