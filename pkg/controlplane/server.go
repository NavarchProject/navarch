package controlplane

import (
	"context"
	"fmt"
	"log"
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
func NewServer(database db.DB, cfg Config) *Server {
	return &Server{
		db:     database,
		config: cfg,
	}
}

// RegisterNode registers a new node with the control plane.
func (s *Server) RegisterNode(ctx context.Context, req *connect.Request[pb.RegisterNodeRequest]) (*connect.Response[pb.RegisterNodeResponse], error) {
	log.Printf("Registering node: %s (provider=%s, region=%s, zone=%s, type=%s)",
		req.Msg.NodeId, req.Msg.Provider, req.Msg.Region, req.Msg.Zone, req.Msg.InstanceType)

	// Validate request
	if req.Msg.NodeId == "" {
		return connect.NewResponse(&pb.RegisterNodeResponse{
			Success: false,
			Message: "node_id is required",
		}), nil
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
		log.Printf("Failed to register node %s: %v", req.Msg.NodeId, err)
		return connect.NewResponse(&pb.RegisterNodeResponse{
			Success: false,
			Message: fmt.Sprintf("registration failed: %v", err),
		}), nil
	}

	log.Printf("Node %s registered successfully", req.Msg.NodeId)

	return connect.NewResponse(&pb.RegisterNodeResponse{
		Success: true,
		Message: "registration successful",
		Config:  record.Config,
	}), nil
}

// ReportHealth handles health check reports from nodes.
func (s *Server) ReportHealth(ctx context.Context, req *connect.Request[pb.ReportHealthRequest]) (*connect.Response[pb.ReportHealthResponse], error) {
	log.Printf("Health report from node %s: %d checks", req.Msg.NodeId, len(req.Msg.Results))

	// Get node to determine current status
	node, err := s.db.GetNode(ctx, req.Msg.NodeId)
	if err != nil {
		log.Printf("Node %s not found: %v", req.Msg.NodeId, err)
		return connect.NewResponse(&pb.ReportHealthResponse{
			Acknowledged: false,
			NodeStatus:   pb.NodeStatus_NODE_STATUS_UNKNOWN,
		}), nil
	}

	// Record health check
	healthRecord := &db.HealthCheckRecord{
		NodeID:    req.Msg.NodeId,
		Timestamp: time.Now(),
		Results:   req.Msg.Results,
	}

	if err := s.db.RecordHealthCheck(ctx, healthRecord); err != nil {
		log.Printf("Failed to record health check for node %s: %v", req.Msg.NodeId, err)
		return connect.NewResponse(&pb.ReportHealthResponse{
			Acknowledged: false,
			NodeStatus:   node.Status,
		}), nil
	}

	// Fetch updated node status (RecordHealthCheck may have updated it)
	node, _ = s.db.GetNode(ctx, req.Msg.NodeId)

	return connect.NewResponse(&pb.ReportHealthResponse{
		Acknowledged: true,
		NodeStatus:   node.Status,
	}), nil
}

// SendHeartbeat handles heartbeat messages from nodes.
func (s *Server) SendHeartbeat(ctx context.Context, req *connect.Request[pb.HeartbeatRequest]) (*connect.Response[pb.HeartbeatResponse], error) {
	log.Printf("Heartbeat from node %s", req.Msg.NodeId)

	// Update heartbeat timestamp
	timestamp := time.Now()
	if req.Msg.Timestamp != nil {
		timestamp = req.Msg.Timestamp.AsTime()
	}

	if err := s.db.UpdateNodeHeartbeat(ctx, req.Msg.NodeId, timestamp); err != nil {
		log.Printf("Failed to update heartbeat for node %s: %v", req.Msg.NodeId, err)
		return connect.NewResponse(&pb.HeartbeatResponse{
			Acknowledged: false,
		}), nil
	}

	// TODO: Store metrics if provided

	return connect.NewResponse(&pb.HeartbeatResponse{
		Acknowledged: true,
	}), nil
}

// GetNodeCommands returns pending commands for a node.
func (s *Server) GetNodeCommands(ctx context.Context, req *connect.Request[pb.GetNodeCommandsRequest]) (*connect.Response[pb.GetNodeCommandsResponse], error) {
	commands, err := s.db.GetPendingCommands(ctx, req.Msg.NodeId)
	if err != nil {
		log.Printf("Failed to get commands for node %s: %v", req.Msg.NodeId, err)
		return connect.NewResponse(&pb.GetNodeCommandsResponse{
			Commands: []*pb.NodeCommand{},
		}), nil
	}

	if len(commands) > 0 {
		log.Printf("Returning %d pending commands for node %s", len(commands), req.Msg.NodeId)
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
		_ = s.db.UpdateCommandStatus(ctx, cmd.CommandID, "acknowledged")
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

	log.Printf("Issued command %s to node %s: type=%s", commandID, nodeID, cmdType)
	return commandID, nil
}

// ListNodes returns all registered nodes.
// This is not part of the ControlPlaneService, but a helper for control plane operations.
func (s *Server) ListNodes(ctx context.Context) ([]*db.NodeRecord, error) {
	return s.db.ListNodes(ctx)
}
