package models

import "time"

// NodeStatus represents the health status of a node in the replication cluster.
type NodeStatus string

const (
	NodeStatusUnknown     NodeStatus = "unknown"
	NodeStatusHealthy     NodeStatus = "healthy"
	NodeStatusDegraded    NodeStatus = "degraded"
	NodeStatusUnreachable NodeStatus = "unreachable"
	NodeStatusOffline     NodeStatus = "offline"
)

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
