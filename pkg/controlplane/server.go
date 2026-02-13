package controlplane

import (
	"context"
	"fmt"
	"log/slog"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/NavarchProject/navarch/pkg/clock"
	"github.com/NavarchProject/navarch/pkg/controlplane/db"
	"github.com/NavarchProject/navarch/pkg/gpu"
	"github.com/NavarchProject/navarch/pkg/health"
	"github.com/NavarchProject/navarch/pkg/notifier"
	pb "github.com/NavarchProject/navarch/proto"
)

// NodeHealthObserver is notified when node health status changes.
// Implement this interface to react to health transitions (e.g., auto-replacement).
type NodeHealthObserver interface {
	OnNodeUnhealthy(ctx context.Context, nodeID string)
}

// Server implements the ControlPlaneService Connect service.
type Server struct {
	db              db.DB
	config          Config
	clock           clock.Clock
	logger          *slog.Logger
	metricsSource   *DBMetricsSource
	instanceManager *InstanceManager
	healthObserver  NodeHealthObserver
	healthEvaluator *health.Evaluator
	notifier        notifier.Notifier
}

// Config holds configuration for the control plane server.
type Config struct {
	HealthCheckIntervalSeconds int32
	HeartbeatIntervalSeconds   int32
	EnabledHealthChecks        []string
	HealthPolicy               *health.Policy // Health policy for CEL evaluation. If nil, uses default.
	Clock                      clock.Clock    // Clock for time operations. If nil, uses real time.
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
// The instanceManager parameter is optional; if nil, instance lifecycle tracking
// on node registration is disabled.
func NewServer(database db.DB, cfg Config, instanceManager *InstanceManager, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}

	clk := cfg.Clock
	if clk == nil {
		clk = clock.Real()
	}

	metricsSource := NewDBMetricsSourceWithClock(database, clk, logger)

	// Create health evaluator
	policy := cfg.HealthPolicy
	if policy == nil {
		policy = health.DefaultPolicy()
	}
	evaluator, err := health.NewEvaluator(policy)
	if err != nil {
		logger.Error("failed to create health evaluator, CEL policies disabled", slog.String("error", err.Error()))
		evaluator = nil
	}

	return &Server{
		db:              database,
		config:          cfg,
		clock:           clk,
		logger:          logger,
		metricsSource:   metricsSource,
		instanceManager: instanceManager,
		healthEvaluator: evaluator,
	}
}

// SetHealthObserver sets the observer to be notified on health status changes.
func (s *Server) SetHealthObserver(observer NodeHealthObserver) {
	s.healthObserver = observer
}

// SetNotifier sets the notifier for cordon/drain operations.
// If not set, cordon/drain only update internal status without notifying
// an external workload system.
func (s *Server) SetNotifier(n notifier.Notifier) {
	s.notifier = n
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

	// Update instance tracking if enabled
	// The node_id is the same as the instance_id from the cloud provider
	if s.instanceManager != nil {
		if err := s.instanceManager.TrackNodeRegistered(ctx, req.Msg.NodeId, req.Msg.NodeId); err != nil {
			// Log but don't fail - the node is still registered successfully
			s.logger.WarnContext(ctx, "failed to update instance tracking",
				slog.String("node_id", req.Msg.NodeId),
				slog.String("error", err.Error()),
			)
		}
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
		slog.Int("event_count", len(req.Msg.Events)),
	)

	if req.Msg.NodeId == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("node_id is required"))
	}

	// Get node to determine current status before health check
	node, err := s.db.GetNode(ctx, req.Msg.NodeId)
	if err != nil {
		s.logger.WarnContext(ctx, "received health report from unregistered node",
			slog.String("node_id", req.Msg.NodeId),
		)
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("node not found: %s", req.Msg.NodeId))
	}
	wasUnhealthy := node.Status == pb.NodeStatus_NODE_STATUS_UNHEALTHY

	// Evaluate health events with CEL policies if present
	results := req.Msg.Results
	if len(req.Msg.Events) > 0 && s.healthEvaluator != nil {
		evalResult := s.evaluateHealthEvents(ctx, req.Msg.Events)
		if evalResult != nil {
			results = append(results, evalResult)
		}
	}

	healthRecord := &db.HealthCheckRecord{
		NodeID:    req.Msg.NodeId,
		Timestamp: s.clock.Now(),
		Results:   results,
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

	// Notify observer if node transitioned to unhealthy.
	// Use background context since request context may be cancelled after response.
	if !wasUnhealthy && node.Status == pb.NodeStatus_NODE_STATUS_UNHEALTHY && s.healthObserver != nil {
		go s.healthObserver.OnNodeUnhealthy(context.Background(), req.Msg.NodeId)
	}

	return connect.NewResponse(&pb.ReportHealthResponse{
		Acknowledged: true,
		NodeStatus:   node.Status,
	}), nil
}

// evaluateHealthEvents evaluates raw health events against CEL policies.
func (s *Server) evaluateHealthEvents(ctx context.Context, protoEvents []*pb.HealthEvent) *pb.HealthCheckResult {
	// Convert proto events to internal format
	events := gpu.HealthEventsFromProto(protoEvents)

	result, err := s.healthEvaluator.Evaluate(ctx, events)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to evaluate health events",
			slog.String("error", err.Error()),
		)
		return nil
	}

	// Convert evaluation result to health check result
	var status pb.HealthStatus
	switch result.Status {
	case health.ResultHealthy:
		status = pb.HealthStatus_HEALTH_STATUS_HEALTHY
	case health.ResultDegraded:
		status = pb.HealthStatus_HEALTH_STATUS_DEGRADED
	case health.ResultUnhealthy:
		status = pb.HealthStatus_HEALTH_STATUS_UNHEALTHY
	default:
		status = pb.HealthStatus_HEALTH_STATUS_UNKNOWN
	}

	msg := fmt.Sprintf("CEL policy evaluation: %s", result.Status)
	if result.MatchedRule != "" {
		msg = fmt.Sprintf("CEL policy: %s (rule: %s)", result.Status, result.MatchedRule)
	}

	s.logger.DebugContext(ctx, "health events evaluated",
		slog.String("status", string(result.Status)),
		slog.String("matched_rule", result.MatchedRule),
		slog.Int("event_count", len(events)),
	)

	return &pb.HealthCheckResult{
		CheckName: "cel_policy",
		Status:    status,
		Message:   msg,
	}
}

// SendHeartbeat handles heartbeat messages from nodes.
func (s *Server) SendHeartbeat(ctx context.Context, req *connect.Request[pb.HeartbeatRequest]) (*connect.Response[pb.HeartbeatResponse], error) {
	s.logger.DebugContext(ctx, "heartbeat received", slog.String("node_id", req.Msg.NodeId))

	if req.Msg.NodeId == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("node_id is required"))
	}

	timestamp := s.clock.Now()
	if req.Msg.Timestamp != nil {
		timestamp = req.Msg.Timestamp.AsTime()
	}

	if err := s.db.UpdateNodeHeartbeat(ctx, req.Msg.NodeId, timestamp); err != nil {
		s.logger.WarnContext(ctx, "received heartbeat from unregistered node",
			slog.String("node_id", req.Msg.NodeId),
		)
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("node not found: %s", req.Msg.NodeId))
	}

	// Store metrics if provided
	if req.Msg.Metrics != nil {
		if err := s.metricsSource.StoreMetrics(ctx, req.Msg.NodeId, req.Msg.Metrics); err != nil {
			s.logger.WarnContext(ctx, "failed to store metrics",
				slog.String("node_id", req.Msg.NodeId),
				slog.String("error", err.Error()),
			)
			// Don't fail the heartbeat if metrics storage fails
		}
	}

	return connect.NewResponse(&pb.HeartbeatResponse{
		Acknowledged: true,
	}), nil
}

// GetNodeCommands returns pending commands for a node.
func (s *Server) GetNodeCommands(ctx context.Context, req *connect.Request[pb.GetNodeCommandsRequest]) (*connect.Response[pb.GetNodeCommandsResponse], error) {
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

	pbCommands := make([]*pb.NodeCommand, len(commands))
	for i, cmd := range commands {
		pbCommands[i] = &pb.NodeCommand{
			CommandId:  cmd.CommandID,
			Type:       cmd.Type,
			Parameters: cmd.Parameters,
			IssuedAt:   timestamppb.New(cmd.IssuedAt),
		}

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

	var filtered []*db.NodeRecord
	for _, node := range nodes {
		if req.Msg.Provider != "" && node.Provider != req.Msg.Provider {
			continue
		}
		if req.Msg.Region != "" && node.Region != req.Msg.Region {
			continue
		}
		if req.Msg.Status != pb.NodeStatus_NODE_STATUS_UNKNOWN && node.Status != req.Msg.Status {
			continue
		}
		filtered = append(filtered, node)
	}

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
	if req.Msg.NodeId == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("node_id is required"))
	}
	if req.Msg.CommandType == pb.NodeCommandType_NODE_COMMAND_TYPE_UNKNOWN {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("command_type is required"))
	}

	node, err := s.db.GetNode(ctx, req.Msg.NodeId)
	if err != nil {
		s.logger.WarnContext(ctx, "cannot issue command to unknown node",
			slog.String("node_id", req.Msg.NodeId),
		)
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("node not found: %s", req.Msg.NodeId))
	}

	// Handle node status updates for cordon/uncordon/drain commands.
	reason := req.Msg.Parameters["reason"]
	nodeID := req.Msg.NodeId
	previousStatus := node.Status

	switch req.Msg.CommandType {
	case pb.NodeCommandType_NODE_COMMAND_TYPE_CORDON:
		err := s.updateStatusAndNotify(ctx, nodeID, pb.NodeStatus_NODE_STATUS_CORDONED, previousStatus,
			func() error { return s.notifier.Cordon(ctx, nodeID, reason) })
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to cordon node: %w", err))
		}

	case pb.NodeCommandType_NODE_COMMAND_TYPE_UNCORDON:
		if node.Status != pb.NodeStatus_NODE_STATUS_CORDONED {
			return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("node %s is not cordoned (current status: %s)", nodeID, node.Status.String()))
		}
		err := s.updateStatusAndNotify(ctx, nodeID, pb.NodeStatus_NODE_STATUS_ACTIVE, previousStatus,
			func() error { return s.notifier.Uncordon(ctx, nodeID) })
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to uncordon node: %w", err))
		}

	case pb.NodeCommandType_NODE_COMMAND_TYPE_DRAIN:
		err := s.updateStatusAndNotify(ctx, nodeID, pb.NodeStatus_NODE_STATUS_DRAINING, previousStatus,
			func() error { return s.notifier.Drain(ctx, nodeID, reason) })
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to drain node: %w", err))
		}
	}

	// Cordon/uncordon/drain are control-plane-only operations.
	// Record them in the DB for audit trail, but mark as "completed" so they
	// don't appear in GetPendingCommands (nodes don't need to act on them).
	if req.Msg.CommandType == pb.NodeCommandType_NODE_COMMAND_TYPE_CORDON ||
		req.Msg.CommandType == pb.NodeCommandType_NODE_COMMAND_TYPE_UNCORDON ||
		req.Msg.CommandType == pb.NodeCommandType_NODE_COMMAND_TYPE_DRAIN {

		commandID := uuid.New().String()
		issuedAt := s.clock.Now()

		record := &db.CommandRecord{
			CommandID:  commandID,
			NodeID:     nodeID,
			Type:       req.Msg.CommandType,
			Parameters: req.Msg.Parameters,
			IssuedAt:   issuedAt,
			Status:     "completed", // CP-only: don't queue to node
		}

		if err := s.db.CreateCommand(ctx, record); err != nil {
			s.logger.ErrorContext(ctx, "failed to record command",
				slog.String("command_id", commandID),
				slog.String("node_id", nodeID),
				slog.String("error", err.Error()),
			)
			// Non-fatal: command succeeded, just failed to record
		}

		s.logger.InfoContext(ctx, "processed control plane command",
			slog.String("command_id", commandID),
			slog.String("node_id", nodeID),
			slog.String("command_type", req.Msg.CommandType.String()),
		)

		return connect.NewResponse(&pb.IssueCommandResponse{
			CommandId: commandID,
			IssuedAt:  timestamppb.New(issuedAt),
		}), nil
	}

	// For other command types, queue to the node agent
	commandID := uuid.New().String()
	issuedAt := s.clock.Now()

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

// ListInstances returns all tracked instances with optional filters.
func (s *Server) ListInstances(ctx context.Context, req *connect.Request[pb.ListInstancesRequest]) (*connect.Response[pb.ListInstancesResponse], error) {
	instances, err := s.db.ListInstances(ctx)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to list instances",
			slog.String("error", err.Error()),
		)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to list instances: %w", err))
	}

	var filtered []*db.InstanceRecord
	for _, instance := range instances {
		if req.Msg.Provider != "" && instance.Provider != req.Msg.Provider {
			continue
		}
		if req.Msg.Region != "" && instance.Region != req.Msg.Region {
			continue
		}
		if req.Msg.State != pb.InstanceState_INSTANCE_STATE_UNKNOWN && instance.State != req.Msg.State {
			continue
		}
		if req.Msg.PoolName != "" && instance.PoolName != req.Msg.PoolName {
			continue
		}
		filtered = append(filtered, instance)
	}

	pbInstances := make([]*pb.InstanceInfo, len(filtered))
	for i, instance := range filtered {
		pbInstances[i] = s.instanceRecordToProto(instance)
	}

	s.logger.DebugContext(ctx, "listed instances",
		slog.Int("total", len(instances)),
		slog.Int("filtered", len(filtered)),
	)

	return connect.NewResponse(&pb.ListInstancesResponse{
		Instances: pbInstances,
	}), nil
}

// GetInstance returns details about a specific instance.
func (s *Server) GetInstance(ctx context.Context, req *connect.Request[pb.GetInstanceRequest]) (*connect.Response[pb.GetInstanceResponse], error) {
	if req.Msg.InstanceId == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("instance_id is required"))
	}

	instance, err := s.db.GetInstance(ctx, req.Msg.InstanceId)
	if err != nil {
		s.logger.WarnContext(ctx, "instance not found",
			slog.String("instance_id", req.Msg.InstanceId),
		)
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("instance not found: %s", req.Msg.InstanceId))
	}

	return connect.NewResponse(&pb.GetInstanceResponse{
		Instance: s.instanceRecordToProto(instance),
	}), nil
}

// instanceRecordToProto converts a db.InstanceRecord to a pb.InstanceInfo.
func (s *Server) instanceRecordToProto(record *db.InstanceRecord) *pb.InstanceInfo {
	info := &pb.InstanceInfo{
		InstanceId:    record.InstanceID,
		Provider:      record.Provider,
		Region:        record.Region,
		Zone:          record.Zone,
		InstanceType:  record.InstanceType,
		State:         record.State,
		PoolName:      record.PoolName,
		NodeId:        record.NodeID,
		StatusMessage: record.StatusMessage,
		Labels:        record.Labels,
	}

	if !record.CreatedAt.IsZero() {
		info.CreatedAt = timestamppb.New(record.CreatedAt)
	}
	if !record.ReadyAt.IsZero() {
		info.ReadyAt = timestamppb.New(record.ReadyAt)
	}
	if !record.TerminatedAt.IsZero() {
		info.TerminatedAt = timestamppb.New(record.TerminatedAt)
	}

	return info
}

// updateStatusAndNotify updates a node's status and notifies the external system.
// If notification fails, the status change is rolled back.
func (s *Server) updateStatusAndNotify(
	ctx context.Context,
	nodeID string,
	newStatus pb.NodeStatus,
	previousStatus pb.NodeStatus,
	notify func() error,
) error {
	if err := s.db.UpdateNodeStatus(ctx, nodeID, newStatus); err != nil {
		return err
	}

	if s.notifier != nil && notify != nil {
		if err := notify(); err != nil {
			s.logger.ErrorContext(ctx, "notifier failed, rolling back status",
				slog.String("node_id", nodeID),
				slog.String("error", err.Error()),
			)
			if rbErr := s.db.UpdateNodeStatus(ctx, nodeID, previousStatus); rbErr != nil {
				s.logger.ErrorContext(ctx, "failed to roll back node status",
					slog.String("node_id", nodeID),
					slog.String("error", rbErr.Error()),
				)
			}
			return err
		}
	}
	return nil
}
