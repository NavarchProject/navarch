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

// NewServer creates a new control plane server.
// If logger is nil, a default logger is used.
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

// RegisterNode registers a new node with the control plane.
func (s *Server) RegisterNode(ctx context.Context, req *connect.Request[pb.RegisterNodeRequest]) (*connect.Response[pb.RegisterNodeResponse], error) {
	s.logger.InfoContext(ctx, "registering node",
		slog.String("node_id", req.Msg.NodeId),
		slog.String("provider", req.Msg.Provider),
		slog.String("region", req.Msg.Region),
		slog.String("zone", req.Msg.Zone),
		slog.String("instance_type", req.Msg.InstanceType),
	)

	// Validate request
	if req.Msg.NodeId == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("node_id is required"))
	}

	// Create node record
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

	// Store in database
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
		s.logger.WarnContext(ctx, "node not found for health report",
			slog.String("node_id", req.Msg.NodeId),
			slog.String("error", err.Error()),
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
		s.logger.ErrorContext(ctx, "failed to update heartbeat",
			slog.String("node_id", req.Msg.NodeId),
			slog.String("error", err.Error()),
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

// IssueCommand issues a command to a specific node.
// This is not part of the ControlPlaneService, but a helper for control plane operations.
func (s *Server) IssueCommand(ctx context.Context, nodeID string, cmdType pb.NodeCommandType, params map[string]string) (string, error) {
	commandID := uuid.New().String()

	record := &db.CommandRecord{
		CommandID:  commandID,
		NodeID:     nodeID,
		Type:       cmdType,
		Parameters: params,
		IssuedAt:   time.Now(),
		Status:     "pending",
	}

	if err := s.db.CreateCommand(ctx, record); err != nil {
		return "", fmt.Errorf("failed to create command: %w", err)
	}

	s.logger.InfoContext(ctx, "issued command",
		slog.String("command_id", commandID),
		slog.String("node_id", nodeID),
		slog.String("command_type", cmdType.String()),
	)
	return commandID, nil
}

// ListNodes returns all registered nodes.
// This is not part of the ControlPlaneService, but a helper for control plane operations.
func (s *Server) ListNodes(ctx context.Context) ([]*db.NodeRecord, error) {
	return s.db.ListNodes(ctx)
}
