# Pool management

This guide covers how to configure and manage GPU node pools with Navarch, including autoscaling strategies and health-based replacement.

For detailed information on autoscaling strategies and metrics, see [architecture](architecture.md) and [metrics](metrics.md).

For the complete configuration reference, see the [configuration reference](configuration.md).

## Overview

A pool is a group of GPU nodes that share the same:

- Cloud provider (Lambda, GCP, AWS)
- Instance type
- Region
- Scaling limits and autoscaler configuration
- Health policies

Pools enable you to manage heterogeneous GPU resources with independent scaling policies. For example, you might have separate pools for training workloads (large H100 instances with conservative scaling) and inference (smaller A100 instances with aggressive autoscaling).

## Configuration

Pools are configured via a YAML file passed to the control plane:

```bash
navarch-control-plane --pools-config pools.yaml
```

### Basic pool configuration

```yaml
pools:
  - name: training
    provider: lambda
    instance_type: gpu_8x_h100_sxm5
    region: us-west-2
    
    scaling:
      min_nodes: 2
      max_nodes: 20
      cooldown_period: 5m
      
      autoscaler:
        type: reactive
        scale_up_threshold: 80
        scale_down_threshold: 20
    
    health:
      unhealthy_threshold: 2
      auto_replace: true
    
    labels:
      workload: training
```

### Configuration reference

| Field | Description | Required |
|-------|-------------|----------|
| `name` | Unique pool identifier | Yes |
| `provider` | Cloud provider: `lambda`, `gcp`, `aws` | Yes |
| `instance_type` | Provider-specific instance type | Yes |
| `region` | Cloud region | Yes |
| `zones` | List of availability zones for multi-zone pools | No |
| `scaling.min_nodes` | Minimum nodes to maintain | Yes |
| `scaling.max_nodes` | Maximum nodes allowed | Yes |
| `scaling.cooldown_period` | Time between scaling actions (e.g., `5m`) | No |
| `health.unhealthy_threshold` | Consecutive failures before node is unhealthy | No |
| `health.auto_replace` | Automatically replace unhealthy nodes | No |
| `labels` | Key-value labels for workload routing | No |

## Autoscaler strategies

Navarch supports multiple autoscaling strategies that you can configure per pool.

### Reactive autoscaler

Scales based on current GPU utilization. Use this for workloads with steady demand patterns.

```yaml
autoscaler:
  type: reactive
  scale_up_threshold: 80    # Scale up when utilization > 80%
  scale_down_threshold: 20  # Scale down when utilization < 20%
```

### Queue-based autoscaler

Scales based on job queue depth. Use this for batch processing workloads where you want to clear the queue quickly.

```yaml
autoscaler:
  type: queue
  jobs_per_node: 100  # Target 100 jobs per node
```

### Scheduled autoscaler

Adjusts scaling limits based on time of day or day of week. Use this for predictable demand patterns.

```yaml
autoscaler:
  type: scheduled
  schedule:
    # Business hours: larger capacity
    - days: [monday, tuesday, wednesday, thursday, friday]
      start_hour: 9
      end_hour: 18
      min_nodes: 10
      max_nodes: 100
    # Weekends: minimal capacity
    - days: [saturday, sunday]
      start_hour: 0
      end_hour: 24
      min_nodes: 0
      max_nodes: 10
  # Fall back to reactive scaling within the adjusted limits
  fallback:
    type: reactive
    scale_up_threshold: 80
    scale_down_threshold: 20
```

### Predictive autoscaler

Uses historical utilization data to anticipate demand. Use this for workloads with gradual ramp-up patterns.

```yaml
autoscaler:
  type: predictive
  lookback_window: 30    # Analyze last 30 utilization samples
  growth_factor: 1.5     # Scale 1.5x the predicted need
  fallback:
    type: reactive
    scale_up_threshold: 70
    scale_down_threshold: 30
```

### Composite autoscaler

Combines multiple strategies for complex scenarios. The mode determines how recommendations are combined.

```yaml
autoscaler:
  type: composite
  mode: max  # Options: max, min, avg
  autoscalers:
    - type: reactive
      scale_up_threshold: 70
      scale_down_threshold: 30
    - type: queue
      jobs_per_node: 50
```

Modes:

- `max`: Take the highest recommendation (most aggressive scaling)
- `min`: Take the lowest recommendation (most conservative)
- `avg`: Average all recommendations

## Provider configuration

Providers require credentials to provision instances.

### Fake provider (local development)

The fake provider simulates cloud instances by running node agents as goroutines. Use this for local development and testing without cloud costs:

```yaml
pools:
  - name: dev-pool
    provider: fake
    instance_type: gpu_8x_h100
    region: local
    # ... scaling and health config

providers:
  fake:
    gpu_count: 8  # GPUs per fake instance
```

No credentials are required. Each provisioned "instance" spawns a goroutine that:

- Registers with the control plane.
- Sends heartbeats and health check results.
- Responds to commands (cordon, drain).

See `examples/pools-dev.yaml` for a complete local development configuration.

### Lambda Labs

```yaml
providers:
  lambda:
    api_key_secret: navarch/lambda-api-key
```

Set the API key via environment variable:

```bash
export LAMBDA_API_KEY=your-api-key
```

### GCP

```yaml
providers:
  gcp:
    project: my-gcp-project
    credentials_secret: navarch/gcp-credentials
```

### AWS

```yaml
providers:
  aws:
    region: us-east-1
    credentials_secret: navarch/aws-credentials
```

## Global settings

Apply default settings across all pools:

```yaml
global:
  ssh_key_names:
    - ops-team
    - ml-team
  
  agent:
    server: https://control-plane.example.com
    heartbeat_interval: 30s
    health_check_interval: 60s
```

## Health-based replacement

When `auto_replace: true` is set, Navarch automatically replaces nodes that fail consecutive health checks.

```yaml
health:
  unhealthy_threshold: 2   # Replace after 2 consecutive failures
  auto_replace: true
```

The replacement process:

1. Node fails health checks (XID error, NVML failure, etc.)
2. After `unhealthy_threshold` consecutive failures, node is marked unhealthy
3. If `auto_replace` is enabled, the unhealthy node is terminated
4. A replacement node is provisioned immediately

This ensures your pools maintain capacity even when GPU hardware fails.

## Scaling behavior

### Cooldown period

The `cooldown_period` prevents thrashing by requiring a minimum time between scaling actions:

```yaml
scaling:
  cooldown_period: 5m
```

During cooldown, no scaling actions occur even if the autoscaler recommends them.

### Scaling limits

`min_nodes` and `max_nodes` are hard limits that the autoscaler respects:

- Nodes are never scaled below `min_nodes`
- Nodes are never scaled above `max_nodes`
- Scale-down prefers removing cordoned nodes first

### Manual scaling

You can manually scale pools via the CLI:

```bash
# Scale to specific count
navarch pool scale training --nodes 10

# View pool status
navarch pool status training
```

## Example configurations

### High-availability inference

```yaml
pools:
  - name: inference-prod
    provider: lambda
    instance_type: gpu_1x_a100_sxm4
    region: us-east-1
    
    scaling:
      min_nodes: 5          # Always have capacity
      max_nodes: 50
      cooldown_period: 2m   # React quickly
      
      autoscaler:
        type: composite
        mode: max           # Aggressive scaling
        autoscalers:
          - type: reactive
            scale_up_threshold: 60
            scale_down_threshold: 30
          - type: queue
            jobs_per_node: 100
    
    health:
      unhealthy_threshold: 1   # Replace immediately
      auto_replace: true
    
    labels:
      workload: inference
      tier: production
```

### Cost-optimized training

```yaml
pools:
  - name: training-batch
    provider: lambda
    instance_type: gpu_8x_h100_sxm5
    region: us-west-2
    
    scaling:
      min_nodes: 0          # Scale to zero when idle
      max_nodes: 20
      cooldown_period: 10m  # Conservative scaling
      
      autoscaler:
        type: scheduled
        schedule:
          - days: [monday, tuesday, wednesday, thursday, friday]
            start_hour: 8
            end_hour: 20
            min_nodes: 5
            max_nodes: 20
        fallback:
          type: reactive
          scale_up_threshold: 90
          scale_down_threshold: 10
    
    health:
      unhealthy_threshold: 3
      auto_replace: true
    
    labels:
      workload: training
      tier: batch
```

## Monitoring

The control plane logs all scaling decisions with reasons:

```
INFO scaling up pool=training from=5 to=8 adding=3 reason="utilization 85.0% > 80.0% threshold"
INFO scaling down pool=inference from=10 to=7 removing=3 reason="utilization 15.0% < 20.0% threshold"
```

Pool status is available via the API:

```bash
curl http://localhost:50051/api/v1/pools/training/status
```

Response:

```json
{
  "name": "training",
  "total_nodes": 8,
  "healthy_nodes": 8,
  "unhealthy_nodes": 0,
  "cordoned_nodes": 0,
  "can_scale_up": true,
  "can_scale_down": true
}
```

## Integrating metrics

To provide utilization and queue metrics to the autoscaler, implement the `MetricsSource` interface:

```go
type MetricsSource interface {
    GetPoolMetrics(ctx context.Context, poolName string) (*PoolMetrics, error)
}

type PoolMetrics struct {
    Utilization        float64   // Average GPU utilization (0-100)
    PendingJobs        int       // Jobs waiting to be scheduled
    QueueDepth         int       // Total jobs in queue
    UtilizationHistory []float64 // For predictive autoscaler
}
```

Connect your workload system (Kubernetes, Ray, custom scheduler) to provide these metrics. Without a metrics source, autoscalers that depend on utilization or queue depth will not scale.

