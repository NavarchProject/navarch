# Stress Testing

The simulator includes a comprehensive stress testing framework for validating system behavior at scale with realistic failure patterns.

## Overview

Stress tests allow you to:

- Simulate thousands of nodes simultaneously
- Inject failures with realistic distributions based on production data
- Test cascading failure scenarios
- Simulate scheduled outages (zone, region, provider)
- Measure system resilience and recovery
- Generate detailed reports

## Running stress tests

```bash
# Run a stress test
./bin/simulator run scenarios/stress/1000-node-chaos.yaml -v

# Run with specific seed for reproducibility
./bin/simulator run scenarios/stress/1000-node-chaos.yaml --seed 12345 -v

# Validate before running
./bin/simulator validate scenarios/stress/5000-node-extreme.yaml
```

## Configuration

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
  html_report_file: stress-report.html
  log_file: stress-report.log

  fleet_gen:
    # Fleet generation config...

  chaos:
    # Chaos engineering config...
```

## Fleet generation

Instead of defining individual nodes, generate fleets from templates:

```yaml
fleet_gen:
  total_nodes: 1000

  templates:
    - name: h100-8gpu
      weight: 60
      gpu_count: 8
      gpu_type: "NVIDIA H100 80GB HBM3"
      instance_type: a3-highgpu-8g
      labels:
        tier: premium
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
```

| Field | Description |
|-------|-------------|
| `total_nodes` | Total number of nodes to generate |
| `templates` | Node templates with relative weights |
| `providers` | Provider distribution (percentages) |
| `regions` | Region distribution (percentages) |
| `startup` | How nodes join the cluster |

### Startup patterns

| Pattern | Description |
|---------|-------------|
| `instant` | All nodes start immediately |
| `linear` | Nodes start at constant rate |
| `exponential` | Start slow, accelerate (1, 2, 4, 8, ...) |
| `wave` | Start in batches with pauses |

```yaml
startup:
  pattern: wave
  duration: 5m
  batch_size: 100
  jitter_percent: 20
  cold_start_min: 30s
  cold_start_max: 2m
```

Cold start delays simulate provisioning time. Use `cold_start_min`/`cold_start_max` for uniform distribution, or `cold_start_mean`/`cold_start_stddev` for normal distribution.

## Chaos engineering

Control failure injection:

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
```

### Failure rate

Failures per minute per 1000 nodes:
- 1000 nodes with rate 10.0 = ~10 failures/minute
- 5000 nodes with rate 10.0 = ~50 failures/minute

### Failure types

| Type | Description |
|------|-------------|
| `xid_error` | GPU XID error with specified distribution |
| `temperature` | Thermal throttling/shutdown |
| `backend_error` | GPU backend failure (alias: `nvml_failure`) |
| `boot_failure` | GPU boot/detection failure |
| `network` | Network connectivity loss |
| `memory_error` | ECC memory error |
| `nvlink_error` | NVLink communication error |

## Cascading failures

Simulate realistic failure propagation:

```yaml
cascading:
  enabled: true
  probability: 0.15      # 15% chance a failure cascades
  max_depth: 3           # Maximum cascade chain length
  min_delay: 1s
  max_delay: 10s
  scope: zone            # Cascade scope
  max_affected_percent: 0.1
```

**Cascade scopes:**

| Scope | Description |
|-------|-------------|
| `rack` | Same rack (first 3 node ID segments match) |
| `zone` | Same availability zone |
| `region` | Same region |
| `provider` | Same cloud provider |
| `random` | Any node in cluster |

## Automatic recovery

Configure recovery for non-fatal failures:

```yaml
recovery:
  enabled: true
  probability: 0.7      # 70% of non-fatal errors recover
  mean_time: 5m
  std_dev: 2m
  replace_fatal: true   # Replace nodes with fatal errors
  replace_cold_start: 45s
```

Recovery only applies to non-fatal XID codes and recoverable failure types.

## Scheduled outages

Simulate planned or unplanned outages:

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
    target: "10"
    failure_type: temperature
```

**Outage scopes:** `zone`, `region`, `provider`, `percentage`

## Correlated failures

Define failures that trigger related failures:

```yaml
correlated_failures:
  - name: nvlink-gpu-cascade
    trigger: "74"           # NVLink error triggers this
    response: xid_error
    probability: 0.6
    delay: 1s
    scope: same_node

  - name: thermal-propagation
    trigger: temperature
    response: temperature
    probability: 0.4
    delay: 3s
    scope: same_rack
```

**Correlation scopes:** `same_node`, `same_rack`, `same_zone`, `random`

## Reports

Configure report outputs:

```yaml
stress:
  report_file: stress-report.json
  html_report_file: stress-report.html
  log_file: stress-report.log
```

### HTML report

Interactive web visualization with:

- **Results tab**: Summary statistics, failure breakdowns, interactive charts
  - Node health over time
  - Failures vs recoveries
  - XID error distribution (pie chart)
  - Failure types breakdown (bar chart)
- **Configuration tab**: Full test configuration

### JSON report

Structured data for programmatic analysis:

```json
{
  "name": "1000-node-chaos-test",
  "duration": "10m0s",
  "summary": {
    "nodes_started": 1000,
    "peak_healthy_nodes": 1000,
    "min_healthy_nodes": 847,
    "total_failures": 98,
    "total_recoveries": 45
  },
  "failures": {
    "by_type": {"xid_error": 68, "temperature": 12},
    "by_xid": {"31": 15, "79": 8, "48": 7},
    "cascading_failures": 12
  }
}
```

### Log file

Verbose debug output from all components. Useful for:

- Debugging specific failure sequences
- Post-mortem investigation
- Providing context to LLMs for analysis

## Example scenarios

### 1000-node-chaos.yaml

Standard chaos test:
- 10 minute duration
- Mixed H100/A100 fleet across GCP, AWS, Lambda
- Realistic XID distribution
- Cascading failures enabled
- Automatic recovery

```bash
./bin/simulator run scenarios/stress/1000-node-chaos.yaml -v
```

### 5000-node-extreme.yaml

Extreme stress test:
- 30 minute duration
- 5000 nodes across 8 regions
- Aggressive failure rate (50/min/1000 nodes)
- Multiple scheduled outages
- High cascade probability

### xid-comprehensive.yaml

XID error testing:
- All known XID codes tested equally
- High recovery rate
- No cascading (isolates XID behavior)

### cascading-failures.yaml

Cascade testing:
- High cascade probability (50%)
- Deep cascade chains (depth 5)
- Scheduled outages that trigger cascades
- Tests blast radius containment

## Performance considerations

| Node Count | Recommended Startup | Memory Usage |
|------------|---------------------|--------------|
| 100-500 | linear, 30s | ~200MB |
| 500-1000 | linear, 1m | ~500MB |
| 1000-2000 | exponential, 2m | ~1GB |
| 2000-5000 | wave, 5m | ~2-3GB |
| 5000+ | wave, 10m+ | ~5GB+ |

**Tips:**

- Start with smaller node counts (100-500) during development
- Use `--seed` for debugging specific failure sequences
- Monitor memory usage for very large fleets
- Allow adequate startup time for large fleets
- Use validate command to check scenario syntax
