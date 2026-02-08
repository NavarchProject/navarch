# Scenario Reference

Scenarios are YAML files that define a fleet configuration and a sequence of events to execute.

## File format

```yaml
name: example-scenario
description: A brief description of what this scenario tests.

fleet:
  - id: node-1
    provider: gcp
    region: us-central1
    zone: us-central1-a
    instance_type: a3-highgpu-8g
    gpu_count: 8
    gpu_type: "NVIDIA H100 80GB HBM3"
    labels:
      environment: test

events:
  - at: 0s
    action: start_fleet

  - at: 5s
    action: inject_failure
    target: node-1
    params:
      failure_type: xid_error
      xid_code: 79

assertions:
  - type: health_status
    target: node-1
    expected: unhealthy
```

## Fleet definition

Each node in the fleet requires:

| Field | Description |
|-------|-------------|
| `id` | Unique identifier for the node |
| `provider` | Cloud provider name (gcp, aws, lambda) |
| `region` | Cloud region |
| `zone` | Availability zone |
| `instance_type` | Instance type (a3-highgpu-8g, p5.48xlarge) |
| `gpu_count` | Number of GPUs on the node |
| `gpu_type` | GPU model name |
| `labels` | Optional key-value labels |

## Events

Events execute at specified times relative to scenario start. Times use Go duration format (`5s`, `1m30s`, `500ms`).

Events with the same time execute sequentially in file order.

## Actions

### start_fleet

Starts all nodes. Each node registers with the control plane and begins sending heartbeats.

```yaml
- at: 0s
  action: start_fleet
```

### stop_fleet

Stops all running nodes.

```yaml
- at: 30s
  action: stop_fleet
```

### inject_failure

Injects a failure condition into a node.

```yaml
- at: 5s
  action: inject_failure
  target: node-1
  params:
    failure_type: xid_error
    xid_code: 79
    gpu_index: 3
    message: "GPU has fallen off the bus"
```

**Parameters:**

| Parameter | Description |
|-----------|-------------|
| `failure_type` | Type of failure (see below) |
| `xid_code` | XID error code (for `xid_error` type) |
| `gpu_index` | Affected GPU index (0-based) |
| `message` | Custom error message |

**Failure types:**

| Type | Description |
|------|-------------|
| `xid_error` | NVIDIA XID error on a specific GPU |
| `temperature` | Thermal event (high GPU temperature) |
| `memory_error` | ECC memory error |
| `nvlink_error` | NVLink communication error |
| `backend_error` | GPU backend failure |
| `boot_failure` | GPU boot/initialization failure |

### recover_failure

Clears failures from a node.

```yaml
- at: 20s
  action: recover_failure
  target: node-1
  params:
    failure_type: xid_error  # Optional: clear only this type
```

### issue_command

Issues a command to a node through the control plane.

```yaml
- at: 10s
  action: issue_command
  target: node-1
  params:
    command_type: cordon
    command_args:
      reason: "maintenance"
```

**Command types:** `cordon`, `drain`, `terminate`, `run_diagnostic`

### wait_for_status

Waits for a node to reach a specific status.

```yaml
- at: 12s
  action: wait_for_status
  target: node-1
  params:
    expected_status: unhealthy
    timeout: 15s
```

**Valid statuses:** `active`, `cordoned`, `draining`, `unhealthy`, `terminated`

### wait

Pauses execution.

```yaml
- at: 10s
  action: wait
```

### log

Prints a message to the output.

```yaml
- at: 5s
  action: log
  params:
    log_message: "Injecting GPU failure..."
```

### assert

Checks a condition immediately. Fails the scenario if not met.

```yaml
- at: 25s
  action: assert
  target: node-1
  params:
    expected_status: unhealthy
```

## Assertions

Assertions at the end of the scenario verify final state. All must pass.

```yaml
assertions:
  - type: node_status
    target: node-1
    expected: active

  - type: health_status
    target: node-2
    expected: unhealthy
```

| Type | Description |
|------|-------------|
| `node_status` | Check node status (active, cordoned, draining, unhealthy, terminated) |
| `health_status` | Check health status (healthy, degraded, unhealthy) |

## XID error codes

The simulator includes known XID codes with severity classification.

**Fatal XID codes** (require node replacement):

| Code | Name |
|------|------|
| 43 | GPU stopped processing |
| 48 | Double Bit ECC Error |
| 63 | ECC page retirement failure |
| 74 | NVLink Error |
| 79 | GPU has fallen off the bus |
| 95 | Uncontained ECC error |

**Recoverable XID codes:**

| Code | Name |
|------|------|
| 13 | Graphics Engine Exception |
| 31 | GPU memory page fault |
| 32 | Invalid push buffer stream |
| 45 | Preemptive cleanup |
| 64 | ECC page retirement event |
| 68 | NVDEC0 Exception |
| 92 | High single-bit ECC error rate |
| 94 | Contained ECC error |

## Example scenarios

The `scenarios/` directory contains examples:

### basic-fleet.yaml

Tests node registration and health reporting.

- Starts a three-node fleet (GCP and AWS)
- Waits for nodes to register
- Asserts all nodes reach `active` status

### gpu-failure.yaml

Tests fatal GPU failure detection.

- Starts two nodes
- Injects XID 79 on one node
- Waits for unhealthy status
- Issues cordon command
- Asserts affected node is unhealthy, other is active

### xid-classification.yaml

Tests XID code classification.

- Injects fatal XID on one node
- Injects recoverable XID on another
- Recovers the recoverable node
- Asserts correct final states

### cordon-drain.yaml

Tests cordon and drain command flow.

- Starts two nodes
- Issues cordon then drain to one node
- Asserts other node remains active

## Writing custom scenarios

1. Create a YAML file following the format above
2. Validate: `./bin/simulator validate your-scenario.yaml`
3. Run: `./bin/simulator run your-scenario.yaml -v`

**Tips:**

- Start with `start_fleet` at time 0s
- Allow 2-3 seconds after `start_fleet` for nodes to register
- After injecting failures, allow 5-10 seconds for health checks to propagate
- Use `log` actions to document what the scenario is doing
- Use `wait_for_status` instead of fixed delays when possible
