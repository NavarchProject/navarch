package pool

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/NavarchProject/navarch/pkg/provider"
)

// Config defines a GPU node pool configuration.
type Config struct {
	Name string // Unique pool identifier

	InstanceType string   // Abstract or provider-specific instance type
	Region       string   // Default region for provisioning
	Zones        []string // Availability zones for multi-zone pools
	SSHKeyNames  []string // SSH key names to install on instances

	MinNodes int // Minimum nodes to maintain (hard floor)
	MaxNodes int // Maximum nodes allowed (hard ceiling)

	ScaleUpThreshold   int           // Utilization percentage to trigger scale up
	ScaleDownThreshold int           // Utilization percentage to trigger scale down
	ScaleDownDelay     time.Duration // Grace period before scaling down idle nodes
	CooldownPeriod     time.Duration // Minimum time between scaling actions

	UnhealthyThreshold int  // Consecutive health check failures before node is unhealthy
	AutoReplace        bool // Automatically replace unhealthy nodes

	Labels map[string]string // Key-value labels for workload routing
}

// ProviderConfig holds configuration for a single provider within a pool.
type ProviderConfig struct {
	Name         string           // Provider name
	Provider     provider.Provider
	Priority     int              // Lower = preferred
	Weight       int              // For round-robin weighting
	Regions      []string         // Regions to use with this provider
	InstanceType string           // Provider-specific instance type override
}

// Pool represents a managed group of GPU nodes backed by one or more providers.
type Pool struct {
	config    Config
	providers []ProviderConfig
	selector  ProviderSelector
	mu        sync.RWMutex
	nodes     map[string]*ManagedNode
	lastScale time.Time
}

// ManagedNode tracks a node within a pool.
type ManagedNode struct {
	Node            *provider.Node // Underlying provider node
	Pool            string         // Name of the pool this node belongs to
	ProviderName    string         // Which provider created this node
	HealthFailures  int            // Consecutive health check failures
	LastHealthCheck time.Time      // When the last health check ran
	Cordoned        bool           // If true, node is unschedulable for new workloads
	ProvisionedAt   time.Time      // When this node was created
}

// Status represents the current state of a pool.
type Status struct {
	Name           string  // Pool name
	TotalNodes     int     // Total nodes in pool
	HealthyNodes   int     // Nodes passing health checks
	UnhealthyNodes int     // Nodes failing health checks
	CordonedNodes  int     // Nodes marked unschedulable
	Utilization    float64 // Average utilization percentage
	CanScaleUp     bool    // True if pool is below MaxNodes
	CanScaleDown   bool    // True if pool is above MinNodes
}

// NewPoolOptions configures pool creation.
type NewPoolOptions struct {
	Config           Config
	Providers        []ProviderConfig
	ProviderStrategy string // priority, cost, availability, round-robin
}

// New creates a pool with a single provider.
func New(cfg Config, prov provider.Provider) (*Pool, error) {
	return NewSimple(cfg, prov, prov.Name())
}

// NewWithOptions creates a new pool with multiple providers.
func NewWithOptions(opts NewPoolOptions) (*Pool, error) {
	if opts.Config.Name == "" {
		return nil, fmt.Errorf("pool name is required")
	}
	if opts.Config.MinNodes < 0 {
		return nil, fmt.Errorf("min_nodes must be >= 0")
	}
	if opts.Config.MaxNodes < opts.Config.MinNodes {
		return nil, fmt.Errorf("max_nodes must be >= min_nodes")
	}
	if opts.Config.MaxNodes == 0 {
		return nil, fmt.Errorf("max_nodes must be > 0")
	}
	if len(opts.Providers) == 0 {
		return nil, fmt.Errorf("at least one provider is required")
	}

	candidates := make([]ProviderCandidate, len(opts.Providers))
	for i, pc := range opts.Providers {
		instanceType := pc.InstanceType
		if instanceType == "" {
			instanceType = provider.ResolveInstanceType(opts.Config.InstanceType, pc.Name)
		}

		candidates[i] = ProviderCandidate{
			Provider:     pc.Provider,
			Name:         pc.Name,
			Priority:     pc.Priority,
			Weight:       pc.Weight,
			Regions:      pc.Regions,
			InstanceType: instanceType,
		}
	}

	selector, err := NewSelector(opts.ProviderStrategy, candidates)
	if err != nil {
		return nil, err
	}

	return &Pool{
		config:    opts.Config,
		providers: opts.Providers,
		selector:  selector,
		nodes:     make(map[string]*ManagedNode),
	}, nil
}

// NewSimple creates a pool with a single provider (convenience function).
func NewSimple(cfg Config, prov provider.Provider, providerName string) (*Pool, error) {
	return NewWithOptions(NewPoolOptions{
		Config: cfg,
		Providers: []ProviderConfig{
			{Name: providerName, Provider: prov},
		},
		ProviderStrategy: "priority",
	})
}

// Config returns the pool configuration.
func (p *Pool) Config() Config {
	return p.config
}

// Status returns the current pool status.
func (p *Pool) Status() Status {
	p.mu.RLock()
	defer p.mu.RUnlock()

	status := Status{
		Name:       p.config.Name,
		TotalNodes: len(p.nodes),
	}

	for _, mn := range p.nodes {
		if mn.Cordoned {
			status.CordonedNodes++
		} else if mn.HealthFailures >= p.config.UnhealthyThreshold {
			status.UnhealthyNodes++
		} else {
			status.HealthyNodes++
		}
	}

	status.CanScaleUp = status.TotalNodes < p.config.MaxNodes
	status.CanScaleDown = status.TotalNodes > p.config.MinNodes

	return status
}

// ScaleUp adds nodes to the pool using the configured provider selection strategy.
// On provider failure, falls back to the next available provider.
func (p *Pool) ScaleUp(ctx context.Context, count int) ([]*provider.Node, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	currentCount := len(p.nodes)
	available := p.config.MaxNodes - currentCount
	if count > available {
		count = available
	}
	if count <= 0 {
		return nil, fmt.Errorf("pool at maximum capacity (%d nodes)", p.config.MaxNodes)
	}
	if time.Since(p.lastScale) < p.config.CooldownPeriod {
		return nil, fmt.Errorf("cooldown period not elapsed (%.0fs remaining)",
			(p.config.CooldownPeriod - time.Since(p.lastScale)).Seconds())
	}

	var nodes []*provider.Node
	for i := 0; i < count; i++ {
		node, providerName, err := p.provisionWithFallback(ctx, currentCount+i+1)
		if err != nil {
			return nodes, fmt.Errorf("failed to provision node %d: %w", i+1, err)
		}

		p.nodes[node.ID] = &ManagedNode{
			Node:          node,
			Pool:          p.config.Name,
			ProviderName:  providerName,
			ProvisionedAt: time.Now(),
		}
		nodes = append(nodes, node)
	}

	p.lastScale = time.Now()
	return nodes, nil
}

// provisionWithFallback tries providers in order until one succeeds.
func (p *Pool) provisionWithFallback(ctx context.Context, nodeNum int) (*provider.Node, string, error) {
	candidates := p.buildCandidates()

	var lastErr error
	for {
		candidate, err := p.selector.Select(ctx, candidates)
		if err != nil {
			if lastErr != nil {
				return nil, "", fmt.Errorf("all providers failed, last error: %w", lastErr)
			}
			return nil, "", err
		}

		region := p.config.Region
		if len(candidate.Regions) > 0 {
			region = candidate.Regions[0]
		}

		node, err := candidate.Provider.Provision(ctx, provider.ProvisionRequest{
			Name:         fmt.Sprintf("%s-%d", p.config.Name, nodeNum),
			InstanceType: candidate.InstanceType,
			Region:       region,
			SSHKeyNames:  p.config.SSHKeyNames,
			Labels:       p.config.Labels,
		})
		if err != nil {
			p.selector.RecordFailure(candidate.Name, err)
			lastErr = fmt.Errorf("%s: %w", candidate.Name, err)
			continue
		}

		p.selector.RecordSuccess(candidate.Name)
		return node, candidate.Name, nil
	}
}

// buildCandidates creates provider candidates from configuration.
func (p *Pool) buildCandidates() []ProviderCandidate {
	candidates := make([]ProviderCandidate, len(p.providers))
	for i, pc := range p.providers {
		instanceType := pc.InstanceType
		if instanceType == "" {
			instanceType = provider.ResolveInstanceType(p.config.InstanceType, pc.Name)
		}

		candidates[i] = ProviderCandidate{
			Provider:     pc.Provider,
			Name:         pc.Name,
			Priority:     pc.Priority,
			Weight:       pc.Weight,
			Regions:      pc.Regions,
			InstanceType: instanceType,
		}
	}
	return candidates
}

// ScaleDown removes nodes from the pool.
func (p *Pool) ScaleDown(ctx context.Context, count int) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	currentCount := len(p.nodes)
	removable := currentCount - p.config.MinNodes
	if count > removable {
		count = removable
	}
	if count <= 0 {
		return fmt.Errorf("pool at minimum capacity (%d nodes)", p.config.MinNodes)
	}
	if time.Since(p.lastScale) < p.config.CooldownPeriod {
		return fmt.Errorf("cooldown period not elapsed")
	}

	toRemove := p.selectForRemoval(count)

	for _, nodeID := range toRemove {
		mn := p.nodes[nodeID]
		prov := p.getProvider(mn.ProviderName)
		if prov == nil {
			return fmt.Errorf("provider %s not found for node %s", mn.ProviderName, nodeID)
		}
		if err := prov.Terminate(ctx, nodeID); err != nil {
			return fmt.Errorf("failed to terminate node %s: %w", nodeID, err)
		}
		delete(p.nodes, nodeID)
	}

	p.lastScale = time.Now()
	return nil
}

// getProvider returns the provider by name.
func (p *Pool) getProvider(name string) provider.Provider {
	for _, pc := range p.providers {
		if pc.Name == name {
			return pc.Provider
		}
	}
	return nil
}

// selectForRemoval picks nodes to remove, preferring cordoned nodes.
func (p *Pool) selectForRemoval(count int) []string {
	var cordoned, healthy []string

	for id, mn := range p.nodes {
		if mn.Cordoned {
			cordoned = append(cordoned, id)
		} else {
			healthy = append(healthy, id)
		}
	}

	var result []string
	for _, id := range cordoned {
		if len(result) >= count {
			break
		}
		result = append(result, id)
	}
	for _, id := range healthy {
		if len(result) >= count {
			break
		}
		result = append(result, id)
	}

	return result
}

// Cordon marks a node as unschedulable (no new workloads).
func (p *Pool) Cordon(nodeID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	mn, ok := p.nodes[nodeID]
	if !ok {
		return fmt.Errorf("node %s not found in pool", nodeID)
	}
	mn.Cordoned = true
	return nil
}

// Uncordon marks a node as schedulable again.
func (p *Pool) Uncordon(nodeID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	mn, ok := p.nodes[nodeID]
	if !ok {
		return fmt.Errorf("node %s not found in pool", nodeID)
	}
	mn.Cordoned = false
	return nil
}

// ReplaceNode terminates an unhealthy node and provisions a replacement.
// Currently uses fallback behavior (tries all providers). Future enhancement:
// add ReplacementStrategy config to prefer same provider for stateful workloads
// that need storage locality (e.g., training with checkpoints).
func (p *Pool) ReplaceNode(ctx context.Context, nodeID string) (*provider.Node, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	mn, ok := p.nodes[nodeID]
	if !ok {
		return nil, fmt.Errorf("node %s not found in pool", nodeID)
	}

	prov := p.getProvider(mn.ProviderName)
	if prov == nil {
		return nil, fmt.Errorf("provider %s not found for node %s", mn.ProviderName, nodeID)
	}

	if err := prov.Terminate(ctx, nodeID); err != nil {
		return nil, fmt.Errorf("failed to terminate node: %w", err)
	}
	delete(p.nodes, nodeID)

	node, providerName, err := p.provisionWithFallback(ctx, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to provision replacement: %w", err)
	}

	p.nodes[node.ID] = &ManagedNode{
		Node:          node,
		Pool:          p.config.Name,
		ProviderName:  providerName,
		ProvisionedAt: time.Now(),
	}

	return node, nil
}

// RecordHealthFailure increments the health failure count for a node.
func (p *Pool) RecordHealthFailure(nodeID string) (shouldReplace bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	mn, ok := p.nodes[nodeID]
	if !ok {
		return false
	}

	mn.HealthFailures++
	mn.LastHealthCheck = time.Now()

	return p.config.AutoReplace && mn.HealthFailures >= p.config.UnhealthyThreshold
}

// RecordHealthSuccess resets the health failure count for a node.
func (p *Pool) RecordHealthSuccess(nodeID string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if mn, ok := p.nodes[nodeID]; ok {
		mn.HealthFailures = 0
		mn.LastHealthCheck = time.Now()
	}
}

// Nodes returns all nodes in the pool.
func (p *Pool) Nodes() []*ManagedNode {
	p.mu.RLock()
	defer p.mu.RUnlock()

	nodes := make([]*ManagedNode, 0, len(p.nodes))
	for _, mn := range p.nodes {
		nodes = append(nodes, mn)
	}
	return nodes
}
