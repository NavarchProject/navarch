package controlplane

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/NavarchProject/navarch/pkg/pool"
	"github.com/NavarchProject/navarch/pkg/provider"
)

// PoolManager orchestrates multiple GPU node pools, running autoscalers
// and acting on scaling recommendations. It integrates with InstanceManager
// to maintain visibility into instance lifecycle from provisioning through
// termination.
type PoolManager struct {
	pools           map[string]*managedPool
	mu              sync.RWMutex
	logger          *slog.Logger
	interval        time.Duration
	metrics         MetricsSource
	instanceManager *InstanceManager
}

type managedPool struct {
	pool       *pool.Pool
	autoscaler pool.Autoscaler
	cancel     context.CancelFunc
}

// MetricsSource provides pool metrics for autoscaler decisions.
// Implement this interface to connect your workload system.
type MetricsSource interface {
	GetPoolMetrics(ctx context.Context, poolName string) (*PoolMetrics, error)
}

// PoolMetrics contains current metrics for a pool.
type PoolMetrics struct {
	Utilization        float64
	PendingJobs        int
	QueueDepth         int
	UtilizationHistory []float64
}

// PoolManagerConfig configures the pool manager.
type PoolManagerConfig struct {
	EvaluationInterval time.Duration // How often to run autoscaler (default: 30s)
}

// NewPoolManager creates a new pool manager.
// The instanceManager parameter is optional; if nil, instance lifecycle tracking is disabled.
func NewPoolManager(cfg PoolManagerConfig, metrics MetricsSource, instanceManager *InstanceManager, logger *slog.Logger) *PoolManager {
	if logger == nil {
		logger = slog.Default()
	}
	interval := cfg.EvaluationInterval
	if interval == 0 {
		interval = 30 * time.Second
	}
	return &PoolManager{
		pools:           make(map[string]*managedPool),
		logger:          logger,
		interval:        interval,
		metrics:         metrics,
		instanceManager: instanceManager,
	}
}

// AddPool registers a pool with its autoscaler.
func (pm *PoolManager) AddPool(p *pool.Pool, autoscaler pool.Autoscaler) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	name := p.Config().Name
	if _, exists := pm.pools[name]; exists {
		return fmt.Errorf("pool %q already exists", name)
	}

	pm.pools[name] = &managedPool{
		pool:       p,
		autoscaler: autoscaler,
	}
	pm.logger.Info("pool registered", slog.String("pool", name))
	return nil
}

// RemovePool unregisters a pool and stops its autoscaler.
func (pm *PoolManager) RemovePool(name string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	mp, exists := pm.pools[name]
	if !exists {
		return fmt.Errorf("pool %q not found", name)
	}
	if mp.cancel != nil {
		mp.cancel()
	}
	delete(pm.pools, name)
	pm.logger.Info("pool removed", slog.String("pool", name))
	return nil
}

// Start begins the autoscaler evaluation loop for all pools.
func (pm *PoolManager) Start(ctx context.Context) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	for name, mp := range pm.pools {
		loopCtx, cancel := context.WithCancel(ctx)
		mp.cancel = cancel
		go pm.runAutoscalerLoop(loopCtx, name, mp)
	}
	pm.logger.Info("pool manager started", slog.Int("pools", len(pm.pools)))
}

// Stop halts all autoscaler loops.
func (pm *PoolManager) Stop() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	for _, mp := range pm.pools {
		if mp.cancel != nil {
			mp.cancel()
		}
	}
	pm.logger.Info("pool manager stopped")
}

// GetPool returns a pool by name.
func (pm *PoolManager) GetPool(name string) (*pool.Pool, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	mp, ok := pm.pools[name]
	if !ok {
		return nil, false
	}
	return mp.pool, true
}

// ListPools returns all pool names.
func (pm *PoolManager) ListPools() []string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	names := make([]string, 0, len(pm.pools))
	for name := range pm.pools {
		names = append(names, name)
	}
	return names
}

// GetPoolStatus returns the status of a pool.
func (pm *PoolManager) GetPoolStatus(name string) (pool.Status, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	mp, ok := pm.pools[name]
	if !ok {
		return pool.Status{}, fmt.Errorf("pool %q not found", name)
	}
	return mp.pool.Status(), nil
}

func (pm *PoolManager) runAutoscalerLoop(ctx context.Context, name string, mp *managedPool) {
	ticker := time.NewTicker(pm.interval)
	defer ticker.Stop()

	pm.logger.Info("autoscaler loop started",
		slog.String("pool", name),
		slog.Duration("interval", pm.interval),
	)

	for {
		select {
		case <-ctx.Done():
			pm.logger.Info("autoscaler loop stopped", slog.String("pool", name))
			return
		case <-ticker.C:
			pm.evaluate(ctx, name, mp)
		}
	}
}

func (pm *PoolManager) evaluate(ctx context.Context, name string, mp *managedPool) {
	if mp.autoscaler == nil {
		return
	}

	state, err := pm.buildPoolState(ctx, name, mp)
	if err != nil {
		pm.logger.Error("failed to build pool state",
			slog.String("pool", name),
			slog.String("error", err.Error()),
		)
		return
	}

	rec, err := mp.autoscaler.Recommend(ctx, state)
	if err != nil {
		pm.logger.Error("autoscaler recommendation failed",
			slog.String("pool", name),
			slog.String("error", err.Error()),
		)
		return
	}

	pm.actOnRecommendation(ctx, name, mp, state.CurrentNodes, rec)
}

func (pm *PoolManager) buildPoolState(ctx context.Context, name string, mp *managedPool) (pool.PoolState, error) {
	cfg := mp.pool.Config()
	status := mp.pool.Status()

	state := pool.PoolState{
		Name:           name,
		CurrentNodes:   status.TotalNodes,
		HealthyNodes:   status.HealthyNodes,
		MinNodes:       cfg.MinNodes,
		MaxNodes:       cfg.MaxNodes,
		CooldownPeriod: cfg.CooldownPeriod,
		TimeOfDay:      time.Now(),
		DayOfWeek:      time.Now().Weekday(),
	}

	if pm.metrics != nil {
		metrics, err := pm.metrics.GetPoolMetrics(ctx, name)
		if err != nil {
			pm.logger.Warn("failed to get pool metrics, using defaults",
				slog.String("pool", name),
				slog.String("error", err.Error()),
			)
		} else if metrics != nil {
			state.Utilization = metrics.Utilization
			state.PendingJobs = metrics.PendingJobs
			state.QueueDepth = metrics.QueueDepth
			state.UtilizationHistory = metrics.UtilizationHistory
		}
	}

	return state, nil
}

func (pm *PoolManager) actOnRecommendation(ctx context.Context, name string, mp *managedPool, current int, rec pool.ScaleRecommendation) {
	if rec.TargetNodes == current {
		pm.logger.Debug("no scaling action needed",
			slog.String("pool", name),
			slog.Int("nodes", current),
			slog.String("reason", rec.Reason),
		)
		return
	}

	if rec.TargetNodes > current {
		count := rec.TargetNodes - current
		pm.logger.Info("scaling up",
			slog.String("pool", name),
			slog.Int("from", current),
			slog.Int("to", rec.TargetNodes),
			slog.Int("adding", count),
			slog.String("reason", rec.Reason),
		)
		nodes, err := mp.pool.ScaleUp(ctx, count)
		if err != nil {
			pm.logger.Error("scale up failed",
				slog.String("pool", name),
				slog.String("error", err.Error()),
			)
			return
		}

		// Track provisioned instances
		pm.trackProvisionedInstances(ctx, name, mp, nodes)

		pm.logger.Info("scale up complete",
			slog.String("pool", name),
			slog.Int("provisioned", len(nodes)),
		)
	} else {
		count := current - rec.TargetNodes
		pm.logger.Info("scaling down",
			slog.String("pool", name),
			slog.Int("from", current),
			slog.Int("to", rec.TargetNodes),
			slog.Int("removing", count),
			slog.String("reason", rec.Reason),
		)

		// Get nodes before scale down to track terminations
		nodesBefore := mp.pool.Nodes()

		if err := mp.pool.ScaleDown(ctx, count); err != nil {
			pm.logger.Error("scale down failed",
				slog.String("pool", name),
				slog.String("error", err.Error()),
			)
			return
		}

		// Track terminated instances
		nodesAfter := mp.pool.Nodes()
		pm.trackTerminatedInstances(ctx, nodesBefore, nodesAfter)

		pm.logger.Info("scale down complete", slog.String("pool", name))
	}
}

// trackProvisionedInstances creates instance records for newly provisioned nodes.
func (pm *PoolManager) trackProvisionedInstances(ctx context.Context, poolName string, mp *managedPool, nodes []*provider.Node) {
	if pm.instanceManager == nil {
		return
	}

	cfg := mp.pool.Config()
	for _, node := range nodes {
		// Create instance record in PENDING_REGISTRATION state
		// (the instance is provisioned, waiting for the node agent to register)
		if err := pm.instanceManager.TrackProvisioning(ctx, node.ID, node.Provider, node.Region, node.Zone, node.InstanceType, poolName, cfg.Labels); err != nil {
			pm.logger.Warn("failed to track provisioning start",
				slog.String("instance_id", node.ID),
				slog.String("error", err.Error()),
			)
			continue
		}

		// Mark as pending registration since provisioning already succeeded
		if err := pm.instanceManager.TrackProvisioningComplete(ctx, node.ID); err != nil {
			pm.logger.Warn("failed to track provisioning complete",
				slog.String("instance_id", node.ID),
				slog.String("error", err.Error()),
			)
		}
	}
}

// trackTerminatedInstances marks instances as terminated that were removed during scale down.
func (pm *PoolManager) trackTerminatedInstances(ctx context.Context, before, after []*pool.ManagedNode) {
	if pm.instanceManager == nil {
		return
	}

	// Build a set of nodes that still exist
	remaining := make(map[string]bool)
	for _, node := range after {
		remaining[node.Node.ID] = true
	}

	// Find nodes that were removed
	for _, node := range before {
		if !remaining[node.Node.ID] {
			if err := pm.instanceManager.TrackTerminated(ctx, node.Node.ID); err != nil {
				pm.logger.Warn("failed to track instance termination",
					slog.String("instance_id", node.Node.ID),
					slog.String("error", err.Error()),
				)
			}
		}
	}
}

// ScalePool manually scales a pool to the target count.
func (pm *PoolManager) ScalePool(ctx context.Context, name string, target int) error {
	pm.mu.RLock()
	mp, ok := pm.pools[name]
	pm.mu.RUnlock()
	if !ok {
		return fmt.Errorf("pool %q not found", name)
	}

	status := mp.pool.Status()
	if target == status.TotalNodes {
		return nil
	}

	if target > status.TotalNodes {
		_, err := mp.pool.ScaleUp(ctx, target-status.TotalNodes)
		return err
	}
	return mp.pool.ScaleDown(ctx, status.TotalNodes-target)
}

// ProviderFactory creates providers by name.
type ProviderFactory func(name string, config map[string]any) (provider.Provider, error)
