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
	Name     string // Unique pool identifier
	Provider string // Cloud provider name: "lambda", "gcp", "aws"

	InstanceType string   // Instance type to provision (e.g., "gpu_8x_h100_sxm5")
	Region       string   // Cloud region (e.g., "us-west-2")
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

// Pool represents a managed group of GPU nodes.
type Pool struct {
	config    Config
	provider  provider.Provider
	mu        sync.RWMutex
	nodes     map[string]*ManagedNode
	lastScale time.Time
}

// ManagedNode tracks a node within a pool.
type ManagedNode struct {
	Node            *provider.Node // Underlying provider node
	Pool            string         // Name of the pool this node belongs to
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

// New creates a new pool with the given configuration.
func New(cfg Config, prov provider.Provider) (*Pool, error) {
	if cfg.Name == "" {
		return nil, fmt.Errorf("pool name is required")
	}
	if cfg.MinNodes < 0 {
		return nil, fmt.Errorf("min_nodes must be >= 0")
	}
	if cfg.MaxNodes < cfg.MinNodes {
		return nil, fmt.Errorf("max_nodes must be >= min_nodes")
	}
	if cfg.MaxNodes == 0 {
		return nil, fmt.Errorf("max_nodes must be > 0")
	}

	return &Pool{
		config:   cfg,
		provider: prov,
		nodes:    make(map[string]*ManagedNode),
	}, nil
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

// ScaleUp adds nodes to the pool.
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
		node, err := p.provider.Provision(ctx, provider.ProvisionRequest{
			Name:         fmt.Sprintf("%s-%d", p.config.Name, currentCount+i+1),
			InstanceType: p.config.InstanceType,
			Region:       p.config.Region,
			SSHKeyNames:  p.config.SSHKeyNames,
			Labels:       p.config.Labels,
		})
		if err != nil {
			// Return what we provisioned so far
			return nodes, fmt.Errorf("failed to provision node %d: %w", i+1, err)
		}

		p.nodes[node.ID] = &ManagedNode{
			Node:          node,
			Pool:          p.config.Name,
			ProvisionedAt: time.Now(),
		}
		nodes = append(nodes, node)
	}

	p.lastScale = time.Now()
	return nodes, nil
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
		if err := p.provider.Terminate(ctx, nodeID); err != nil {
			return fmt.Errorf("failed to terminate node %s: %w", nodeID, err)
		}
		delete(p.nodes, nodeID)
	}

	p.lastScale = time.Now()
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
func (p *Pool) ReplaceNode(ctx context.Context, nodeID string) (*provider.Node, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	mn, ok := p.nodes[nodeID]
	if !ok {
		return nil, fmt.Errorf("node %s not found in pool", nodeID)
	}

	if err := p.provider.Terminate(ctx, nodeID); err != nil {
		return nil, fmt.Errorf("failed to terminate node: %w", err)
	}
	delete(p.nodes, nodeID)

	node, err := p.provider.Provision(ctx, provider.ProvisionRequest{
		Name:         mn.Node.ID + "-replacement",
		InstanceType: p.config.InstanceType,
		Region:       p.config.Region,
		SSHKeyNames:  p.config.SSHKeyNames,
		Labels:       p.config.Labels,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to provision replacement: %w", err)
	}

	p.nodes[node.ID] = &ManagedNode{
		Node:          node,
		Pool:          p.config.Name,
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
