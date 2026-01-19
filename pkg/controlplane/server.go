package controlplane

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/NavarchProject/navarch/pkg/controlplane/db"
	pb "github.com/NavarchProject/navarch/proto"
)

// Server implements the ControlPlaneService Connect service.
type Server struct {
	db     db.DB
	config Config
	logger *slog.Logger
}

// Config holds configuration for the control plane server.
type Config struct {
	// Default health check interval in seconds
	HealthCheckIntervalSeconds int32

	// Default heartbeat interval in seconds
	HeartbeatIntervalSeconds int32

	// Enabled health checks
	EnabledHealthChecks []string
}

// DefaultConfig returns a sensible default configuration.
func DefaultConfig() Config {
	return Config{
		HealthCheckIntervalSeconds: 60,
		HeartbeatIntervalSeconds:   30,
		EnabledHealthChecks:        []string{"boot", "nvml", "xid"},
	}
}

// NewServer creates a new Server. If logger is nil, slog.Default() is used.
func NewServer(database db.DB, cfg Config, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{
		db:     database,
		config: cfg,
		logger: logger,
	}
}

func (s *Server) RegisterNode(ctx context.Context, req *connect.Request[pb.RegisterNodeRequest]) (*connect.Response[pb.RegisterNodeResponse], error) {
	s.logger.InfoContext(ctx, "registering node",
		slog.String("node_id", req.Msg.NodeId),
		slog.String("provider", req.Msg.Provider),
		slog.String("region", req.Msg.Region),
		slog.String("zone", req.Msg.Zone),
		slog.String("instance_type", req.Msg.InstanceType),
	)

	if req.Msg.NodeId == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("node_id is required"))
	}

	record := &db.NodeRecord{
		NodeID:       req.Msg.NodeId,
		Provider:     req.Msg.Provider,
		Region:       req.Msg.Region,
		Zone:         req.Msg.Zone,
		InstanceType: req.Msg.InstanceType,
		GPUs:         req.Msg.Gpus,
		Metadata:     req.Msg.Metadata,
		Status:       pb.NodeStatus_NODE_STATUS_ACTIVE,
		Config: &pb.NodeConfig{
			HealthCheckIntervalSeconds: s.config.HealthCheckIntervalSeconds,
			HeartbeatIntervalSeconds:   s.config.HeartbeatIntervalSeconds,
			EnabledHealthChecks:        s.config.EnabledHealthChecks,
		},
	}

	if err := s.db.RegisterNode(ctx, record); err != nil {
		s.logger.ErrorContext(ctx, "failed to register node",
			slog.String("node_id", req.Msg.NodeId),
			slog.String("error", err.Error()),
		)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("registration failed: %w", err))
	}

	s.logger.InfoContext(ctx, "node registered successfully", slog.String("node_id", req.Msg.NodeId))

	return connect.NewResponse(&pb.RegisterNodeResponse{
		Success: true,
		Message: "registration successful",
		Config:  record.Config,
	}), nil
}

// ReportHealth handles health check reports from nodes.
func (s *Server) ReportHealth(ctx context.Context, req *connect.Request[pb.ReportHealthRequest]) (*connect.Response[pb.ReportHealthResponse], error) {
	s.logger.DebugContext(ctx, "health report received",
		slog.String("node_id", req.Msg.NodeId),
		slog.Int("check_count", len(req.Msg.Results)),
	)

	// Validate request
	if req.Msg.NodeId == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("node_id is required"))
	}

	// Get node to determine current status
	node, err := s.db.GetNode(ctx, req.Msg.NodeId)
	if err != nil {
		s.logger.WarnContext(ctx, "received health report from unregistered node",
			slog.String("node_id", req.Msg.NodeId),
		)
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("node not found: %s", req.Msg.NodeId))
	}

	// Record health check
	healthRecord := &db.HealthCheckRecord{
		NodeID:    req.Msg.NodeId,
		Timestamp: time.Now(),
		Results:   req.Msg.Results,
	}

	if err := s.db.RecordHealthCheck(ctx, healthRecord); err != nil {
		s.logger.ErrorContext(ctx, "failed to record health check",
			slog.String("node_id", req.Msg.NodeId),
			slog.String("error", err.Error()),
		)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to record health check: %w", err))
	}

	// Fetch updated node status (RecordHealthCheck may have updated it)
	node, err = s.db.GetNode(ctx, req.Msg.NodeId)
	if err != nil {
		// This shouldn't happen since we just verified the node exists
		s.logger.ErrorContext(ctx, "unexpected error fetching node after health check",
			slog.String("node_id", req.Msg.NodeId),
			slog.String("error", err.Error()),
		)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to fetch node status: %w", err))
	}

	return connect.NewResponse(&pb.ReportHealthResponse{
		Acknowledged: true,
		NodeStatus:   node.Status,
	}), nil
}

// SendHeartbeat handles heartbeat messages from nodes.
func (s *Server) SendHeartbeat(ctx context.Context, req *connect.Request[pb.HeartbeatRequest]) (*connect.Response[pb.HeartbeatResponse], error) {
	s.logger.DebugContext(ctx, "heartbeat received", slog.String("node_id", req.Msg.NodeId))

	// Validate request
	if req.Msg.NodeId == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("node_id is required"))
	}

	// Update heartbeat timestamp
	timestamp := time.Now()
	if req.Msg.Timestamp != nil {
		timestamp = req.Msg.Timestamp.AsTime()
	}

	if err := s.db.UpdateNodeHeartbeat(ctx, req.Msg.NodeId, timestamp); err != nil {
		s.logger.WarnContext(ctx, "received heartbeat from unregistered node",
			slog.String("node_id", req.Msg.NodeId),
		)
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("node not found: %s", req.Msg.NodeId))
	}

	// TODO: Store metrics if provided

	return connect.NewResponse(&pb.HeartbeatResponse{
		Acknowledged: true,
	}), nil
}

// GetNodeCommands returns pending commands for a node.
func (s *Server) GetNodeCommands(ctx context.Context, req *connect.Request[pb.GetNodeCommandsRequest]) (*connect.Response[pb.GetNodeCommandsResponse], error) {
	// Validate request
	if req.Msg.NodeId == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("node_id is required"))
	}

	commands, err := s.db.GetPendingCommands(ctx, req.Msg.NodeId)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to get commands",
			slog.String("node_id", req.Msg.NodeId),
			slog.String("error", err.Error()),
		)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get commands: %w", err))
	}

	if len(commands) > 0 {
		s.logger.DebugContext(ctx, "returning pending commands",
			slog.String("node_id", req.Msg.NodeId),
			slog.Int("command_count", len(commands)),
		)
	}

	// Convert to proto messages
	pbCommands := make([]*pb.NodeCommand, len(commands))
	for i, cmd := range commands {
		pbCommands[i] = &pb.NodeCommand{
			CommandId:  cmd.CommandID,
			Type:       cmd.Type,
			Parameters: cmd.Parameters,
			IssuedAt:   timestamppb.New(cmd.IssuedAt),
		}

		// Mark as acknowledged
		if err := s.db.UpdateCommandStatus(ctx, cmd.CommandID, "acknowledged"); err != nil {
			s.logger.WarnContext(ctx, "failed to mark command as acknowledged",
				slog.String("command_id", cmd.CommandID),
				slog.String("error", err.Error()),
			)
			// Continue processing other commands - this is a non-fatal error
		}
	}

	return connect.NewResponse(&pb.GetNodeCommandsResponse{
		Commands: pbCommands,
	}), nil
}

// ListNodes returns all registered nodes with optional filters.
func (s *Server) ListNodes(ctx context.Context, req *connect.Request[pb.ListNodesRequest]) (*connect.Response[pb.ListNodesResponse], error) {
	nodes, err := s.db.ListNodes(ctx)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to list nodes",
			slog.String("error", err.Error()),
		)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to list nodes: %w", err))
	}

	// Apply filters
	var filtered []*db.NodeRecord
	for _, node := range nodes {
		// Filter by provider
		if req.Msg.Provider != "" && node.Provider != req.Msg.Provider {
			continue
		}
		// Filter by region
		if req.Msg.Region != "" && node.Region != req.Msg.Region {
			continue
		}
		// Filter by status
		if req.Msg.Status != pb.NodeStatus_NODE_STATUS_UNKNOWN && node.Status != req.Msg.Status {
			continue
		}
		filtered = append(filtered, node)
	}

	// Convert to proto
	pbNodes := make([]*pb.NodeInfo, len(filtered))
	for i, node := range filtered {
		pbNodes[i] = &pb.NodeInfo{
			NodeId:        node.NodeID,
			Provider:      node.Provider,
			Region:        node.Region,
			Zone:          node.Zone,
			InstanceType:  node.InstanceType,
			Status:        node.Status,
			HealthStatus:  node.HealthStatus,
			LastHeartbeat: timestamppb.New(node.LastHeartbeat),
			Gpus:          node.GPUs,
			Metadata:      node.Metadata,
		}
	}

	s.logger.DebugContext(ctx, "listed nodes",
		slog.Int("total", len(nodes)),
		slog.Int("filtered", len(filtered)),
	)

	return connect.NewResponse(&pb.ListNodesResponse{
		Nodes: pbNodes,
	}), nil
}

// GetNode returns details about a specific node.
func (s *Server) GetNode(ctx context.Context, req *connect.Request[pb.GetNodeRequest]) (*connect.Response[pb.GetNodeResponse], error) {
	if req.Msg.NodeId == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("node_id is required"))
	}

	node, err := s.db.GetNode(ctx, req.Msg.NodeId)
	if err != nil {
		s.logger.WarnContext(ctx, "node not found",
			slog.String("node_id", req.Msg.NodeId),
		)
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("node not found: %s", req.Msg.NodeId))
	}

	pbNode := &pb.NodeInfo{
		NodeId:        node.NodeID,
		Provider:      node.Provider,
		Region:        node.Region,
		Zone:          node.Zone,
		InstanceType:  node.InstanceType,
		Status:        node.Status,
		HealthStatus:  node.HealthStatus,
		LastHeartbeat: timestamppb.New(node.LastHeartbeat),
		Gpus:          node.GPUs,
		Metadata:      node.Metadata,
	}

	return connect.NewResponse(&pb.GetNodeResponse{
		Node: pbNode,
	}), nil
}

// IssueCommand issues a command to a specific node.
func (s *Server) IssueCommand(ctx context.Context, req *connect.Request[pb.IssueCommandRequest]) (*connect.Response[pb.IssueCommandResponse], error) {
	// Validate request
	if req.Msg.NodeId == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("node_id is required"))
	}
	if req.Msg.CommandType == pb.NodeCommandType_NODE_COMMAND_TYPE_UNKNOWN {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("command_type is required"))
	}

	// Verify node exists
	_, err := s.db.GetNode(ctx, req.Msg.NodeId)
	if err != nil {
		s.logger.WarnContext(ctx, "cannot issue command to unknown node",
			slog.String("node_id", req.Msg.NodeId),
		)
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("node not found: %s", req.Msg.NodeId))
	}

	commandID := uuid.New().String()
	issuedAt := time.Now()

	record := &db.CommandRecord{
		CommandID:  commandID,
		NodeID:     req.Msg.NodeId,
		Type:       req.Msg.CommandType,
		Parameters: req.Msg.Parameters,
		IssuedAt:   issuedAt,
		Status:     "pending",
	}

	if err := s.db.CreateCommand(ctx, record); err != nil {
		s.logger.ErrorContext(ctx, "failed to create command",
			slog.String("command_id", commandID),
			slog.String("node_id", req.Msg.NodeId),
			slog.String("error", err.Error()),
		)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create command: %w", err))
	}

	s.logger.InfoContext(ctx, "issued command",
		slog.String("command_id", commandID),
		slog.String("node_id", req.Msg.NodeId),
		slog.String("command_type", req.Msg.CommandType.String()),
	)

	return connect.NewResponse(&pb.IssueCommandResponse{
		CommandId: commandID,
		IssuedAt:  timestamppb.New(issuedAt),
	}), nil
}
