package simulator

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"connectrpc.com/connect"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/NavarchProject/navarch/pkg/controlplane"
	"github.com/NavarchProject/navarch/pkg/controlplane/db"
	pb "github.com/NavarchProject/navarch/proto"
	"github.com/NavarchProject/navarch/proto/protoconnect"
)

// Runner executes simulation scenarios.
type Runner struct {
	scenario         *Scenario
	controlPlaneAddr string
	nodes            map[string]*SimulatedNode
	logger           *slog.Logger
	client           protoconnect.ControlPlaneServiceClient
	waitForCancel    bool

	server     *http.Server
	serverDone chan struct{}

	// Stress testing components
	chaos   *ChaosEngine
	metrics *StressMetrics
	seed    int64
}

// RunnerOption configures a Runner.
type RunnerOption func(*Runner)

// WithLogger sets the logger for the runner.
func WithLogger(logger *slog.Logger) RunnerOption {
	return func(r *Runner) {
		r.logger = logger
	}
}

// WithControlPlaneAddr sets a custom control plane address.
func WithControlPlaneAddr(addr string) RunnerOption {
	return func(r *Runner) {
		r.controlPlaneAddr = addr
	}
}

// WithWaitForCancel keeps the runner alive after scenario completion until
// the context is canceled. This is useful for interactive mode.
func WithWaitForCancel() RunnerOption {
	return func(r *Runner) {
		r.waitForCancel = true
	}
}

// WithSeed sets the random seed for reproducible stress tests.
func WithSeed(seed int64) RunnerOption {
	return func(r *Runner) {
		r.seed = seed
	}
}

// NewRunner creates a new scenario runner.
func NewRunner(scenario *Scenario, opts ...RunnerOption) *Runner {
	r := &Runner{
		scenario:         scenario,
		controlPlaneAddr: "http://localhost:8080",
		nodes:            make(map[string]*SimulatedNode),
		logger:           slog.Default(),
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Run executes the scenario.
func (r *Runner) Run(ctx context.Context) error {
	// Check if this is a stress test scenario
	if r.scenario.IsStressTest() {
		return r.runStressTest(ctx)
	}

	return r.runRegularScenario(ctx)
}

// runRegularScenario executes a standard scenario with explicit events.
func (r *Runner) runRegularScenario(ctx context.Context) error {
	r.logger.Info("starting scenario",
		slog.String("name", r.scenario.Name),
		slog.Int("fleet_size", len(r.scenario.Fleet)),
		slog.Int("event_count", len(r.scenario.Events)),
	)

	// Start embedded control plane
	if err := r.startControlPlane(ctx); err != nil {
		return fmt.Errorf("failed to start control plane: %w", err)
	}
	defer r.stopControlPlane()

	// Give control plane time to start
	time.Sleep(100 * time.Millisecond)

	// Create client for admin operations
	r.client = protoconnect.NewControlPlaneServiceClient(
		http.DefaultClient,
		r.controlPlaneAddr,
	)

	// Sort events by time
	events := make([]Event, len(r.scenario.Events))
	copy(events, r.scenario.Events)
	sort.Slice(events, func(i, j int) bool {
		return events[i].At.Duration() < events[j].At.Duration()
	})

	startTime := time.Now()

	for i, event := range events {
		// Wait until the event time
		elapsed := time.Since(startTime)
		waitTime := event.At.Duration() - elapsed
		if waitTime > 0 {
			r.logger.Debug("waiting for next event",
				slog.Duration("wait", waitTime),
				slog.String("action", event.Action),
			)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(waitTime):
			}
		}

		r.logger.Info("executing event",
			slog.Int("index", i),
			slog.String("action", event.Action),
			slog.String("target", event.Target),
			slog.Duration("at", event.At.Duration()),
		)

		if err := r.executeEvent(ctx, event); err != nil {
			return fmt.Errorf("event %d (%s) failed: %w", i, event.Action, err)
		}
	}

	// Run final assertions
	for _, assertion := range r.scenario.Assertions {
		if err := r.checkAssertion(ctx, assertion); err != nil {
			return fmt.Errorf("assertion failed: %w", err)
		}
	}

	r.logger.Info("scenario completed successfully")

	if r.waitForCancel {
		<-ctx.Done()
	}
	return nil
}

// runStressTest executes a stress test scenario.
func (r *Runner) runStressTest(ctx context.Context) error {
	stress := r.scenario.Stress
	duration := stress.Duration.Duration()
	if duration == 0 {
		duration = 10 * time.Minute
	}

	// Set up file logging if configured
	var logFile *os.File
	if stress.LogFile != "" {
		var err error
		logFile, err = os.Create(stress.LogFile)
		if err != nil {
			return fmt.Errorf("failed to create log file: %w", err)
		}
		defer logFile.Close()

		// Create a logger that writes to file at debug level
		fileHandler := slog.NewTextHandler(logFile, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		})
		r.logger = slog.New(fileHandler)

		// Write header to log file
		fmt.Fprintf(logFile, "=== STRESS TEST LOG: %s ===\n", r.scenario.Name)
		fmt.Fprintf(logFile, "Started: %s\n", time.Now().Format(time.RFC3339))
		fmt.Fprintf(logFile, "Duration: %s, Nodes: %d, Seed: %d\n\n",
			duration, stress.FleetGen.TotalNodes, r.seed)
	}

	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Printf("║  STRESS TEST: %-47s ║\n", r.scenario.Name)
	fmt.Println("╠══════════════════════════════════════════════════════════════╣")
	fmt.Printf("║  Duration: %-10s  Nodes: %-6d  Seed: %-15d ║\n",
		duration.String(),
		stress.FleetGen.TotalNodes,
		r.seed)
	if stress.Chaos != nil && stress.Chaos.Enabled {
		fmt.Printf("║  Failure Rate: %.1f/min/1000   Cascading: %-16v ║\n",
			stress.Chaos.FailureRate,
			stress.Chaos.Cascading != nil && stress.Chaos.Cascading.Enabled)
	}
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
	fmt.Println()

	r.logger.Info("initializing stress test")

	// Initialize metrics collector
	r.metrics = NewStressMetrics(r.logger)

	// Start embedded control plane
	if err := r.startControlPlane(ctx); err != nil {
		return fmt.Errorf("failed to start control plane: %w", err)
	}
	defer r.stopControlPlane()

	time.Sleep(100 * time.Millisecond)

	// Create client
	r.client = protoconnect.NewControlPlaneServiceClient(
		http.DefaultClient,
		r.controlPlaneAddr,
	)

	// Generate fleet if using fleet_gen
	var fleet []NodeSpec
	if stress.FleetGen != nil {
		generator := NewFleetGenerator(stress.FleetGen, r.seed, r.logger)
		fleet = generator.GenerateFleet()
		r.logger.Info("generated fleet",
			slog.Int("nodes", len(fleet)),
		)
	} else {
		fleet = r.scenario.Fleet
	}

	// Start metrics sampling
	metricsInterval := stress.MetricsInterval.Duration()
	if metricsInterval == 0 {
		metricsInterval = 5 * time.Second
	}
	go r.metrics.StartSampling(ctx, metricsInterval)

	// Start fleet with configured pattern
	startupConfig := StartupConfig{
		Pattern:       "linear",
		Duration:      Duration(30 * time.Second),
		JitterPercent: 10,
	}
	if stress.FleetGen != nil && stress.FleetGen.Startup.Pattern != "" {
		startupConfig = stress.FleetGen.Startup
	}

	starter := NewNodeStarter(startupConfig, r.controlPlaneAddr, r.seed, r.logger)

	r.logger.Info("starting fleet",
		slog.String("pattern", startupConfig.Pattern),
		slog.Int("nodes", len(fleet)),
	)

	nodes, err := starter.StartFleet(ctx, fleet)
	if err != nil {
		return fmt.Errorf("failed to start fleet: %w", err)
	}
	r.nodes = nodes

	// Record started nodes
	for nodeID := range nodes {
		r.metrics.RecordNodeStart(nodeID)
	}

	// Start chaos engine if configured
	if stress.Chaos != nil && stress.Chaos.Enabled {
		r.chaos = NewChaosEngine(
			stress.Chaos,
			func() map[string]*SimulatedNode { return r.nodes },
			r.metrics,
			r.seed,
			r.logger,
		)
		r.chaos.Start(ctx)
		defer r.chaos.Stop()
	}

	// Also run any explicit events
	if len(r.scenario.Events) > 0 {
		go r.runEventsInBackground(ctx)
	}

	// Run for the configured duration
	fmt.Printf("\n⏱  Running stress test for %s...\n\n", duration)

	// Progress logging
	progressTicker := time.NewTicker(10 * time.Second)
	defer progressTicker.Stop()

	startTime := time.Now()
	testCtx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()

	for {
		select {
		case <-testCtx.Done():
			goto finished
		case <-progressTicker.C:
			elapsed := time.Since(startTime)
			remaining := duration - elapsed
			if remaining < 0 {
				remaining = 0
			}
			pct := float64(elapsed) / float64(duration) * 100
			stats := r.metrics.GetCurrentStats()

			fmt.Printf("\r[%5.1f%%] %s elapsed, %s remaining | Nodes: %d healthy | Failures: %d (cascade: %d) | Recoveries: %d    ",
				pct,
				elapsed.Round(time.Second),
				remaining.Round(time.Second),
				stats["nodes_healthy"],
				stats["total_failures"],
				stats["cascading"],
				stats["recoveries"])
		}
	}

finished:
	fmt.Printf("\r%s\n", strings.Repeat(" ", 120)) // Clear progress line
	fmt.Println()

	// Print summary
	r.metrics.PrintSummary()

	// Generate reports if configured
	var reportFiles []string

	// Add log file to report list if it was configured
	if stress.LogFile != "" && logFile != nil {
		// Write summary to log file before closing
		fmt.Fprintf(logFile, "\n=== TEST COMPLETED ===\n")
		fmt.Fprintf(logFile, "End time: %s\n", time.Now().Format(time.RFC3339))
		fmt.Fprintf(logFile, "Total failures: %d, Cascading: %d, Recoveries: %d\n",
			r.metrics.GetCurrentStats()["total_failures"],
			r.metrics.GetCurrentStats()["cascading"],
			r.metrics.GetCurrentStats()["recoveries"])
		reportFiles = append(reportFiles, stress.LogFile+" (Log)")
	}

	if stress.ReportFile != "" || stress.HTMLReportFile != "" {
		report := r.metrics.GenerateReport(r.scenario.Name, stress)

		// Write JSON report
		if stress.ReportFile != "" {
			if err := r.metrics.WriteReport(report, stress.ReportFile); err != nil {
				r.logger.Error("failed to write JSON report", slog.String("error", err.Error()))
			} else {
				reportFiles = append(reportFiles, stress.ReportFile+" (JSON)")
			}
		}

		// Write HTML report
		if stress.HTMLReportFile != "" {
			if err := r.metrics.WriteHTMLReport(report, stress, stress.HTMLReportFile); err != nil {
				r.logger.Error("failed to write HTML report", slog.String("error", err.Error()))
			} else {
				reportFiles = append(reportFiles, stress.HTMLReportFile+" (HTML)")
			}
		}
	}

	// Print report locations
	if len(reportFiles) > 0 {
		fmt.Println("\n📄 Reports generated:")
		for _, f := range reportFiles {
			fmt.Printf("   • %s\n", f)
		}
	}

	// Run assertions if any
	for _, assertion := range r.scenario.Assertions {
		if err := r.checkAssertion(ctx, assertion); err != nil {
			return fmt.Errorf("assertion failed: %w", err)
		}
	}

	fmt.Println("\n✅ Stress test completed successfully")

	if r.waitForCancel {
		<-ctx.Done()
	}
	return nil
}

func (r *Runner) runEventsInBackground(ctx context.Context) {
	events := make([]Event, len(r.scenario.Events))
	copy(events, r.scenario.Events)
	sort.Slice(events, func(i, j int) bool {
		return events[i].At.Duration() < events[j].At.Duration()
	})

	startTime := time.Now()

	for _, event := range events {
		elapsed := time.Since(startTime)
		waitTime := event.At.Duration() - elapsed
		if waitTime > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(waitTime):
			}
		}

		if err := r.executeEvent(ctx, event); err != nil {
			r.logger.Error("event execution failed",
				slog.String("action", event.Action),
				slog.String("error", err.Error()),
			)
		}
	}
}

func (r *Runner) startControlPlane(ctx context.Context) error {
	database := db.NewInMemDB()
	cfg := controlplane.DefaultConfig()
	// Use faster intervals for simulation
	cfg.HealthCheckIntervalSeconds = 5
	cfg.HeartbeatIntervalSeconds = 3

	server := controlplane.NewServer(database, cfg, r.logger.With(slog.String("component", "control-plane")))

	mux := http.NewServeMux()
	path, handler := protoconnect.NewControlPlaneServiceHandler(server)
	mux.Handle(path, handler)

	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		return fmt.Errorf("failed to create listener: %w", err)
	}

	addr := listener.Addr().(*net.TCPAddr)
	r.controlPlaneAddr = fmt.Sprintf("http://localhost:%d", addr.Port)

	r.server = &http.Server{
		Handler: h2c.NewHandler(mux, &http2.Server{}),
	}
	r.serverDone = make(chan struct{})

	go func() {
		defer close(r.serverDone)
		if err := r.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			r.logger.Error("control plane server error", slog.String("error", err.Error()))
		}
	}()

	r.logger.Info("control plane started", slog.String("addr", r.controlPlaneAddr))
	return nil
}

func (r *Runner) stopControlPlane() {
	if r.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		r.server.Shutdown(ctx)
		<-r.serverDone
	}

	// Stop all nodes
	for _, node := range r.nodes {
		node.Stop()
	}
}

func (r *Runner) executeEvent(ctx context.Context, event Event) error {
	switch event.Action {
	case "start_fleet":
		return r.startFleet(ctx)
	case "stop_fleet":
		return r.stopFleet()
	case "inject_failure":
		return r.injectFailure(ctx, event)
	case "recover_failure":
		return r.recoverFailure(ctx, event)
	case "issue_command":
		return r.issueCommand(ctx, event)
	case "wait_for_status":
		return r.waitForStatus(ctx, event)
	case "wait":
		// Already waited by the scheduler
		return nil
	case "log":
		r.logger.Info(event.Params.LogMessage)
		return nil
	case "assert":
		return r.checkAssertionFromEvent(ctx, event)
	default:
		return fmt.Errorf("unknown action: %s", event.Action)
	}
}

func (r *Runner) startFleet(ctx context.Context) error {
	for _, spec := range r.scenario.Fleet {
		node := NewSimulatedNode(spec, r.controlPlaneAddr, r.logger)
		if err := node.Start(ctx); err != nil {
			return fmt.Errorf("failed to start node %s: %w", spec.ID, err)
		}
		r.nodes[spec.ID] = node
		r.logger.Info("started simulated node", slog.String("node_id", spec.ID))
	}
	return nil
}

func (r *Runner) stopFleet() error {
	for id, node := range r.nodes {
		node.Stop()
		r.logger.Info("stopped simulated node", slog.String("node_id", id))
	}
	return nil
}

func (r *Runner) injectFailure(ctx context.Context, event Event) error {
	node, ok := r.nodes[event.Target]
	if !ok {
		return fmt.Errorf("node not found: %s", event.Target)
	}

	failure := InjectedFailure{
		Type:     event.Params.FailureType,
		XIDCode:  event.Params.XIDCode,
		GPUIndex: event.Params.GPUIndex,
		Message:  event.Params.Message,
	}

	node.InjectFailure(failure)
	return nil
}

func (r *Runner) recoverFailure(ctx context.Context, event Event) error {
	node, ok := r.nodes[event.Target]
	if !ok {
		return fmt.Errorf("node not found: %s", event.Target)
	}

	if event.Params.FailureType != "" {
		node.RecoverFailure(event.Params.FailureType)
	} else {
		node.ClearFailures()
	}
	return nil
}

func (r *Runner) issueCommand(ctx context.Context, event Event) error {
	var cmdType pb.NodeCommandType
	switch event.Params.CommandType {
	case "cordon":
		cmdType = pb.NodeCommandType_NODE_COMMAND_TYPE_CORDON
	case "drain":
		cmdType = pb.NodeCommandType_NODE_COMMAND_TYPE_DRAIN
	case "terminate":
		cmdType = pb.NodeCommandType_NODE_COMMAND_TYPE_TERMINATE
	case "run_diagnostic":
		cmdType = pb.NodeCommandType_NODE_COMMAND_TYPE_RUN_DIAGNOSTIC
	default:
		return fmt.Errorf("unknown command type: %s", event.Params.CommandType)
	}

	req := connect.NewRequest(&pb.IssueCommandRequest{
		NodeId:      event.Target,
		CommandType: cmdType,
		Parameters:  event.Params.CommandArgs,
	})

	resp, err := r.client.IssueCommand(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to issue command: %w", err)
	}

	r.logger.Info("command issued",
		slog.String("command_id", resp.Msg.CommandId),
		slog.String("node_id", event.Target),
		slog.String("type", event.Params.CommandType),
	)

	return nil
}

func (r *Runner) waitForStatus(ctx context.Context, event Event) error {
	timeout := event.Params.Timeout.Duration()
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	expectedStatus := parseNodeStatus(event.Params.ExpectedStatus)

	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return fmt.Errorf("timeout waiting for node %s to reach status %s", event.Target, event.Params.ExpectedStatus)
			}

			req := connect.NewRequest(&pb.GetNodeRequest{
				NodeId: event.Target,
			})
			resp, err := r.client.GetNode(ctx, req)
			if err != nil {
				continue // Node might not be registered yet
			}

			if resp.Msg.Node.Status == expectedStatus {
				r.logger.Info("node reached expected status",
					slog.String("node_id", event.Target),
					slog.String("status", event.Params.ExpectedStatus),
				)
				return nil
			}
		}
	}
}

func (r *Runner) checkAssertionFromEvent(ctx context.Context, event Event) error {
	assertion := Assertion{
		Type:     event.Params.FailureType, // Reusing field for assertion type
		Target:   event.Target,
		Expected: event.Params.ExpectedStatus,
	}
	return r.checkAssertion(ctx, assertion)
}

func (r *Runner) checkAssertion(ctx context.Context, assertion Assertion) error {
	switch assertion.Type {
	case "node_status", "":
		req := connect.NewRequest(&pb.GetNodeRequest{
			NodeId: assertion.Target,
		})
		resp, err := r.client.GetNode(ctx, req)
		if err != nil {
			return fmt.Errorf("failed to get node %s: %w", assertion.Target, err)
		}

		expectedStatus := parseNodeStatus(assertion.Expected)
		if resp.Msg.Node.Status != expectedStatus {
			return fmt.Errorf("node %s has status %s, expected %s",
				assertion.Target,
				resp.Msg.Node.Status.String(),
				assertion.Expected,
			)
		}
		r.logger.Info("assertion passed",
			slog.String("type", "node_status"),
			slog.String("node_id", assertion.Target),
			slog.String("status", assertion.Expected),
		)

	case "health_status":
		req := connect.NewRequest(&pb.GetNodeRequest{
			NodeId: assertion.Target,
		})
		resp, err := r.client.GetNode(ctx, req)
		if err != nil {
			return fmt.Errorf("failed to get node %s: %w", assertion.Target, err)
		}

		expectedHealth := parseHealthStatus(assertion.Expected)
		if resp.Msg.Node.HealthStatus != expectedHealth {
			return fmt.Errorf("node %s has health status %s, expected %s",
				assertion.Target,
				resp.Msg.Node.HealthStatus.String(),
				assertion.Expected,
			)
		}
		r.logger.Info("assertion passed",
			slog.String("type", "health_status"),
			slog.String("node_id", assertion.Target),
			slog.String("health_status", assertion.Expected),
		)

	case "node_count":
		req := connect.NewRequest(&pb.ListNodesRequest{})
		resp, err := r.client.ListNodes(ctx, req)
		if err != nil {
			return fmt.Errorf("failed to list nodes: %w", err)
		}
		// For node_count, Expected should be a number but we'll skip this for now
		r.logger.Info("node count",
			slog.Int("count", len(resp.Msg.Nodes)),
		)

	default:
		return fmt.Errorf("unknown assertion type: %s", assertion.Type)
	}

	return nil
}

func parseNodeStatus(s string) pb.NodeStatus {
	switch s {
	case "active":
		return pb.NodeStatus_NODE_STATUS_ACTIVE
	case "cordoned":
		return pb.NodeStatus_NODE_STATUS_CORDONED
	case "draining":
		return pb.NodeStatus_NODE_STATUS_DRAINING
	case "unhealthy":
		return pb.NodeStatus_NODE_STATUS_UNHEALTHY
	case "terminated":
		return pb.NodeStatus_NODE_STATUS_TERMINATED
	default:
		return pb.NodeStatus_NODE_STATUS_UNKNOWN
	}
}

func parseHealthStatus(s string) pb.HealthStatus {
	switch s {
	case "healthy":
		return pb.HealthStatus_HEALTH_STATUS_HEALTHY
	case "degraded":
		return pb.HealthStatus_HEALTH_STATUS_DEGRADED
	case "unhealthy":
		return pb.HealthStatus_HEALTH_STATUS_UNHEALTHY
	default:
		return pb.HealthStatus_HEALTH_STATUS_UNKNOWN
	}
}

// PrintFleetStatus prints the current status of all nodes.
func (r *Runner) PrintFleetStatus(ctx context.Context) error {
	req := connect.NewRequest(&pb.ListNodesRequest{})
	resp, err := r.client.ListNodes(ctx, req)
	if err != nil {
		return err
	}

	r.logger.Info("fleet status", slog.Int("total_nodes", len(resp.Msg.Nodes)))
	for _, node := range resp.Msg.Nodes {
		r.logger.Info("  node",
			slog.String("id", node.NodeId),
			slog.String("status", node.Status.String()),
			slog.String("health", node.HealthStatus.String()),
			slog.String("provider", node.Provider),
		)
	}
	return nil
}

