# Navarch node daemon

The Navarch node daemon runs on each GPU instance and handles registration, health monitoring, and command execution. The daemon communicates with the control plane to report node status and GPU health metrics.

## Overview

The node daemon performs the following tasks:

- Registers the node with the control plane on startup.
- Detects and reports GPU device information.
- Sends periodic heartbeats to maintain node liveness.
- Runs health checks (boot, NVML, XID) and reports results.
- Polls for and executes commands from the control plane.

## Installation

Build the node daemon binary:

```bash
go build -o node ./cmd/node
```

## Configuration

The daemon accepts the following command-line flags:

- `--server`: Control plane HTTP address (default: `http://localhost:50051`).
- `--node-id`: Unique identifier for this node (default: hostname).
- `--provider`: Cloud provider name (default: `gcp`).
- `--region`: Cloud region (optional).
- `--zone`: Cloud availability zone (optional).
- `--instance-type`: Instance type (optional).

Environment variables:

- `NAVARCH_GPU_COUNT`: Number of fake GPUs to create when running without hardware (default: `8`).

## Running the daemon

Basic usage:

```bash
./node --server http://control-plane:50051 \
  --node-id node-1 \
  --provider gcp \
  --region us-central1 \
  --zone us-central1-a \
  --instance-type a3-highgpu-8g
```

The daemon automatically detects the hostname if `--node-id` is not provided.

## GPU support

The node daemon uses an abstraction layer (`pkg/gpu`) to support different GPU environments:

### Fake GPU manager (development)

When running without actual GPU hardware, the daemon uses a fake GPU manager that simulates GPU devices. This mode is useful for development and testing.

To configure the number of fake GPUs:

```bash
NAVARCH_GPU_COUNT=4 ./node --server http://localhost:50051 --node-id test-node
```

The fake GPU manager generates realistic device information:

- Device UUIDs and names
- PCI bus IDs
- Memory capacity (80GB per device)
- Temperature and utilization metrics
- Power usage statistics

### Real GPU manager (production)

Production deployments will use the NVML-based GPU manager to interact with actual NVIDIA GPUs. The daemon automatically selects the appropriate implementation based on environment detection.

## Health checks

The daemon runs three types of health checks:

### Boot check

Verifies that GPU devices are detected and accessible. This check runs on startup and periodically to ensure the GPU driver and devices remain available.

Status conditions:

- **Healthy**: All expected GPUs are detected.
- **Unhealthy**: No GPUs detected or device count query fails.

### NVML check

Monitors GPU health metrics through NVML (NVIDIA Management Library):

- Temperature readings
- Power usage
- Memory utilization
- GPU utilization percentage

Status conditions:

- **Healthy**: All GPUs operating within normal parameters.
- **Degraded**: One or more GPUs show elevated temperature (>85°C).
- **Unhealthy**: Unable to query GPU metrics.

### XID check

Scans system logs for GPU XID errors. XID errors indicate hardware issues that may require node replacement.

Status conditions:

- **Healthy**: No XID errors detected.
- **Unhealthy**: One or more XID errors found in logs.

## Health check intervals

The control plane configures health check and heartbeat intervals during registration. Default values:

- Heartbeat interval: 30 seconds
- Health check interval: 60 seconds
- Command poll interval: 10 seconds

## Command execution

The daemon polls the control plane for pending commands and executes them:

- **Cordon**: Mark node as unschedulable.
- **Drain**: Evict workloads and mark unschedulable.
- **Uncordon**: Mark node as schedulable again.

Command execution is currently in development.

## Logging

The daemon uses structured logging (slog) with the following levels:

- **Info**: Normal operational messages (registration, GPU detection).
- **Warn**: Non-fatal issues (failed to detect GPUs, command execution warnings).
- **Error**: Operational failures (health check failures, heartbeat errors).
- **Debug**: Detailed operational information (heartbeat acknowledgments).

Logs are written to stdout in JSON format for easy parsing and ingestion by log aggregation systems.

## Example deployment

Typical deployment on a GCP A3 instance:

```bash
./node \
  --server https://control-plane.example.com \
  --node-id $(hostname) \
  --provider gcp \
  --region us-central1 \
  --zone us-central1-a \
  --instance-type a3-highgpu-8g
```

The daemon will:

1. Initialize the GPU manager (real or fake).
2. Connect to the control plane at the specified address.
3. Register with GPU device information and metadata.
4. Start heartbeat, health check, and command polling loops.
5. Run until interrupted with SIGINT or SIGTERM.

## Graceful shutdown

The daemon handles shutdown signals (SIGINT, SIGTERM) gracefully:

1. Stops all background loops (heartbeat, health checks, command polling).
2. Shuts down the GPU manager.
3. Exits cleanly.

## Troubleshooting

### Node fails to register

Verify the control plane address is correct and reachable:

```bash
curl http://control-plane:50051/healthz
```

Check daemon logs for connection errors.

### Health checks report unhealthy status

Examine the specific health check that failed:

- **Boot check**: Verify GPU driver is loaded (`nvidia-smi`).
- **NVML check**: Check GPU temperatures and utilization.
- **XID check**: Review `dmesg` output for GPU errors.

### No GPUs detected in fake mode

Set the `NAVARCH_GPU_COUNT` environment variable to the desired number of devices.

### Daemon exits immediately

Check for configuration errors:

- Missing `--server` flag or invalid address.
- Missing `--node-id` and unable to determine hostname.

Review daemon logs for initialization errors.

