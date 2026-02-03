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

	// Fleet metrics (pulled from DB on each scrape)
	nodesTotal *prometheus.GaugeVec
	gpusTotal  *prometheus.GaugeVec

	// Health metrics (pulled from DB on each scrape)
	nodeHealthStatus *prometheus.GaugeVec
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
		nodeHealthStatus: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "navarch_node_health_status",
				Help: "Health status of each node (1=healthy, 0.5=degraded, 0=unhealthy)",
			},
			[]string{"node_id", "status"},
		),
	}

	return pm
}

// Describe implements prometheus.Collector.
func (pm *PrometheusMetrics) Describe(ch chan<- *prometheus.Desc) {
	pm.nodesTotal.Describe(ch)
	pm.gpusTotal.Describe(ch)
	pm.nodeHealthStatus.Describe(ch)
}

// Collect implements prometheus.Collector and updates metrics from the database.
func (pm *PrometheusMetrics) Collect(ch chan<- prometheus.Metric) {
	ctx := context.Background()
	pm.collectNodeMetrics(ctx)
	pm.collectHealthMetrics(ctx)

	pm.nodesTotal.Collect(ch)
	pm.gpusTotal.Collect(ch)
	pm.nodeHealthStatus.Collect(ch)
}

func (pm *PrometheusMetrics) collectNodeMetrics(ctx context.Context) {
	nodes, err := pm.db.ListNodes(ctx)
	if err != nil {
		return
	}

	statusCounts := make(map[string]float64)
	providerGPUCounts := make(map[string]float64)

	for _, node := range nodes {
		status := nodeStatusString(node.Status)
		statusCounts[status]++

		gpuCount := float64(len(node.GPUs))
		providerGPUCounts[node.Provider] += gpuCount
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
