# Navarch simulator

The Navarch simulator is a testing and validation tool that simulates GPU fleet scenarios. It creates virtual nodes, injects failures, and validates that the control plane responds correctly.

## Overview

The simulator provides:

- Scenario-based testing with YAML configuration.
- Virtual node fleet creation with configurable GPU counts.
- Failure injection (XID errors, health check failures).
- Event sequencing with precise timing control.
- Control plane validation and behavior verification.
- Interactive mode for manual testing and exploration.

## Installation

Build the simulator binary:

```bash
go build -o simulator ./cmd/simulator
```

## Usage

Run a scenario:

```bash
./simulator run scenarios/basic-fleet.yaml
```

Run in interactive mode:

```bash
./simulator run scenarios/basic-fleet.yaml --interactive
```

In interactive mode, the simulator keeps nodes running and displays their status. Use the control plane at the displayed address to interact with the simulated fleet.

## Scenario format

Scenarios are defined in YAML files. A scenario specifies the fleet configuration and a sequence of events.

Example scenario:

```yaml
name: Basic fleet test
description: Tests basic registration and health reporting

fleet:
  nodes:
    - id: node-1
      provider: gcp
      region: us-central1
      zone: us-central1-a
      instance_type: a3-highgpu-8g
      gpu_count: 8
    - id: node-2
      provider: gcp
      region: us-central1
      zone: us-central1-b
      instance_type: a3-highgpu-8g
      gpu_count: 8

events:
  - action: wait
    duration: 65s
    description: Wait for health checks to complete
```

## Fleet configuration

The `fleet` section defines the nodes to simulate:

- `id`: Unique node identifier.
- `provider`: Cloud provider (gcp, aws, azure).
- `region`: Cloud region.
- `zone`: Availability zone.
- `instance_type`: Instance type.
- `gpu_count`: Number of GPUs per node.

All nodes start automatically when the scenario begins.

## Event actions

The simulator supports the following event actions:

### Wait

Pause for a specified duration:

```yaml
- action: wait
  duration: 30s
  description: Wait for heartbeats
```

### Inject failure

Inject a health check failure on a specific node:

```yaml
- action: inject_failure
  target: node-1
  failure_type: xid
  params:
    xid_code: 79
  description: Inject XID 79 on node-1
```

Supported failure types:

- `xid`: GPU XID error (requires `xid_code` parameter).
- `boot`: Boot check failure.
- `nvml`: NVML check failure.

### Recover failure

Clear a previously injected failure:

```yaml
- action: recover_failure
  target: node-1
  failure_type: xid
  description: Clear XID error from node-1
```

### Run command

Execute a control plane command against a node:

```yaml
- action: run_command
  target: node-1
  command: cordon
  description: Cordon node-1
```

Supported commands:

- `cordon`: Mark node unschedulable.
- `drain`: Evict workloads and mark unschedulable.

## Example scenarios

### Basic fleet

Tests node registration and health reporting:

```yaml
name: Basic fleet
fleet:
  nodes:
    - id: node-1
      gpu_count: 8
events:
  - action: wait
    duration: 65s
```

### GPU failure simulation

Tests control plane response to GPU failures:

```yaml
name: GPU failure
fleet:
  nodes:
    - id: node-1
      gpu_count: 8
events:
  - action: wait
    duration: 65s
  - action: inject_failure
    target: node-1
    failure_type: xid
    params:
      xid_code: 79
  - action: wait
    duration: 65s
```

### Cordon and drain

Tests node lifecycle management:

```yaml
name: Cordon and drain
fleet:
  nodes:
    - id: node-1
      gpu_count: 8
events:
  - action: wait
    duration: 65s
  - action: run_command
    target: node-1
    command: cordon
  - action: wait
    duration: 10s
  - action: run_command
    target: node-1
    command: drain
  - action: wait
    duration: 30s
```

## Interactive mode

Interactive mode keeps the simulator running after the scenario completes:

```bash
./simulator run scenarios/basic-fleet.yaml --interactive
```

The simulator displays:

- Control plane address for CLI access.
- Instructions for using the CLI to interact with nodes.
- Real-time node status updates.

Example output:

```
Control plane running at: http://localhost:57284

You can interact with the fleet using the navarch CLI:
  navarch list -s http://localhost:57284
  navarch get <node-id> -s http://localhost:57284
  navarch cordon <node-id> -s http://localhost:57284

Press Ctrl+C to stop the simulator
```

Use the CLI to explore the simulated fleet:

```bash
# List all nodes
navarch list -s http://localhost:57284

# Get node details
navarch get node-1 -s http://localhost:57284

# Cordon a node
navarch cordon node-1 -s http://localhost:57284
```

## Logging

The simulator uses a custom human-friendly log format with emojis:

- 🚀 Node startup and initialization.
- ❤️ Heartbeat sent.
- 🏥 Health check completed.
- 💥 Failure injected.
- 🔧 Failure recovered.
- ⚙️ Command received and executed.

Example log output:

```
15:30:45 INFO  🚀 starting node (node=node-1, gpus=8)
15:30:45 INFO  ✅ registered with control plane (node=node-1)
15:30:45 INFO  ❤️  heartbeat sent (node=node-1)
15:31:45 INFO  🏥 health check completed (node=node-1, status=healthy)
```

This format makes it easy to understand scenario execution and identify issues during testing.

## Dynamic port allocation

The simulator automatically allocates an available port for the control plane. This enables:

- Running multiple simulators in parallel.
- CI/CD integration without port conflicts.
- Repeatable test execution.

The allocated port is displayed when the simulator starts and is used in all log messages.

## Testing best practices

### Scenario design

Design scenarios to test specific behaviors:

- **Happy path**: Basic registration and health reporting.
- **Failure injection**: GPU errors, health check failures.
- **Recovery**: Failure resolution and node restoration.
- **Lifecycle**: Cordon, drain, and termination flows.

### Timing

Allow sufficient time between events:

- Wait at least 65 seconds for initial health checks (default 60s interval + processing).
- Wait at least 35 seconds between heartbeats (default 30s interval + processing).
- Add extra time for command execution and propagation.

### Validation

After each event, verify the expected behavior:

- Check node status with the CLI.
- Examine health check results.
- Verify command execution.

Interactive mode is ideal for manual validation and exploration.

## CI/CD integration

Run scenarios in CI/CD pipelines:

```bash
# Run all scenarios
for scenario in scenarios/*.yaml; do
  ./simulator run "$scenario"
done
```

The simulator exits with a non-zero status code if any scenario fails.

## Development

Run simulator tests:

```bash
go test ./pkg/simulator/...
```

Create new scenarios:

1. Copy an existing scenario from `scenarios/`.
2. Modify the fleet and events to test your use case.
3. Run the scenario: `./simulator run scenarios/my-scenario.yaml`.
4. Use interactive mode to validate behavior.

## Troubleshooting

### Scenario fails to start

Verify the YAML syntax is correct:

```bash
# Check for YAML errors
yamllint scenarios/my-scenario.yaml
```

Check the simulator logs for parsing errors.

### Events do not execute as expected

Verify timing in the scenario:

- Increase wait durations to ensure operations complete.
- Check that target node IDs match fleet configuration.
- Confirm failure types and parameters are valid.

### Interactive mode does not respond to CLI commands

Verify the control plane address is correct:

- Copy the address from simulator output.
- Ensure the CLI uses the same address (`-s` flag).
- Check for firewall or network issues.

### Port already in use

The simulator uses dynamic port allocation, so port conflicts should not occur. If you see port errors, check for processes holding ports in the dynamic range (typically 32768-65535).

