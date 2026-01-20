# Test scripts

This directory contains test scripts for validating Navarch on GPU instances.

## Scripts

### stress-test.sh

Comprehensive system stress test using the fake provider. Does not require GPU hardware.

```bash
./scripts/stress-test.sh
```

What it does:

1. Creates a test configuration with multiple pools.
2. Starts control plane with autoscaling enabled.
3. Tests initial node provisioning (16 nodes across 4 pools).
4. Runs 100 concurrent list operations.
5. Tests concurrent get operations on all nodes.
6. Tests cordon and drain operations.
7. Monitors autoscaler behavior.
8. Runs sustained load test (157+ requests).
9. Checks memory usage and logs for errors.

This test validates the control plane, pool manager, autoscaler, and CLI under load without requiring GPU hardware.

### test-gpu.sh

Basic GPU test that validates NVIDIA driver and runs all GPU-related tests.

```bash
./scripts/test-gpu.sh
```

What it does:

1. Checks for NVIDIA GPU and driver.
2. Runs GPU package unit tests.
3. Runs all project tests.

### test-xid-parsing.sh

Tests XID error parsing with the unit test suite.

```bash
./scripts/test-xid-parsing.sh
```

What it does:

1. Runs XID parsing tests with synthetic log entries.
2. Validates severity classification.
3. Tests fatal error detection.

### test-e2e.sh

Full end-to-end test that starts the control plane and node daemon.

```bash
./scripts/test-e2e.sh
```

What it does:

1. Builds all binaries.
2. Starts control plane.
3. Starts node daemon with real NVML.
4. Verifies node registration.
5. Waits for health checks.
6. Tests cordon command delivery.
7. Cleans up on exit.

### stress-gpu.sh

GPU stress test with health monitoring.

```bash
./scripts/stress-gpu.sh [duration_seconds]
```

What it does:

1. Shows initial GPU state.
2. Monitors GPU metrics every 10 seconds.
3. Runs nvidia-smi dmon for specified duration.
4. Checks for XID errors in dmesg.
5. Shows final GPU state.

Default duration is 60 seconds.

## Usage on a GPU instance

Copy the navarch directory to your GPU instance and run:

```bash
# Basic test
./scripts/test-gpu.sh

# End-to-end test
./scripts/test-e2e.sh

# Stress test for 5 minutes
./scripts/stress-gpu.sh 300
```

## Requirements

- NVIDIA GPU with driver installed
- Go 1.21 or later
- Root access for dmesg reading (stress test)

