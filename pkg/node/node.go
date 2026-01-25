package node

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"connectrpc.com/connect"

	"github.com/NavarchProject/navarch/pkg/gpu"
	"github.com/NavarchProject/navarch/pkg/node/metrics"
	pb "github.com/NavarchProject/navarch/proto"
	"github.com/NavarchProject/navarch/proto/protoconnect"
)

// NodeState represents the current state of a node in its lifecycle.
type NodeState int

const (
	// StateActive is the default state where the node accepts workloads.
	StateActive NodeState = iota
	// StateCordoned means the node is not accepting new workloads but can be uncordoned.
	StateCordoned
	// StateDraining means the node is gracefully draining workloads and cannot go back.
	StateDraining
	// StateTerminating means the node is shutting down (terminal state).
	StateTerminating
)

func (s NodeState) String() string {
	switch s {
	case StateActive:
		return "active"
	case StateCordoned:
		return "cordoned"
	case StateDraining:
		return "draining"
	case StateTerminating:
		return "terminating"
	default:
		return "unknown"
	}
}

// validTransitions defines the allowed state transitions.
// Key is current state, value is set of valid target states.
var validTransitions = map[NodeState]map[NodeState]bool{
	StateActive: {
		StateCordoned:    true,
		StateDraining:    true,
		StateTerminating: true,
	},
	StateCordoned: {
		StateActive:      true, // Can uncordon
		StateDraining:    true,
		StateTerminating: true,
	},
	StateDraining: {
		StateTerminating: true, // Can only move forward to terminating
	},
	StateTerminating: {
		// Terminal state - no valid transitions out
	},
}

// ErrInvalidStateTransition is returned when a command would cause an invalid state transition.
type ErrInvalidStateTransition struct {
	From    NodeState
	To      NodeState
	Command string
}

func (e *ErrInvalidStateTransition) Error() string {
	return fmt.Sprintf("invalid state transition from %s to %s (command: %s)", e.From, e.To, e.Command)
}

// canTransition checks if a state transition is valid.
func canTransition(from, to NodeState) bool {
	if from == to {
		return true // No-op transitions are always valid
	}
	targets, ok := validTransitions[from]
	if !ok {
		return false
	}
	return targets[to]
}

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

	// Node state (protected by stateMu)
	stateMu sync.RWMutex
	state   NodeState

	// Shutdown coordination
	shutdownCh chan struct{}
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
		state:               StateActive,
		shutdownCh:          make(chan struct{}),
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
		if err := n.executeCommand(ctx, cmd); err != nil {
			n.logger.ErrorContext(ctx, "failed to execute command",
				slog.String("command_id", cmd.CommandId),
				slog.String("command_type", cmd.Type.String()),
				slog.String("error", err.Error()),
			)
		}
	}

	return nil
}

// executeCommand dispatches a command to the appropriate handler.
func (n *Node) executeCommand(ctx context.Context, cmd *pb.NodeCommand) error {
	switch cmd.Type {
	case pb.NodeCommandType_NODE_COMMAND_TYPE_CORDON:
		return n.executeCordon(ctx, cmd)
	case pb.NodeCommandType_NODE_COMMAND_TYPE_DRAIN:
		return n.executeDrain(ctx, cmd)
	case pb.NodeCommandType_NODE_COMMAND_TYPE_RUN_DIAGNOSTIC:
		return n.executeDiagnostic(ctx, cmd)
	case pb.NodeCommandType_NODE_COMMAND_TYPE_TERMINATE:
		return n.executeTerminate(ctx, cmd)
	default:
		return fmt.Errorf("unknown command type: %s", cmd.Type.String())
	}
}

// executeCordon marks the node as cordoned (not accepting new workloads).
func (n *Node) executeCordon(ctx context.Context, cmd *pb.NodeCommand) error {
	n.stateMu.Lock()
	defer n.stateMu.Unlock()

	if n.state == StateCordoned {
		n.logger.InfoContext(ctx, "node already cordoned",
			slog.String("command_id", cmd.CommandId),
		)
		return nil
	}

	if !canTransition(n.state, StateCordoned) {
		return &ErrInvalidStateTransition{
			From:    n.state,
			To:      StateCordoned,
			Command: "cordon",
		}
	}

	n.state = StateCordoned
	reason := cmd.Parameters["reason"]
	n.logger.InfoContext(ctx, "node cordoned",
		slog.String("command_id", cmd.CommandId),
		slog.String("reason", reason),
	)

	return nil
}

// executeDrain initiates graceful draining of workloads.
func (n *Node) executeDrain(ctx context.Context, cmd *pb.NodeCommand) error {
	n.stateMu.Lock()
	if n.state == StateDraining {
		n.stateMu.Unlock()
		n.logger.InfoContext(ctx, "node already draining",
			slog.String("command_id", cmd.CommandId),
		)
		return nil
	}

	if !canTransition(n.state, StateDraining) {
		n.stateMu.Unlock()
		return &ErrInvalidStateTransition{
			From:    n.state,
			To:      StateDraining,
			Command: "drain",
		}
	}

	n.state = StateDraining
	n.stateMu.Unlock()

	n.logger.InfoContext(ctx, "node draining started",
		slog.String("command_id", cmd.CommandId),
	)

	// In a real implementation, this would:
	// 1. Signal running workloads to gracefully terminate
	// 2. Wait for workloads to complete (with timeout)
	// 3. Report drain completion
	//
	// For now, we simulate this with a brief delay and log completion.
	// The actual workload management would be handled by an external
	// scheduler or orchestrator that monitors the node's cordoned state.

	go func() {
		// Parse timeout from parameters, default to 5 minutes
		timeout := 5 * time.Minute
		if timeoutStr, ok := cmd.Parameters["timeout"]; ok {
			if parsed, err := time.ParseDuration(timeoutStr); err == nil {
				timeout = parsed
			}
		}

		drainCtx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		// Simulate waiting for workloads to drain
		// In production, this would poll for active workloads
		select {
		case <-drainCtx.Done():
			n.logger.Warn("drain timeout exceeded",
				slog.String("command_id", cmd.CommandId),
			)
		case <-time.After(1 * time.Second):
			// Simulated drain completion
		case <-n.shutdownCh:
			n.logger.Info("drain interrupted by shutdown",
				slog.String("command_id", cmd.CommandId),
			)
			return
		}

		n.logger.Info("node drain completed",
			slog.String("command_id", cmd.CommandId),
		)
	}()

	return nil
}

// executeDiagnostic runs GPU diagnostic tests.
func (n *Node) executeDiagnostic(ctx context.Context, cmd *pb.NodeCommand) error {
	n.stateMu.RLock()
	currentState := n.state
	n.stateMu.RUnlock()

	// Diagnostics cannot run on a terminating node
	if currentState == StateTerminating {
		return &ErrInvalidStateTransition{
			From:    currentState,
			To:      currentState, // Not actually transitioning, but command is invalid
			Command: "run_diagnostic",
		}
	}

	testType := cmd.Parameters["test_type"]
	if testType == "" {
		testType = "quick"
	}

	n.logger.InfoContext(ctx, "starting diagnostic",
		slog.String("command_id", cmd.CommandId),
		slog.String("test_type", testType),
		slog.String("state", currentState.String()),
	)

	// Run diagnostics asynchronously to not block command polling
	go func() {
		diagCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		results, err := n.runDiagnostics(diagCtx, testType)
		if err != nil {
			n.logger.Error("diagnostic failed",
				slog.String("command_id", cmd.CommandId),
				slog.String("error", err.Error()),
			)
			return
		}

		n.logger.Info("diagnostic completed",
			slog.String("command_id", cmd.CommandId),
			slog.String("test_type", testType),
			slog.String("result", results),
		)
	}()

	return nil
}

// runDiagnostics executes GPU diagnostic tests.
func (n *Node) runDiagnostics(ctx context.Context, testType string) (string, error) {
	count, err := n.gpu.GetDeviceCount(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get device count: %w", err)
	}

	var results []string
	for i := 0; i < count; i++ {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		health, err := n.gpu.GetDeviceHealth(ctx, i)
		if err != nil {
			results = append(results, fmt.Sprintf("GPU %d: ERROR - %v", i, err))
			continue
		}

		// Basic health validation
		status := "PASS"
		var issues []string

		if health.Temperature > 85 {
			status = "WARN"
			issues = append(issues, fmt.Sprintf("high temp %d°C", health.Temperature))
		}
		if health.Temperature > 95 {
			status = "FAIL"
		}

		result := fmt.Sprintf("GPU %d: %s (temp=%d°C, power=%.0fW, util=%d%%)",
			i, status, health.Temperature, health.PowerUsage, health.GPUUtilization)
		if len(issues) > 0 {
			result += fmt.Sprintf(" [%s]", issues[0])
		}
		results = append(results, result)
	}

	return fmt.Sprintf("Diagnostic complete: %d GPUs tested. %v", count, results), nil
}

// executeTerminate prepares the node for shutdown.
func (n *Node) executeTerminate(ctx context.Context, cmd *pb.NodeCommand) error {
	n.stateMu.Lock()
	if n.state == StateTerminating {
		n.stateMu.Unlock()
		n.logger.InfoContext(ctx, "node already shutting down",
			slog.String("command_id", cmd.CommandId),
		)
		return nil
	}

	if !canTransition(n.state, StateTerminating) {
		n.stateMu.Unlock()
		return &ErrInvalidStateTransition{
			From:    n.state,
			To:      StateTerminating,
			Command: "terminate",
		}
	}

	n.state = StateTerminating
	n.stateMu.Unlock()

	n.logger.InfoContext(ctx, "node termination initiated",
		slog.String("command_id", cmd.CommandId),
	)

	// Signal shutdown to drain goroutine and other components
	close(n.shutdownCh)

	// In production, this would:
	// 1. Complete any in-flight requests
	// 2. Flush metrics/logs
	// 3. Deregister from service discovery
	// 4. Signal the main process to exit

	return nil
}

// State returns the current state of the node.
func (n *Node) State() NodeState {
	n.stateMu.RLock()
	defer n.stateMu.RUnlock()
	return n.state
}

// IsCordoned returns whether the node is cordoned (not accepting new workloads).
// This is true for StateCordoned, StateDraining, and StateTerminating.
func (n *Node) IsCordoned() bool {
	n.stateMu.RLock()
	defer n.stateMu.RUnlock()
	return n.state >= StateCordoned
}

// IsDraining returns whether the node is draining workloads.
// This is true for StateDraining and StateTerminating.
func (n *Node) IsDraining() bool {
	n.stateMu.RLock()
	defer n.stateMu.RUnlock()
	return n.state >= StateDraining
}

// IsShuttingDown returns whether the node is shutting down.
func (n *Node) IsShuttingDown() bool {
	n.stateMu.RLock()
	defer n.stateMu.RUnlock()
	return n.state == StateTerminating
}

// ShutdownCh returns a channel that is closed when the node begins termination.
func (n *Node) ShutdownCh() <-chan struct{} {
	return n.shutdownCh
}