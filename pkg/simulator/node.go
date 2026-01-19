package simulator

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/NavarchProject/navarch/proto"
	"github.com/NavarchProject/navarch/proto/protoconnect"
)

// SimulatedNode represents a fake node that communicates with the control plane.
type SimulatedNode struct {
	spec   NodeSpec
	client protoconnect.ControlPlaneServiceClient
	logger *slog.Logger

	mu              sync.RWMutex
	failures        []InjectedFailure
	running         bool
	healthCheckInterval time.Duration
	heartbeatInterval   time.Duration
	cancel          context.CancelFunc
}

// InjectedFailure represents a failure condition injected into the node.
type InjectedFailure struct {
	Type     string
	XIDCode  int
	GPUIndex int
	Message  string
	Since    time.Time
}

// NewSimulatedNode creates a new simulated node.
func NewSimulatedNode(spec NodeSpec, controlPlaneAddr string, logger *slog.Logger) *SimulatedNode {
	if logger == nil {
		logger = slog.Default()
	}
	return &SimulatedNode{
		spec: spec,
		client: protoconnect.NewControlPlaneServiceClient(
			http.DefaultClient,
			controlPlaneAddr,
		),
		logger:              logger.With(slog.String("node_id", spec.ID)),
		failures:            make([]InjectedFailure, 0),
		healthCheckInterval: 5 * time.Second,  // Faster for simulation
		heartbeatInterval:   3 * time.Second,
	}
}

// Start begins the simulated node's operation.
func (n *SimulatedNode) Start(ctx context.Context) error {
	n.mu.Lock()
	if n.running {
		n.mu.Unlock()
		return fmt.Errorf("node already running")
	}
	n.running = true
	ctx, n.cancel = context.WithCancel(ctx)
	n.mu.Unlock()

	if err := n.register(ctx); err != nil {
		n.mu.Lock()
		n.running = false
		n.mu.Unlock()
		return err
	}

	go n.heartbeatLoop(ctx)
	go n.healthCheckLoop(ctx)
	go n.commandPollLoop(ctx)

	return nil
}

// Stop halts the simulated node.
func (n *SimulatedNode) Stop() {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.cancel != nil {
		n.cancel()
	}
	n.running = false
}

// InjectFailure adds a failure condition to the node.
func (n *SimulatedNode) InjectFailure(failure InjectedFailure) {
	n.mu.Lock()
	defer n.mu.Unlock()
	failure.Since = time.Now()
	n.failures = append(n.failures, failure)
	n.logger.Info("injected failure",
		slog.String("type", failure.Type),
		slog.Int("xid_code", failure.XIDCode),
		slog.Int("gpu_index", failure.GPUIndex),
	)
}

// ClearFailures removes all injected failures.
func (n *SimulatedNode) ClearFailures() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.failures = make([]InjectedFailure, 0)
	n.logger.Info("cleared all failures")
}

// RecoverFailure removes a specific failure type.
func (n *SimulatedNode) RecoverFailure(failureType string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	var remaining []InjectedFailure
	for _, f := range n.failures {
		if f.Type != failureType {
			remaining = append(remaining, f)
		}
	}
	n.failures = remaining
	n.logger.Info("recovered from failure", slog.String("type", failureType))
}

// GetFailures returns current injected failures.
func (n *SimulatedNode) GetFailures() []InjectedFailure {
	n.mu.RLock()
	defer n.mu.RUnlock()
	result := make([]InjectedFailure, len(n.failures))
	copy(result, n.failures)
	return result
}

// ID returns the node's ID.
func (n *SimulatedNode) ID() string {
	return n.spec.ID
}

func (n *SimulatedNode) register(ctx context.Context) error {
	gpus := make([]*pb.GPUInfo, n.spec.GPUCount)
	for i := 0; i < n.spec.GPUCount; i++ {
		gpus[i] = &pb.GPUInfo{
			Index:         int32(i),
			Uuid:          fmt.Sprintf("GPU-%s-%d", n.spec.ID, i),
			Name:          n.spec.GPUType,
			PciBusId:      fmt.Sprintf("0000:%02x:00.0", i),
			MemoryTotal:   80 * 1024 * 1024 * 1024, // 80GB
			DriverVersion: "550.54.15",
			CudaVersion:   "12.4",
		}
	}

	req := connect.NewRequest(&pb.RegisterNodeRequest{
		NodeId:       n.spec.ID,
		Provider:     n.spec.Provider,
		Region:       n.spec.Region,
		Zone:         n.spec.Zone,
		InstanceType: n.spec.InstanceType,
		Gpus:         gpus,
		Metadata: &pb.NodeMetadata{
			Hostname:   n.spec.ID,
			InternalIp: fmt.Sprintf("10.0.0.%d", hash(n.spec.ID)%254+1),
			Labels:     n.spec.Labels,
		},
	})

	resp, err := n.client.RegisterNode(ctx, req)
	if err != nil {
		return fmt.Errorf("registration failed: %w", err)
	}

	if !resp.Msg.Success {
		return fmt.Errorf("registration rejected: %s", resp.Msg.Message)
	}

	if resp.Msg.Config != nil {
		n.healthCheckInterval = time.Duration(resp.Msg.Config.HealthCheckIntervalSeconds) * time.Second
		n.heartbeatInterval = time.Duration(resp.Msg.Config.HeartbeatIntervalSeconds) * time.Second
		// Use faster intervals for simulation
		if n.healthCheckInterval > 5*time.Second {
			n.healthCheckInterval = 5 * time.Second
		}
		if n.heartbeatInterval > 3*time.Second {
			n.heartbeatInterval = 3 * time.Second
		}
	}

	n.logger.Info("registered with control plane")
	return nil
}

func (n *SimulatedNode) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(n.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := n.sendHeartbeat(ctx); err != nil {
				n.logger.Error("failed to send heartbeat", slog.String("error", err.Error()))
			}
		}
	}
}

func (n *SimulatedNode) sendHeartbeat(ctx context.Context) error {
	gpuMetrics := make([]*pb.GPUMetrics, n.spec.GPUCount)
	for i := 0; i < n.spec.GPUCount; i++ {
		gpuMetrics[i] = &pb.GPUMetrics{
			GpuIndex:          int32(i),
			Temperature:       65, // Normal temp
			PowerUsage:        400,
			UtilizationPercent: 85.0,
			MemoryUsed:        60 * 1024 * 1024 * 1024,
		}
	}

	req := connect.NewRequest(&pb.HeartbeatRequest{
		NodeId:    n.spec.ID,
		Timestamp: timestamppb.Now(),
		Metrics: &pb.NodeMetrics{
			CpuUsagePercent:    45.0,
			MemoryUsagePercent: 60.0,
			GpuMetrics:         gpuMetrics,
		},
	})

	_, err := n.client.SendHeartbeat(ctx, req)
	return err
}

func (n *SimulatedNode) healthCheckLoop(ctx context.Context) {
	// Run immediately on start
	if err := n.runHealthChecks(ctx); err != nil {
		n.logger.Error("failed to run health checks", slog.String("error", err.Error()))
	}

	ticker := time.NewTicker(n.healthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := n.runHealthChecks(ctx); err != nil {
				n.logger.Error("failed to run health checks", slog.String("error", err.Error()))
			}
		}
	}
}

func (n *SimulatedNode) runHealthChecks(ctx context.Context) error {
	n.mu.RLock()
	failures := make([]InjectedFailure, len(n.failures))
	copy(failures, n.failures)
	n.mu.RUnlock()

	results := []*pb.HealthCheckResult{
		n.generateBootCheck(failures),
		n.generateNvmlCheck(failures),
		n.generateXidCheck(failures),
	}

	req := connect.NewRequest(&pb.ReportHealthRequest{
		NodeId:  n.spec.ID,
		Results: results,
	})

	resp, err := n.client.ReportHealth(ctx, req)
	if err != nil {
		return err
	}

	n.logger.Debug("health report acknowledged",
		slog.String("node_status", resp.Msg.NodeStatus.String()),
	)

	return nil
}

func (n *SimulatedNode) generateBootCheck(failures []InjectedFailure) *pb.HealthCheckResult {
	for _, f := range failures {
		if f.Type == "boot_failure" {
			return &pb.HealthCheckResult{
				CheckName: "boot",
				Status:    pb.HealthStatus_HEALTH_STATUS_UNHEALTHY,
				Message:   f.Message,
				Timestamp: timestamppb.Now(),
			}
		}
	}
	return &pb.HealthCheckResult{
		CheckName: "boot",
		Status:    pb.HealthStatus_HEALTH_STATUS_HEALTHY,
		Message:   "Boot check passed",
		Timestamp: timestamppb.Now(),
	}
}

func (n *SimulatedNode) generateNvmlCheck(failures []InjectedFailure) *pb.HealthCheckResult {
	for _, f := range failures {
		if f.Type == "nvml_failure" || f.Type == "temperature" {
			return &pb.HealthCheckResult{
				CheckName: "nvml",
				Status:    pb.HealthStatus_HEALTH_STATUS_UNHEALTHY,
				Message:   f.Message,
				Timestamp: timestamppb.Now(),
				Details: map[string]string{
					"gpu_index": fmt.Sprintf("%d", f.GPUIndex),
				},
			}
		}
	}
	return &pb.HealthCheckResult{
		CheckName: "nvml",
		Status:    pb.HealthStatus_HEALTH_STATUS_HEALTHY,
		Message:   fmt.Sprintf("All %d GPUs healthy", n.spec.GPUCount),
		Timestamp: timestamppb.Now(),
	}
}

func (n *SimulatedNode) generateXidCheck(failures []InjectedFailure) *pb.HealthCheckResult {
	for _, f := range failures {
		if f.Type == "xid_error" {
			xidInfo, known := XIDCodes[f.XIDCode]
			status := pb.HealthStatus_HEALTH_STATUS_DEGRADED
			if known && xidInfo.Fatal {
				status = pb.HealthStatus_HEALTH_STATUS_UNHEALTHY
			}

			message := f.Message
			if message == "" && known {
				message = fmt.Sprintf("XID %d: %s", f.XIDCode, xidInfo.Name)
			}

			return &pb.HealthCheckResult{
				CheckName: "xid",
				Status:    status,
				Message:   message,
				Timestamp: timestamppb.Now(),
				Details: map[string]string{
					"xid_code":  fmt.Sprintf("%d", f.XIDCode),
					"gpu_index": fmt.Sprintf("%d", f.GPUIndex),
					"fatal":     fmt.Sprintf("%t", known && xidInfo.Fatal),
				},
			}
		}
	}
	return &pb.HealthCheckResult{
		CheckName: "xid",
		Status:    pb.HealthStatus_HEALTH_STATUS_HEALTHY,
		Message:   "No XID errors detected",
		Timestamp: timestamppb.Now(),
	}
}

func (n *SimulatedNode) commandPollLoop(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second) // Fast polling for simulation
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := n.pollCommands(ctx); err != nil {
				n.logger.Error("failed to poll commands", slog.String("error", err.Error()))
			}
		}
	}
}

func (n *SimulatedNode) pollCommands(ctx context.Context) error {
	req := connect.NewRequest(&pb.GetNodeCommandsRequest{
		NodeId: n.spec.ID,
	})

	resp, err := n.client.GetNodeCommands(ctx, req)
	if err != nil {
		return err
	}

	for _, cmd := range resp.Msg.Commands {
		n.logger.Info("received command",
			slog.String("command_type", cmd.Type.String()),
			slog.String("command_id", cmd.CommandId),
		)
		// In a real scenario, we'd execute the command here
		// For simulation, we just log it
	}

	return nil
}

// Simple hash for generating pseudo-random but deterministic values.
func hash(s string) int {
	h := 0
	for _, c := range s {
		h = 31*h + int(c)
	}
	if h < 0 {
		h = -h
	}
	return h
}

