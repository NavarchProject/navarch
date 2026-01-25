package simulator

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
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
	// Compute template weights
	g.templateWeights = make([]int, len(g.config.Templates))
	for i, t := range g.config.Templates {
		g.templateWeights[i] = t.Weight
	}

	// Compute provider distribution
	for provider, weight := range g.config.Providers {
		g.providerList = append(g.providerList, provider)
		g.providerWeights = append(g.providerWeights, weight)
	}

	// Default providers if not specified
	if len(g.providerList) == 0 {
		g.providerList = []string{"gcp", "aws", "lambda"}
		g.providerWeights = []int{50, 35, 15}
	}

	// Compute region distribution
	for region, weight := range g.config.Regions {
		g.regionList = append(g.regionList, region)
		g.regionWeights = append(g.regionWeights, weight)
	}

	// Default regions if not specified
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
	if template.InstanceType != "" {
		return template.InstanceType
	}

	// Default instance types by provider and GPU count
	switch provider {
	case "gcp":
		if template.GPUCount == 8 {
			return "a3-highgpu-8g"
		}
		return fmt.Sprintf("a3-highgpu-%dg", template.GPUCount)
	case "aws":
		if template.GPUCount == 8 {
			return "p5.48xlarge"
		}
		return "p4d.24xlarge"
	case "lambda":
		return fmt.Sprintf("gpu_%dx_%s", template.GPUCount, "h100")
	default:
		return "generic-gpu"
	}
}

// NodeStarter handles starting nodes with various patterns.
type NodeStarter struct {
	config         StartupConfig
	controlPlaneAddr string
	logger         *slog.Logger
	rng            *rand.Rand

	mu     sync.Mutex
	nodes  map[string]*SimulatedNode
	started int64
	failed  int64
}

// NewNodeStarter creates a new node starter.
func NewNodeStarter(config StartupConfig, controlPlaneAddr string, seed int64, logger *slog.Logger) *NodeStarter {
	if seed == 0 {
		seed = time.Now().UnixNano()
	}
	return &NodeStarter{
		config:         config,
		controlPlaneAddr: controlPlaneAddr,
		logger:         logger,
		rng:            rand.New(rand.NewSource(seed)),
		nodes:          make(map[string]*SimulatedNode),
	}
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

	// Start nodes concurrently with bounded parallelism
	semaphore := make(chan struct{}, 100) // Max 100 concurrent starts

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

	// Collect errors
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

		// Add jitter
		jitteredInterval := s.addJitter(interval)
		if i > 0 {
			time.Sleep(jitteredInterval)
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
			time.Sleep(s.addJitter(roundInterval))
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
			time.Sleep(s.addJitter(batchInterval))
		}
	}

	return s.nodes, nil
}

func (s *NodeStarter) startNode(ctx context.Context, spec NodeSpec) error {
	node := NewSimulatedNode(spec, s.controlPlaneAddr, s.logger)
	if err := node.Start(ctx); err != nil {
		atomic.AddInt64(&s.failed, 1)
		return err
	}

	s.mu.Lock()
	s.nodes[spec.ID] = node
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

// StopAll stops all started nodes.
func (s *NodeStarter) StopAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, node := range s.nodes {
		node.Stop()
	}
}
