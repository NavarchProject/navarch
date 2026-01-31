package simulator

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"net"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"

	"connectrpc.com/connect"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/NavarchProject/navarch/pkg/clock"
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
	clock            clock.Clock
	client           protoconnect.ControlPlaneServiceClient
	waitForCancel    bool

	httpServer    *http.Server
	serverDone    chan struct{}
	database      *db.InMemDB
	cpServer      *controlplane.Server

	// Stress testing components
	chaos         *ChaosEngine
	metrics       *StressMetrics
	seed          int64
	rng           *rand.Rand
	runDir        *RunDir
	startupConfig StartupConfig
	mu            sync.Mutex
	nodeSpecs     map[string]NodeSpec // nodeID -> spec for replacement
}

// simHealthObserver implements NodeHealthObserver for the simulator.
type simHealthObserver struct {
	runner   *Runner
	ctx      context.Context
	recovery *RecoveryConfig
}

func (o *simHealthObserver) OnNodeUnhealthy(ctx context.Context, nodeID string) {
	go o.runner.handleUnhealthyNode(o.ctx, nodeID, o.recovery)
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
		r.rng = rand.New(rand.NewSource(seed))
	}
}

// WithClock sets the clock for time operations.
// Use clock.NewFakeClock for deterministic time-accelerated simulations.
func WithClock(clk clock.Clock) RunnerOption {
	return func(r *Runner) {
		r.clock = clk
	}
}

// NewRunner creates a new scenario runner.
func NewRunner(scenario *Scenario, opts ...RunnerOption) *Runner {
	r := &Runner{
		scenario:         scenario,
		controlPlaneAddr: "http://localhost:8080",
		nodes:            make(map[string]*SimulatedNode),
		logger:           slog.Default(),
		clock:            clock.Real(),
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Run executes the scenario.
func (r *Runner) Run(ctx context.Context) error {
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

	if err := r.startControlPlane(ctx); err != nil {
		return fmt.Errorf("failed to start control plane: %w", err)
	}
	defer r.stopControlPlane()

	time.Sleep(100 * time.Millisecond)

	r.client = protoconnect.NewControlPlaneServiceClient(
		http.DefaultClient,
		r.controlPlaneAddr,
	)

	events := make([]Event, len(r.scenario.Events))
	copy(events, r.scenario.Events)
	sort.Slice(events, func(i, j int) bool {
		return events[i].At.Duration() < events[j].At.Duration()
	})

	startTime := time.Now()

	for i, event := range events {
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

	nodeCount := len(r.scenario.Fleet)
	if stress.FleetGen != nil {
		nodeCount = stress.FleetGen.TotalNodes
	}

	var logFile *os.File
	if stress.LogFile != "" {
		var err error
		logFile, err = os.Create(stress.LogFile)
		if err != nil {
			return fmt.Errorf("failed to create log file: %w", err)
		}
		defer logFile.Close()

		fileHandler := slog.NewTextHandler(logFile, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		})
		r.logger = slog.New(fileHandler)

		fmt.Fprintf(logFile, "=== STRESS TEST LOG: %s ===\n", r.scenario.Name)
		fmt.Fprintf(logFile, "Started: %s\n", time.Now().Format(time.RFC3339))
		fmt.Fprintf(logFile, "Duration: %s, Nodes: %d, Seed: %d\n\n",
			duration, nodeCount, r.seed)
	}

	console := NewConsole()
	failureRate := 0.0
	cascading := false
	if stress.Chaos != nil && stress.Chaos.Enabled {
		failureRate = stress.Chaos.FailureRate
		cascading = stress.Chaos.Cascading != nil && stress.Chaos.Cascading.Enabled
	}
	console.PrintHeader(r.scenario.Name, duration, nodeCount, r.seed, failureRate, cascading)

	// Create run directory for all artifacts
	runDir, err := NewRunDir("", r.scenario)
	if err != nil {
		r.logger.Warn("failed to create run directory", slog.String("error", err.Error()))
	} else {
		r.runDir = runDir
		defer runDir.Close()
		r.logger.Info("run directory", slog.String("path", runDir.Dir()))
	}

	r.logger.Info("initializing stress test")

	r.metrics = NewStressMetrics(r.logger)

	if err := r.startControlPlane(ctx); err != nil {
		return fmt.Errorf("failed to start control plane: %w", err)
	}
	defer r.stopControlPlane()
	time.Sleep(100 * time.Millisecond)

	r.client = protoconnect.NewControlPlaneServiceClient(
		http.DefaultClient,
		r.controlPlaneAddr,
	)

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

	metricsInterval := stress.MetricsInterval.Duration()
	if metricsInterval == 0 {
		metricsInterval = 5 * time.Second
	}
	go r.metrics.StartSampling(ctx, metricsInterval)

	r.startupConfig = StartupConfig{
		Pattern:       "linear",
		Duration:      Duration(30 * time.Second),
		JitterPercent: 10,
	}
	if stress.FleetGen != nil && stress.FleetGen.Startup.Pattern != "" {
		r.startupConfig = stress.FleetGen.Startup
	}

	// Store node specs for potential replacement
	r.nodeSpecs = make(map[string]NodeSpec)
	for _, spec := range fleet {
		r.nodeSpecs[spec.ID] = spec
	}

	// Set up replacement observer for fatal failures
	if stress.Chaos != nil && stress.Chaos.Recovery != nil && stress.Chaos.Recovery.ReplaceFatal {
		r.cpServer.SetHealthObserver(&simHealthObserver{
			runner:   r,
			ctx:      ctx,
			recovery: stress.Chaos.Recovery,
		})
	}

	starter := NewNodeStarter(r.startupConfig, r.controlPlaneAddr, r.seed, r.logger)
	if r.runDir != nil {
		starter.SetRunDir(r.runDir)
	}

	r.logger.Info("starting fleet",
		slog.String("pattern", r.startupConfig.Pattern),
		slog.Int("nodes", len(fleet)),
	)

	nodes, err := starter.StartFleet(ctx, fleet)
	if err != nil {
		return fmt.Errorf("failed to start fleet: %w", err)
	}
	r.nodes = nodes

	coldStartDelays := starter.GetColdStartDelays()
	for _, spec := range fleet {
		r.metrics.RegisterNode(spec)
		if _, ok := nodes[spec.ID]; ok {
			r.metrics.RecordNodeStart(spec.ID)
			if delay, ok := coldStartDelays[spec.ID]; ok && delay > 0 {
				r.metrics.RecordColdStartDelay(spec.ID, delay)
			}
		}
	}

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

	if len(r.scenario.Events) > 0 {
		go r.runEventsInBackground(ctx)
	}

	console.PrintRunning(duration)

	progressTicker := time.NewTicker(time.Second)
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

			console.PrintProgress(pct, elapsed, remaining,
				stats["nodes_healthy"].(int64),
				stats["total_failures"].(int64),
				stats["cascading"].(int64),
				stats["recoveries"].(int64))
		}
	}

finished:
	console.ClearProgress()

	// Take a final sample to ensure accurate end-state data
	r.metrics.TakeSample()

	results := r.metrics.GetStressResults()
	console.PrintResults(results)

	var reportFiles []string

	if stress.LogFile != "" && logFile != nil {
		fmt.Fprintf(logFile, "\n=== TEST COMPLETED ===\n")
		fmt.Fprintf(logFile, "End time: %s\n", time.Now().Format(time.RFC3339))
		fmt.Fprintf(logFile, "Total failures: %d, Cascading: %d, Recoveries: %d\n",
			r.metrics.GetCurrentStats()["total_failures"],
			r.metrics.GetCurrentStats()["cascading"],
			r.metrics.GetCurrentStats()["recoveries"])
		reportFiles = append(reportFiles, stress.LogFile+" (Log)")
	}

	report := r.metrics.GenerateReport(r.scenario.Name, stress)

	if r.runDir != nil {
		// Use relative paths so links work when HTML is opened from run directory
		report.LogsDirectory = "logs"
		for i := range report.Nodes {
			report.Nodes[i].LogFile = "logs/" + sanitizeFilename(report.Nodes[i].NodeID) + ".log"
		}

		// Write all artifacts to run directory
		if err := r.metrics.WriteReport(report, r.runDir.ReportPath()); err != nil {
			r.logger.Error("failed to write JSON report", slog.String("error", err.Error()))
		}
		if err := r.metrics.WriteHTMLReport(report, stress, r.runDir.HTMLReportPath()); err != nil {
			r.logger.Error("failed to write HTML report", slog.String("error", err.Error()))
		}
		reportFiles = append(reportFiles, r.runDir.Dir())
	}
	console.PrintReports(reportFiles)

	for _, assertion := range r.scenario.Assertions {
		if err := r.checkAssertion(ctx, assertion); err != nil {
			return fmt.Errorf("assertion failed: %w", err)
		}
	}

	console.PrintSuccess("Stress test completed successfully")

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
	r.database = db.NewInMemDBWithClock(r.clock)
	cfg := controlplane.DefaultConfig()
	// Use faster intervals for simulation
	cfg.HealthCheckIntervalSeconds = 5
	cfg.HeartbeatIntervalSeconds = 3
	cfg.Clock = r.clock

	// Pass nil for instance manager in simulator (not needed for basic simulation)
	r.cpServer = controlplane.NewServer(r.database, cfg, nil, r.logger.With(slog.String("component", "control-plane")))

	mux := http.NewServeMux()
	path, handler := protoconnect.NewControlPlaneServiceHandler(r.cpServer)
	mux.Handle(path, handler)

	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		return fmt.Errorf("failed to create listener: %w", err)
	}

	addr := listener.Addr().(*net.TCPAddr)
	r.controlPlaneAddr = fmt.Sprintf("http://localhost:%d", addr.Port)

	r.httpServer = &http.Server{
		Handler: h2c.NewHandler(mux, &http2.Server{}),
	}
	r.serverDone = make(chan struct{})

	go func() {
		defer close(r.serverDone)
		if err := r.httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			r.logger.Error("control plane server error", slog.String("error", err.Error()))
		}
	}()

	r.logger.Info("control plane started", slog.String("addr", r.controlPlaneAddr))
	return nil
}

func (r *Runner) stopControlPlane() {
	if r.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		r.httpServer.Shutdown(ctx)
		<-r.serverDone
	}

	for _, node := range r.nodes {
		node.Stop()
	}
}

func (r *Runner) handleUnhealthyNode(ctx context.Context, nodeID string, recovery *RecoveryConfig) {
	r.mu.Lock()
	spec, ok := r.nodeSpecs[nodeID]
	if !ok {
		r.mu.Unlock()
		return
	}
	existingNode, exists := r.nodes[nodeID]
	if !exists || existingNode == nil {
		r.mu.Unlock()
		return
	}
	existingNode.Stop()
	delete(r.nodes, nodeID)
	r.mu.Unlock()

	r.logger.Info("node became unhealthy, scheduling replacement",
		slog.String("node_id", nodeID),
	)

	// Calculate cold start delay
	coldStart := recovery.ReplaceColdStart.Duration()
	if coldStart == 0 && r.rng != nil {
		if r.startupConfig.ColdStartMin.Duration() > 0 {
			min := r.startupConfig.ColdStartMin.Duration()
			max := r.startupConfig.ColdStartMax.Duration()
			if max <= min {
				max = min + time.Second
			}
			coldStart = min + time.Duration(r.rng.Int63n(int64(max-min)))
		} else {
			coldStart = 30*time.Second + time.Duration(r.rng.Int63n(int64(30*time.Second)))
		}
	}

	r.logger.Info("provisioning replacement node",
		slog.String("old_node_id", nodeID),
		slog.Duration("cold_start", coldStart),
	)

	select {
	case <-ctx.Done():
		return
	case <-time.After(coldStart):
	}

	newSpec := spec
	newSpec.ID = fmt.Sprintf("%s-gen%d", spec.BaseID(), spec.Generation+1)
	newSpec.Generation = spec.Generation + 1

	newNode := NewSimulatedNode(newSpec, r.controlPlaneAddr, r.logger)
	if err := newNode.Start(ctx); err != nil {
		r.logger.Error("failed to start replacement node",
			slog.String("node_id", newSpec.ID),
			slog.String("error", err.Error()),
		)
		return
	}

	r.mu.Lock()
	r.nodes[newSpec.ID] = newNode
	r.nodeSpecs[newSpec.ID] = newSpec
	r.mu.Unlock()

	r.logger.Info("replacement node started",
		slog.String("old_node_id", nodeID),
		slog.String("new_node_id", newSpec.ID),
		slog.Duration("cold_start", coldStart),
	)

	if r.metrics != nil {
		r.metrics.RegisterNode(newSpec)
		r.metrics.RecordNodeStart(newSpec.ID)
		r.metrics.RecordColdStartDelay(newSpec.ID, coldStart)
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
		if r.metrics != nil {
			r.metrics.RegisterNode(spec)
		}
		node := NewSimulatedNode(spec, r.controlPlaneAddr, r.logger)
		if err := node.Start(ctx); err != nil {
			return fmt.Errorf("failed to start node %s: %w", spec.ID, err)
		}
		r.nodes[spec.ID] = node
		if r.metrics != nil {
			r.metrics.RecordNodeStart(spec.ID)
		}
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
