package simulator

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/NavarchProject/navarch/pkg/clock"
)

// FleetGenerator generates nodes based on configuration templates.
type FleetGenerator struct {
	config *FleetGeneratorConfig
	rng    *rand.Rand
	logger *slog.Logger

	// Computed distributions
	templateWeights []int
	providerList    []string
	providerWeights []int
	regionList      []string
	regionWeights   []int
}

// NewFleetGenerator creates a new fleet generator.
func NewFleetGenerator(config *FleetGeneratorConfig, seed int64, logger *slog.Logger) *FleetGenerator {
	if seed == 0 {
		seed = time.Now().UnixNano()
	}

	gen := &FleetGenerator{
		config: config,
		rng:    rand.New(rand.NewSource(seed)),
		logger: logger,
	}

	gen.computeDistributions()
	return gen
}

func (g *FleetGenerator) computeDistributions() {
	g.templateWeights = make([]int, len(g.config.Templates))
	for i, t := range g.config.Templates {
		g.templateWeights[i] = t.Weight
	}

	// Sort provider keys for deterministic iteration order (reproducibility with seeded RNG)
	providers := make([]string, 0, len(g.config.Providers))
	for provider := range g.config.Providers {
		providers = append(providers, provider)
	}
	sort.Strings(providers)
	for _, provider := range providers {
		g.providerList = append(g.providerList, provider)
		g.providerWeights = append(g.providerWeights, g.config.Providers[provider])
	}
	if len(g.providerList) == 0 {
		g.providerList = []string{"gcp", "aws", "lambda"}
		g.providerWeights = []int{50, 35, 15}
	}

	// Sort region keys for deterministic iteration order (reproducibility with seeded RNG)
	regions := make([]string, 0, len(g.config.Regions))
	for region := range g.config.Regions {
		regions = append(regions, region)
	}
	sort.Strings(regions)
	for _, region := range regions {
		g.regionList = append(g.regionList, region)
		g.regionWeights = append(g.regionWeights, g.config.Regions[region])
	}
	if len(g.regionList) == 0 {
		g.regionList = []string{"us-central1", "us-east1", "us-west1", "europe-west1", "asia-east1"}
		g.regionWeights = []int{30, 25, 20, 15, 10}
	}
}

// GenerateFleet generates the full fleet of NodeSpecs.
func (g *FleetGenerator) GenerateFleet() []NodeSpec {
	specs := make([]NodeSpec, g.config.TotalNodes)

	for i := 0; i < g.config.TotalNodes; i++ {
		specs[i] = g.generateNode(i)
	}

	g.logger.Info("fleet generated",
		slog.Int("total_nodes", len(specs)),
		slog.Int("templates", len(g.config.Templates)),
	)

	return specs
}

func (g *FleetGenerator) generateNode(index int) NodeSpec {
	template := g.selectTemplate()
	provider := g.selectFromWeighted(g.providerList, g.providerWeights)
	region := g.selectFromWeighted(g.regionList, g.regionWeights)
	zone := g.selectZone(region)

	nodeID := fmt.Sprintf("%s-%s-%s-%04d", provider, region, template.Name, index)

	labels := make(map[string]string)
	for k, v := range template.Labels {
		labels[k] = v
	}
	labels["template"] = template.Name
	labels["generated"] = "true"
	labels["index"] = fmt.Sprintf("%d", index)

	return NodeSpec{
		ID:           nodeID,
		Provider:     provider,
		Region:       region,
		Zone:         zone,
		InstanceType: g.getInstanceType(template, provider),
		GPUCount:     template.GPUCount,
		GPUType:      template.GPUType,
		Labels:       labels,
	}
}

func (g *FleetGenerator) selectTemplate() NodeTemplate {
	return g.config.Templates[g.selectWeightedIndex(g.templateWeights)]
}

func (g *FleetGenerator) selectFromWeighted(items []string, weights []int) string {
	if len(items) == 0 {
		return ""
	}
	return items[g.selectWeightedIndex(weights)]
}

func (g *FleetGenerator) selectWeightedIndex(weights []int) int {
	total := 0
	for _, w := range weights {
		total += w
	}
	if total == 0 {
		return 0
	}

	roll := g.rng.Intn(total)
	cumulative := 0
	for i, w := range weights {
		cumulative += w
		if roll < cumulative {
			return i
		}
	}
	return len(weights) - 1
}

func (g *FleetGenerator) selectZone(region string) string {
	// Check if zones are specified for this region
	if zones, ok := g.config.Zones[region]; ok && len(zones) > 0 {
		return zones[g.rng.Intn(len(zones))]
	}

	// Generate zone suffix (a, b, c, d)
	suffixes := []string{"a", "b", "c", "d"}
	return region + "-" + suffixes[g.rng.Intn(len(suffixes))]
}

func (g *FleetGenerator) getInstanceType(template NodeTemplate, provider string) string {
	// Map GPU type to provider-specific instance types
	gpuType := template.GPUType

	switch provider {
	case "gcp":
		return g.getGCPInstanceType(gpuType, template.GPUCount)
	case "aws":
		return g.getAWSInstanceType(gpuType, template.GPUCount)
	case "lambda":
		return g.getLambdaInstanceType(gpuType, template.GPUCount)
	default:
		return "generic-gpu"
	}
}

func (g *FleetGenerator) getGCPInstanceType(gpuType string, gpuCount int) string {
	switch {
	case containsIgnoreCase(gpuType, "H100"):
		// A3 instances for H100
		switch gpuCount {
		case 8:
			return "a3-highgpu-8g"
		default:
			return fmt.Sprintf("a3-highgpu-%dg", gpuCount)
		}
	case containsIgnoreCase(gpuType, "A100"):
		// A2 instances for A100
		if containsIgnoreCase(gpuType, "80GB") {
			switch gpuCount {
			case 8:
				return "a2-ultragpu-8g"
			case 4:
				return "a2-ultragpu-4g"
			case 2:
				return "a2-ultragpu-2g"
			case 1:
				return "a2-ultragpu-1g"
			default:
				return "a2-ultragpu-8g"
			}
		}
		switch gpuCount {
		case 16:
			return "a2-megagpu-16g"
		case 8:
			return "a2-highgpu-8g"
		case 4:
			return "a2-highgpu-4g"
		case 2:
			return "a2-highgpu-2g"
		case 1:
			return "a2-highgpu-1g"
		default:
			return "a2-highgpu-8g"
		}
	case containsIgnoreCase(gpuType, "L4"):
		// G2 instances for L4
		switch gpuCount {
		case 8:
			return "g2-standard-96"
		case 4:
			return "g2-standard-48"
		case 2:
			return "g2-standard-24"
		case 1:
			return "g2-standard-12"
		default:
			return "g2-standard-48"
		}
	case containsIgnoreCase(gpuType, "T4"):
		// N1 instances with T4
		return fmt.Sprintf("n1-standard-8") // T4 uses n1 instances
	default:
		return "a3-highgpu-8g"
	}
}

func (g *FleetGenerator) getAWSInstanceType(gpuType string, gpuCount int) string {
	switch {
	case containsIgnoreCase(gpuType, "H100"):
		// P5 instances for H100
		return "p5.48xlarge" // 8x H100
	case containsIgnoreCase(gpuType, "A100"):
		// P4d/P4de instances for A100
		if containsIgnoreCase(gpuType, "80GB") {
			return "p4de.24xlarge" // 8x A100 80GB
		}
		return "p4d.24xlarge" // 8x A100 40GB
	case containsIgnoreCase(gpuType, "A10G"):
		// G5 instances for A10G
		switch gpuCount {
		case 8:
			return "g5.48xlarge"
		case 4:
			return "g5.24xlarge"
		case 2:
			return "g5.12xlarge"
		case 1:
			return "g5.xlarge"
		default:
			return "g5.48xlarge"
		}
	case containsIgnoreCase(gpuType, "L4"):
		// G6 instances for L4
		switch gpuCount {
		case 8:
			return "g6.48xlarge"
		case 4:
			return "g6.24xlarge"
		case 2:
			return "g6.12xlarge"
		case 1:
			return "g6.xlarge"
		default:
			return "g6.48xlarge"
		}
	case containsIgnoreCase(gpuType, "T4"):
		// G4dn instances for T4
		switch gpuCount {
		case 8:
			return "g4dn.metal"
		case 4:
			return "g4dn.12xlarge"
		case 1:
			return "g4dn.xlarge"
		default:
			return "g4dn.12xlarge"
		}
	default:
		return "p5.48xlarge"
	}
}

func (g *FleetGenerator) getLambdaInstanceType(gpuType string, gpuCount int) string {
	// Lambda Labs uses a simpler naming scheme
	gpuName := "h100"
	switch {
	case containsIgnoreCase(gpuType, "H100"):
		gpuName = "h100"
	case containsIgnoreCase(gpuType, "A100"):
		gpuName = "a100"
	case containsIgnoreCase(gpuType, "A10"):
		gpuName = "a10"
	case containsIgnoreCase(gpuType, "RTX"):
		gpuName = "rtx6000"
	}
	return fmt.Sprintf("gpu_%dx_%s", gpuCount, gpuName)
}

func containsIgnoreCase(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

// NodeStarter handles starting nodes with various patterns.
type NodeStarter struct {
	config           StartupConfig
	controlPlaneAddr string
	logger           *slog.Logger
	clock            clock.Clock
	rng              *rand.Rand
	runDir           *RunDir

	mu              sync.Mutex
	nodes           map[string]*SimulatedNode
	coldStartDelays map[string]time.Duration
	started         int64
	failed          int64
}

// NewNodeStarter creates a new node starter.
func NewNodeStarter(config StartupConfig, controlPlaneAddr string, seed int64, clk clock.Clock, logger *slog.Logger) *NodeStarter {
	if seed == 0 {
		seed = time.Now().UnixNano()
	}
	if clk == nil {
		clk = clock.Real()
	}
	return &NodeStarter{
		config:           config,
		controlPlaneAddr: controlPlaneAddr,
		logger:           logger,
		clock:            clk,
		rng:              rand.New(rand.NewSource(seed)),
		nodes:            make(map[string]*SimulatedNode),
		coldStartDelays:  make(map[string]time.Duration),
	}
}

// SetRunDir configures file-based logging for each node.
func (s *NodeStarter) SetRunDir(rd *RunDir) {
	s.runDir = rd
}

// StartFleet starts all nodes according to the configured pattern.
func (s *NodeStarter) StartFleet(ctx context.Context, specs []NodeSpec) (map[string]*SimulatedNode, error) {
	switch s.config.Pattern {
	case "instant", "":
		return s.startInstant(ctx, specs)
	case "linear":
		return s.startLinear(ctx, specs)
	case "exponential":
		return s.startExponential(ctx, specs)
	case "wave":
		return s.startWave(ctx, specs)
	default:
		return nil, fmt.Errorf("unknown startup pattern: %s", s.config.Pattern)
	}
}

func (s *NodeStarter) startInstant(ctx context.Context, specs []NodeSpec) (map[string]*SimulatedNode, error) {
	s.logger.Info("starting fleet instantly", slog.Int("count", len(specs)))

	var wg sync.WaitGroup
	errCh := make(chan error, len(specs))
	semaphore := make(chan struct{}, 100)

	for _, spec := range specs {
		wg.Add(1)
		go func(sp NodeSpec) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			if err := s.startNode(ctx, sp); err != nil {
				errCh <- fmt.Errorf("failed to start %s: %w", sp.ID, err)
			}
		}(spec)
	}

	wg.Wait()
	close(errCh)

	var errs []error
	for err := range errCh {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		s.logger.Warn("some nodes failed to start",
			slog.Int("failed", len(errs)),
			slog.Int("total", len(specs)),
		)
	}

	s.logger.Info("fleet started",
		slog.Int64("started", atomic.LoadInt64(&s.started)),
		slog.Int64("failed", atomic.LoadInt64(&s.failed)),
	)

	return s.nodes, nil
}

func (s *NodeStarter) startLinear(ctx context.Context, specs []NodeSpec) (map[string]*SimulatedNode, error) {
	duration := s.config.Duration.Duration()
	if duration == 0 {
		duration = 30 * time.Second
	}

	interval := duration / time.Duration(len(specs))
	s.logger.Info("starting fleet linearly",
		slog.Int("count", len(specs)),
		slog.Duration("duration", duration),
		slog.Duration("interval", interval),
	)

	for i, spec := range specs {
		select {
		case <-ctx.Done():
			return s.nodes, ctx.Err()
		default:
		}

		jitteredInterval := s.addJitter(interval)
		if i > 0 {
			s.clock.Sleep(jitteredInterval)
		}

		if err := s.startNode(ctx, spec); err != nil {
			s.logger.Error("failed to start node", slog.String("node_id", spec.ID), slog.String("error", err.Error()))
		}
	}

	return s.nodes, nil
}

func (s *NodeStarter) startExponential(ctx context.Context, specs []NodeSpec) (map[string]*SimulatedNode, error) {
	duration := s.config.Duration.Duration()
	if duration == 0 {
		duration = 2 * time.Minute
	}

	s.logger.Info("starting fleet exponentially",
		slog.Int("count", len(specs)),
		slog.Duration("duration", duration),
	)

	// Start with batch size of 1, doubling each round
	remaining := specs
	batchSize := 1
	totalDuration := time.Duration(0)
	rounds := 0

	// Calculate number of rounds needed
	temp := len(specs)
	for temp > 0 {
		temp -= batchSize
		if batchSize < len(specs) {
			batchSize *= 2
		}
		rounds++
	}

	roundInterval := duration / time.Duration(rounds)
	batchSize = 1

	for len(remaining) > 0 {
		select {
		case <-ctx.Done():
			return s.nodes, ctx.Err()
		default:
		}

		// Start current batch
		toStart := batchSize
		if toStart > len(remaining) {
			toStart = len(remaining)
		}

		var wg sync.WaitGroup
		for i := 0; i < toStart; i++ {
			wg.Add(1)
			go func(sp NodeSpec) {
				defer wg.Done()
				if err := s.startNode(ctx, sp); err != nil {
					s.logger.Error("failed to start node", slog.String("node_id", sp.ID))
				}
			}(remaining[i])
		}
		wg.Wait()

		remaining = remaining[toStart:]
		batchSize *= 2 // Double batch size for next round

		if len(remaining) > 0 {
			s.clock.Sleep(s.addJitter(roundInterval))
		}
		totalDuration += roundInterval
	}

	return s.nodes, nil
}

func (s *NodeStarter) startWave(ctx context.Context, specs []NodeSpec) (map[string]*SimulatedNode, error) {
	duration := s.config.Duration.Duration()
	if duration == 0 {
		duration = 5 * time.Minute
	}

	batchSize := s.config.BatchSize
	if batchSize <= 0 {
		batchSize = 100
	}

	numBatches := (len(specs) + batchSize - 1) / batchSize
	batchInterval := duration / time.Duration(numBatches)

	s.logger.Info("starting fleet in waves",
		slog.Int("count", len(specs)),
		slog.Int("batch_size", batchSize),
		slog.Int("num_batches", numBatches),
		slog.Duration("batch_interval", batchInterval),
	)

	for i := 0; i < len(specs); i += batchSize {
		select {
		case <-ctx.Done():
			return s.nodes, ctx.Err()
		default:
		}

		end := i + batchSize
		if end > len(specs) {
			end = len(specs)
		}
		batch := specs[i:end]

		var wg sync.WaitGroup
		for _, spec := range batch {
			wg.Add(1)
			go func(sp NodeSpec) {
				defer wg.Done()
				if err := s.startNode(ctx, sp); err != nil {
					s.logger.Error("failed to start node", slog.String("node_id", sp.ID))
				}
			}(spec)
		}
		wg.Wait()

		s.logger.Debug("batch started",
			slog.Int("batch", i/batchSize+1),
			slog.Int("nodes_in_batch", len(batch)),
		)

		if i+batchSize < len(specs) {
			s.clock.Sleep(s.addJitter(batchInterval))
		}
	}

	return s.nodes, nil
}

func (s *NodeStarter) startNode(ctx context.Context, spec NodeSpec) error {
	logger := s.logger
	if s.runDir != nil {
		var err error
		logger, err = s.runDir.CreateNodeLogger(spec.ID)
		if err != nil {
			s.logger.Warn("failed to create node logger, using default", slog.String("node", spec.ID), slog.String("error", err.Error()))
			logger = s.logger
		}
	}

	delay := s.coldStartDelay()
	if delay > 0 {
		s.logger.Debug("cold start delay",
			slog.String("node_id", spec.ID),
			slog.Duration("delay", delay),
		)
		timer := s.clock.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C():
		}
	}

	node := NewSimulatedNodeWithClock(spec, s.controlPlaneAddr, s.clock, logger)
	if err := node.Start(ctx); err != nil {
		atomic.AddInt64(&s.failed, 1)
		return err
	}

	s.mu.Lock()
	s.nodes[spec.ID] = node
	s.coldStartDelays[spec.ID] = delay
	s.mu.Unlock()

	atomic.AddInt64(&s.started, 1)
	return nil
}

func (s *NodeStarter) addJitter(d time.Duration) time.Duration {
	if s.config.JitterPercent <= 0 {
		return d
	}

	jitterFactor := 1.0 + (s.rng.Float64()-0.5)*2*float64(s.config.JitterPercent)/100
	return time.Duration(float64(d) * jitterFactor)
}

func (s *NodeStarter) coldStartDelay() time.Duration {
	mean := s.config.ColdStartMean.Duration()
	stddev := s.config.ColdStartStdDev.Duration()
	min := s.config.ColdStartMin.Duration()
	max := s.config.ColdStartMax.Duration()

	if mean > 0 {
		delay := mean
		if stddev > 0 {
			delay = time.Duration(s.rng.NormFloat64()*float64(stddev) + float64(mean))
			if delay < 0 {
				delay = 0
			}
		}
		if min > 0 && delay < min {
			delay = min
		}
		if max > 0 && delay > max {
			delay = max
		}
		return delay
	}

	if min > 0 || max > 0 {
		if max <= 0 {
			max = min
		}
		if min < 0 {
			min = 0
		}
		if max < min {
			max = min
		}
		if max == min {
			return min
		}
		return min + time.Duration(s.rng.Int63n(int64(max-min)))
	}

	return 0
}

// GetNodes returns all started nodes.
func (s *NodeStarter) GetNodes() map[string]*SimulatedNode {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make(map[string]*SimulatedNode, len(s.nodes))
	for k, v := range s.nodes {
		result[k] = v
	}
	return result
}

// GetColdStartDelays returns cold start delays for all started nodes.
func (s *NodeStarter) GetColdStartDelays() map[string]time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make(map[string]time.Duration, len(s.coldStartDelays))
	for k, v := range s.coldStartDelays {
		result[k] = v
	}
	return result
}

// StopAll stops all started nodes.
func (s *NodeStarter) StopAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, node := range s.nodes {
		node.Stop()
	}
}
