package node

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"connectrpc.com/connect"

	"github.com/NavarchProject/navarch/pkg/gpu"
	"github.com/NavarchProject/navarch/pkg/node/metrics"
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

	// Labels are user-defined key-value labels for this node.
	Labels map[string]string

	// GPU is the GPU manager to use. If nil, a fake GPU will be created.
	GPU gpu.Manager
}

// Node represents the node daemon that communicates with the control plane.
type Node struct {
	config           Config
	client           protoconnect.ControlPlaneServiceClient
	logger           *slog.Logger
	gpu              gpu.Manager
	metricsCollector metrics.Collector

	// Configuration received from control plane
	healthCheckInterval time.Duration
	heartbeatInterval   time.Duration
}

// New creates a new Node. If logger is nil, slog.Default() is used.
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

	gpuManager := cfg.GPU
	if gpuManager == nil {
		gpuManager = createGPUManager(logger)
	}

	metricsCollector := metrics.NewCollector(gpuManager, nil)

	return &Node{
		config:              cfg,
		logger:              logger,
		gpu:                 gpuManager,
		metricsCollector:    metricsCollector,
		healthCheckInterval: 60 * time.Second,
		heartbeatInterval:   30 * time.Second,
	}, nil
}

// createGPUManager creates a GPU manager based on environment and hardware.
// It uses NVML if available, otherwise falls back to a fake implementation.
func createGPUManager(logger *slog.Logger) gpu.Manager {
	// Check if user explicitly wants fake GPUs
	if os.Getenv("NAVARCH_FAKE_GPU") == "true" {
		return createFakeGPU(logger)
	}

	// Try to use real NVML
	if gpu.IsNVMLAvailable() {
		logger.Info("using NVML GPU manager")
		return gpu.NewNVML()
	}

	// Fall back to fake
	logger.Info("NVML not available, using fake GPU manager")
	return createFakeGPU(logger)
}

func createFakeGPU(logger *slog.Logger) gpu.Manager {
	gpuCount := 8
	if envCount := os.Getenv("NAVARCH_GPU_COUNT"); envCount != "" {
		fmt.Sscanf(envCount, "%d", &gpuCount)
	}
	logger.Info("using fake GPU manager", slog.Int("device_count", gpuCount))
	return gpu.NewFake(gpuCount)
}

func (n *Node) Start(ctx context.Context) error {
	if err := n.gpu.Initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize GPU manager: %w", err)
	}

	n.client = protoconnect.NewControlPlaneServiceClient(
		http.DefaultClient,
		n.config.ControlPlaneAddr,
	)

	n.logger.InfoContext(ctx, "connected to control plane",
		slog.String("addr", n.config.ControlPlaneAddr),
	)

	if err := n.register(ctx); err != nil {
		return fmt.Errorf("failed to register with control plane: %w", err)
	}

	n.logger.InfoContext(ctx, "successfully registered with control plane")

	go n.heartbeatLoop(ctx)
	go n.healthCheckLoop(ctx)
	go n.commandPollLoop(ctx)

	return nil
}

func (n *Node) Stop() error {
	n.logger.Info("stopping node daemon")
	
	ctx := context.Background()
	if err := n.gpu.Shutdown(ctx); err != nil {
		n.logger.Warn("failed to shutdown GPU manager", slog.String("error", err.Error()))
	}
	
	return nil
}

// register registers the node with the control plane.
func (n *Node) register(ctx context.Context) error {
	gpuInfo, err := n.detectGPUs(ctx)
	if err != nil {
		n.logger.Warn("failed to detect GPUs", slog.String("error", err.Error()))
		gpuInfo = []*pb.GPUInfo{}
	}

	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = n.config.NodeID
	}

	labels := make(map[string]string)
	if n.config.Labels != nil {
		for k, v := range n.config.Labels {
			labels[k] = v
		}
	}

	req := connect.NewRequest(&pb.RegisterNodeRequest{
		NodeId:       n.config.NodeID,
		Provider:     n.config.Provider,
		Region:       n.config.Region,
		Zone:         n.config.Zone,
		InstanceType: n.config.InstanceType,
		Gpus:         gpuInfo,
		Metadata:     &pb.NodeMetadata{
			Hostname:   hostname,
			InternalIp: "",
			ExternalIp: "",
			Labels:     labels,
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

// detectGPUs queries the GPU manager and returns GPU information.
func (n *Node) detectGPUs(ctx context.Context) ([]*pb.GPUInfo, error) {
	count, err := n.gpu.GetDeviceCount(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get device count: %w", err)
	}

	n.logger.InfoContext(ctx, "detected GPUs", slog.Int("count", count))

	gpus := make([]*pb.GPUInfo, count)
	for i := 0; i < count; i++ {
		info, err := n.gpu.GetDeviceInfo(ctx, i)
		if err != nil {
			n.logger.WarnContext(ctx, "failed to get GPU info",
				slog.Int("index", i),
				slog.String("error", err.Error()),
			)
			continue
		}

		gpus[i] = &pb.GPUInfo{
			Index:       int32(info.Index),
			Uuid:        info.UUID,
			Name:        info.Name,
			PciBusId:    info.PCIBusID,
			MemoryTotal: int64(info.Memory),
		}
	}

	return gpus, nil
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
	// Collect metrics from system and GPUs
	nodeMetrics, err := n.metricsCollector.Collect(ctx)
	if err != nil {
		n.logger.WarnContext(ctx, "failed to collect metrics, sending empty metrics",
			slog.String("error", err.Error()),
		)
		nodeMetrics = &pb.NodeMetrics{}
	}

	n.logger.DebugContext(ctx, "collected metrics",
		slog.Float64("cpu_percent", nodeMetrics.CpuUsagePercent),
		slog.Float64("memory_percent", nodeMetrics.MemoryUsagePercent),
		slog.Int("gpu_count", len(nodeMetrics.GpuMetrics)),
	)

	req := connect.NewRequest(&pb.HeartbeatRequest{
		NodeId:  n.config.NodeID,
		Metrics: nodeMetrics,
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
	var results []*pb.HealthCheckResult

	bootCheck := n.runBootCheck(ctx)
	results = append(results, bootCheck)

	nvmlCheck := n.runNVMLCheck(ctx)
	results = append(results, nvmlCheck)

	xidCheck := n.runXIDCheck(ctx)
	results = append(results, xidCheck)

	req := connect.NewRequest(&pb.ReportHealthRequest{
		NodeId:  n.config.NodeID,
		Results: results,
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

// runBootCheck verifies GPU devices are detected and accessible.
func (n *Node) runBootCheck(ctx context.Context) *pb.HealthCheckResult {
	count, err := n.gpu.GetDeviceCount(ctx)
	if err != nil {
		return &pb.HealthCheckResult{
			CheckName: "boot",
			Status:    pb.HealthStatus_HEALTH_STATUS_UNHEALTHY,
			Message:   fmt.Sprintf("failed to get device count: %v", err),
		}
	}

	if count == 0 {
		return &pb.HealthCheckResult{
			CheckName: "boot",
			Status:    pb.HealthStatus_HEALTH_STATUS_UNHEALTHY,
			Message:   "no GPUs detected",
		}
	}

	return &pb.HealthCheckResult{
		CheckName: "boot",
		Status:    pb.HealthStatus_HEALTH_STATUS_HEALTHY,
		Message:   fmt.Sprintf("%d GPUs detected and accessible", count),
	}
}

// runNVMLCheck verifies GPU health via NVML metrics.
func (n *Node) runNVMLCheck(ctx context.Context) *pb.HealthCheckResult {
	count, err := n.gpu.GetDeviceCount(ctx)
	if err != nil {
		return &pb.HealthCheckResult{
			CheckName: "nvml",
			Status:    pb.HealthStatus_HEALTH_STATUS_UNHEALTHY,
			Message:   fmt.Sprintf("failed to get device count: %v", err),
		}
	}

	for i := 0; i < count; i++ {
		health, err := n.gpu.GetDeviceHealth(ctx, i)
		if err != nil {
			return &pb.HealthCheckResult{
				CheckName: "nvml",
				Status:    pb.HealthStatus_HEALTH_STATUS_UNHEALTHY,
				Message:   fmt.Sprintf("failed to get health for GPU %d: %v", i, err),
			}
		}

		if health.Temperature > 85 {
			return &pb.HealthCheckResult{
				CheckName: "nvml",
				Status:    pb.HealthStatus_HEALTH_STATUS_DEGRADED,
				Message:   fmt.Sprintf("GPU %d temperature high: %d°C", i, health.Temperature),
			}
		}
	}

	return &pb.HealthCheckResult{
		CheckName: "nvml",
		Status:    pb.HealthStatus_HEALTH_STATUS_HEALTHY,
		Message:   fmt.Sprintf("all %d GPUs healthy", count),
	}
}

// runXIDCheck checks for GPU XID errors.
func (n *Node) runXIDCheck(ctx context.Context) *pb.HealthCheckResult {
	errors, err := n.gpu.GetXIDErrors(ctx)
	if err != nil {
		return &pb.HealthCheckResult{
			CheckName: "xid",
			Status:    pb.HealthStatus_HEALTH_STATUS_UNHEALTHY,
			Message:   fmt.Sprintf("failed to get XID errors: %v", err),
		}
	}

	if len(errors) > 0 {
		return &pb.HealthCheckResult{
			CheckName: "xid",
			Status:    pb.HealthStatus_HEALTH_STATUS_UNHEALTHY,
			Message:   fmt.Sprintf("detected %d XID error(s)", len(errors)),
		}
	}

	return &pb.HealthCheckResult{
		CheckName: "xid",
		Status:    pb.HealthStatus_HEALTH_STATUS_HEALTHY,
		Message:   "no XID errors detected",
	}
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