# Fleet simulator

The fleet simulator runs the real Navarch control plane and node code with simulated GPUs, allowing you to test the full system without cloud resources or GPU hardware.

## Overview

The simulator uses the actual production control plane and node daemon implementations. Only the GPU layer is simulated, using an injectable fake that supports failure injection. This means you're testing real node behavior, real health checks, and real command handling—the same code that runs in production.

Use the simulator to:

- Test the actual control plane and node code locally
- Validate failure detection and recovery logic
- Verify command flows (cordon, drain, terminate)
- Run integration tests without GPU hardware
- Demo Navarch behavior

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
| `-v, --verbose` | Enable verbose output (INFO level) |
| `--debug` | Enable debug output (DEBUG level) |

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
| `id` | Unique identifier for the node |
| `provider` | Cloud provider (`gcp`, `aws`, `lambda`) |
| `region` | Cloud region |
| `zone` | Availability zone |
| `instance_type` | Instance type (`a3-highgpu-8g`, `p5.48xlarge`) |
| `gpu_count` | Number of GPUs on the node |
| `gpu_type` | GPU model name |
| `labels` | Optional key-value labels |

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
| `failure_type` | Type of failure: `xid_error`, `nvml_failure`, `boot_failure`, `temperature` |
| `xid_code` | XID error code (for `xid_error` type) |
| `gpu_index` | Affected GPU index (0-based) |
| `message` | Custom error message |

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
| `cordon` | Stop accepting new workloads |
| `drain` | Gracefully drain existing workloads |
| `terminate` | Prepare for shutdown |
| `run_diagnostic` | Run a diagnostic test |

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
| `node_status` | Check node status (`active`, `cordoned`, `draining`, `unhealthy`, `terminated`) |
| `health_status` | Check health status (`healthy`, `degraded`, `unhealthy`) |

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

The `scenarios/` directory contains example scenarios:

| Scenario | Purpose |
|----------|---------|
| `basic-fleet.yaml` | Node registration and multi-cloud fleet health |
| `gpu-failure.yaml` | Fatal XID error detection and node status transitions |
| `xid-classification.yaml` | Fatal vs recoverable XID code handling |
| `cordon-drain.yaml` | Cordon and drain command flow |

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

## Stress testing

The simulator includes a comprehensive stress testing framework for validating system behavior at scale with realistic failure patterns.

### Overview

Stress tests allow you to:

- Simulate thousands of nodes simultaneously
- Inject failures with realistic distributions based on production data
- Test cascading failure scenarios
- Simulate scheduled outages (zone, region, provider)
- Measure system resilience and recovery
- Generate detailed reports

### Running a stress test

```bash
# Run a stress test scenario
./bin/simulator run scenarios/stress/1000-node-chaos.yaml -v

# Run with a specific seed for reproducibility
./bin/simulator run scenarios/stress/1000-node-chaos.yaml --seed 12345 -v

# Validate a stress test scenario
./bin/simulator validate scenarios/stress/5000-node-extreme.yaml
```

### Console output

Stress tests display real-time progress during execution:

```
[ 50.0%] 5m0s elapsed, 5m0s remaining | Nodes: 947 healthy | Failures: 48 (cascade: 7) | Recoveries: 21
```

When complete, the simulator prints a summary with node statistics, failure counts, and XID error breakdown. It also displays the run directory path with a clickable link to the HTML report.

### Stress test configuration

Stress tests use an extended scenario format with a `stress` section:

```yaml
name: my-stress-test
description: Large-scale chaos testing

fleet: []  # Empty when using fleet_gen

stress:
  duration: 10m
  metrics_interval: 5s
  seed: 12345

  fleet_gen:
    total_nodes: 1000
    templates:
      - name: h100-8gpu
        weight: 60
        gpu_count: 8
        gpu_type: "NVIDIA H100 80GB HBM3"
        instance_type: a3-highgpu-8g
      - name: a100-8gpu
        weight: 40
        gpu_count: 8
        gpu_type: "NVIDIA A100 80GB"
        instance_type: a2-ultragpu-8g

    providers:
      gcp: 50
      aws: 35
      lambda: 15

    regions:
      us-central1: 40
      us-east1: 30
      europe-west1: 30

    startup:
      pattern: exponential
      duration: 2m
      jitter_percent: 15

  chaos:
    enabled: true
    failure_rate: 10.0
    # ... chaos configuration
```

### Fleet generation

Instead of defining individual nodes, stress tests can generate fleets from templates:

| Field | Description |
|-------|-------------|
| `total_nodes` | Total number of nodes to generate |
| `templates` | List of node templates with weights |
| `providers` | Provider distribution (percentages) |
| `regions` | Region distribution (percentages) |
| `zones` | Zone lists per region |
| `startup` | Node startup pattern configuration |

#### Node templates

Templates define node configurations with relative weights:

```yaml
templates:
  - name: h100-8gpu
    weight: 60        # 60% of nodes use this template
    gpu_count: 8
    gpu_type: "NVIDIA H100 80GB HBM3"
    instance_type: a3-highgpu-8g
    labels:
      tier: premium
```

#### Startup patterns

Control how nodes join the cluster:

| Pattern | Description |
|---------|-------------|
| `instant` | All nodes start immediately |
| `linear` | Nodes start at a constant rate over the duration |
| `exponential` | Start slow, accelerate (1, 2, 4, 8, ...) |
| `wave` | Start in batches with pauses between |

```yaml
startup:
  pattern: wave
  duration: 5m
  batch_size: 100
  jitter_percent: 20
```

### Chaos engineering

The `chaos` section controls failure injection:

```yaml
chaos:
  enabled: true
  failure_rate: 10.0  # Failures per minute per 1000 nodes

  xid_distribution:
    79: 30   # GPU fallen off bus (fatal)
    48: 20   # Double Bit ECC Error (fatal)
    31: 50   # GPU memory page fault (recoverable)

  failure_types:
    - type: xid_error
      weight: 70
    - type: temperature
      weight: 15
    - type: network
      weight: 15
```

#### Failure rate

The `failure_rate` specifies failures per minute per 1000 nodes. For example:
- 1000 nodes with rate 10.0 = ~10 failures per minute
- 5000 nodes with rate 10.0 = ~50 failures per minute

#### XID distribution

Specify the relative weight of each XID code. See [XID error codes](#xid-error-codes) for the full list of supported codes and their severity classification.

#### Failure types

| Type | Description |
|------|-------------|
| `xid_error` | GPU XID error with specified code distribution |
| `temperature` | Thermal throttling/shutdown |
| `nvml_failure` | NVML communication failure |
| `boot_failure` | GPU boot/detection failure |
| `network` | Network connectivity loss |

### Cascading failures

Cascading failures simulate realistic failure propagation:

```yaml
cascading:
  enabled: true
  probability: 0.15      # 15% chance a failure cascades
  max_depth: 3           # Maximum cascade chain length
  min_delay: 1s          # Minimum delay before cascade
  max_delay: 10s         # Maximum delay before cascade
  scope: zone            # Cascade scope
  max_affected_percent: 0.1  # Max 10% of scoped nodes affected
```

Cascade scopes:
- `rack`: Same rack (first 3 node ID segments match)
- `zone`: Same availability zone
- `region`: Same region
- `provider`: Same cloud provider
- `random`: Any node in the cluster

### Automatic recovery

Configure automatic recovery for non-fatal failures:

```yaml
recovery:
  enabled: true
  probability: 0.7    # 70% of non-fatal errors recover
  mean_time: 5m       # Average recovery time
  std_dev: 2m         # Recovery time variation
```

Recovery only applies to non-fatal XID codes and other recoverable failure types.

### Scheduled outages

Simulate planned or unplanned outage events:

```yaml
scheduled_outages:
  - name: zone-network-partition
    start_time: 10m
    duration: 5m
    scope: zone
    target: us-central1-a
    failure_type: network

  - name: provider-degradation
    start_time: 20m
    duration: 8m
    scope: provider
    target: lambda
    failure_type: xid_error

  - name: random-thermal-event
    start_time: 15m
    duration: 3m
    scope: percentage
    target: "10"        # 10% of nodes
    failure_type: temperature
```

Outage scopes:
- `zone`: All nodes in the specified zone
- `region`: All nodes in the specified region
- `provider`: All nodes from the specified provider
- `percentage`: Random percentage of all nodes

### Correlated failures

Define failures that trigger related failures:

```yaml
correlated_failures:
  - name: nvlink-gpu-cascade
    trigger: "74"         # NVLink error triggers this
    response: xid_error   # Inject XID error in response
    probability: 0.6      # 60% chance
    delay: 1s             # Wait before triggering
    scope: same_node      # Affect same node

  - name: thermal-propagation
    trigger: temperature
    response: temperature
    probability: 0.4
    delay: 3s
    scope: same_rack
```

Correlation scopes:
- `same_node`: Same node (multi-GPU failures)
- `same_rack`: Nearby nodes in same rack
- `same_zone`: Nodes in same availability zone
- `random`: Random node in cluster

### Run directory

Stress tests organize all artifacts in a timestamped run directory under `./sim-runs/`:

```
sim-runs/
└── 2024-01-15_10-30-45.123456789/
    ├── logs/              # Per-node and control plane logs
    ├── scenario.yaml      # Copy of input scenario
    ├── report.json        # JSON report
    └── report.html        # HTML report
```

### Per-node logs

Each simulated node writes detailed logs to its own file in the `logs/` subdirectory. Logs capture node registration, health checks, GPU status changes, failure events, and command execution.

The control plane also maintains its own log file (`control-plane.log`) with cluster-wide events. Log file paths appear as clickable links in the HTML report.

### Reports

Stress tests generate reports in the run directory:

- **HTML report** (`report.html`): Interactive visualization with summary statistics, failure breakdowns, and charts. Open in any browser.
- **JSON report** (`report.json`): Structured data for programmatic analysis, including configuration, node statistics, failure breakdowns, and a timeline of metrics.

### Example stress test scenarios

The `scenarios/stress/` directory contains ready-to-use scenarios:

| Scenario | Nodes | Duration | Purpose |
|----------|-------|----------|---------|
| `1000-node-chaos.yaml` | 1000 | 10m | Standard chaos test with realistic failure distribution |
| `5000-node-extreme.yaml` | 5000 | 30m | Maximum load with aggressive failures and outages |
| `xid-comprehensive.yaml` | 500 | 15m | All XID codes tested equally, no cascading |
| `cascading-failures.yaml` | 1000 | 20m | High cascade probability with deep chains |

### Writing custom stress tests

1. Start from an existing stress test scenario.
2. Adjust `total_nodes` based on your requirements.
3. Configure templates to match your production fleet.
4. Set `failure_rate` based on expected failure patterns.
5. Enable or disable features (cascading, recovery, outages) as needed.
6. Set a `seed` for reproducible tests.

Tips:

- Start with smaller node counts (100-500) during development.
- Use `--seed` to reproduce specific failure sequences.
- Monitor memory usage for large fleets (5000+ nodes).
- Allow adequate startup time for large fleets.
- Use `validate` to check scenario syntax before running.

### Performance considerations

For large-scale stress tests:

| Node Count | Recommended Startup | Memory Usage |
|------------|---------------------|--------------|
| 100-500 | linear, 30s | ~200MB |
| 500-1000 | linear, 1m | ~500MB |
| 1000-2000 | exponential, 2m | ~1GB |
| 2000-5000 | wave, 5m | ~2-3GB |
| 5000+ | wave, 10m+ | ~5GB+ |

