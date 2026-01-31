# Health package

This package provides CEL-based health policy evaluation for GPU nodes.

## Overview

The health package evaluates GPU health events against configurable policies to determine node health status. It uses the Common Expression Language (CEL) for flexible, declarative policy rules.

Key features:

- CEL expressions for matching health events.
- YAML-based policy configuration with file-order evaluation.
- Three-state health model: healthy, degraded, unhealthy.
- Worst-status-wins aggregation for multiple events.

## Health model

Nodes can be in one of three health states:

| Status | Description |
|--------|-------------|
| `healthy` | No issues detected. |
| `degraded` | Minor issues that do not require immediate action. |
| `unhealthy` | Critical issues requiring node replacement. |

When multiple events are evaluated, the worst status wins: unhealthy > degraded > healthy.

## Policy file format

Policies are defined in YAML files. Rules are evaluated in definition order; the first matching rule determines the result. Place more specific rules before general ones.

```yaml
version: v1

metadata:
  name: my-policy
  description: Custom health policy

rules:
  - name: fatal-xid
    description: XID errors indicating unrecoverable failure
    condition: |
      event.event_type == "xid" && event.metrics.xid_code in [48, 79, 95]
    result: unhealthy

  - name: recoverable-xid
    condition: event.event_type == "xid"
    result: degraded

  - name: default
    condition: "true"
    result: healthy
```

## Rule structure

Each rule has the following fields:

| Field | Required | Description |
|-------|----------|-------------|
| `name` | Yes | Unique identifier for logging and debugging. |
| `description` | No | Human-readable context for the rule. |
| `condition` | Yes | CEL expression evaluated against each event. |
| `result` | Yes | Health status when this rule matches: `healthy`, `degraded`, or `unhealthy`. |

## CEL expressions

Rules match against a single event at a time. The event is available as the `event` variable with these fields:

| Field | Type | Description |
|-------|------|-------------|
| `event.event_type` | string | Event category: xid, thermal, ecc_dbe, ecc_sbe, nvlink, pcie, power. |
| `event.system` | string | DCGM health watch system identifier. |
| `event.gpu_index` | int | GPU index (0-based, -1 for node-level events). |
| `event.gpu_uuid` | string | GPU unique identifier. |
| `event.message` | string | Human-readable description. |
| `event.metrics` | map | Event-specific metrics. |

Common metrics by event type:

| Event Type | Metric | Type | Description |
|------------|--------|------|-------------|
| `xid` | `xid_code` | int | NVIDIA XID error code. |
| `thermal` | `temperature` | int | GPU temperature in Celsius. |
| `ecc_dbe` | `ecc_dbe_count` | int | Double-bit ECC error count. |
| `ecc_sbe` | `ecc_sbe_count` | int | Single-bit ECC error count. |

### Example expressions

```cel
// Match fatal XID errors
event.event_type == "xid" && event.metrics.xid_code in [79, 119, 94, 95]

// Match high temperature
event.event_type == "thermal" && event.metrics.temperature >= 95

// Match double-bit ECC errors
event.event_type == "ecc_dbe"

// Match NVLink errors from any source
event.event_type == "nvlink" || event.system == "DCGM_HEALTH_WATCH_NVLINK"
```

## Loading policies

Load a policy from a file:

```go
policy, err := health.LoadPolicy("/path/to/policy.yaml")
if err != nil {
    log.Fatal(err)
}
```

Load the built-in default policy:

```go
policy, err := health.LoadDefaultPolicy()
// or use the convenience function that handles errors:
policy := health.DefaultPolicy()
```

Parse a policy from YAML bytes:

```go
data := []byte(`
version: v1
rules:
  - name: default
    condition: "true"
    result: healthy
`)
policy, err := health.ParsePolicy(data)
```

## Default policy

The package includes an embedded default policy covering common GPU failure modes. The default policy is defined in `default_policy.yaml` and includes rules for:

- Fatal XID errors (79, 48, 95, 119, etc.) → unhealthy
- Recoverable XID errors → degraded
- Double-bit ECC errors → unhealthy
- High single-bit ECC rate → degraded
- Critical temperature (≥95°C) → unhealthy
- High temperature (≥85°C) → degraded
- NVLink errors → degraded
- PCIe errors → degraded
- Power issues → degraded

See `default_policy.yaml` for the complete rule set and XID code list.

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

fmt.Printf("Status: %s\n", result.Status)           // unhealthy
fmt.Printf("Matched rule: %s\n", result.MatchedRule) // fatal-xid
```

## Custom policies

Create custom policies for your environment:

```go
policy := &health.Policy{
    Rules: []health.Rule{
        {
            Name:      "critical_temp",
            Condition: `event.event_type == "thermal" && event.metrics.temperature > 85`,
            Result:    health.ResultUnhealthy,
        },
        {
            Name:      "warm_temp",
            Condition: `event.event_type == "thermal" && event.metrics.temperature > 75`,
            Result:    health.ResultDegraded,
        },
        {
            Name:      "default",
            Condition: "true",
            Result:    health.ResultHealthy,
        },
    },
}

evaluator, err := health.NewEvaluator(policy)
```

Or load a custom policy from a YAML file at runtime. See the [configuration guide](../../docs/configuration.md#health-policy) for the YAML format.

## Integration with control plane

The control plane server uses the health evaluator to process health reports from nodes:

1. Node collects health events from GPU manager.
2. Node sends events to control plane via `ReportHealth` RPC.
3. Control plane evaluates events against policy.
4. If status is unhealthy, control plane marks node unhealthy.
5. Pool manager may trigger node replacement if configured.

To use a custom policy file with the control plane, set `health_policy` in the server configuration:

```yaml
server:
  address: ":50051"
  health_policy: ./health-policy.yaml
```

## Testing

```bash
go test ./pkg/health/... -v
```

The test suite covers:

- CEL expression compilation and evaluation.
- Default policy rule matching.
- Policy file parsing and validation.
- Duplicate rule name detection.
- Status aggregation across multiple events.
- Edge cases (empty events, unknown event types).
