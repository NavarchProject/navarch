package simulator

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"
)

// ChaosEngine manages failure injection with realistic patterns.
type ChaosEngine struct {
	config  *ChaosConfig
	rng     *rand.Rand
	logger  *slog.Logger
	nodes   func() map[string]*SimulatedNode // Node accessor
	metrics *StressMetrics

	mu                sync.RWMutex
	running           bool
	cancel            context.CancelFunc
	pendingRecoveries map[string]time.Time
	failureHistory    []FailureEvent
}

// FailureEvent records a failure that occurred.
type FailureEvent struct {
	Timestamp   time.Time
	NodeID      string
	Type        string
	XIDCode     int
	GPUIndex    int
	Message     string
	IsCascade   bool
	CascadeFrom string
	Recovered   bool
}

// NewChaosEngine creates a new chaos engine.
func NewChaosEngine(config *ChaosConfig, nodeAccessor func() map[string]*SimulatedNode, metrics *StressMetrics, seed int64, logger *slog.Logger) *ChaosEngine {
	if seed == 0 {
		seed = time.Now().UnixNano()
	}
	return &ChaosEngine{
		config:            config,
		rng:               rand.New(rand.NewSource(seed)),
		logger:            logger.With(slog.String("component", "chaos-engine")),
		nodes:             nodeAccessor,
		metrics:           metrics,
		pendingRecoveries: make(map[string]time.Time),
		failureHistory:    make([]FailureEvent, 0, 10000),
	}
}

// Start begins the chaos engine.
func (c *ChaosEngine) Start(ctx context.Context) {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return
	}
	c.running = true
	ctx, c.cancel = context.WithCancel(ctx)
	c.mu.Unlock()

	if c.config == nil || !c.config.Enabled {
		c.logger.Info("chaos engine disabled")
		return
	}

	c.logger.Info("chaos engine started",
		slog.Float64("failure_rate", c.config.FailureRate),
		slog.Bool("cascading_enabled", c.config.Cascading != nil && c.config.Cascading.Enabled),
	)

	go c.failureInjectionLoop(ctx)
	go c.recoveryLoop(ctx)
	go c.scheduledOutageLoop(ctx)
}

// Stop halts the chaos engine.
func (c *ChaosEngine) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
	}
	c.running = false
}

// GetFailureHistory returns recorded failure events.
func (c *ChaosEngine) GetFailureHistory() []FailureEvent {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make([]FailureEvent, len(c.failureHistory))
	copy(result, c.failureHistory)
	return result
}

// InjectFailure manually injects a failure into a specific node.
func (c *ChaosEngine) InjectFailure(nodeID string, failure InjectedFailure) error {
	nodes := c.nodes()
	node, ok := nodes[nodeID]
	if !ok {
		return nil // Node not found
	}

	node.InjectFailure(failure)

	event := FailureEvent{
		Timestamp: time.Now(),
		NodeID:    nodeID,
		Type:      failure.Type,
		XIDCode:   failure.XIDCode,
		GPUIndex:  failure.GPUIndex,
		Message:   failure.Message,
	}

	c.recordFailure(event)
	if c.metrics != nil {
		c.metrics.RecordFailure(event)
		status := c.determineNodeStatus(node)
		c.logger.Debug("updating node health",
			slog.String("node_id", nodeID),
			slog.String("status", status),
			slog.Int("failure_count", len(node.GetFailures())),
		)
		c.metrics.RecordNodeHealth(nodeID, status)
	}

	// Check for cascading
	if c.config.Cascading != nil && c.config.Cascading.Enabled {
		c.maybeTriggerCascade(nodeID, failure, 0)
	}

	// Schedule recovery for recoverable failures
	if c.config.Recovery != nil && c.config.Recovery.Enabled && c.isRecoverable(failure) {
		c.scheduleRecovery(nodeID, failure.Type)
	}

	return nil
}

func (c *ChaosEngine) determineNodeStatus(node *SimulatedNode) string {
	failures := node.GetFailures()
	if len(failures) == 0 {
		return "healthy"
	}
	for _, f := range failures {
		switch f.Type {
		case "boot_failure", "nvml_failure", "network", "device_error":
			return "unhealthy"
		}
	}
	return "degraded"
}

func (c *ChaosEngine) failureInjectionLoop(ctx context.Context) {
	if c.config.FailureRate <= 0 {
		return
	}

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.maybeInjectFailure()
		}
	}
}

func (c *ChaosEngine) maybeInjectFailure() {
	nodes := c.nodes()
	nodeCount := len(nodes)
	if nodeCount == 0 {
		return
	}

	// FailureRate is failures per minute per 1000 nodes
	failuresPerSecond := c.config.FailureRate / 60.0
	adjustedRate := failuresPerSecond * float64(nodeCount) / 1000.0

	if c.rng.Float64() >= adjustedRate {
		return
	}

	c.injectRandomFailure()
}

func (c *ChaosEngine) injectRandomFailure() {
	nodes := c.nodes()
	if len(nodes) == 0 {
		return
	}

	nodeList := make([]*SimulatedNode, 0, len(nodes))
	for _, n := range nodes {
		nodeList = append(nodeList, n)
	}
	node := nodeList[c.rng.Intn(len(nodeList))]
	failureType := c.selectFailureType()

	var failure InjectedFailure
	switch failureType {
	case "xid_error":
		failure = c.generateXIDFailure()
	case "temperature":
		failure = c.generateTemperatureFailure()
	case "nvml_failure":
		failure = c.generateNVMLFailure()
	case "boot_failure":
		failure = c.generateBootFailure()
	case "network":
		failure = c.generateNetworkFailure()
	default:
		failure = c.generateXIDFailure()
	}

	c.InjectFailure(node.ID(), failure)
}

func (c *ChaosEngine) selectFailureType() string {
	if len(c.config.FailureTypes) == 0 {
		return "xid_error"
	}

	totalWeight := 0
	for _, ft := range c.config.FailureTypes {
		totalWeight += ft.Weight
	}
	if totalWeight == 0 {
		return "xid_error"
	}

	roll := c.rng.Intn(totalWeight)
	cumulative := 0
	for _, ft := range c.config.FailureTypes {
		cumulative += ft.Weight
		if roll < cumulative {
			return ft.Type
		}
	}
	return "xid_error"
}

func (c *ChaosEngine) selectXIDCode() int {
	if len(c.config.XIDDistribution) == 0 {
		// Default distribution based on real-world frequencies
		codes := []int{13, 31, 32, 43, 45, 48, 63, 64, 74, 79, 92, 94, 95}
		return codes[c.rng.Intn(len(codes))]
	}

	// Sort codes for deterministic iteration order (reproducibility with --seed)
	codes := make([]int, 0, len(c.config.XIDDistribution))
	totalWeight := 0
	for code, weight := range c.config.XIDDistribution {
		codes = append(codes, code)
		totalWeight += weight
	}
	if totalWeight == 0 {
		return 79
	}
	sort.Ints(codes)

	roll := c.rng.Intn(totalWeight)
	cumulative := 0
	for _, code := range codes {
		cumulative += c.config.XIDDistribution[code]
		if roll < cumulative {
			return code
		}
	}
	return 79
}

func (c *ChaosEngine) generateXIDFailure() InjectedFailure {
	xidCode := c.selectXIDCode()
	gpuIndex := c.rng.Intn(8) // Assume max 8 GPUs

	xidInfo, known := XIDCodes[xidCode]
	message := "Unknown XID error"
	if known {
		message = xidInfo.Name
	}

	return InjectedFailure{
		Type:     "xid_error",
		XIDCode:  xidCode,
		GPUIndex: gpuIndex,
		Message:  message,
	}
}

func (c *ChaosEngine) generateTemperatureFailure() InjectedFailure {
	gpuIndex := c.rng.Intn(8)
	temps := []struct {
		temp int
		msg  string
	}{
		{95, "CRITICAL: GPU thermal shutdown imminent"},
		{90, "SEVERE: GPU throttling due to high temperature"},
		{85, "WARNING: GPU temperature elevated"},
	}
	selected := temps[c.rng.Intn(len(temps))]

	return InjectedFailure{
		Type:     "temperature",
		GPUIndex: gpuIndex,
		Message:  selected.msg,
	}
}

func (c *ChaosEngine) generateNVMLFailure() InjectedFailure {
	messages := []string{
		"NVML: Failed to query device info",
		"NVML: Driver not loaded",
		"NVML: Device not found",
		"NVML: Communication failure with GPU",
		"NVML: Timeout waiting for device response",
		"NVML: Insufficient permissions",
		"NVML: GPU initialization failed",
	}

	return InjectedFailure{
		Type:     "nvml_failure",
		GPUIndex: c.rng.Intn(8),
		Message:  messages[c.rng.Intn(len(messages))],
	}
}

func (c *ChaosEngine) generateBootFailure() InjectedFailure {
	messages := []string{
		"GPU not detected during boot sequence",
		"PCIe link training failed",
		"GPU VBIOS checksum mismatch",
		"GPU firmware initialization timeout",
		"GPU BAR memory allocation failed",
		"NVLink topology initialization failed",
		"GPU power delivery fault detected",
	}

	return InjectedFailure{
		Type:    "boot_failure",
		Message: messages[c.rng.Intn(len(messages))],
	}
}

func (c *ChaosEngine) generateNetworkFailure() InjectedFailure {
	messages := []string{
		"Network connectivity lost to control plane",
		"Heartbeat timeout exceeded",
		"gRPC connection refused",
		"DNS resolution failed",
		"TCP connection reset",
	}

	return InjectedFailure{
		Type:    "network",
		Message: messages[c.rng.Intn(len(messages))],
	}
}

func (c *ChaosEngine) isRecoverable(failure InjectedFailure) bool {
	if failure.Type != "xid_error" {
		return true // Non-XID failures generally recoverable
	}

	xidInfo, known := XIDCodes[failure.XIDCode]
	if !known {
		return false
	}

	return !xidInfo.Fatal
}

func (c *ChaosEngine) maybeTriggerCascade(sourceNodeID string, failure InjectedFailure, depth int) {
	if c.config.Cascading == nil || !c.config.Cascading.Enabled {
		return
	}

	if depth >= c.config.Cascading.MaxDepth {
		return
	}

	if c.rng.Float64() > c.config.Cascading.Probability {
		return
	}

	nodes := c.nodes()
	sourceNode, ok := nodes[sourceNodeID]
	if !ok {
		return
	}

	// Find candidate nodes in same scope
	var candidates []*SimulatedNode
	for _, n := range nodes {
		if n.ID() == sourceNodeID {
			continue
		}
		if c.matchesScope(sourceNode, n, c.config.Cascading.Scope) {
			candidates = append(candidates, n)
		}
	}

	if len(candidates) == 0 {
		return
	}

	maxAffected := int(float64(len(candidates)) * c.config.Cascading.MaxAffectedPercent)
	if maxAffected < 1 {
		maxAffected = 1
	}
	numAffected := 1 + c.rng.Intn(maxAffected)

	c.rng.Shuffle(len(candidates), func(i, j int) {
		candidates[i], candidates[j] = candidates[j], candidates[i]
	})

	for i := 0; i < numAffected && i < len(candidates); i++ {
		targetNode := candidates[i]
		minDelay := c.config.Cascading.MinDelay.Duration()
		maxDelay := c.config.Cascading.MaxDelay.Duration()
		if maxDelay <= minDelay {
			maxDelay = minDelay + time.Second
		}
		delay := minDelay + time.Duration(c.rng.Int63n(int64(maxDelay-minDelay)))

		go func(target *SimulatedNode, d int, delay time.Duration) {
			time.Sleep(delay)

			cascadeFailure := c.generateXIDFailure()
			target.InjectFailure(cascadeFailure)

			event := FailureEvent{
				Timestamp:   time.Now(),
				NodeID:      target.ID(),
				Type:        cascadeFailure.Type,
				XIDCode:     cascadeFailure.XIDCode,
				GPUIndex:    cascadeFailure.GPUIndex,
				Message:     cascadeFailure.Message,
				IsCascade:   true,
				CascadeFrom: sourceNodeID,
			}

			c.recordFailure(event)
			if c.metrics != nil {
				c.metrics.RecordFailure(event)
				c.metrics.RecordNodeHealth(target.ID(), c.determineNodeStatus(target))
			}

			c.logger.Debug("cascading failure triggered",
				slog.String("source", sourceNodeID),
				slog.String("target", target.ID()),
				slog.Int("depth", d+1),
			)

			c.maybeTriggerCascade(target.ID(), cascadeFailure, d+1)
		}(targetNode, depth, delay)
	}
}

func (c *ChaosEngine) matchesScope(source, target *SimulatedNode, scope string) bool {
	// Extract topology info from node ID patterns
	// Format: provider-region-template-index (e.g., gcp-us-central1-h100-8gpu-0001)
	// Zone info is typically embedded in region (e.g., us-central1-a)
	switch scope {
	case "zone":
		// Match by provider, region, and zone (first 3 segments for zone-level locality)
		// This is more restrictive than region
		return extractSegments(source.ID(), 3) == extractSegments(target.ID(), 3)
	case "region":
		// Match by provider and region (first 2 segments)
		return extractSegments(source.ID(), 2) == extractSegments(target.ID(), 2)
	case "provider":
		// Match by provider (first segment)
		return extractSegments(source.ID(), 1) == extractSegments(target.ID(), 1)
	case "rack":
		// Match first 4 segments for rack-level (most restrictive)
		return extractSegments(source.ID(), 4) == extractSegments(target.ID(), 4)
	case "random":
		return true
	default:
		return true
	}
}

func extractSegments(s string, n int) string {
	count := 0
	for i, ch := range s {
		if ch == '-' {
			count++
			if count == n {
				return s[:i]
			}
		}
	}
	return s
}

func (c *ChaosEngine) scheduleRecovery(nodeID, failureType string) {
	if c.config.Recovery == nil || !c.config.Recovery.Enabled {
		return
	}

	if c.rng.Float64() > c.config.Recovery.Probability {
		return // This failure won't recover
	}

	// Calculate recovery time with normal distribution
	meanNs := c.config.Recovery.MeanTime.Duration().Nanoseconds()
	stdDevNs := c.config.Recovery.StdDev.Duration().Nanoseconds()
	if stdDevNs == 0 {
		stdDevNs = meanNs / 4 // Default 25% std dev
	}

	recoveryNs := int64(c.rng.NormFloat64()*float64(stdDevNs)) + meanNs
	if recoveryNs < int64(10*time.Second) {
		recoveryNs = int64(10 * time.Second) // Minimum recovery time
	}

	recoveryTime := time.Now().Add(time.Duration(recoveryNs))

	c.mu.Lock()
	c.pendingRecoveries[nodeID+":"+failureType] = recoveryTime
	c.mu.Unlock()
}

func (c *ChaosEngine) recoveryLoop(ctx context.Context) {
	if c.config.Recovery == nil || !c.config.Recovery.Enabled {
		return
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.processRecoveries()
		}
	}
}

func (c *ChaosEngine) processRecoveries() {
	now := time.Now()
	nodes := c.nodes()

	c.mu.Lock()
	toRecover := make(map[string]string)
	for key, recoveryTime := range c.pendingRecoveries {
		if now.After(recoveryTime) {
			parsed := false
			for i := len(key) - 1; i >= 0; i-- {
				if key[i] == ':' {
					toRecover[key[:i]] = key[i+1:]
					parsed = true
					break
				}
			}
			if parsed {
				delete(c.pendingRecoveries, key)
			} else {
				c.logger.Warn("invalid recovery key format, expected nodeID:failureType",
					slog.String("key", key),
				)
				delete(c.pendingRecoveries, key) // Remove malformed key to prevent infinite loop
			}
		}
	}
	c.mu.Unlock()

	for nodeID, failureType := range toRecover {
		if node, ok := nodes[nodeID]; ok {
			node.RecoverFailure(failureType)
			c.logger.Debug("node recovered from failure",
				slog.String("node_id", nodeID),
				slog.String("failure_type", failureType),
			)
			if c.metrics != nil {
				c.metrics.RecordRecovery(nodeID, failureType)
				c.metrics.RecordNodeHealth(nodeID, c.determineNodeStatus(node))
			}
		}
	}
}

func (c *ChaosEngine) scheduledOutageLoop(ctx context.Context) {
	if len(c.config.ScheduledOutages) == 0 {
		return
	}

	startTime := time.Now()

	for _, outage := range c.config.ScheduledOutages {
		go func(o ScheduledOutage) {
			waitTime := o.StartTime.Duration() - time.Since(startTime)
			if waitTime > 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(waitTime):
				}
			}

			c.executeOutage(ctx, o)
		}(outage)
	}
}

func (c *ChaosEngine) executeOutage(ctx context.Context, outage ScheduledOutage) {
	c.logger.Warn("executing scheduled outage",
		slog.String("name", outage.Name),
		slog.String("scope", outage.Scope),
		slog.String("target", outage.Target),
		slog.Duration("duration", outage.Duration.Duration()),
	)

	nodes := c.nodes()
	var affected []*SimulatedNode

	for _, node := range nodes {
		if c.nodeMatchesOutageScope(node, outage) {
			affected = append(affected, node)
		}
	}

	for _, node := range affected {
		failure := InjectedFailure{
			Type:    outage.FailureType,
			Message: "Scheduled outage: " + outage.Name,
		}
		if outage.FailureType == "xid_error" {
			failure.XIDCode = 79
		}
		node.InjectFailure(failure)

		event := FailureEvent{
			Timestamp: time.Now(),
			NodeID:    node.ID(),
			Type:      outage.FailureType,
			Message:   failure.Message,
		}
		c.recordFailure(event)
		if c.metrics != nil {
			c.metrics.RecordFailure(event)
			c.metrics.RecordNodeHealth(node.ID(), c.determineNodeStatus(node))
		}
	}

	if c.metrics != nil {
		c.metrics.RecordOutage(outage.Name, len(affected))
	}

	c.logger.Info("outage started",
		slog.String("name", outage.Name),
		slog.Int("affected_nodes", len(affected)),
	)

	select {
	case <-ctx.Done():
		return
	case <-time.After(outage.Duration.Duration()):
	}

	c.logger.Info("outage ended, recovering nodes",
		slog.String("name", outage.Name),
		slog.Int("affected_count", len(affected)),
	)

	for _, node := range affected {
		node.ClearFailures()
		if c.metrics != nil {
			c.metrics.RecordNodeHealth(node.ID(), "healthy")
		}
	}
}

func (c *ChaosEngine) nodeMatchesOutageScope(node *SimulatedNode, outage ScheduledOutage) bool {
	nodeID := node.ID()

	switch outage.Scope {
	case "zone":
		return extractSegments(nodeID, 2) == outage.Target ||
			strings.Contains(nodeID, outage.Target)
	case "region":
		return extractSegments(nodeID, 2) == outage.Target ||
			strings.Contains(nodeID, outage.Target)
	case "provider":
		return extractSegments(nodeID, 1) == outage.Target
	case "percentage":
		// Random percentage of nodes
		return c.rng.Float64()*100 < parsePercentage(outage.Target)
	default:
		return false
	}
}

func parsePercentage(s string) float64 {
	var pct float64
	fmt.Sscanf(s, "%f", &pct)
	return pct
}

func (c *ChaosEngine) recordFailure(event FailureEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Limit history size
	if len(c.failureHistory) >= 10000 {
		c.failureHistory = c.failureHistory[1000:]
	}
	c.failureHistory = append(c.failureHistory, event)
}
