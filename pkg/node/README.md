# Node package

This package implements the Navarch node daemon that runs on each GPU node.

## Overview

The node daemon is responsible for:

- Registering with the control plane on startup.
- Sending periodic heartbeats with metrics.
- Running health checks and reporting results.
- Collecting GPU health events for CEL policy evaluation.
- Executing commands from the control plane.

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                     Node Daemon                          │
│                                                          │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐     │
│  │ Heartbeat   │  │ Health      │  │ Command     │     │
│  │ Loop        │  │ Check Loop  │  │ Poll Loop   │     │
│  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘     │
│         │                │                │             │
│         └────────────────┼────────────────┘             │
│                          │                              │
│                    ┌─────┴─────┐                        │
│                    │ gRPC      │                        │
│                    │ Client    │                        │
│                    └─────┬─────┘                        │
└──────────────────────────┼──────────────────────────────┘
                           │
                           ▼
                    Control Plane
```

## Configuration

```go
type Config struct {
    ControlPlaneAddr string            // Required: control plane address
    NodeID           string            // Required: unique node identifier
    Provider         string            // Cloud provider (gcp, aws, lambda)
    Region           string            // Cloud region
    Zone             string            // Availability zone
    InstanceType     string            // Instance type
    Labels           map[string]string // User-defined labels
    GPU              gpu.Manager       // GPU manager (nil = auto-detect)
    Clock            clock.Clock       // For testing (nil = real time)
}
```

## Usage

```go
import "github.com/NavarchProject/navarch/pkg/node"

cfg := node.Config{
    ControlPlaneAddr: "http://control-plane:8080",
    NodeID:           os.Getenv("NODE_ID"),
    Provider:         "gcp",
    Region:           "us-central1",
    Zone:             "us-central1-a",
    InstanceType:     "a3-highgpu-8g",
}

n, err := node.New(cfg, logger)
if err != nil {
    log.Fatal(err)
}

ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
defer cancel()

if err := n.Run(ctx); err != nil && err != context.Canceled {
    log.Fatal(err)
}
```

## Startup sequence

1. Initialize GPU manager (detect GPUs).
2. Connect to control plane.
3. Send `RegisterNode` request with GPU info.
4. Receive configuration (health check interval, heartbeat interval).
5. Start background loops.

## Background loops

### Heartbeat loop

Sends periodic heartbeats with current metrics:

- CPU and memory usage.
- GPU utilization and memory.
- Temperature and power consumption.

The control plane uses heartbeats to detect node liveness.

### Health check loop

Runs configured health checks and reports results:

- **Boot check**: Validates node started correctly.
- **GPU check**: Queries GPU driver and metrics.
- **Health event check**: Collects GPU health events (XID errors, thermal events, etc.).

Health events are sent to the control plane where CEL policies evaluate them.

### Command poll loop

Polls for pending commands from the control plane:

- **Cordon**: Stop accepting new workloads.
- **Drain**: Wait for running workloads to complete.
- **Terminate**: Shut down the node.
- **Run diagnostic**: Execute diagnostic commands.

## Command handling

Register custom command handlers:

```go
n.RegisterCommandHandler(pb.NodeCommandType_NODE_COMMAND_TYPE_RUN_DIAGNOSTIC,
    func(ctx context.Context, cmd *pb.NodeCommand) error {
        // Run diagnostic and return result
        return runDiagnostic(cmd.Parameters["type"])
    })
```

## Metrics collection

The node collects metrics via the `metrics.Collector` interface:

```go
type Collector interface {
    Collect(ctx context.Context) (*pb.NodeMetrics, error)
}
```

Metrics include:

- System metrics (CPU, memory, disk).
- GPU metrics (utilization, memory, temperature, power).

## Retry behavior

Network operations use exponential backoff retry:

- Registration retries on connection failure.
- Heartbeats retry with short delays.
- Health reports retry on transient errors.

## Testing

Use a mock GPU manager and FakeClock for testing:

```go
gpu := gpu.NewInjectable(8, "")
clk := clock.NewFakeClock(time.Now())

cfg := node.Config{
    ControlPlaneAddr: "http://localhost:8080",
    NodeID:           "test-node",
    GPU:              gpu,
    Clock:            clk,
}

n, _ := node.New(cfg, nil)
```

Inject GPU failures to test health reporting:

```go
gpu.InjectXIDHealthEvent(0, 79, "GPU fell off bus")
// Health check will detect and report the event
```

## Testing

```bash
go test ./pkg/node/... -v
```
