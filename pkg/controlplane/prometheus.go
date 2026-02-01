package controlplane

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/NavarchProject/navarch/pkg/controlplane/db"
	pb "github.com/NavarchProject/navarch/proto"
)

// PrometheusMetrics provides Prometheus metrics for the control plane.
type PrometheusMetrics struct {
	db db.DB

	// Fleet metrics
	nodesTotal *prometheus.GaugeVec
	gpusTotal  *prometheus.GaugeVec

	// Health metrics
	healthEventsTotal *prometheus.CounterVec
	nodeHealthStatus  *prometheus.GaugeVec

	// Autoscaler metrics
	poolTargetNodes  *prometheus.GaugeVec
	poolCurrentNodes *prometheus.GaugeVec
	scalingEvents    *prometheus.CounterVec
}

// NewPrometheusMetrics creates a new PrometheusMetrics instance.
func NewPrometheusMetrics(database db.DB) *PrometheusMetrics {
	pm := &PrometheusMetrics{
		db: database,
		nodesTotal: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "navarch_nodes_total",
				Help: "Total number of nodes by status",
			},
			[]string{"status"},
		),
		gpusTotal: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "navarch_gpus_total",
				Help: "Total number of GPUs by provider",
			},
			[]string{"provider"},
		),
		healthEventsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "navarch_health_events_total",
				Help: "Total number of health events by type",
			},
			[]string{"type"},
		),
		nodeHealthStatus: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "navarch_node_health_status",
				Help: "Health status of each node (1=healthy, 0.5=degraded, 0=unhealthy)",
			},
			[]string{"node_id", "status"},
		),
		poolTargetNodes: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "navarch_pool_target_nodes",
				Help: "Target number of nodes for each pool",
			},
			[]string{"pool"},
		),
		poolCurrentNodes: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "navarch_pool_current_nodes",
				Help: "Current number of nodes in each pool",
			},
			[]string{"pool"},
		),
		scalingEvents: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "navarch_scaling_events_total",
				Help: "Total number of scaling events by pool and direction",
			},
			[]string{"pool", "direction"},
		),
	}

	return pm
}

// Describe implements prometheus.Collector.
func (pm *PrometheusMetrics) Describe(ch chan<- *prometheus.Desc) {
	pm.nodesTotal.Describe(ch)
	pm.gpusTotal.Describe(ch)
	pm.healthEventsTotal.Describe(ch)
	pm.nodeHealthStatus.Describe(ch)
	pm.poolTargetNodes.Describe(ch)
	pm.poolCurrentNodes.Describe(ch)
	pm.scalingEvents.Describe(ch)
}

// Collect implements prometheus.Collector and updates metrics from the database.
func (pm *PrometheusMetrics) Collect(ch chan<- prometheus.Metric) {
	ctx := context.Background()
	pm.collectNodeMetrics(ctx)
	pm.collectHealthMetrics(ctx)

	pm.nodesTotal.Collect(ch)
	pm.gpusTotal.Collect(ch)
	pm.healthEventsTotal.Collect(ch)
	pm.nodeHealthStatus.Collect(ch)
	pm.poolTargetNodes.Collect(ch)
	pm.poolCurrentNodes.Collect(ch)
	pm.scalingEvents.Collect(ch)
}

func (pm *PrometheusMetrics) collectNodeMetrics(ctx context.Context) {
	nodes, err := pm.db.ListNodes(ctx)
	if err != nil {
		return
	}

	statusCounts := make(map[string]float64)
	providerGPUCounts := make(map[string]float64)
	poolNodeCounts := make(map[string]float64)

	for _, node := range nodes {
		status := nodeStatusString(node.Status)
		statusCounts[status]++

		gpuCount := float64(len(node.GPUs))
		providerGPUCounts[node.Provider] += gpuCount

		if node.Metadata != nil && node.Metadata.Labels != nil {
			if poolName, ok := node.Metadata.Labels["pool"]; ok {
				poolNodeCounts[poolName]++
			}
		}
	}

	pm.nodesTotal.Reset()
	for status, count := range statusCounts {
		pm.nodesTotal.WithLabelValues(status).Set(count)
	}

	pm.gpusTotal.Reset()
	for provider, count := range providerGPUCounts {
		if provider == "" {
			provider = "unknown"
		}
		pm.gpusTotal.WithLabelValues(provider).Set(count)
	}

	pm.poolCurrentNodes.Reset()
	for pool, count := range poolNodeCounts {
		pm.poolCurrentNodes.WithLabelValues(pool).Set(count)
	}
}

func (pm *PrometheusMetrics) collectHealthMetrics(ctx context.Context) {
	nodes, err := pm.db.ListNodes(ctx)
	if err != nil {
		return
	}

	pm.nodeHealthStatus.Reset()
	for _, node := range nodes {
		healthStatus := healthStatusString(node.HealthStatus)
		healthValue := healthStatusValue(node.HealthStatus)
		pm.nodeHealthStatus.WithLabelValues(node.NodeID, healthStatus).Set(healthValue)
	}
}

// RecordHealthEvent increments the health event counter.
func (pm *PrometheusMetrics) RecordHealthEvent(eventType string) {
	pm.healthEventsTotal.WithLabelValues(eventType).Inc()
}

// RecordScalingEvent increments the scaling event counter.
func (pm *PrometheusMetrics) RecordScalingEvent(pool, direction string) {
	pm.scalingEvents.WithLabelValues(pool, direction).Inc()
}

// SetPoolTargetNodes sets the target node count for a pool.
func (pm *PrometheusMetrics) SetPoolTargetNodes(pool string, count int) {
	pm.poolTargetNodes.WithLabelValues(pool).Set(float64(count))
}

func nodeStatusString(status pb.NodeStatus) string {
	switch status {
	case pb.NodeStatus_NODE_STATUS_ACTIVE:
		return "active"
	case pb.NodeStatus_NODE_STATUS_CORDONED:
		return "cordoned"
	case pb.NodeStatus_NODE_STATUS_DRAINING:
		return "draining"
	case pb.NodeStatus_NODE_STATUS_UNHEALTHY:
		return "unhealthy"
	case pb.NodeStatus_NODE_STATUS_TERMINATED:
		return "terminated"
	default:
		return "unknown"
	}
}

func healthStatusString(status pb.HealthStatus) string {
	switch status {
	case pb.HealthStatus_HEALTH_STATUS_HEALTHY:
		return "healthy"
	case pb.HealthStatus_HEALTH_STATUS_DEGRADED:
		return "degraded"
	case pb.HealthStatus_HEALTH_STATUS_UNHEALTHY:
		return "unhealthy"
	default:
		return "unknown"
	}
}

func healthStatusValue(status pb.HealthStatus) float64 {
	switch status {
	case pb.HealthStatus_HEALTH_STATUS_HEALTHY:
		return 1.0
	case pb.HealthStatus_HEALTH_STATUS_DEGRADED:
		return 0.5
	case pb.HealthStatus_HEALTH_STATUS_UNHEALTHY:
		return 0.0
	default:
		return 0.0
	}
}
