package node

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"connectrpc.com/connect"

	pb "github.com/NavarchProject/navarch/proto"
	"github.com/NavarchProject/navarch/proto/protoconnect"
)

// Config holds configuration for the node daemon.
type Config struct {
	// ControlPlaneAddr is the address of the control plane HTTP server.
	ControlPlaneAddr string

	// NodeID is the unique identifier for this node.
	NodeID string

	// Provider is the cloud provider name (e.g., "gcp", "aws").
	Provider string

	// Region is the cloud region.
	Region string

	// Zone is the availability zone.
	Zone string

	// InstanceType is the instance type (e.g., "a3-highgpu-8g").
	InstanceType string
}

// Node represents the node daemon that communicates with the control plane.
type Node struct {
	config Config
	client protoconnect.ControlPlaneServiceClient
	logger *slog.Logger

	// Configuration received from control plane
	healthCheckInterval time.Duration
	heartbeatInterval   time.Duration
}

// New creates a new node daemon.
// If logger is nil, a default logger is used.
func New(cfg Config, logger *slog.Logger) (*Node, error) {
	if cfg.ControlPlaneAddr == "" {
		return nil, fmt.Errorf("control plane address is required")
	}
	if cfg.NodeID == "" {
		return nil, fmt.Errorf("node ID is required")
	}
	if logger == nil {
		logger = slog.Default()
	}

	return &Node{
		config:              cfg,
		logger:              logger,
		healthCheckInterval: 60 * time.Second, // Default values
		heartbeatInterval:   30 * time.Second,
	}, nil
}

// Start initializes the connection to the control plane and starts the node daemon.
func (n *Node) Start(ctx context.Context) error {
	// Create Connect client
	n.client = protoconnect.NewControlPlaneServiceClient(
		http.DefaultClient,
		n.config.ControlPlaneAddr,
	)

	n.logger.InfoContext(ctx, "connected to control plane",
		slog.String("addr", n.config.ControlPlaneAddr),
	)

	// Register with control plane
	if err := n.register(ctx); err != nil {
		return fmt.Errorf("failed to register with control plane: %w", err)
	}

	n.logger.InfoContext(ctx, "successfully registered with control plane")

	// Start background goroutines
	go n.heartbeatLoop(ctx)
	go n.healthCheckLoop(ctx)
	go n.commandPollLoop(ctx)

	return nil
}

// Stop gracefully shuts down the node daemon.
func (n *Node) Stop() error {
	n.logger.Info("stopping node daemon")
	// No connection to close with Connect (uses http.Client)
	return nil
}

// register registers the node with the control plane.
func (n *Node) register(ctx context.Context) error {
	req := connect.NewRequest(&pb.RegisterNodeRequest{
		NodeId:       n.config.NodeID,
		Provider:     n.config.Provider,
		Region:       n.config.Region,
		Zone:         n.config.Zone,
		InstanceType: n.config.InstanceType,
		Gpus:         []*pb.GPUInfo{}, // TODO: Detect GPUs
		Metadata:     &pb.NodeMetadata{
			Hostname:   n.config.NodeID, // TODO: Get actual hostname
			InternalIp: "",               // TODO: Get actual IP
			ExternalIp: "",
			Labels:     make(map[string]string),
		},
	})
	
	resp, err := n.client.RegisterNode(ctx, req)
	if err != nil {
		return fmt.Errorf("registration failed: %w", err)
	}
	
	if !resp.Msg.Success {
		return fmt.Errorf("registration rejected: %s", resp.Msg.Message)
	}
	
	// Update configuration from control plane
	if resp.Msg.Config != nil {
		n.healthCheckInterval = time.Duration(resp.Msg.Config.HealthCheckIntervalSeconds) * time.Second
		n.heartbeatInterval = time.Duration(resp.Msg.Config.HeartbeatIntervalSeconds) * time.Second
		n.logger.InfoContext(ctx, "received config from control plane",
			slog.Duration("health_check_interval", n.healthCheckInterval),
			slog.Duration("heartbeat_interval", n.heartbeatInterval),
		)
	}

	return nil
}

// heartbeatLoop sends periodic heartbeats to the control plane.
func (n *Node) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(n.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := n.sendHeartbeat(ctx); err != nil {
				n.logger.ErrorContext(ctx, "failed to send heartbeat",
					slog.String("error", err.Error()),
				)
			}
		}
	}
}

// sendHeartbeat sends a heartbeat to the control plane.
func (n *Node) sendHeartbeat(ctx context.Context) error {
	req := connect.NewRequest(&pb.HeartbeatRequest{
		NodeId: n.config.NodeID,
		Metrics: &pb.NodeMetrics{
			CpuUsagePercent:    0.0, // TODO: Collect actual metrics
			MemoryUsagePercent: 0.0,
			GpuMetrics:         []*pb.GPUMetrics{},
		},
	})

	resp, err := n.client.SendHeartbeat(ctx, req)
	if err != nil {
		return err
	}

	if resp.Msg.Acknowledged {
		n.logger.DebugContext(ctx, "heartbeat acknowledged")
	}

	return nil
}

// healthCheckLoop runs health checks periodically and reports results.
func (n *Node) healthCheckLoop(ctx context.Context) {
	ticker := time.NewTicker(n.healthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := n.runHealthChecks(ctx); err != nil {
				n.logger.ErrorContext(ctx, "failed to run health checks",
					slog.String("error", err.Error()),
				)
			}
		}
	}
}

// runHealthChecks runs all health checks and reports results to the control plane.
func (n *Node) runHealthChecks(ctx context.Context) error {
	// TODO: Actually run health checks
	req := connect.NewRequest(&pb.ReportHealthRequest{
		NodeId: n.config.NodeID,
		Results: []*pb.HealthCheckResult{
			{
				CheckName: "boot",
				Status:    pb.HealthStatus_HEALTH_STATUS_HEALTHY,
				Message:   "System boot check passed",
			},
		},
	})
	
	resp, err := n.client.ReportHealth(ctx, req)
	if err != nil {
		return err
	}

	if resp.Msg.Acknowledged {
		n.logger.DebugContext(ctx, "health report acknowledged",
			slog.String("node_status", resp.Msg.NodeStatus.String()),
		)
	}

	return nil
}

// commandPollLoop polls for commands from the control plane.
func (n *Node) commandPollLoop(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second) // Poll every 10 seconds
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := n.pollCommands(ctx); err != nil {
				n.logger.ErrorContext(ctx, "failed to poll commands",
					slog.String("error", err.Error()),
				)
			}
		}
	}
}

// pollCommands polls for pending commands from the control plane.
func (n *Node) pollCommands(ctx context.Context) error {
	req := connect.NewRequest(&pb.GetNodeCommandsRequest{
		NodeId: n.config.NodeID,
	})

	resp, err := n.client.GetNodeCommands(ctx, req)
	if err != nil {
		return err
	}

	for _, cmd := range resp.Msg.Commands {
		n.logger.InfoContext(ctx, "received command",
			slog.String("command_type", cmd.Type.String()),
			slog.String("command_id", cmd.CommandId),
		)
		// TODO: Execute commands
	}

	return nil
}

