package grpc

import (
	"context"
	"fmt"
	"log"
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
// Implemented in T030 (Phase 4: User Story 2).
func (s *InitServer) PrepareInit(ctx context.Context, req *pb.PrepareInitRequest) (*pb.PrepareInitResponse, error) {
	s.logRequest("PrepareInit", req.NodeId)

	if req.NodeId == "" {
		return nil, status.Error(codes.InvalidArgument, "node_id is required")
	}
	if req.SlotName == "" {
		return nil, status.Error(codes.InvalidArgument, "slot_name is required")
	}

	// This is a skeleton - actual implementation in T030
	return &pb.PrepareInitResponse{
		Success: false,
		Error:   "not implemented: PrepareInit (see T030)",
	}, nil
}

// CompleteInit handles the CompleteInit RPC.
// Implemented in T032 (Phase 4: User Story 2).
func (s *InitServer) CompleteInit(ctx context.Context, req *pb.CompleteInitRequest) (*pb.CompleteInitResponse, error) {
	s.logRequest("CompleteInit", req.TargetNodeId)

	if req.TargetNodeId == "" {
		return nil, status.Error(codes.InvalidArgument, "target_node_id is required")
	}
	if req.SourceNodeId == "" {
		return nil, status.Error(codes.InvalidArgument, "source_node_id is required")
	}

	// This is a skeleton - actual implementation in T032
	return &pb.CompleteInitResponse{
		Success: false,
		Error:   "not implemented: CompleteInit (see T032)",
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
	if err := s.manager.StartReinit(ctx, opts); err != nil {
		return &pb.StartReinitResponse{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	return &pb.StartReinitResponse{
		Success: true,
		State:   pb.InitState_INIT_STATE_UNINITIALIZED,
	}, nil
}

// CompareSchemas handles the CompareSchemas RPC.
// Implemented in T057 (Phase 7: User Story 5).
func (s *InitServer) CompareSchemas(ctx context.Context, req *pb.CompareSchemasRequest) (*pb.CompareSchemasResponse, error) {
	s.logRequest("CompareSchemas", fmt.Sprintf("%s vs %s", req.LocalNodeId, req.RemoteNodeId))

	if req.LocalNodeId == "" {
		return nil, status.Error(codes.InvalidArgument, "local_node_id is required")
	}
	if req.RemoteNodeId == "" {
		return nil, status.Error(codes.InvalidArgument, "remote_node_id is required")
	}

	// This is a skeleton - actual implementation in T057
	return &pb.CompareSchemasResponse{
		Success:      false,
		Error:        "not implemented: CompareSchemas (see T057)",
		MatchCount:   0,
		MismatchCount: 0,
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

	// This is a skeleton - actual implementation in T080
	return status.Error(codes.Unimplemented, "not implemented: GenerateSnapshot (see T080)")
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
	default:
		return config.InitMethodSnapshot // Default to snapshot
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
