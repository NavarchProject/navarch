package controlplane

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/NavarchProject/navarch/pkg/clock"
	"github.com/NavarchProject/navarch/pkg/controlplane/db"
	pb "github.com/NavarchProject/navarch/proto"
)

// HeartbeatMonitor detects nodes that have stopped sending heartbeats and
// marks them as unhealthy. This handles the case where a node dies without
// reporting unhealthy status.
type HeartbeatMonitor struct {
	db     db.DB
	clock  clock.Clock
	logger *slog.Logger
	config HeartbeatMonitorConfig

	mu       sync.Mutex
	started  bool
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	observer NodeHealthObserver
}

// HeartbeatMonitorConfig configures the heartbeat monitor behavior.
type HeartbeatMonitorConfig struct {
	// HeartbeatTimeout is how long to wait without a heartbeat before marking
	// a node as unhealthy. Should be at least 2-3x the heartbeat interval.
	// Default: 2 minutes.
	HeartbeatTimeout time.Duration

	// CheckInterval is how often to scan for stale heartbeats.
	// Default: 30 seconds.
	CheckInterval time.Duration

	// Clock is the clock to use for time operations. If nil, uses real time.
	Clock clock.Clock
}

// DefaultHeartbeatMonitorConfig returns sensible defaults.
func DefaultHeartbeatMonitorConfig() HeartbeatMonitorConfig {
	return HeartbeatMonitorConfig{
		HeartbeatTimeout: 2 * time.Minute,
		CheckInterval:    30 * time.Second,
	}
}

// NewHeartbeatMonitor creates a new heartbeat monitor.
func NewHeartbeatMonitor(database db.DB, config HeartbeatMonitorConfig, logger *slog.Logger) *HeartbeatMonitor {
	if logger == nil {
		logger = slog.Default()
	}
	if config.HeartbeatTimeout == 0 {
		config.HeartbeatTimeout = DefaultHeartbeatMonitorConfig().HeartbeatTimeout
	}
	if config.CheckInterval == 0 {
		config.CheckInterval = DefaultHeartbeatMonitorConfig().CheckInterval
	}

	clk := config.Clock
	if clk == nil {
		clk = clock.Real()
	}

	return &HeartbeatMonitor{
		db:     database,
		clock:  clk,
		logger: logger.With(slog.String("component", "heartbeat-monitor")),
		config: config,
	}
}

// SetHealthObserver sets the observer to notify when nodes become unhealthy.
func (m *HeartbeatMonitor) SetHealthObserver(observer NodeHealthObserver) {
	m.observer = observer
}

// Start begins monitoring heartbeats in the background.
func (m *HeartbeatMonitor) Start(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return
	}

	ctx, m.cancel = context.WithCancel(ctx)
	m.started = true

	m.wg.Add(1)
	go m.monitorLoop(ctx)

	m.logger.Info("heartbeat monitor started",
		slog.Duration("timeout", m.config.HeartbeatTimeout),
		slog.Duration("check_interval", m.config.CheckInterval),
	)
}

// Stop stops the heartbeat monitor.
func (m *HeartbeatMonitor) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		return
	}

	m.cancel()
	m.wg.Wait()
	m.started = false

	m.logger.Info("heartbeat monitor stopped")
}

func (m *HeartbeatMonitor) monitorLoop(ctx context.Context) {
	defer m.wg.Done()

	ticker := m.clock.NewTicker(m.config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C():
			m.checkHeartbeats(ctx)
		}
	}
}

func (m *HeartbeatMonitor) checkHeartbeats(ctx context.Context) {
	nodes, err := m.db.ListNodes(ctx)
	if err != nil {
		m.logger.Error("failed to list nodes for heartbeat check",
			slog.String("error", err.Error()),
		)
		return
	}

	now := m.clock.Now()
	timeout := m.config.HeartbeatTimeout

	for _, node := range nodes {
		// Skip nodes that are already unhealthy or terminated
		if node.Status == pb.NodeStatus_NODE_STATUS_UNHEALTHY ||
			node.Status == pb.NodeStatus_NODE_STATUS_TERMINATED {
			continue
		}

		// Skip nodes that have never sent a heartbeat (still registering)
		if node.LastHeartbeat.IsZero() {
			continue
		}

		age := now.Sub(node.LastHeartbeat)
		if age > timeout {
			m.markNodeUnhealthy(ctx, node.NodeID, age)
		}
	}
}

func (m *HeartbeatMonitor) markNodeUnhealthy(ctx context.Context, nodeID string, age time.Duration) {
	m.logger.Warn("node heartbeat timeout",
		slog.String("node_id", nodeID),
		slog.Duration("last_heartbeat_age", age),
		slog.Duration("timeout", m.config.HeartbeatTimeout),
	)

	err := m.db.UpdateNodeStatus(ctx, nodeID, pb.NodeStatus_NODE_STATUS_UNHEALTHY)
	if err != nil {
		m.logger.Error("failed to mark node unhealthy",
			slog.String("node_id", nodeID),
			slog.String("error", err.Error()),
		)
		return
	}

	// Notify observer if set
	if m.observer != nil {
		go m.observer.OnNodeUnhealthy(ctx, nodeID)
	}
}
