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
| `failure_type` | Type of failure: `xid_error`, `temperature`, `memory_error`, `nvlink_error`, `backend_error`, `boot_failure`. |
| `xid_code` | XID error code (for xid_error type). |
| `gpu_index` | Affected GPU index (0-based). |
| `message` | Custom error message. |

Failure types:

| Type | Description |
|------|-------------|
| `xid_error` | Simulates an NVIDIA XID error on a specific GPU. |
| `temperature` | Simulates a thermal event (high GPU temperature). |
| `memory_error` | Simulates an ECC memory error. |
| `nvlink_error` | Simulates an NVLink communication error. |
| `backend_error` | Simulates a GPU backend failure (all GPU operations fail). |
| `boot_failure` | Simulates a GPU boot/initialization failure. |

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
  report_file: stress-report.json
  html_report_file: stress-report.html  # Interactive web UI
  log_file: stress-report.log           # Debug log

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
  cold_start_min: 30s
  cold_start_max: 2m
```

Cold start delays simulate provisioning time before a node comes online. Configure either a range or a normal distribution:

- Use `cold_start_min` and `cold_start_max` to sample uniformly between the two values.
- Use `cold_start_mean` and `cold_start_stddev` to sample from a normal distribution and clamp to the optional min/max.

Cold start delays also apply to replacement nodes when auto-replacement is enabled. The simulator respects the configured cold start parameters when provisioning replacements for unhealthy nodes.

### Chaos engineering

The chaos configuration controls failure injection:

```yaml
chaos:
  enabled: true
  failure_rate: 10.0  # Failures per minute per 1000 nodes

  xid_distribution:
    13: 15  # Graphics Engine Exception
    31: 20  # GPU memory page fault
    48: 12  # Double Bit ECC Error
    79: 6   # GPU fallen off bus

  failure_types:
    - type: xid_error
      weight: 70
    - type: temperature
      weight: 10
    - type: nvml_failure
      weight: 8
    - type: network
      weight: 10
    - type: boot_failure
      weight: 2

  cascading:
    enabled: true
    probability: 0.15
    max_depth: 3
    min_delay: 1s
    max_delay: 10s
    scope: zone
    max_affected_percent: 0.1

  recovery:
    enabled: true
    probability: 0.7
    mean_time: 5m
    std_dev: 2m
```

#### Failure rate

The `failure_rate` specifies failures per minute per 1000 nodes. For example:
- 1000 nodes with rate 10.0 = ~10 failures per minute
- 5000 nodes with rate 10.0 = ~50 failures per minute

#### XID distribution

Specify the relative weight of each XID code. The simulator includes all known XID codes with their fatal/recoverable classification:

**Fatal XID codes** (require node replacement):
- 43: GPU stopped processing
- 48: Double Bit ECC Error
- 63: ECC page retirement failure
- 74: NVLink Error
- 79: GPU has fallen off the bus
- 95: Uncontained ECC error

**Recoverable XID codes**:
- 13: Graphics Engine Exception
- 31: GPU memory page fault
- 32: Invalid push buffer stream
- 45: Preemptive cleanup
- 64: ECC page retirement event
- 92: High single-bit ECC rate
- 94: Contained ECC error

#### Failure types

| Type | Description |
|------|-------------|
| `xid_error` | GPU XID error with specified code distribution |
| `temperature` | Thermal throttling/shutdown |
| `backend_error` | GPU backend communication failure (alias: `nvml_failure`) |
| `boot_failure` | GPU boot/detection failure |
| `network` | Network connectivity loss |
| `memory_error` | ECC memory error |
| `nvlink_error` | NVLink communication error |

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
  replace_fatal: true # Replace nodes with fatal errors
  replace_cold_start: 45s  # Cold start delay for replacements
```

Recovery only applies to non-fatal XID codes and other recoverable failure types.

When `replace_fatal` is enabled, the simulator automatically provisions replacement nodes for nodes that experience fatal failures (like fatal XID errors). The replacement uses the same node specification and applies the configured cold start delay.

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

### Stress test reports

Stress tests can generate multiple report formats:

```yaml
stress:
  report_file: stress-report.json       # JSON report with raw data
  html_report_file: stress-report.html  # Interactive HTML report (web UI)
  log_file: stress-report.log           # Detailed debug log
```

#### HTML report (web UI)

The HTML report provides an interactive web-based visualization of stress test results. Open the file in any browser to view:

- **Results tab**: Summary statistics, failure breakdowns, and interactive charts
  - Node health over time (line chart)
  - Failures vs recoveries (line chart)
  - XID error distribution (pie chart)
  - Failure types breakdown (bar chart)
- **Configuration tab**: Full test configuration including fleet generation settings, chaos parameters, cascading failure config, and recovery settings

To generate an HTML report:

```bash
./bin/simulator run scenarios/stress/high-failure-test.yaml -v
# Reports generated at paths specified in the scenario
```

Example output:
```
📄 Reports generated:
   • /tmp/stress-report.log (Log)
   • /tmp/stress-report.json (JSON)
   • /tmp/stress-report.html (HTML)
```

#### Log file

The log file captures verbose debug-level output from all components (control plane, nodes, chaos engine) during the stress test. This is useful for:

- Debugging specific failure sequences
- Providing context to LLMs for analysis
- Post-mortem investigation of cascading failures

#### JSON report

The JSON report contains structured data for programmatic analysis.

Report contents:
- Configuration summary
- Test duration and timing
- Node statistics (started, failed, healthy, unhealthy)
- Failure breakdown by type and XID code
- Cascading failure statistics
- Recovery statistics
- Timeline of metric samples

Example report structure:

```json
{
  "name": "1000-node-chaos-test",
  "start_time": "2024-01-15T10:00:00Z",
  "end_time": "2024-01-15T10:10:00Z",
  "duration": "10m0s",
  "configuration": {
    "total_nodes": 1000,
    "failure_rate_per_min": 10.0,
    "cascading_enabled": true,
    "recovery_enabled": true
  },
  "summary": {
    "nodes_started": 1000,
    "nodes_failed_to_start": 0,
    "peak_healthy_nodes": 1000,
    "min_healthy_nodes": 847,
    "avg_healthy_nodes": 923.5,
    "total_failures": 98,
    "total_recoveries": 45,
    "total_outages": 0
  },
  "failures": {
    "by_type": {
      "xid_error": 68,
      "temperature": 12,
      "network": 10,
      "nvml_failure": 8
    },
    "by_xid": {
      "31": 15,
      "79": 8,
      "48": 7
    },
    "cascading_failures": 12,
    "top_xid_codes": [
      {"code": 31, "name": "GPU memory page fault", "count": 15, "fatal": false},
      {"code": 79, "name": "GPU has fallen off the bus", "count": 8, "fatal": true}
    ]
  },
  "timeline": [...]
}
```

### Example stress test scenarios

The `scenarios/stress/` directory contains ready-to-use stress test scenarios:

#### 1000-node-chaos.yaml

Standard chaos test with 1000 nodes:
- 10 minute duration
- Mixed H100/A100 fleet across GCP, AWS, Lambda
- Realistic XID distribution
- Cascading failures enabled
- Automatic recovery for non-fatal errors

```bash
./bin/simulator run scenarios/stress/1000-node-chaos.yaml -v
```

#### 5000-node-extreme.yaml

Extreme stress test for maximum load:
- 30 minute duration
- 5000 nodes across 8 regions
- Aggressive failure rate (50/min/1000 nodes)
- Multiple scheduled outages
- High cascade probability

```bash
./bin/simulator run scenarios/stress/5000-node-extreme.yaml -v
```

#### xid-comprehensive.yaml

Comprehensive XID error testing:
- All known XID codes tested equally
- High recovery rate to verify recovery paths
- No cascading (isolates XID behavior)

```bash
./bin/simulator run scenarios/stress/xid-comprehensive.yaml -v
```

#### cascading-failures.yaml

Focused cascading failure testing:
- High cascade probability (50%)
- Deep cascade chains (depth 5)
- Scheduled outages that trigger cascades
- Tests blast radius containment

```bash
./bin/simulator run scenarios/stress/cascading-failures.yaml -v
```

### Writing custom stress tests

1. Start from an existing stress test scenario
2. Adjust `total_nodes` based on your test requirements
3. Configure templates to match your production fleet
4. Set `failure_rate` based on expected failure patterns
5. Enable/disable features (cascading, recovery, outages) as needed
6. Set a `seed` for reproducible tests
7. Configure report outputs:
   - `report_file` for JSON data (programmatic analysis)
   - `html_report_file` for interactive web visualization
   - `log_file` for detailed debug logs

Tips:
- Start with smaller node counts (100-500) during development
- Use `--seed` for debugging specific failure sequences
- Monitor memory usage for very large fleets (5000+)
- Allow adequate startup time for large fleets
- Use the validate command to check scenario syntax
- Open the HTML report in a browser to visualize results with interactive charts

### Performance considerations

For large-scale stress tests:

| Node Count | Recommended Startup | Memory Usage |
|------------|---------------------|--------------|
| 100-500 | linear, 30s | ~200MB |
| 500-1000 | linear, 1m | ~500MB |
| 1000-2000 | exponential, 2m | ~1GB |
| 2000-5000 | wave, 5m | ~2-3GB |
| 5000+ | wave, 10m+ | ~5GB+ |

## Makefile targets

```bash
# Run interactive mode
make sim

# Run a specific scenario
make sim-run SCENARIO=scenarios/gpu-failure.yaml

# Validate a scenario
make sim-validate SCENARIO=scenarios/basic-fleet.yaml

# Run stress tests
make sim-run SCENARIO=scenarios/stress/1000-node-chaos.yaml
```

