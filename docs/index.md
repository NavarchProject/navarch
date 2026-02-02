# Navarch

Navarch is an open-source GPU fleet management system that monitors health, manages lifecycle, and orchestrates GPU nodes across cloud providers.

## What Navarch does

Navarch provides a unified control plane for managing GPU infrastructure:

- Monitors GPU health using health events and CEL-based policy evaluation.
- Manages node lifecycle with cordon, drain, and terminate operations.
- Autoscales node pools based on utilization, queue depth, or schedules.
- Supports multiple cloud providers (Lambda Labs, GCP, AWS).
- Replaces unhealthy nodes automatically.

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                       Control Plane                              │
│                                                                  │
│  ┌───────────┐  ┌─────────────┐  ┌──────────────┐              │
│  │ API       │  │ Pool        │  │ Health       │              │
│  │ Server    │  │ Manager     │  │ Monitor      │              │
│  └───────────┘  └─────────────┘  └──────────────┘              │
└──────────────────────────┬──────────────────────────────────────┘
                           │
         ┌─────────────────┼─────────────────┐
         │                 │                 │
         ▼                 ▼                 ▼
┌─────────────┐   ┌─────────────┐   ┌─────────────┐
│   Node 1    │   │   Node 2    │   │   Node N    │
│  ┌───────┐  │   │  ┌───────┐  │   │  ┌───────┐  │
│  │ Agent │  │   │  │ Agent │  │   │  │ Agent │  │
│  └───────┘  │   │  └───────┘  │   │  └───────┘  │
│   8x H100   │   │   8x H100   │   │   8x H100   │
└─────────────┘   └─────────────┘   └─────────────┘
```

The control plane manages all nodes. Node agents report health and receive commands but do not manage their own lifecycle.

## Key concepts

Pools group GPU nodes with shared configuration:

- Same cloud provider and instance type.
- Common scaling limits and autoscaler strategy.
- Unified health policies.

Providers abstract cloud-specific operations:

- Provisioning and terminating instances.
- Listing available instance types.
- Managing SSH keys and startup scripts.

Health checks detect GPU issues:

- Boot validation confirms the node started correctly.
- GPU checks verify driver communication and metrics.
- Health event monitoring catches XID errors, thermal issues, and other faults.

## Documentation

To get started quickly, see the [getting started guide](getting-started.md).

To understand the fundamental building blocks, see [core concepts](concepts.md).

To understand how Navarch works and integrates with Kubernetes, see [architecture](architecture.md).

To understand configuration options, see the [configuration reference](configuration.md).

To learn about pool management and autoscaling, see [pool management](pool-management.md).

To set up a production deployment, see the [deployment guide](deployment.md).

To use the command-line interface, see the [CLI reference](cli.md).

To test scenarios without real hardware, see the [simulator guide](simulator.md).

## Requirements

- Go 1.21 or later for building from source.
- NVIDIA drivers for GPU health monitoring.
- Network access between nodes and the control plane.

## Source code

Navarch is open source and available on GitHub:

https://github.com/NavarchProject/navarch

