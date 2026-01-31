# Health package

This package provides CEL-based health policy evaluation for GPU nodes.

## Overview

The health package evaluates GPU health events against configurable policies to determine node health status. It uses the Common Expression Language (CEL) for flexible, declarative policy rules.

Key features:

- CEL expressions for matching health events.
- Configurable policies with named rules.
- Three-state health model: healthy, degraded, unhealthy.
- Worst-status-wins aggregation for multiple events.

## Health model

Nodes can be in one of three health states:

| Status | Description |
|--------|-------------|
| `Healthy` | No issues detected. |
| `Degraded` | Minor issues that do not require immediate action. |
| `Unhealthy` | Critical issues requiring node replacement. |

When multiple events are evaluated, the worst status wins: unhealthy > degraded > healthy.

## Policy structure

A policy contains named rules, each with a CEL expression and a resulting status:

```go
type Policy struct {
    Rules []Rule
}

type Rule struct {
    Name       string  // Human-readable rule name
    Expression string  // CEL expression
    Status     Result  // healthy, degraded, or unhealthy
}
```

## CEL expressions

Rules match against a single event at a time. The event is available as the `event` variable with these fields:

| Field | Type | Description |
|-------|------|-------------|
| `event.event_type` | string | Event category (xid, thermal, memory, etc.) |
| `event.system` | string | DCGM health watch system |
| `event.gpu_index` | int | GPU index (0-based) |
| `event.gpu_uuid` | string | GPU unique identifier |
| `event.message` | string | Human-readable description |
| `event.metrics` | map | Event-specific metrics |

### Example expressions

```cel
// Match fatal XID errors
event.event_type == "xid" && event.metrics.xid_code in [79, 119, 94, 95]

// Match high temperature
event.event_type == "thermal" && event.metrics.temperature > 90

// Match double-bit ECC errors
event.event_type == "ecc_dbe"

// Match NVLink errors
event.event_type == "nvlink"
```

## Default policy

The package includes a default policy covering common GPU failure modes:

```go
policy := health.DefaultPolicy()
```

Default rules include:

- **Fatal XID errors** (79, 119, 94, 95, etc.) -> unhealthy
- **Critical XID errors** (48, 74, etc.) -> unhealthy
- **Double-bit ECC errors** -> unhealthy
- **NVLink errors** -> degraded
- **High temperature** (>90C) -> unhealthy
- **Elevated temperature** (85-90C) -> degraded
- **High single-bit ECC rate** -> degraded

## Evaluator usage

```go
// Create evaluator with default policy
evaluator, err := health.NewEvaluator(health.DefaultPolicy())
if err != nil {
    log.Fatal(err)
}

// Evaluate events from a node
events := []gpu.HealthEvent{
    gpu.NewXIDEvent(0, "GPU-123", 79, "GPU has fallen off the bus"),
}

result, err := evaluator.Evaluate(ctx, events)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Status: %s\n", result.Status)        // unhealthy
fmt.Printf("Matched rule: %s\n", result.MatchedRule)  // fatal_xid
```

## Custom policies

Create custom policies for your environment:

```go
policy := &health.Policy{
    Rules: []health.Rule{
        {
            Name:       "critical_temp",
            Expression: `event.event_type == "thermal" && event.metrics.temperature > 85`,
            Status:     health.Unhealthy,
        },
        {
            Name:       "warm_temp",
            Expression: `event.event_type == "thermal" && event.metrics.temperature > 75`,
            Status:     health.Degraded,
        },
    },
}

evaluator, err := health.NewEvaluator(policy)
```

## Integration with control plane

The control plane server uses the health evaluator to process health reports from nodes:

1. Node collects health events from GPU manager.
2. Node sends events to control plane via `ReportHealth` RPC.
3. Control plane evaluates events against policy.
4. If status is unhealthy, control plane marks node unhealthy.
5. Pool manager may trigger node replacement if configured.

## Testing

```bash
go test ./pkg/health/... -v
```

The test suite covers:

- CEL expression compilation and evaluation.
- Default policy rule matching.
- Status aggregation across multiple events.
- Edge cases (empty events, unknown event types).
