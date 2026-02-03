package controlplane

import (
	"context"
	"log/slog"
	"time"

	"github.com/NavarchProject/navarch/pkg/clock"
	"github.com/NavarchProject/navarch/pkg/controlplane/db"
	pb "github.com/NavarchProject/navarch/proto"
)

// DBMetricsSource implements MetricsSource by aggregating metrics from the database.
type DBMetricsSource struct {
	db     db.DB
	clock  clock.Clock
	logger *slog.Logger
}

// NewDBMetricsSource creates a metrics source that reads from the database.
func NewDBMetricsSource(database db.DB, logger *slog.Logger) *DBMetricsSource {
	return NewDBMetricsSourceWithClock(database, clock.Real(), logger)
}

// NewDBMetricsSourceWithClock creates a metrics source with a custom clock.
func NewDBMetricsSourceWithClock(database db.DB, clk clock.Clock, logger *slog.Logger) *DBMetricsSource {
	if logger == nil {
		logger = slog.Default()
	}
	if clk == nil {
		clk = clock.Real()
	}
	return &DBMetricsSource{
		db:     database,
		clock:  clk,
		logger: logger,
	}
}

// GetPoolMetrics aggregates metrics for all nodes in a pool.
func (m *DBMetricsSource) GetPoolMetrics(ctx context.Context, poolName string) (*PoolMetrics, error) {
	nodes, err := m.db.ListNodes(ctx)
	if err != nil {
		return nil, err
	}

	// Filter nodes by pool name (via labels) and collect their metrics
	var poolNodes []*db.NodeRecord
	for _, node := range nodes {
		if node.Metadata != nil && node.Metadata.Labels != nil {
			if node.Metadata.Labels["pool"] == poolName {
				poolNodes = append(poolNodes, node)
			}
		}
	}

	if len(poolNodes) == 0 {
		return &PoolMetrics{
			Utilization:        0,
			PendingJobs:        0,
			QueueDepth:         0,
			UtilizationHistory: []float64{},
		}, nil
	}

	// Aggregate GPU utilization across all nodes in the pool
	var totalUtilization float64
	var gpuCount int
	var utilizationHistory []float64

	for _, node := range poolNodes {
		// Get recent metrics (last 5 minutes)
		recentMetrics, err := m.db.GetRecentMetrics(ctx, node.NodeID, 5*time.Minute)
		if err != nil {
			m.logger.Warn("failed to get metrics for node",
				slog.String("node_id", node.NodeID),
				slog.String("error", err.Error()),
			)
			continue
		}

		// Use the most recent metric for current utilization
		if len(recentMetrics) > 0 {
			latest := recentMetrics[len(recentMetrics)-1]
			if latest.Metrics != nil && len(latest.Metrics.GpuMetrics) > 0 {
				for _, gpu := range latest.Metrics.GpuMetrics {
					totalUtilization += gpu.UtilizationPercent
					gpuCount++
				}
			}
		}

		// Collect historical data for trend analysis
		for _, record := range recentMetrics {
			if record.Metrics != nil && len(record.Metrics.GpuMetrics) > 0 {
				var nodeAvgUtil float64
				for _, gpu := range record.Metrics.GpuMetrics {
					nodeAvgUtil += gpu.UtilizationPercent
				}
				if len(record.Metrics.GpuMetrics) > 0 {
					nodeAvgUtil /= float64(len(record.Metrics.GpuMetrics))
					utilizationHistory = append(utilizationHistory, nodeAvgUtil)
				}
			}
		}
	}

	avgUtilization := 0.0
	if gpuCount > 0 {
		avgUtilization = totalUtilization / float64(gpuCount)
	}

	return &PoolMetrics{
		Utilization:        avgUtilization,
		PendingJobs:        0, // Not tracked yet - requires external scheduler integration
		QueueDepth:         0, // Not tracked yet - requires external scheduler integration
		UtilizationHistory: utilizationHistory,
	}, nil
}

// StoreMetrics is a helper to store metrics from a heartbeat.
func (m *DBMetricsSource) StoreMetrics(ctx context.Context, nodeID string, metrics *pb.NodeMetrics) error {
	return m.db.RecordMetrics(ctx, &db.MetricsRecord{
		NodeID:    nodeID,
		Timestamp: m.clock.Now(),
		Metrics:   metrics,
	})
}

// PoolNodeCounts holds node counts for a pool from the database.
type PoolNodeCounts struct {
	Total     int
	Healthy   int
	Unhealthy int
}

// GetPoolNodeCounts returns the count of nodes in a pool from the database.
// This counts all registered nodes with the pool label, regardless of how they were provisioned.
func (m *DBMetricsSource) GetPoolNodeCounts(ctx context.Context, poolName string) (PoolNodeCounts, error) {
	nodes, err := m.db.ListNodes(ctx)
	if err != nil {
		return PoolNodeCounts{}, err
	}

	var counts PoolNodeCounts
	for _, node := range nodes {
		// Check if node belongs to this pool via labels
		if node.Metadata != nil && node.Metadata.Labels != nil {
			if node.Metadata.Labels["pool"] == poolName {
				// Skip terminated nodes
				if node.Status == pb.NodeStatus_NODE_STATUS_TERMINATED {
					continue
				}
				counts.Total++
				if node.Status == pb.NodeStatus_NODE_STATUS_UNHEALTHY ||
					node.HealthStatus == pb.HealthStatus_HEALTH_STATUS_UNHEALTHY {
					counts.Unhealthy++
				} else {
					counts.Healthy++
				}
			}
		}
	}

	return counts, nil
}

// GetNodePool returns the pool name for a node by looking up its "pool" label.
// Returns empty string if the node doesn't exist or has no pool label.
func (m *DBMetricsSource) GetNodePool(ctx context.Context, nodeID string) (string, error) {
	nodes, err := m.db.ListNodes(ctx)
	if err != nil {
		return "", err
	}

	for _, node := range nodes {
		if node.NodeID == nodeID {
			if node.Metadata != nil && node.Metadata.Labels != nil {
				return node.Metadata.Labels["pool"], nil
			}
			return "", nil
		}
	}

	return "", nil
}

