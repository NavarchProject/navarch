# Fleet simulator

The Navarch fleet simulator creates a simulated GPU fleet and control plane for testing, development, and demonstration purposes.

## Overview

The simulator runs an embedded control plane and spawns simulated nodes that behave like real GPU instances. You can inject failures, issue commands, and observe how the system responds—all without provisioning actual cloud resources.

Use the simulator to:

- Test health check logic and failure detection.
- Verify command flows (cordon, drain, terminate).
- Develop and debug new features locally.
- Run automated integration tests.
- Demo Navarch to others.

## Building the simulator

```bash
make build
```

This creates `bin/simulator` along with the other Navarch binaries.

## Running scenarios

Scenarios are YAML files that define a fleet configuration and a sequence of events to execute.

To run a scenario:

```bash
./bin/simulator run scenarios/gpu-failure.yaml -v
```

To validate a scenario without running it:

```bash
./bin/simulator validate scenarios/gpu-failure.yaml
```

### Command-line options

| Flag | Description |
|------|-------------|
| `-v, --verbose` | Enable verbose output (INFO level). |
| `--debug` | Enable debug output (DEBUG level). |

## Interactive mode

Interactive mode starts a control plane and a default two-node fleet, then waits for you to interact with it using the Navarch CLI.

```bash
./bin/simulator interactive -v
```

Once running, use the CLI in another terminal:

```bash
# List all nodes
navarch list -s http://localhost:8080

# Get details about a node
navarch get node-1 -s http://localhost:8080

# Cordon a node
navarch cordon node-1 -s http://localhost:8080
```

Press `Ctrl+C` to stop the simulation.

## Scenario file format

Scenarios are YAML files with three sections: fleet definition, events, and optional assertions.

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

### Fleet definition

Each node in the fleet requires:

| Field | Description |
|-------|-------------|
| `id` | Unique identifier for the node. |
| `provider` | Cloud provider name (gcp, aws, azure). |
| `region` | Cloud region. |
| `zone` | Availability zone. |
| `instance_type` | Instance type (a3-highgpu-8g, p5.48xlarge). |
| `gpu_count` | Number of GPUs on the node. |
| `gpu_type` | GPU model name. |
| `labels` | Optional key-value labels. |

### Events

Events execute at specified times relative to scenario start. Times use Go duration format (5s, 1m30s, 500ms).

Events execute in order of their `at` time. Events with the same time execute sequentially in the order they appear in the file.

## Available actions

### start_fleet

Starts all nodes defined in the fleet. Each node registers with the control plane and begins sending heartbeats and health reports.

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

Injects a failure condition into a node. The node reports this failure in subsequent health checks.

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

Parameters:

| Parameter | Description |
|-----------|-------------|
| `failure_type` | Type of failure: `xid_error`, `nvml_failure`, `boot_failure`, `temperature`. |
| `xid_code` | XID error code (for xid_error type). |
| `gpu_index` | Affected GPU index (0-based). |
| `message` | Custom error message. |

### recover_failure

Clears failures from a node. If `failure_type` is specified, only that type is cleared. Otherwise, all failures are cleared.

```yaml
- at: 20s
  action: recover_failure
  target: node-1
  params:
    failure_type: xid_error
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

Command types:

| Type | Description |
|------|-------------|
| `cordon` | Stop accepting new workloads. |
| `drain` | Gracefully drain existing workloads. |
| `terminate` | Prepare for shutdown. |
| `run_diagnostic` | Run a diagnostic test. |

### wait_for_status

Waits for a node to reach a specific status. Fails if the timeout is exceeded.

```yaml
- at: 12s
  action: wait_for_status
  target: node-1
  params:
    expected_status: unhealthy
    timeout: 15s
```

Valid statuses: `active`, `cordoned`, `draining`, `unhealthy`, `terminated`.

### wait

Pauses execution. Useful for allowing time for asynchronous operations.

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

Checks a condition immediately. Fails the scenario if the condition is not met.

```yaml
- at: 25s
  action: assert
  target: node-1
  params:
    expected_status: unhealthy
```

## Assertions

Assertions at the end of the scenario verify final state. All assertions must pass for the scenario to succeed.

```yaml
assertions:
  - type: node_status
    target: node-1
    expected: active

  - type: health_status
    target: node-2
    expected: unhealthy
```

Assertion types:

| Type | Description |
|------|-------------|
| `node_status` | Check node status (active, cordoned, draining, unhealthy, terminated). |
| `health_status` | Check health status (healthy, degraded, unhealthy). |

## XID error codes

The simulator includes a database of known XID error codes with their severity classification.

Fatal XID codes (require node replacement):

| Code | Name |
|------|------|
| 43 | GPU stopped processing |
| 48 | Double Bit ECC Error |
| 63 | ECC page retirement failure |
| 74 | NVLink Error |
| 79 | GPU has fallen off the bus |
| 95 | Uncontained ECC error |

Recoverable XID codes (may resolve without replacement):

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

The `scenarios/` directory contains example scenarios that validate core Navarch functionality. Each scenario is designed to test a specific aspect of the system.

### basic-fleet.yaml

Tests that nodes can register with the control plane and reach a healthy state.

This scenario validates:

- Node registration with the control plane works correctly.
- Health reporting establishes an active status.
- Multi-cloud fleets (GCP and AWS nodes together) function properly.

The scenario starts a three-node fleet spanning GCP and AWS, waits for all nodes to register and report healthy, then asserts that each node reaches the `active` status.

### gpu-failure.yaml

Tests detection and handling of a fatal GPU failure.

This scenario validates:

- Fatal XID errors are detected by the health monitoring system.
- Affected nodes transition to an unhealthy status.
- Commands (cordon) can be issued to unhealthy nodes.
- Unaffected nodes remain healthy and active.

The scenario starts a two-node fleet, injects XID 79 (GPU has fallen off the bus) on one node, waits for the node to become unhealthy, then issues a cordon command. It asserts that the affected node is unhealthy while the other node remains active.

### xid-classification.yaml

Tests that XID error codes are correctly classified as fatal or recoverable.

This scenario validates:

- Fatal XID codes (like XID 48 - Double Bit ECC Error) mark a node as unhealthy.
- Recoverable XID codes (like XID 31 - GPU memory page fault) allow the node to recover.
- The `recover_failure` action clears transient failures.
- Nodes return to healthy status after recovering from a recoverable error.

The scenario starts two nodes, injects a fatal XID on one and a recoverable XID on the other, recovers the recoverable node, then asserts that the fatal node is unhealthy while the recovered node is healthy.

### cordon-drain.yaml

Tests the cordon and drain command flow for graceful node removal.

This scenario validates:

- The cordon command is accepted and processed by nodes.
- The drain command is accepted and processed by nodes.
- Commands can be issued in sequence.
- Other nodes in the fleet are not affected by commands targeting a specific node.

The scenario starts a two-node fleet, issues cordon then drain commands to one node, and asserts that the other node remains active throughout the process.

## Writing custom scenarios

To create a new scenario:

1. Create a YAML file following the format above.
2. Validate it with `./bin/simulator validate your-scenario.yaml`.
3. Run it with `./bin/simulator run your-scenario.yaml -v`.

Tips:

- Start with `start_fleet` at time 0s.
- Allow 2-3 seconds after `start_fleet` for nodes to register.
- After injecting failures, allow 5-10 seconds for health checks to run and propagate.
- Use `log` actions to document what the scenario is doing.
- Use `wait_for_status` instead of fixed delays when possible.

## Makefile targets

```bash
# Run interactive mode
make sim

# Run a specific scenario
make sim-run SCENARIO=scenarios/gpu-failure.yaml

# Validate a scenario
make sim-validate SCENARIO=scenarios/basic-fleet.yaml
```

