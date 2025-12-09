//! Custom PostgreSQL enum types for steep_repl.
//!
//! These types provide compile-time safety for status and phase fields,
//! preventing bugs like using 'running' instead of 'generating'.

use pgrx::prelude::*;
use serde::{Deserialize, Serialize};

/// Status for snapshot operations.
///
/// Maps to PostgreSQL enum type `steep_repl.snapshot_status`.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize, PostgresEnum)]
pub enum SnapshotStatus {
    /// Snapshot created but not yet started
    Pending,
    /// Snapshot generation in progress
    Generating,
    /// Snapshot generation complete
    Complete,
    /// Snapshot apply in progress
    Applying,
    /// Snapshot successfully applied
    Applied,
    /// Operation failed
    Failed,
    /// Operation cancelled by user
    Cancelled,
    /// Snapshot expired and cleaned up
    Expired,
}

impl SnapshotStatus {
    /// Convert to SQL string literal for use in queries.
    pub fn as_str(&self) -> &'static str {
        match self {
            SnapshotStatus::Pending => "pending",
            SnapshotStatus::Generating => "generating",
            SnapshotStatus::Complete => "complete",
            SnapshotStatus::Applying => "applying",
            SnapshotStatus::Applied => "applied",
            SnapshotStatus::Failed => "failed",
            SnapshotStatus::Cancelled => "cancelled",
            SnapshotStatus::Expired => "expired",
        }
    }
}

impl std::fmt::Display for SnapshotStatus {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{}", self.as_str())
    }
}

/// Phase within a snapshot operation.
///
/// Maps to PostgreSQL enum type `steep_repl.snapshot_phase`.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize, PostgresEnum)]
pub enum SnapshotPhase {
    /// No operation in progress
    Idle,
    /// Processing schema (DDL)
    Schema,
    /// Copying table data
    Data,
    /// Creating indexes
    Indexes,
    /// Creating constraints
    Constraints,
    /// Syncing sequences
    Sequences,
    /// Verifying data integrity
    Verify,
    /// Final cleanup and commit
    Finalizing,
    /// Phase complete (terminal)
    Complete,
}

impl SnapshotPhase {
    /// Convert to SQL string literal for use in queries.
    pub fn as_str(&self) -> &'static str {
        match self {
            SnapshotPhase::Idle => "idle",
            SnapshotPhase::Schema => "schema",
            SnapshotPhase::Data => "data",
            SnapshotPhase::Indexes => "indexes",
            SnapshotPhase::Constraints => "constraints",
            SnapshotPhase::Sequences => "sequences",
            SnapshotPhase::Verify => "verify",
            SnapshotPhase::Finalizing => "finalizing",
            SnapshotPhase::Complete => "complete",
        }
    }
}

impl std::fmt::Display for SnapshotPhase {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{}", self.as_str())
    }
}

/// Status for work queue entries.
///
/// Maps to PostgreSQL enum type `steep_repl.work_status`.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize, PostgresEnum)]
pub enum WorkStatus {
    /// Job queued but not started
    Pending,
    /// Job currently being processed
    Running,
    /// Job completed successfully
    Complete,
    /// Job failed with error
    Failed,
    /// Job cancelled by user
    Cancelled,
}

impl WorkStatus {
    /// Convert to SQL string literal for use in queries.
    pub fn as_str(&self) -> &'static str {
        match self {
            WorkStatus::Pending => "pending",
            WorkStatus::Running => "running",
            WorkStatus::Complete => "complete",
            WorkStatus::Failed => "failed",
            WorkStatus::Cancelled => "cancelled",
        }
    }
}

impl std::fmt::Display for WorkStatus {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{}", self.as_str())
    }
}

/// Type of operation in the work queue.
///
/// Maps to PostgreSQL enum type `steep_repl.work_operation`.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize, PostgresEnum)]
pub enum WorkOperation {
    /// Generate a snapshot from source node
    SnapshotGenerate,
    /// Apply a snapshot to target node
    SnapshotApply,
    /// Bidirectional merge between nodes
    BidirectionalMerge,
}

impl WorkOperation {
    /// Convert to SQL string literal for use in queries.
    pub fn as_str(&self) -> &'static str {
        match self {
            WorkOperation::SnapshotGenerate => "snapshot_generate",
            WorkOperation::SnapshotApply => "snapshot_apply",
            WorkOperation::BidirectionalMerge => "bidirectional_merge",
        }
    }

    /// Parse from string.
    pub fn from_str(s: &str) -> Option<Self> {
        match s {
            "snapshot_generate" => Some(WorkOperation::SnapshotGenerate),
            "snapshot_apply" => Some(WorkOperation::SnapshotApply),
            "bidirectional_merge" => Some(WorkOperation::BidirectionalMerge),
            _ => None,
        }
    }
}

impl std::fmt::Display for WorkOperation {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{}", self.as_str())
    }
}

#[cfg(any(test, feature = "pg_test"))]
#[pg_schema]
mod tests {
    use super::*;

    #[pg_test]
    fn test_snapshot_status_as_str() {
        assert_eq!(SnapshotStatus::Pending.as_str(), "pending");
        assert_eq!(SnapshotStatus::Generating.as_str(), "generating");
        assert_eq!(SnapshotStatus::Complete.as_str(), "complete");
        assert_eq!(SnapshotStatus::Failed.as_str(), "failed");
    }

    #[pg_test]
    fn test_snapshot_phase_as_str() {
        assert_eq!(SnapshotPhase::Idle.as_str(), "idle");
        assert_eq!(SnapshotPhase::Schema.as_str(), "schema");
        assert_eq!(SnapshotPhase::Data.as_str(), "data");
    }

    #[pg_test]
    fn test_work_status_as_str() {
        assert_eq!(WorkStatus::Pending.as_str(), "pending");
        assert_eq!(WorkStatus::Running.as_str(), "running");
        assert_eq!(WorkStatus::Complete.as_str(), "complete");
    }

    #[pg_test]
    fn test_work_operation_roundtrip() {
        assert_eq!(
            WorkOperation::from_str(WorkOperation::SnapshotGenerate.as_str()),
            Some(WorkOperation::SnapshotGenerate)
        );
        assert_eq!(
            WorkOperation::from_str(WorkOperation::SnapshotApply.as_str()),
            Some(WorkOperation::SnapshotApply)
        );
        assert_eq!(
            WorkOperation::from_str(WorkOperation::BidirectionalMerge.as_str()),
            Some(WorkOperation::BidirectionalMerge)
        );
    }
}
