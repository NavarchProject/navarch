# Pool management

This package provides GPU node pool management with pluggable autoscaling strategies.

## Overview

A pool is a managed group of GPU nodes with:

- Scaling limits (min/max nodes).
- Health tracking and automatic replacement.
- Pluggable autoscaling strategies.
- Cordon/uncordon for graceful maintenance.

## Pool configuration

```go
pool, err := pool.New(pool.Config{
    Name:         "training",
    Provider:     "lambda",
    InstanceType: "gpu_8x_h100_sxm5",
    Region:       "us-west-2",
    
    MinNodes: 2,
    MaxNodes: 20,
    
    CooldownPeriod:     5 * time.Minute,
    UnhealthyThreshold: 2,
    AutoReplace:        true,
    
    Labels: map[string]string{
        "workload": "training",
    },
}, provider)
```

## Autoscaler interface

The `Autoscaler` interface allows pluggable scaling strategies:

```go
type Autoscaler interface {
    Recommend(ctx context.Context, state PoolState) (ScaleRecommendation, error)
}

type ScaleRecommendation struct {
    TargetNodes int    // Desired node count
    Reason      string // Human-readable explanation for logging/debugging
}
```

The autoscaler receives current pool state and returns a recommendation with the target node count and a reason. The reason is useful for observability and debugging scaling decisions.

### PoolState

```go
type PoolState struct {
    Name           string
    CurrentNodes   int
    HealthyNodes   int
    MinNodes       int
    MaxNodes       int
    Utilization    float64        // 0-100 percent
    QueueDepth     int
    LastScaleTime  time.Time
    CooldownPeriod time.Duration
    
    // For predictive scaling
    UtilizationHistory []float64
    TimeOfDay          time.Time
    DayOfWeek          time.Weekday
}
```

## Built-in autoscalers

### ReactiveAutoscaler

Scales based on current utilization thresholds. Simple and predictable.

```go
as := pool.NewReactiveAutoscaler(
    80,  // Scale up when utilization > 80%
    20,  // Scale down when utilization < 20%
)
```

Use when:

- Workload patterns are unpredictable.
- You want simple, threshold-based scaling.
- Low latency between demand and scaling is acceptable.

### QueueBasedAutoscaler

Scales based on pending job queue depth. Targets a specific jobs-per-node ratio.

```go
as := pool.NewQueueBasedAutoscaler(
    100, // Target 100 jobs per node
)
```

Use when:

- You have a job queue with measurable depth.
- Workload is batch-oriented.
- You want to maintain consistent per-node load.

### ScheduledAutoscaler

Scales based on time-of-day patterns. Wraps another autoscaler and overrides limits by schedule.

```go
schedule := []pool.ScheduleEntry{
    {
        DaysOfWeek: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday},
        StartHour:  9,
        EndHour:    17,
        MinNodes:   10,
        MaxNodes:   50,
    },
    {
        DaysOfWeek: []time.Weekday{time.Saturday, time.Sunday},
        StartHour:  0,
        EndHour:    24,
        MinNodes:   1,
        MaxNodes:   5,
    },
}

as := pool.NewScheduledAutoscaler(schedule, reactiveAutoscaler)
```

Use when:

- You have predictable daily or weekly patterns.
- Business hours require different capacity than off-hours.
- You want to enforce cost controls by time period.

### PredictiveAutoscaler

Uses historical utilization data to forecast demand and scale proactively.

```go
as := pool.NewPredictiveAutoscaler(
    30,   // Look back 30 samples
    1.5,  // Scale 1.5x predicted need
    fallbackAutoscaler,
)
```

Use when:

- You have sufficient historical data.
- Workload has gradual ramp-up patterns.
- You want to scale before demand spikes.

The predictive autoscaler falls back to another autoscaler when insufficient history is available.

### CompositeAutoscaler

Combines multiple autoscalers and selects the recommendation based on mode.

```go
as := pool.NewCompositeAutoscaler(
    pool.ModeMax,  // Take the highest recommendation
    reactiveAutoscaler,
    queueAutoscaler,
)
```

Modes:

| Mode | Behavior |
|------|----------|
| `ModeMax` | Takes the highest recommendation (aggressive) |
| `ModeMin` | Takes the lowest recommendation (conservative) |
| `ModeAvg` | Averages all recommendations |

Use when:

- You want to combine multiple signals.
- Different autoscalers have different strengths.
- You need aggressive scaling (max) or cost control (min).

## Implementing a custom autoscaler

To implement a custom autoscaler, implement the `Autoscaler` interface:

```go
type MyMLAutoscaler struct {
    model *MyForecastModel
}

func (a *MyMLAutoscaler) Recommend(ctx context.Context, state pool.PoolState) (pool.ScaleRecommendation, error) {
    // Use your ML model to predict demand
    prediction := a.model.Predict(state.UtilizationHistory)
    
    // Calculate nodes needed
    needed := int(math.Ceil(prediction / targetUtilization * float64(state.CurrentNodes)))
    
    // Respect limits
    target := max(needed, state.MinNodes)
    target = min(target, state.MaxNodes)
    
    return pool.ScaleRecommendation{
        TargetNodes: target,
        Reason:      fmt.Sprintf("ML prediction: %.2f", prediction),
    }, nil
}
```

## Cooldown period

All autoscalers respect the cooldown period in `PoolState`. During cooldown, they return the current node count unchanged. This prevents rapid scaling oscillations.

## Example: complete pool setup

```go
// Create provider
provider, _ := lambda.New(lambda.Config{
    APIKey: os.Getenv("LAMBDA_API_KEY"),
})

// Create pool
p, _ := pool.New(pool.Config{
    Name:           "inference",
    Provider:       "lambda",
    InstanceType:   "gpu_1x_a100_sxm4",
    Region:         "us-west-2",
    MinNodes:       2,
    MaxNodes:       50,
    CooldownPeriod: 5 * time.Minute,
}, provider)

// Create autoscaler
as := pool.NewCompositeAutoscaler(
    pool.ModeMax,
    pool.NewReactiveAutoscaler(70, 30),
    pool.NewQueueBasedAutoscaler(100),
)

// In your control loop
for {
    state := pool.PoolState{
        CurrentNodes:   len(p.Nodes()),
        MinNodes:       p.Config().MinNodes,
        MaxNodes:       p.Config().MaxNodes,
        Utilization:    getCurrentUtilization(),
        QueueDepth:     getQueueDepth(),
        LastScaleTime:  lastScaleTime,
        CooldownPeriod: p.Config().CooldownPeriod,
    }
    
    rec, _ := as.Recommend(ctx, state)
    log.Printf("autoscaler recommendation: %d nodes (%s)", rec.TargetNodes, rec.Reason)
    
    if rec.TargetNodes > state.CurrentNodes {
        p.ScaleUp(ctx, rec.TargetNodes - state.CurrentNodes)
    } else if rec.TargetNodes < state.CurrentNodes {
        p.ScaleDown(ctx, state.CurrentNodes - rec.TargetNodes)
    }
    
    time.Sleep(30 * time.Second)
}
```

## Testing

Run the pool tests:

```bash
go test ./pkg/pool/... -v
```

The test suite covers:

- All autoscaler implementations.
- Edge cases (threshold boundaries, empty queues, insufficient history).
- Limit enforcement (min/max nodes).
- Cooldown behavior.
- Composite mode combinations.

