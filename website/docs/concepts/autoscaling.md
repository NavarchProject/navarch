# Autoscaling

Autoscaling adjusts pool size based on demand. Navarch supports multiple strategies that you can use alone or combine.

## Scaling limits

Each pool has minimum and maximum node counts:

```yaml
pools:
  training:
    min_nodes: 2   # Never scale below this
    max_nodes: 20  # Never scale above this
```

- `min_nodes`: Floor for scaling. Set to 0 for pools that can be empty.
- `max_nodes`: Ceiling for scaling. Protects against cost overruns.

The autoscaler operates within these limits. Manual scaling commands also respect them.

## Cooldown period

The cooldown period prevents thrashing when metrics fluctuate:

```yaml
pools:
  training:
    cooldown: 5m
```

During cooldown:

- The autoscaler still evaluates state.
- Recommendations are calculated but not acted upon.
- Manual scaling commands are still accepted.

## Autoscaling strategies

### Reactive

Scales based on current GPU utilization. Use for steady workloads where current load predicts future load.

```yaml
autoscaling:
  type: reactive
  scale_up_at: 80    # Scale up when utilization > 80%
  scale_down_at: 20  # Scale down when utilization < 20%
```

**How it works**: Averages GPU utilization across all nodes in the pool. If above `scale_up_at`, adds a node. If below `scale_down_at`, removes a node.

### Queue-based

Scales based on pending job count. Use for batch processing where queue depth indicates required capacity.

```yaml
autoscaling:
  type: queue
  jobs_per_node: 10  # Target 10 jobs per node
```

**How it works**: Queries your scheduler for pending jobs, divides by `jobs_per_node`, rounds up. Requires a [metrics source integration](../metrics.md#integrating-external-schedulers).

See [Metrics Reference](../metrics.md) for Kubernetes and Slurm integration examples.

### Scheduled

Adjusts scaling limits based on time of day. Use for predictable patterns like business hours.

```yaml
autoscaling:
  type: scheduled
  schedule:
    - days: [monday, tuesday, wednesday, thursday, friday]
      start: 9
      end: 18
      min_nodes: 10
      max_nodes: 50
    - days: [saturday, sunday]
      start: 0
      end: 24
      min_nodes: 0
      max_nodes: 5
```

**How it works**: Overrides pool's `min_nodes` and `max_nodes` based on current time. Combine with a fallback strategy for within-window scaling.

### Predictive

Uses historical data to anticipate demand. Use for workloads with gradual ramp-up patterns.

```yaml
autoscaling:
  type: predictive
  lookback_window: 10   # Analyze last 10 samples
  growth_factor: 1.2    # Scale 20% ahead of predicted need
```

**How it works**: Analyzes utilization trend over recent samples. If trending up, scales proactively. Falls back to reactive behavior when trend is flat.

### Composite

Combines multiple strategies. Use for complex scenarios requiring multiple signals.

```yaml
autoscaling:
  type: composite
  mode: max  # Take the highest recommendation
  strategies:
    - type: reactive
      scale_up_at: 80
      scale_down_at: 30
    - type: queue
      jobs_per_node: 10
```

**Modes**:

- `max`: Take highest recommendation (most aggressive)
- `min`: Take lowest recommendation (most conservative)
- `avg`: Average all recommendations

## Choosing a strategy

| Workload | Strategy | Why |
|----------|----------|-----|
| Steady serving load | Reactive | Current utilization predicts future |
| Batch processing | Queue | Job count is the right signal |
| Business hours traffic | Scheduled + Reactive | Predictable pattern with variation |
| Training jobs | Queue or Reactive | Depends on how jobs are submitted |
| Mixed workloads | Composite | Multiple signals needed |

## Example configurations

### High-availability serving

Keep capacity ready, scale aggressively:

```yaml
pools:
  serving:
    min_nodes: 5
    max_nodes: 50
    cooldown: 2m
    autoscaling:
      type: reactive
      scale_up_at: 60   # Scale up early
      scale_down_at: 20
```

### Cost-optimized batch

Scale to zero when idle, scale fast when jobs arrive:

```yaml
pools:
  batch:
    min_nodes: 0
    max_nodes: 100
    cooldown: 1m
    autoscaling:
      type: queue
      jobs_per_node: 1  # One job per node
```

### Business hours with buffer

Higher capacity during work hours:

```yaml
pools:
  dev:
    min_nodes: 0
    max_nodes: 20
    cooldown: 5m
    autoscaling:
      type: scheduled
      schedule:
        - days: [monday, tuesday, wednesday, thursday, friday]
          start: 8
          end: 20
          min_nodes: 5
          max_nodes: 20
      fallback:
        type: reactive
        scale_up_at: 70
        scale_down_at: 30
```

See [Configuration Reference](../configuration.md#autoscaling) for all autoscaling options.
