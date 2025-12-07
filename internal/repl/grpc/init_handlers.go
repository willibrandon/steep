package grpc

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/willibrandon/steep/internal/repl/config"
	pb "github.com/willibrandon/steep/internal/repl/grpc/proto"
	replinit "github.com/willibrandon/steep/internal/repl/init"
	"github.com/willibrandon/steep/internal/repl/models"
)

// InitServer implements the InitService gRPC server.
type InitServer struct {
	pb.UnimplementedInitServiceServer

	manager *replinit.Manager
	logger  *log.Logger
	debug   bool
}

// NewInitServer creates a new InitService server.
func NewInitServer(manager *replinit.Manager, logger *log.Logger, debug bool) *InitServer {
	return &InitServer{
		manager: manager,
		logger:  logger,
		debug:   debug,
	}
}

// RegisterInitServer registers the InitService with a gRPC server.
func RegisterInitServer(s *grpc.Server, initServer *InitServer) {
	pb.RegisterInitServiceServer(s, initServer)
}

// StartInit handles the StartInit RPC.
// Implements T020 (Phase 3: User Story 1).
func (s *InitServer) StartInit(ctx context.Context, req *pb.StartInitRequest) (*pb.StartInitResponse, error) {
	s.logRequest("StartInit", req.TargetNodeId)

	// Validate required fields
	if req.TargetNodeId == "" {
		return nil, status.Error(codes.InvalidArgument, "target_node_id is required")
	}
	if req.SourceNodeId == "" {
		return nil, status.Error(codes.InvalidArgument, "source_node_id is required")
	}

	// Auto-register source node if connection info provided
	if req.SourceNodeInfo != nil && req.SourceNodeInfo.Host != "" {
		sourceInfo := replinit.SourceNodeInfo{
			Host:     req.SourceNodeInfo.Host,
			Port:     int(req.SourceNodeInfo.Port),
			Database: req.SourceNodeInfo.Database,
			User:     req.SourceNodeInfo.User,
		}
		if err := s.manager.RegisterSourceNode(ctx, req.SourceNodeId, sourceInfo); err != nil {
			return &pb.StartInitResponse{
				Success: false,
				Error:   fmt.Sprintf("failed to register source node: %v", err),
			}, nil
		}
	}

	// Convert proto method to config method
	method := protoMethodToConfig(req.Method)

	// Extract options from the Options field
	var parallelWorkers int
	var schemaSync config.SchemaSyncMode

	if req.Options != nil {
		parallelWorkers = int(req.Options.ParallelWorkers)
		schemaSync = protoSchemaSyncToConfig(req.Options.SchemaSyncMode)
	} else {
		parallelWorkers = 4 // Default
		schemaSync = config.SchemaSyncStrict
	}

	// Build request for manager
	initReq := replinit.StartInitRequest{
		TargetNodeID:    req.TargetNodeId,
		SourceNodeID:    req.SourceNodeId,
		Method:          method,
		ParallelWorkers: parallelWorkers,
		SchemaSync:      schemaSync,
	}

	// Start initialization
	err := s.manager.StartInit(ctx, initReq)
	if err != nil {
		return &pb.StartInitResponse{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	return &pb.StartInitResponse{
		Success: true,
	}, nil
}

// PrepareInit handles the PrepareInit RPC.
// Creates a replication slot and records the LSN for manual initialization.
// This should be called on the SOURCE node.
func (s *InitServer) PrepareInit(ctx context.Context, req *pb.PrepareInitRequest) (*pb.PrepareInitResponse, error) {
	s.logRequest("PrepareInit", req.NodeId)

	if req.NodeId == "" {
		return nil, status.Error(codes.InvalidArgument, "node_id is required")
	}
	if req.SlotName == "" {
		return nil, status.Error(codes.InvalidArgument, "slot_name is required")
	}

	// Default expiration: 24 hours
	expiresDuration := 24 * time.Hour

	result, err := s.manager.PrepareInit(ctx, req.NodeId, req.SlotName, expiresDuration)
	if err != nil {
		return &pb.PrepareInitResponse{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	return &pb.PrepareInitResponse{
		Success:   true,
		SlotName:  result.SlotName,
		Lsn:       result.LSN,
		CreatedAt: timestamppb.New(result.CreatedAt),
	}, nil
}

// CompleteInit handles the CompleteInit RPC.
// Finishes manual initialization after user has restored backup.
// This should be called on the TARGET node.
func (s *InitServer) CompleteInit(ctx context.Context, req *pb.CompleteInitRequest) (*pb.CompleteInitResponse, error) {
	s.logRequest("CompleteInit", req.TargetNodeId)

	if req.TargetNodeId == "" {
		return nil, status.Error(codes.InvalidArgument, "target_node_id is required")
	}
	if req.SourceNodeId == "" {
		return nil, status.Error(codes.InvalidArgument, "source_node_id is required")
	}
	if req.SourceNodeInfo == nil || req.SourceNodeInfo.Host == "" {
		return nil, status.Error(codes.InvalidArgument, "source_node_info with host is required")
	}

	// Build complete options
	opts := replinit.CompleteOptions{
		TargetNodeID:    req.TargetNodeId,
		SourceNodeID:    req.SourceNodeId,
		SourceLSN:       req.SourceLsn,
		SourceHost:      req.SourceNodeInfo.Host,
		SourcePort:      int(req.SourceNodeInfo.Port),
		SourceDatabase:  req.SourceNodeInfo.Database,
		SourceUser:      req.SourceNodeInfo.User,
		SourceRemote:    req.SourceRemote,
		SchemaSyncMode:  protoSchemaSyncToConfig(req.SchemaSyncMode),
		SkipSchemaCheck: req.SkipSchemaCheck,
	}

	// Default port if not set
	if opts.SourcePort == 0 {
		opts.SourcePort = 5432
	}

	err := s.manager.CompleteInit(ctx, opts)
	if err != nil {
		return &pb.CompleteInitResponse{
			Success: false,
			Error:   err.Error(),
			State:   pb.InitState_INIT_STATE_FAILED,
		}, nil
	}

	// Get current state after completion
	nodeState, _ := s.manager.GetNodeState(ctx, req.TargetNodeId)

	return &pb.CompleteInitResponse{
		Success: true,
		State:   modelStateToProto(nodeState),
	}, nil
}

// CancelInit handles the CancelInit RPC.
// Implemented in T023 (Phase 3: User Story 1).
func (s *InitServer) CancelInit(ctx context.Context, req *pb.CancelInitRequest) (*pb.CancelInitResponse, error) {
	s.logRequest("CancelInit", req.NodeId)

	if req.NodeId == "" {
		return nil, status.Error(codes.InvalidArgument, "node_id is required")
	}

	err := s.manager.CancelInit(ctx, req.NodeId)
	if err != nil {
		return &pb.CancelInitResponse{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	return &pb.CancelInitResponse{
		Success: true,
	}, nil
}

// GetProgress handles the GetProgress RPC.
// Implemented in T037 (Phase 5: User Story 3).
func (s *InitServer) GetProgress(ctx context.Context, req *pb.GetProgressRequest) (*pb.GetProgressResponse, error) {
	s.logRequest("GetProgress", req.NodeId)

	if req.NodeId == "" {
		return nil, status.Error(codes.InvalidArgument, "node_id is required")
	}

	progress, err := s.manager.GetProgress(ctx, req.NodeId)
	if err != nil {
		return &pb.GetProgressResponse{
			HasProgress: false,
		}, nil
	}

	// Get the node's init_state
	nodeState, err := s.manager.GetNodeState(ctx, req.NodeId)
	if err != nil {
		// Non-fatal, continue with unspecified state
		nodeState = ""
	}

	pbProgress := &pb.InitProgress{
		NodeId:              progress.NodeID,
		State:               modelStateToProto(nodeState),
		Phase:               string(progress.Phase),
		OverallPercent:      float32(progress.OverallPercent),
		TablesTotal:         int32(progress.TablesTotal),
		TablesCompleted:     int32(progress.TablesCompleted),
		CurrentTablePercent: float32(progress.CurrentTablePercent),
		RowsCopied:          progress.RowsCopied,
		BytesCopied:         progress.BytesCopied,
		ThroughputRowsSec:   float32(progress.ThroughputRowsSec),
		StartedAt:           timestamppb.New(progress.StartedAt),
		ParallelWorkers:     int32(progress.ParallelWorkers),
	}

	if progress.CurrentTable != nil {
		pbProgress.CurrentTable = *progress.CurrentTable
	}
	if progress.ETASeconds != nil {
		pbProgress.EtaSeconds = int32(*progress.ETASeconds)
	}
	if progress.ErrorMessage != nil {
		pbProgress.ErrorMessage = *progress.ErrorMessage
	}

	return &pb.GetProgressResponse{
		HasProgress: true,
		Progress:    pbProgress,
	}, nil
}

// StreamProgress handles the StreamProgress RPC.
// Implemented in T038 (Phase 5: User Story 3).
func (s *InitServer) StreamProgress(req *pb.StreamProgressRequest, stream grpc.ServerStreamingServer[pb.ProgressUpdate]) error {
	s.logRequest("StreamProgress", req.NodeId)

	if req.NodeId == "" {
		return status.Error(codes.InvalidArgument, "node_id is required")
	}

	// Get the progress channel from manager
	progressChan := s.manager.Progress()

	interval := time.Duration(req.IntervalMs) * time.Millisecond
	if interval <= 0 {
		interval = 1000 * time.Millisecond
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	ctx := stream.Context()

	for {
		select {
		case <-ctx.Done():
			return nil
		case update, ok := <-progressChan:
			if !ok {
				return nil
			}
			// Filter to requested node
			if update.NodeID != req.NodeId {
				continue
			}

			pbProgress := &pb.InitProgress{
				NodeId:              update.NodeID,
				Phase:               update.Phase,
				OverallPercent:      update.OverallPercent,
				TablesTotal:         int32(update.TablesTotal),
				TablesCompleted:     int32(update.TablesCompleted),
				CurrentTable:        update.CurrentTable,
				CurrentTablePercent: update.CurrentPercent,
				RowsCopied:          update.RowsCopied,
				BytesCopied:         update.BytesCopied,
				ThroughputRowsSec:   update.ThroughputRows,
				EtaSeconds:          int32(update.ETASeconds),
				ParallelWorkers:     int32(update.ParallelWorkers),
				ErrorMessage:        update.Error,
			}

			pbUpdate := &pb.ProgressUpdate{
				Progress:  pbProgress,
				Timestamp: timestamppb.Now(),
			}

			if err := stream.Send(pbUpdate); err != nil {
				return err
			}
		case <-ticker.C:
			// Periodic poll for progress - implementation in T038
		}
	}
}

// StartReinit handles the StartReinit RPC.
func (s *InitServer) StartReinit(ctx context.Context, req *pb.StartReinitRequest) (*pb.StartReinitResponse, error) {
	s.logRequest("StartReinit", req.NodeId)

	if req.NodeId == "" {
		return nil, status.Error(codes.InvalidArgument, "node_id is required")
	}

	if s.manager == nil {
		return nil, status.Error(codes.FailedPrecondition, "init manager not initialized")
	}

	// Convert proto scope to init scope
	opts := replinit.ReinitOptions{
		NodeID: req.NodeId,
	}

	if req.Scope != nil {
		switch scope := req.Scope.Scope.(type) {
		case *pb.ReinitScope_Full:
			opts.Scope.Full = scope.Full
		case *pb.ReinitScope_Tables:
			if scope.Tables != nil {
				opts.Scope.Tables = scope.Tables.Tables
			}
		case *pb.ReinitScope_Schema:
			opts.Scope.Schema = scope.Schema
		}
	}

	// Start reinit via manager
	result, err := s.manager.StartReinit(ctx, opts)
	if err != nil {
		return &pb.StartReinitResponse{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	return &pb.StartReinitResponse{
		Success:        true,
		State:          modelStateToProto(result.FinalState),
		TablesAffected: int32(result.TablesAffected),
	}, nil
}

// CompareSchemas handles the CompareSchemas RPC.
// Compares schema fingerprints between the local database and a remote node.
// The remote node's fingerprints are retrieved via gRPC GetSchemaFingerprints.
func (s *InitServer) CompareSchemas(ctx context.Context, req *pb.CompareSchemasRequest) (*pb.CompareSchemasResponse, error) {
	s.logRequest("CompareSchemas", fmt.Sprintf("%s vs %s", req.LocalNodeId, req.RemoteNodeId))

	if req.LocalNodeId == "" {
		return nil, status.Error(codes.InvalidArgument, "local_node_id is required")
	}
	if req.RemoteNodeId == "" {
		return nil, status.Error(codes.InvalidArgument, "remote_node_id is required")
	}

	// Get the schema comparator from manager
	result, err := s.manager.CompareSchemas(ctx, req.LocalNodeId, req.RemoteNodeId, req.Schemas)
	if err != nil {
		return &pb.CompareSchemasResponse{
			Success: false,
			Error:   fmt.Sprintf("schema comparison failed: %v", err),
		}, nil
	}

	// Convert to proto
	var comparisons []*pb.SchemaComparison
	for _, comp := range result.Comparisons {
		pbComp := &pb.SchemaComparison{
			TableSchema:       comp.TableSchema,
			TableName:         comp.TableName,
			LocalFingerprint:  comp.LocalFingerprint,
			RemoteFingerprint: comp.RemoteFingerprint,
			Status:            comparisonStatusToProto(comp.Status),
		}

		// Convert differences
		for _, diff := range comp.Differences {
			pbComp.Differences = append(pbComp.Differences, &pb.ColumnDifference{
				ColumnName:       diff.ColumnName,
				DifferenceType:   diff.DifferenceType,
				LocalDefinition:  diff.LocalDefinition,
				RemoteDefinition: diff.RemoteDefinition,
			})
		}

		comparisons = append(comparisons, pbComp)
	}

	return &pb.CompareSchemasResponse{
		Success:         true,
		Comparisons:     comparisons,
		MatchCount:      int32(result.MatchCount),
		MismatchCount:   int32(result.MismatchCount),
		LocalOnlyCount:  int32(result.LocalOnlyCount),
		RemoteOnlyCount: int32(result.RemoteOnlyCount),
	}, nil
}

// GetSchemaFingerprints returns schema fingerprints for this node's database.
// Used for remote schema verification without direct database connection.
func (s *InitServer) GetSchemaFingerprints(ctx context.Context, req *pb.GetSchemaFingerprintsRequest) (*pb.GetSchemaFingerprintsResponse, error) {
	s.logRequest("GetSchemaFingerprints", "")

	fingerprints, err := s.manager.GetTableFingerprintsWithDefs(ctx)
	if err != nil {
		return &pb.GetSchemaFingerprintsResponse{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	// Convert map to slice of TableFingerprint
	var result []*pb.TableFingerprint
	for key, info := range fingerprints {
		parts := strings.SplitN(key, ".", 2)
		schema := "public"
		table := key
		if len(parts) == 2 {
			schema = parts[0]
			table = parts[1]
		}
		result = append(result, &pb.TableFingerprint{
			SchemaName:        schema,
			TableName:         table,
			Fingerprint:       info.Fingerprint,
			ColumnDefinitions: info.ColumnDefinitions,
		})
	}

	return &pb.GetSchemaFingerprintsResponse{
		Success:      true,
		Fingerprints: result,
	}, nil
}

// GetColumnDiff handles the GetColumnDiff RPC.
// Returns detailed column-level differences for a specific table.
func (s *InitServer) GetColumnDiff(ctx context.Context, req *pb.GetColumnDiffRequest) (*pb.GetColumnDiffResponse, error) {
	s.logRequest("GetColumnDiff", fmt.Sprintf("%s.%s vs %s", req.TableSchema, req.TableName, req.PeerNodeId))

	if req.TableSchema == "" {
		return nil, status.Error(codes.InvalidArgument, "table_schema is required")
	}
	if req.TableName == "" {
		return nil, status.Error(codes.InvalidArgument, "table_name is required")
	}
	if req.PeerNodeId == "" {
		return nil, status.Error(codes.InvalidArgument, "peer_node_id is required")
	}

	// Get column differences from manager
	diffs, err := s.manager.GetColumnDiff(ctx, req.PeerNodeId, req.TableSchema, req.TableName)
	if err != nil {
		return &pb.GetColumnDiffResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to get column diff: %v", err),
		}, nil
	}

	// Convert to proto
	var pbDiffs []*pb.ColumnDifference
	for _, diff := range diffs {
		pbDiffs = append(pbDiffs, &pb.ColumnDifference{
			ColumnName:       diff.ColumnName,
			DifferenceType:   diff.DifferenceType,
			LocalDefinition:  diff.LocalDefinition,
			RemoteDefinition: diff.RemoteDefinition,
		})
	}

	return &pb.GetColumnDiffResponse{
		Success:     true,
		Differences: pbDiffs,
	}, nil
}

// CaptureFingerprints handles the CaptureFingerprints RPC.
// Captures and stores fingerprints for all tables in the specified schemas.
func (s *InitServer) CaptureFingerprints(ctx context.Context, req *pb.CaptureFingerprintsRequest) (*pb.CaptureFingerprintsResponse, error) {
	s.logRequest("CaptureFingerprints", req.NodeId)

	if req.NodeId == "" {
		return nil, status.Error(codes.InvalidArgument, "node_id is required")
	}

	// Capture fingerprints via manager
	err := s.manager.CaptureFingerprints(ctx, req.NodeId, req.Schemas)
	if err != nil {
		return &pb.CaptureFingerprintsResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to capture fingerprints: %v", err),
		}, nil
	}

	// Get captured fingerprints
	fingerprints, err := s.manager.GetTableFingerprints(ctx)
	if err != nil {
		return &pb.CaptureFingerprintsResponse{
			Success:    true,
			TableCount: 0, // Can't determine count, but capture succeeded
		}, nil
	}

	// Convert to proto format
	protoFingerprints := make([]*pb.TableFingerprint, 0, len(fingerprints))
	for key, fp := range fingerprints {
		// key is "schema.table" format
		parts := strings.SplitN(key, ".", 2)
		schemaName := "public"
		tableName := key
		if len(parts) == 2 {
			schemaName = parts[0]
			tableName = parts[1]
		}
		protoFingerprints = append(protoFingerprints, &pb.TableFingerprint{
			SchemaName:  schemaName,
			TableName:   tableName,
			Fingerprint: fp,
		})
	}

	return &pb.CaptureFingerprintsResponse{
		Success:      true,
		TableCount:   int32(len(fingerprints)),
		Fingerprints: protoFingerprints,
	}, nil
}

// GenerateSnapshot handles the GenerateSnapshot RPC.
// Implemented in T080 (Phase 11: Two-Phase Snapshot).
func (s *InitServer) GenerateSnapshot(req *pb.GenerateSnapshotRequest, stream grpc.ServerStreamingServer[pb.SnapshotProgress]) error {
	s.logRequest("GenerateSnapshot", req.SourceNodeId)

	if req.SourceNodeId == "" {
		return status.Error(codes.InvalidArgument, "source_node_id is required")
	}
	if req.OutputPath == "" {
		return status.Error(codes.InvalidArgument, "output_path is required")
	}

	// Parse compression type
	compression := models.CompressionNone
	switch req.Compression {
	case "gzip":
		compression = models.CompressionGzip
	case "lz4":
		compression = models.CompressionLZ4
	case "zstd":
		compression = models.CompressionZstd
	case "", "none":
		compression = models.CompressionNone
	default:
		return status.Errorf(codes.InvalidArgument, "invalid compression type: %s (supported: none, gzip, lz4, zstd)", req.Compression)
	}

	// Get parallel workers (default to 4)
	parallelWorkers := int(req.ParallelWorkers)
	if parallelWorkers <= 0 {
		parallelWorkers = 4
	}

	// Create snapshot options with progress callback that streams to gRPC
	opts := replinit.TwoPhaseSnapshotOptions{
		OutputPath:      req.OutputPath,
		Compression:     compression,
		ParallelWorkers: parallelWorkers,
		ProgressFn: func(progress replinit.TwoPhaseProgress) {
			// Send progress update to client
			err := stream.Send(&pb.SnapshotProgress{
				SnapshotId:          progress.SnapshotID,
				Phase:               progress.Phase,
				OverallPercent:      progress.OverallPercent,
				CurrentTable:        progress.CurrentTable,
				CurrentTablePercent: progress.CurrentTablePercent,
				BytesProcessed:      progress.BytesProcessed,
				ThroughputMbSec:     progress.ThroughputMBSec,
				EtaSeconds:          int32(progress.ETASeconds),
				Lsn:                 progress.LSN,
				Complete:            progress.Complete,
				Error:               progress.Error,
			})
			if err != nil && s.debug {
				s.logger.Printf("Failed to send snapshot progress: %v", err)
			}
		},
	}

	// Generate the snapshot
	generator := s.manager.SnapshotGenerator()
	manifest, err := generator.Generate(stream.Context(), req.SourceNodeId, opts)
	if err != nil {
		// Send error progress
		_ = stream.Send(&pb.SnapshotProgress{
			Phase:    "error",
			Complete: true,
			Error:    err.Error(),
		})
		return status.Errorf(codes.Internal, "snapshot generation failed: %v", err)
	}

	// Send final completion message
	return stream.Send(&pb.SnapshotProgress{
		SnapshotId:     manifest.SnapshotID,
		Phase:          "complete",
		OverallPercent: 100,
		Lsn:            manifest.LSN,
		Complete:       true,
	})
}

// ApplySnapshot handles the ApplySnapshot RPC.
// Implemented in T083 (Phase 11: Two-Phase Snapshot).
func (s *InitServer) ApplySnapshot(req *pb.ApplySnapshotRequest, stream grpc.ServerStreamingServer[pb.SnapshotProgress]) error {
	s.logRequest("ApplySnapshot", req.TargetNodeId)

	if req.TargetNodeId == "" {
		return status.Error(codes.InvalidArgument, "target_node_id is required")
	}
	if req.InputPath == "" {
		return status.Error(codes.InvalidArgument, "input_path is required")
	}

	// This is a skeleton - actual implementation in T083
	return status.Error(codes.Unimplemented, "not implemented: ApplySnapshot (see T083)")
}

// StartBidirectionalMerge handles the StartBidirectionalMerge RPC.
// Merges existing data on both nodes and sets up bidirectional replication.
func (s *InitServer) StartBidirectionalMerge(ctx context.Context, req *pb.StartBidirectionalMergeRequest) (*pb.StartBidirectionalMergeResponse, error) {
	s.logRequest("StartBidirectionalMerge", fmt.Sprintf("%s <-> %s", req.NodeAId, req.NodeBId))

	// Validate required fields
	if req.NodeAId == "" {
		return nil, status.Error(codes.InvalidArgument, "node_a_id is required")
	}
	if req.NodeBId == "" {
		return nil, status.Error(codes.InvalidArgument, "node_b_id is required")
	}
	if req.NodeBConnStr == "" {
		return nil, status.Error(codes.InvalidArgument, "node_b_conn_str is required")
	}
	if len(req.Tables) == 0 {
		return nil, status.Error(codes.InvalidArgument, "at least one table is required")
	}

	// Build bidirectional merge request
	mergeReq := replinit.BidirectionalMergeRequest{
		NodeAID:          req.NodeAId,
		NodeBID:          req.NodeBId,
		NodeBConnStr:     req.NodeBConnStr,
		Tables:           req.Tables,
		Strategy:         protoStrategyToConfig(req.Strategy),
		DryRun:           req.DryRun,
		SchemaSync:       protoSchemaSyncToConfig(req.SchemaSync),
		QuiesceTimeoutMs: int(req.QuiesceTimeoutMs),
	}

	// Default quiesce timeout
	if mergeReq.QuiesceTimeoutMs == 0 {
		mergeReq.QuiesceTimeoutMs = 5000
	}

	// Execute bidirectional merge
	result, err := s.manager.Bidirectional().Initialize(ctx, mergeReq)
	if err != nil {
		return &pb.StartBidirectionalMergeResponse{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	// Build response
	resp := &pb.StartBidirectionalMergeResponse{
		Success: true,
		Result: &pb.BidirectionalMergeResult{
			ReplicationSetup: result.ReplicationSetup,
			SlotAToB:         result.SlotAToB,
			SlotBToA:         result.SlotBToA,
		},
	}

	// Populate merge result details
	if result.MergeResult != nil {
		resp.Result.TablesProcessed = int64(len(result.MergeResult.Tables))
		resp.Result.ConflictsDetected = result.MergeResult.TotalConflicts
		resp.Result.ConflictsResolved = result.MergeResult.ConflictsResolved
		resp.Result.RowsTransferredAToB = result.MergeResult.RowsTransferredAToB
		resp.Result.RowsTransferredBToA = result.MergeResult.RowsTransferredBToA

		// Note: Per-table results not currently tracked in MergeResult
		// The Tables field is just a list of table names
	}

	if result.Error != nil {
		resp.Error = result.Error.Error()
	}

	return resp, nil
}

// logRequest logs an incoming RPC request.
func (s *InitServer) logRequest(method string, detail string) {
	if !s.debug {
		return
	}

	if detail != "" {
		s.logger.Printf("gRPC InitService.%s: %s", method, detail)
	} else {
		s.logger.Printf("gRPC InitService.%s", method)
	}
}

// protoMethodToConfig converts a proto InitMethod to config.InitMethod.
func protoMethodToConfig(m pb.InitMethod) config.InitMethod {
	switch m {
	case pb.InitMethod_INIT_METHOD_SNAPSHOT:
		return config.InitMethodSnapshot
	case pb.InitMethod_INIT_METHOD_MANUAL:
		return config.InitMethodManual
	case pb.InitMethod_INIT_METHOD_TWO_PHASE:
		return config.InitMethodTwoPhase
	case pb.InitMethod_INIT_METHOD_DIRECT:
		return config.InitMethodDirect
	case pb.InitMethod_INIT_METHOD_BIDIRECTIONAL_MERGE:
		return config.InitMethodBidirectionalMerge
	default:
		return config.InitMethodSnapshot // Default to snapshot
	}
}

// protoStrategyToConfig converts a proto ConflictStrategy to replinit.ConflictStrategy.
func protoStrategyToConfig(s pb.ConflictStrategy) replinit.ConflictStrategy {
	switch s {
	case pb.ConflictStrategy_CONFLICT_STRATEGY_PREFER_NODE_A:
		return replinit.StrategyPreferNodeA
	case pb.ConflictStrategy_CONFLICT_STRATEGY_PREFER_NODE_B:
		return replinit.StrategyPreferNodeB
	case pb.ConflictStrategy_CONFLICT_STRATEGY_LAST_MODIFIED:
		return replinit.StrategyLastModified
	case pb.ConflictStrategy_CONFLICT_STRATEGY_MANUAL:
		return replinit.StrategyManual
	default:
		return replinit.StrategyPreferNodeA // Default
	}
}

// protoSchemaSyncToConfig converts a proto SchemaSyncMode to config.SchemaSyncMode.
func protoSchemaSyncToConfig(m pb.SchemaSyncMode) config.SchemaSyncMode {
	switch m {
	case pb.SchemaSyncMode_SCHEMA_SYNC_STRICT:
		return config.SchemaSyncStrict
	case pb.SchemaSyncMode_SCHEMA_SYNC_AUTO:
		return config.SchemaSyncAuto
	case pb.SchemaSyncMode_SCHEMA_SYNC_MANUAL:
		return config.SchemaSyncManual
	default:
		return config.SchemaSyncStrict // Default to strict
	}
}

// modelStateToProto converts a models.InitState to pb.InitState.
func modelStateToProto(s models.InitState) pb.InitState {
	switch s {
	case models.InitStateUninitialized:
		return pb.InitState_INIT_STATE_UNINITIALIZED
	case models.InitStatePreparing:
		return pb.InitState_INIT_STATE_PREPARING
	case models.InitStateCopying:
		return pb.InitState_INIT_STATE_COPYING
	case models.InitStateCatchingUp:
		return pb.InitState_INIT_STATE_CATCHING_UP
	case models.InitStateSynchronized:
		return pb.InitState_INIT_STATE_SYNCHRONIZED
	case models.InitStateDiverged:
		return pb.InitState_INIT_STATE_DIVERGED
	case models.InitStateFailed:
		return pb.InitState_INIT_STATE_FAILED
	case models.InitStateReinitializing:
		return pb.InitState_INIT_STATE_REINITIALIZING
	default:
		return pb.InitState_INIT_STATE_UNSPECIFIED
	}
}

// comparisonStatusToProto converts an init.ComparisonStatus to pb.ComparisonStatus.
func comparisonStatusToProto(s replinit.ComparisonStatus) pb.ComparisonStatus {
	switch s {
	case replinit.ComparisonMatch:
		return pb.ComparisonStatus_COMPARISON_STATUS_MATCH
	case replinit.ComparisonMismatch:
		return pb.ComparisonStatus_COMPARISON_STATUS_MISMATCH
	case replinit.ComparisonLocalOnly:
		return pb.ComparisonStatus_COMPARISON_STATUS_LOCAL_ONLY
	case replinit.ComparisonRemoteOnly:
		return pb.ComparisonStatus_COMPARISON_STATUS_REMOTE_ONLY
	default:
		return pb.ComparisonStatus_COMPARISON_STATUS_UNSPECIFIED
	}
}
