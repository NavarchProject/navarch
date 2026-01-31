# Control plane package

This package implements the Navarch control plane server and supporting components.

## Overview

The control plane is the central orchestration layer of Navarch. It manages:

- Node registration and lifecycle.
- Health monitoring and CEL policy evaluation.
- Pool management with autoscaling.
- Instance lifecycle tracking from provisioning through termination.
- Metrics collection and aggregation.

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                      Control Plane                               │
│                                                                  │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────────┐ │
│  │   Server    │  │ PoolManager │  │   InstanceManager       │ │
│  │  (Connect)  │  │             │  │                         │ │
│  └──────┬──────┘  └──────┬──────┘  └───────────┬─────────────┘ │
│         │                │                      │               │
│         └────────────────┼──────────────────────┘               │
│                          │                                      │
│                    ┌─────┴─────┐                                │
│                    │    DB     │                                │
│                    │ Interface │                                │
│                    └───────────┘                                │
└─────────────────────────────────────────────────────────────────┘
```

## Components

### Server

The `Server` implements the Connect service for node communication:

- `RegisterNode`: Nodes call this on startup to join the cluster.
- `Heartbeat`: Periodic health and metrics updates from nodes.
- `ReportHealth`: Health events for CEL policy evaluation.
- `PollCommands`: Nodes poll for pending commands.
- `AckCommand`: Nodes acknowledge command completion.

```go
cfg := controlplane.DefaultConfig()
server := controlplane.NewServer(database, cfg, instanceManager, logger)

// Optional: observe health transitions for auto-replacement
server.SetHealthObserver(myObserver)
```

### PoolManager

The `PoolManager` orchestrates multiple GPU node pools:

- Runs autoscalers on a configurable interval.
- Acts on scaling recommendations (scale up/down).
- Integrates with InstanceManager for instance lifecycle tracking.

```go
cfg := controlplane.PoolManagerConfig{
    EvaluationInterval: 30 * time.Second,
    Clock:              clock.Real(),
}
pm := controlplane.NewPoolManager(cfg, metricsSource, instanceManager, logger)

// Register pools with autoscalers
pm.AddPool(trainingPool, reactiveAutoscaler)
pm.AddPool(inferencePool, compositeAutoscaler)

// Start the autoscaler loop
pm.Start(ctx)
defer pm.Stop()
```

### InstanceManager

The `InstanceManager` tracks cloud instance lifecycle:

- Creates instance records when provisioning starts.
- Updates state when nodes register.
- Detects stale instances (provisioned but never registered).
- Cleans up terminated instance records.

```go
cfg := controlplane.InstanceManagerConfig{
    RegistrationTimeout:      10 * time.Minute,
    StaleCheckInterval:       1 * time.Minute,
    RetainTerminatedDuration: 24 * time.Hour,
    Clock:                    clock.Real(),
}
im := controlplane.NewInstanceManager(database, cfg, logger)

// Register callbacks for lifecycle events
im.OnStaleInstance(func(inst *db.InstanceRecord) {
    // Handle stale instance (e.g., terminate, alert)
})
im.OnFailedInstance(func(inst *db.InstanceRecord) {
    // Handle failed provisioning
})

im.Start(ctx)
defer im.Stop()
```

### MetricsSource

The `MetricsSource` interface provides pool metrics for autoscaler decisions:

```go
type MetricsSource interface {
    GetPoolMetrics(ctx context.Context, poolName string) (*PoolMetrics, error)
}

type PoolMetrics struct {
    Utilization        float64   // Current utilization (0-100)
    PendingJobs        int       // Jobs waiting to run
    QueueDepth         int       // Total queue depth
    UtilizationHistory []float64 // Historical samples for prediction
}
```

The package includes `DBMetricsSource` which aggregates metrics from node heartbeats stored in the database.

### NodeHealthObserver

Implement `NodeHealthObserver` to react to health status changes:

```go
type NodeHealthObserver interface {
    OnNodeUnhealthy(ctx context.Context, nodeID string)
}
```

This is useful for triggering automatic node replacement when CEL policies mark a node unhealthy.

## Configuration

### Server configuration

```go
type Config struct {
    HealthCheckIntervalSeconds int32    // How often nodes run health checks
    HeartbeatIntervalSeconds   int32    // How often nodes send heartbeats
    EnabledHealthChecks        []string // Health checks to run (boot, nvml, xid)
    Clock                      clock.Clock
}
```

### Default configuration

```go
cfg := controlplane.DefaultConfig()
// HealthCheckIntervalSeconds: 60
// HeartbeatIntervalSeconds: 30
// EnabledHealthChecks: ["boot", "nvml", "xid"]
```

## Clock injection

All components accept a `clock.Clock` for time operations. Use `clock.NewFakeClock` in tests for deterministic behavior:

```go
fakeClock := clock.NewFakeClock(time.Now())

cfg := controlplane.Config{
    Clock: fakeClock,
}
server := controlplane.NewServer(db, cfg, nil, logger)

// Advance time in tests
fakeClock.Advance(30 * time.Second)
```

## Health evaluation

The server uses CEL policies (from `pkg/health`) to evaluate health events:

1. Node sends health events via `ReportHealth`.
2. Server evaluates events against configured policies.
3. If any rule matches unhealthy, node is marked unhealthy.
4. If `NodeHealthObserver` is set, it is notified.

## Testing

```bash
go test ./pkg/controlplane/... -v
```

The test suite covers:

- Server RPC handlers.
- PoolManager autoscaler integration.
- InstanceManager stale detection.
- Metrics aggregation.
