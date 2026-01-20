package fake

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/NavarchProject/navarch/pkg/gpu"
	"github.com/NavarchProject/navarch/pkg/node"
	"github.com/NavarchProject/navarch/pkg/provider"
)

// Config configures the fake provider.
type Config struct {
	ControlPlaneAddr string       // Address of the control plane to connect to
	GPUCount         int          // Number of fake GPUs per instance
	Logger           *slog.Logger // Logger for provider and node agents
}

// Provider simulates a cloud provider by running fake node agents in goroutines.
type Provider struct {
	config     Config
	mu         sync.RWMutex
	nodes      map[string]*fakeInstance
	nextID     atomic.Uint64
	logger     *slog.Logger
}

type fakeInstance struct {
	node   *provider.Node
	agent  *node.Node
	cancel context.CancelFunc
}

// New creates a new fake provider.
func New(cfg Config) (*Provider, error) {
	if cfg.ControlPlaneAddr == "" {
		return nil, fmt.Errorf("control plane address is required")
	}
	if cfg.GPUCount == 0 {
		cfg.GPUCount = 8
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Provider{
		config: cfg,
		nodes:  make(map[string]*fakeInstance),
		logger: logger,
	}, nil
}

func (p *Provider) Name() string {
	return "fake"
}

func (p *Provider) Provision(ctx context.Context, req provider.ProvisionRequest) (*provider.Node, error) {
	id := fmt.Sprintf("fake-%d", p.nextID.Add(1))

	providerNode := &provider.Node{
		ID:           id,
		Provider:     "fake",
		Region:       req.Region,
		Zone:         req.Zone,
		InstanceType: req.InstanceType,
		Status:       "running",
		IPAddress:    fmt.Sprintf("10.0.0.%d", p.nextID.Load()),
		GPUCount:     p.config.GPUCount,
		GPUType:      "NVIDIA H100 80GB HBM3 (fake)",
		Labels:       req.Labels,
	}

	agentLogger := p.logger.With(slog.String("node_id", id))
	agentCfg := node.Config{
		ControlPlaneAddr: p.config.ControlPlaneAddr,
		NodeID:           id,
		Provider:         "fake",
		Region:           req.Region,
		Zone:             req.Zone,
		InstanceType:     req.InstanceType,
		Labels:           req.Labels,
		GPU:              gpu.NewFake(p.config.GPUCount),
	}

	agent, err := node.New(agentCfg, agentLogger)
	if err != nil {
		return nil, fmt.Errorf("failed to create fake node agent: %w", err)
	}

	agentCtx, cancel := context.WithCancel(ctx)

	fi := &fakeInstance{
		node:   providerNode,
		agent:  agent,
		cancel: cancel,
	}

	p.mu.Lock()
	p.nodes[id] = fi
	p.mu.Unlock()

	go func() {
		if err := agent.Start(agentCtx); err != nil {
			p.logger.Error("fake node agent failed to start",
				slog.String("node_id", id),
				slog.String("error", err.Error()),
			)
			return
		}

		<-agentCtx.Done()
		agent.Stop()
		p.logger.Info("fake node agent stopped", slog.String("node_id", id))
	}()

	p.logger.Info("provisioned fake instance",
		slog.String("node_id", id),
		slog.String("instance_type", req.InstanceType),
		slog.Int("gpu_count", p.config.GPUCount),
	)

	return providerNode, nil
}

func (p *Provider) Terminate(ctx context.Context, nodeID string) error {
	p.mu.Lock()
	fi, ok := p.nodes[nodeID]
	if !ok {
		p.mu.Unlock()
		return fmt.Errorf("node %s not found", nodeID)
	}
	delete(p.nodes, nodeID)
	p.mu.Unlock()

	fi.cancel()
	fi.node.Status = "terminated"

	p.logger.Info("terminated fake instance", slog.String("node_id", nodeID))
	return nil
}

func (p *Provider) List(ctx context.Context) ([]*provider.Node, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	nodes := make([]*provider.Node, 0, len(p.nodes))
	for _, fi := range p.nodes {
		nodes = append(nodes, fi.node)
	}
	return nodes, nil
}

// TerminateAll stops all running fake instances.
func (p *Provider) TerminateAll() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for id, fi := range p.nodes {
		fi.cancel()
		fi.node.Status = "terminated"
		p.logger.Info("terminated fake instance", slog.String("node_id", id))
	}
	p.nodes = make(map[string]*fakeInstance)
}

// RunningCount returns the number of running fake instances.
func (p *Provider) RunningCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.nodes)
}

