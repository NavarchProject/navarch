# Simulator package

This package provides a simulation framework for testing Navarch at scale without real GPU infrastructure.

## Overview

The simulator enables:

- Scenario-based testing with YAML configuration.
- Large-scale stress testing with thousands of simulated nodes.
- Chaos engineering with realistic failure patterns.
- Deterministic replay with seed-based randomness.
- Metrics collection and HTML report generation.

## Architecture

```
┌───────────────────────────────────────────────────────────────┐
│                        Runner                                  │
│                                                                │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐   │
│  │ Simulated   │  │   Chaos     │  │   Stress            │   │
│  │   Nodes     │  │   Engine    │  │   Metrics           │   │
│  └──────┬──────┘  └──────┬──────┘  └──────────┬──────────┘   │
│         │                │                     │              │
│         └────────────────┼─────────────────────┘              │
│                          │                                    │
│                    ┌─────┴─────┐                              │
│                    │  Control  │                              │
│                    │   Plane   │                              │
│                    └───────────┘                              │
└───────────────────────────────────────────────────────────────┘
```

## Scenario types

### Regular scenarios

Define explicit events that occur at specific times:

```yaml
name: xid-recovery
description: Test node recovery after XID error
fleet:
  - id: node-1
    gpu_count: 8
    gpu_type: "NVIDIA H100 80GB HBM3"
events:
  - at: 2s
    action: inject_xid
    target: node-1
    xid_code: 79
    gpu_index: 0
  - at: 5s
    action: assert_node_unhealthy
    target: node-1
```

### Stress tests

Generate large fleets and inject random failures:

```yaml
name: gpu-stress-test
stress:
  duration: 30m
  seed: 12345  # Reproducible randomness

  fleet_gen:
    total_nodes: 100
    templates:
      - name: h100-8x
        weight: 70
        gpu_count: 8
        gpu_type: "NVIDIA H100 80GB HBM3"
      - name: a100-4x
        weight: 30
        gpu_count: 4
        gpu_type: "NVIDIA A100 80GB"
    providers:
      lambda: 60
      gcp: 40
    startup:
      pattern: linear
      duration: 5m
      cold_start_mean: 90s
      cold_start_stddev: 30s

  chaos:
    enabled: true
    failure_rate: 0.1  # 10% of nodes experience failures
    xid_distribution:
      - code: 79
        weight: 30
      - code: 119
        weight: 25
      - code: 48
        weight: 20
    recovery:
      enabled: true
      strategy: auto_replace
      delay_mean: 2m
```

## Components

### Runner

The `Runner` executes simulation scenarios:

```go
scenario, err := simulator.LoadScenario("scenario.yaml")
if err != nil {
    log.Fatal(err)
}

runner := simulator.NewRunner(scenario,
    simulator.WithLogger(logger),
    simulator.WithSeed(12345),
    simulator.WithClock(clock.Real()),
)

if err := runner.Run(ctx); err != nil {
    log.Fatal(err)
}
```

### SimulatedNode

A `SimulatedNode` mimics a real node daemon:

- Registers with the control plane.
- Sends periodic heartbeats.
- Reports health events when injected.
- Responds to commands.

### ChaosEngine

The `ChaosEngine` injects realistic failures:

- Weighted XID code distribution.
- Cascading failures (one failure triggers others).
- Correlated failures (failures cluster in time/space).
- Automatic recovery with configurable delays.

### StressMetrics

Collects metrics during stress tests:

- Node counts over time.
- Failure injection rates.
- Recovery times.
- Health status distribution.

## Scenario configuration

### Fleet specification

```yaml
fleet:
  - id: node-1
    provider: lambda
    region: us-west-2
    gpu_count: 8
    gpu_type: "NVIDIA H100 80GB HBM3"
    instance_type: gpu_8x_h100_sxm5
```

### Event types

| Action | Description |
|--------|-------------|
| `inject_xid` | Inject XID error on a GPU |
| `inject_thermal` | Inject thermal event |
| `inject_memory` | Inject memory error |
| `clear_errors` | Clear errors on a node |
| `assert_node_healthy` | Verify node is healthy |
| `assert_node_unhealthy` | Verify node is unhealthy |

### Chaos configuration

```yaml
chaos:
  enabled: true
  failure_rate: 0.1          # Failures per node per hour
  burst_probability: 0.2     # Chance of burst failures
  burst_size_min: 3
  burst_size_max: 10

  xid_distribution:          # Weighted XID codes
    - code: 79               # GPU fell off bus
      weight: 30
    - code: 119              # GSP RPC timeout
      weight: 25
    - code: 48               # Double bit ECC
      weight: 20
    - code: 74               # NVLink error
      weight: 15
    - code: 94               # Contained ECC
      weight: 10

  cascading:                 # Cascading failures
    enabled: true
    probability: 0.3
    max_depth: 3
    same_node_probability: 0.6

  recovery:                  # Automatic recovery
    enabled: true
    strategy: auto_replace   # or: manual, clear_error
    delay_mean: 2m
    delay_stddev: 30s
```

### Startup patterns

| Pattern | Description |
|---------|-------------|
| `instant` | All nodes start immediately |
| `linear` | Nodes start evenly over duration |
| `exponential` | Slow start, accelerating |
| `wave` | Nodes start in batches |

## Reports

Stress tests generate HTML reports with:

- Summary metrics (duration, node count, failure rate).
- Charts showing node health over time.
- Failure event timeline.
- Recovery statistics.

```yaml
stress:
  report_file: results.json       # Machine-readable
  html_report_file: report.html   # Human-readable with charts
  log_file: simulation.log        # Detailed event log
```

## Clock injection

The simulator accepts a `clock.Clock` for time operations:

```go
runner := simulator.NewRunner(scenario,
    simulator.WithClock(fakeClock),
)
```

This enables faster-than-real-time simulation in tests when combined with `clock.NewFakeClock`.

## Running simulations

```bash
# Run a scenario
go run ./cmd/navsim run scenario.yaml

# Run with fixed seed for reproducibility
go run ./cmd/navsim run scenario.yaml --seed 12345

# Run with verbose logging
go run ./cmd/navsim run scenario.yaml --verbose
```

## Testing

```bash
go test ./pkg/simulator/... -v
```

The test suite covers:

- Scenario parsing and validation.
- Event execution ordering.
- Fleet generation.
- Chaos engine failure injection.
- Health assertion evaluation.
