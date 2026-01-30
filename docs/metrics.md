# Metrics and monitoring

Navarch collects metrics from GPU nodes to enable autoscaling and health monitoring.

## Metrics collection

### What is collected

Every heartbeat (5-30 seconds) includes:

**Node-level metrics:**
- CPU usage percentage
- Memory usage percentage
- Timestamp

**Per-GPU metrics:**
- GPU index
- Utilization percentage (0-100)
- Temperature in Celsius
- Power usage in watts
- Memory used in bytes

**Health status:**
- Boot check results
- GPU communication status
- Health event detection (XID errors, thermal, ECC)

### Collection flow

```
┌─────────────┐     Heartbeat      ┌──────────────┐
│ Node Agent  │ ───────────────────>│ Control Plane│
│             │   (every 5-30s)     │              │
│ - Query GPU │                     │ - Store      │
│ - Collect   │                     │ - Aggregate  │
│ - Check     │                     │ - Autoscale  │
└─────────────┘                     └──────────────┘
```

### Node-side collection

The node daemon uses the `metrics.Collector` to gather system and GPU metrics.

**System metrics** are collected from `/proc` filesystem (Linux):
- **CPU usage**: Calculated from `/proc/stat` using delta between consecutive reads
- **Memory usage**: Read from `/proc/meminfo` using `MemTotal` and `MemAvailable`

**GPU metrics** are collected via the GPU manager interface:
- Queries GPU temperature, power, utilization, and memory
- Collects health events (XID errors, thermal warnings, ECC errors)
- Uses injectable GPU manager for testing/development

**Code location**: `pkg/node/metrics/`

```go
// Create a metrics collector
collector := metrics.NewCollector(gpuManager, nil)

// Collect all metrics
nodeMetrics, err := collector.Collect(ctx)
// Returns: CpuUsagePercent, MemoryUsagePercent, GpuMetrics[]
```

**Custom system reader**: For non-Linux systems or testing, implement `SystemMetricsReader`:

```go
type SystemMetricsReader interface {
    ReadCPUUsage(ctx context.Context) (float64, error)
    ReadMemoryUsage(ctx context.Context) (float64, error)
}

// Use custom reader
customReader := &MyCustomReader{}
collector := metrics.NewCollector(gpuManager, customReader)
```

### Storage and retention

Metrics are stored in-memory per node:
- Up to 100 samples per node
- Oldest samples automatically pruned
- Query window: last 5 minutes (default)

For production deployments requiring longer retention, implement a custom database backend.

## Metrics aggregation

### Pool-level aggregation

The control plane aggregates metrics by pool for autoscaling decisions.

**Current utilization:**
Average GPU utilization across all GPUs in the pool.

Example: Pool has 2 nodes with 8 GPUs each (16 total GPUs):
- Node 1 GPUs: 80%, 90%, 75%, 85%, 70%, 80%, 85%, 75%
- Node 2 GPUs: 60%, 70%, 65%, 55%, 70%, 65%, 60%, 75%

Pool utilization = (80+90+75+85+70+80+85+75+60+70+65+55+70+65+60+75) / 16 = 71.25%

**Utilization history:**
Per-node average utilization for the last 5 minutes. Used for trend analysis by predictive autoscalers.

### Pool filtering

Nodes are assigned to pools via labels:

```yaml
pools:
  training:
    labels:
      pool: training
      team: ml-research
```

The `pool` label is automatically set and used for metrics aggregation. Additional labels are for organization and filtering.

## Autoscaling metrics

Different autoscaler types use different metrics.

### Reactive autoscaler

Uses current GPU utilization:

```yaml
autoscaling:
  type: reactive
  scale_up_at: 75    # Scale up when utilization > 75%
  scale_down_at: 25  # Scale down when utilization < 25%
```

**Evaluation:** Every 30 seconds (configurable via `autoscale_interval`)

**Example:**
- Current: 3 nodes, 85% GPU utilization
- Recommendation: Scale up to 4 nodes (utilization > 75%)
- After cooldown: Actually provision 4th node

### Queue-based autoscaler

Uses job queue depth:

```yaml
autoscaling:
  type: queue
  jobs_per_node: 10
```

**Requires:** External scheduler integration via `MetricsSource` interface.

**Example:**
- Current: 5 nodes, 73 jobs in queue
- Calculation: ceil(73 / 10) = 8 nodes needed
- Recommendation: Scale up to 8 nodes

### Scheduled autoscaler

Does not use metrics. Scales based on time:

```yaml
autoscaling:
  type: scheduled
  schedule:
    - days: [monday, tuesday, wednesday, thursday, friday]
      start: 9
      end: 18
      min_nodes: 10
      max_nodes: 50
```

**Evaluation:** Checks current time and applies appropriate limits.

### Predictive autoscaler

Uses utilization history for trend analysis:

```yaml
autoscaling:
  type: predictive
  lookback_window: 10  # Samples to analyze
  growth_factor: 1.2   # Proactive scaling multiplier
```

Analyzes recent utilization trend and scales preemptively.

### Composite autoscaler

Combines multiple metrics:

```yaml
autoscaling:
  type: composite
  mode: max  # Take maximum recommendation
  autoscalers:
    - type: reactive
      scale_up_at: 80
      scale_down_at: 20
    - type: queue
      jobs_per_node: 10
```

**Use case:** Scale based on both GPU utilization and job queue depth, whichever demands more capacity.

## Integrating external schedulers

To provide queue depth and pending job metrics, implement the `MetricsSource` interface.

### Interface

```go
type MetricsSource interface {
    GetPoolMetrics(ctx context.Context, poolName string) (*PoolMetrics, error)
}

type PoolMetrics struct {
    Utilization        float64   // GPU utilization (provided by Navarch)
    PendingJobs        int       // Jobs waiting to start
    QueueDepth         int       // Pending + running jobs
    UtilizationHistory []float64 // Historical utilization
}
```

### Example: Kubernetes integration

```go
package main

import (
    "context"
    "github.com/NavarchProject/navarch/pkg/controlplane"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/client-go/kubernetes"
)

type KubernetesMetrics struct {
    clientset *kubernetes.Clientset
    dbMetrics *controlplane.DBMetricsSource
}

func (k *KubernetesMetrics) GetPoolMetrics(ctx context.Context, poolName string) (*controlplane.PoolMetrics, error) {
    // Get GPU utilization from Navarch's built-in metrics
    baseMetrics, err := k.dbMetrics.GetPoolMetrics(ctx, poolName)
    if err != nil {
        return nil, err
    }
    
    // Query Kubernetes for pods with pool label
    pods, err := k.clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{
        LabelSelector: "pool=" + poolName,
    })
    if err != nil {
        return nil, err
    }
    
    // Count pending and running pods
    var pending, running int
    for _, pod := range pods.Items {
        if pod.Status.Phase == "Pending" {
            pending++
        } else if pod.Status.Phase == "Running" {
            running++
        }
    }
    
    // Combine metrics
    baseMetrics.PendingJobs = pending
    baseMetrics.QueueDepth = pending + running
    
    return baseMetrics, nil
}
```

Then use it:

```go
k8sMetrics := &KubernetesMetrics{
    clientset: clientset,
    dbMetrics: controlplane.NewDBMetricsSource(database, logger),
}

poolManager := controlplane.NewPoolManager(cfg, k8sMetrics, logger)
```

### Example: Slurm integration

```go
type SlurmMetrics struct {
    slurmHost string
    dbMetrics *controlplane.DBMetricsSource
}

func (s *SlurmMetrics) GetPoolMetrics(ctx context.Context, poolName string) (*controlplane.PoolMetrics, error) {
    baseMetrics, _ := s.dbMetrics.GetPoolMetrics(ctx, poolName)
    
    // Query Slurm via scontrol or sacct
    output, _ := exec.CommandContext(ctx, "squeue", 
        "-h", "-p", poolName, "-t", "PENDING", "-o", "%i").Output()
    
    pending := len(strings.Split(string(output), "\n")) - 1
    
    output, _ = exec.CommandContext(ctx, "squeue",
        "-h", "-p", poolName, "-t", "RUNNING", "-o", "%i").Output()
    
    running := len(strings.Split(string(output), "\n")) - 1
    
    baseMetrics.PendingJobs = pending
    baseMetrics.QueueDepth = pending + running
    
    return baseMetrics, nil
}
```

## Monitoring and observability

### Prometheus metrics

Future work: Expose metrics in Prometheus format at `/metrics` endpoint.

Planned metrics:
- `navarch_pool_nodes_total{pool="name",status="healthy|unhealthy"}`
- `navarch_pool_gpu_utilization{pool="name"}`
- `navarch_pool_autoscaler_recommendations{pool="name"}`
- `navarch_pool_scaling_events_total{pool="name",direction="up|down"}`

### Structured logging

All components emit structured JSON logs with:
- `level`: INFO, WARN, ERROR
- `msg`: Human-readable message
- `time`: RFC3339 timestamp
- Context fields (pool, node_id, error, etc.)

Example:

```json
{
  "time": "2026-01-19T22:00:15Z",
  "level": "INFO",
  "msg": "scaling up",
  "pool": "training",
  "from": 5,
  "to": 8,
  "reason": "utilization 87.3% > 75.0% threshold"
}
```

### Health endpoints

**Liveness probe:** `GET /healthz`
- Returns 200 if control plane is running
- Use for container health checks

**Readiness probe:** `GET /readyz`
- Returns 200 if database is accessible
- Returns 503 if not ready
- Use for load balancer health checks

## Metrics API

Query metrics via gRPC API (future work):

```protobuf
service MetricsService {
  rpc GetNodeMetrics(GetNodeMetricsRequest) returns (NodeMetricsResponse);
  rpc GetPoolMetrics(GetPoolMetricsRequest) returns (PoolMetricsResponse);
  rpc QueryMetrics(QueryMetricsRequest) returns (QueryMetricsResponse);
}
```

Current workaround: Query the in-memory database directly via the CLI or custom tooling.

## Best practices

### Heartbeat intervals

**Fast (5s):** Development and testing  
**Standard (30s):** Production with reactive autoscaling  
**Slow (60s):** Large clusters (1000+ nodes) to reduce control plane load

### Autoscale intervals

**Fast (10s):** Development and testing  
**Standard (30s):** Production with quick response times  
**Slow (60-120s):** Cost-sensitive workloads to avoid over-provisioning

### Cooldown periods

**Short (2-3m):** Development and testing  
**Standard (5m):** Production to prevent oscillation  
**Long (10-15m):** Large nodes (expensive to provision/terminate)

### Metrics retention

The default 100 samples per node retains approximately:
- 8 minutes at 5s heartbeat interval
- 50 minutes at 30s heartbeat interval

For longer retention, implement custom database backend or export to time-series database.

## Troubleshooting

### No metrics for pool

**Symptom:** Autoscaler always reports 0% utilization.

**Causes:**
1. Nodes not registered with pool label
2. Nodes not sending metrics in heartbeats
3. Pool name mismatch

**Debug:**
```bash
# Check node labels
navarch get node-1 --output json | jq .metadata.labels

# Verify pool name in config
grep "pools:" config.yaml -A 5

# Check control plane logs
grep "failed to get metrics" logs.json
```

### Autoscaler not scaling

**Symptom:** Utilization high but no scale up.

**Causes:**
1. Cooldown period active
2. At max nodes limit
3. Provider provisioning failures

**Debug:**
```bash
# Check pool status
navarch list

# Look for scaling events
grep "scaling" logs.json | tail -20

# Check cooldown
grep "cooldown active" logs.json
```

### High control plane memory

**Symptom:** Control plane memory usage growing.

**Causes:**
1. Metrics retention with many nodes (100 samples × number of nodes)
2. Memory leak (report bug)

**Solutions:**
1. Reduce metrics retention (requires code change)
2. Restart control plane periodically
3. Implement external database backend

