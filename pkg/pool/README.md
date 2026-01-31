# Pool management

This package provides GPU node pool management with pluggable autoscaling strategies and multi-provider support for fungible compute.

## Overview

A pool is a managed group of GPU nodes with:

- Multi-provider support (fungible compute across clouds).
- Provider selection strategies (priority, cost, availability, round-robin).
- Scaling limits (min/max nodes).
- Health tracking and automatic replacement.
- Pluggable autoscaling strategies.
- Cordon/uncordon for graceful maintenance.

## Pool configuration

### Single provider

```go
pool, err := pool.NewSimple(pool.Config{
    Name:         "training",
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
}, lambdaProvider, "lambda")
```

### Multi-provider (fungible compute)

Multi-provider pools treat GPUs as fungible compute. If one provider lacks capacity, provisioning falls back to the next.

```go
pool, err := pool.NewWithOptions(pool.NewPoolOptions{
    Config: pool.Config{
        Name:         "fungible",
        InstanceType: "h100-8x",  // Abstract type
        MinNodes:     4,
        MaxNodes:     32,
    },
    Providers: []pool.ProviderConfig{
        {Name: "lambda", Provider: lambdaProvider, Priority: 1},
        {Name: "gcp", Provider: gcpProvider, Priority: 2},
    },
    ProviderStrategy: "priority",  // or "cost", "availability", "round-robin"
})
```

### Provider selection strategies

| Strategy | Description |
|----------|-------------|
| `priority` | Tries providers in priority order (lowest first). Falls back on failure. Default. |
| `cost` | Queries pricing and selects cheapest available. |
| `availability` | Queries capacity and selects first with availability. |
| `round-robin` | Distributes evenly, respecting weights. |

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

## Clock injection

Pools accept a `clock.Clock` for time operations. Use `clock.NewFakeClock` in tests for deterministic behavior:

```go
import "github.com/NavarchProject/navarch/pkg/clock"

fakeClock := clock.NewFakeClock(time.Now())

p, _ := pool.NewWithOptions(pool.NewPoolOptions{
    Config: pool.Config{
        Name:           "test-pool",
        CooldownPeriod: 5 * time.Minute,
    },
    Providers: []pool.ProviderConfig{...},
    Clock:     fakeClock,
})

// Advance time to test cooldown
fakeClock.Advance(5 * time.Minute)
```

## Example: complete pool setup

```go
// Create providers
lambdaProvider, _ := lambda.New(lambda.Config{
    APIKey: os.Getenv("LAMBDA_API_KEY"),
})
gcpProvider, _ := gcp.New(gcp.Config{
    Project: os.Getenv("GCP_PROJECT"),
})

// Create multi-provider pool
p, _ := pool.NewWithOptions(pool.NewPoolOptions{
    Config: pool.Config{
        Name:           "inference",
        InstanceType:   "a100-8x",  // Abstract type
        MinNodes:       2,
        MaxNodes:       50,
        CooldownPeriod: 5 * time.Minute,
    },
    Providers: []pool.ProviderConfig{
        {Name: "lambda", Provider: lambdaProvider, Priority: 1},
        {Name: "gcp", Provider: gcpProvider, Priority: 2},
    },
    ProviderStrategy: "priority",
})

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

