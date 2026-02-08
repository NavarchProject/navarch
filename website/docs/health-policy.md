# Health Policy

Navarch uses [CEL (Common Expression Language)](https://github.com/google/cel-spec) to evaluate GPU health events and determine node health status. You can customize this logic by providing a health policy file.

If no policy is specified, Navarch uses a built-in default policy that classifies fatal XID errors (like XID 79 "GPU has fallen off the bus") as unhealthy and recoverable errors as degraded.

## Enabling a custom policy

Reference the policy file in your configuration:

```yaml
server:
  health_policy: ./health-policy.yaml
```

## Policy file format

```yaml
version: v1

metadata:
  name: my-policy
  description: Custom health policy for my fleet

rules:
  # More specific rules first
  - name: fatal-xid
    description: XID errors indicating unrecoverable GPU failure
    condition: |
      event.event_type == "xid" && event.metrics.xid_code in [48, 79, 95]
    result: unhealthy

  - name: recoverable-xid
    description: XID errors that may recover
    condition: event.event_type == "xid"
    result: degraded

  - name: thermal-critical
    condition: |
      event.event_type == "thermal" &&
      event.metrics.temperature >= 95
    result: unhealthy

  # Default rule must be last
  - name: default
    condition: "true"
    result: healthy
```

Rules are evaluated in order; the first matching rule determines the result. Place more specific rules before general ones, and always include a default rule at the end.

## Rule fields

| Field | Required | Description |
|-------|----------|-------------|
| `name` | Yes | Unique rule identifier |
| `description` | No | Human-readable description |
| `condition` | Yes | CEL expression that returns true when rule matches |
| `result` | Yes | Result when rule matches: `healthy`, `degraded`, or `unhealthy` |

## CEL event fields

The following fields are available in CEL expressions:

| Field | Type | Description |
|-------|------|-------------|
| `event.event_type` | string | Event type: `xid`, `thermal`, `ecc_dbe`, `ecc_sbe`, `nvlink`, `pcie`, `power` |
| `event.system` | string | DCGM health watch system identifier |
| `event.gpu_index` | int | GPU index (0-based, -1 for node-level) |
| `event.metrics` | map | Event-specific metrics |
| `event.message` | string | Human-readable description |

Common metrics by event type:

| Event Type | Metric | Type | Description |
|------------|--------|------|-------------|
| `xid` | `xid_code` | int | NVIDIA XID error code |
| `thermal` | `temperature` | int | GPU temperature in Celsius |
| `ecc_dbe` | `ecc_dbe_count` | int | Double-bit ECC error count |
| `ecc_sbe` | `ecc_sbe_count` | int | Single-bit ECC error count |

## Example policies

### Strict policy

Treat all XID errors as fatal:

```yaml
rules:
  - name: any-xid-fatal
    condition: event.event_type == "xid"
    result: unhealthy
  - name: default
    condition: "true"
    result: healthy
```

### Permissive policy

Only fail on the most severe errors:

```yaml
rules:
  - name: bus-error-only
    condition: |
      event.event_type == "xid" && event.metrics.xid_code == 79
    result: unhealthy
  - name: default
    condition: "true"
    result: healthy
```

### GPU-specific policy

Different thresholds for different GPUs:

```yaml
rules:
  - name: gpu0-strict
    description: GPU 0 is critical, any error is fatal
    condition: event.gpu_index == 0 && event.event_type == "xid"
    result: unhealthy
  - name: other-gpu-permissive
    condition: event.event_type == "xid"
    result: degraded
  - name: default
    condition: "true"
    result: healthy
```

## Testing policies

Use the [simulator](simulator/index.md) to test health policies before deploying to production. The simulator HTML report includes a "Policy Rules" section showing which rules matched for each failure.

```bash
./bin/simulator run scenarios/xid-classification.yaml -v
```

See [Health Monitoring](concepts/health.md) for background on health events and XID errors.
