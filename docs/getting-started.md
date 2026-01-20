# Getting started

This guide walks you through setting up Navarch for local development and testing.

## Prerequisites

- Go 1.21 or later.
- Git for cloning the repository.

## Installation

To clone and build Navarch:

```bash
git clone https://github.com/NavarchProject/navarch.git
cd navarch
go build ./...
```

This creates the following binaries:

- `control-plane` - The central management server.
- `navarch` - The command-line interface.
- `node` - The node agent (runs on GPU instances).
- `simulator` - A testing tool for simulating GPU fleets.

## Quick start with fake provider

The fake provider simulates GPU instances without cloud costs. Use it for local development and testing.

### Step 1: Create a configuration file

Create `navarch.yaml`:

```yaml
apiVersion: navarch.io/v1alpha1
kind: ControlPlane
metadata:
  name: dev
spec:
  address: ":50051"
  autoscaleInterval: 10s
---
apiVersion: navarch.io/v1alpha1
kind: Provider
metadata:
  name: fake
spec:
  type: fake
  fake:
    gpuCount: 8
---
apiVersion: navarch.io/v1alpha1
kind: Pool
metadata:
  name: dev-pool
spec:
  providerRef: fake
  instanceType: gpu_8x_h100
  region: local
  scaling:
    minReplicas: 2
    maxReplicas: 5
    cooldownPeriod: 10s
    autoscaler:
      type: reactive
      scaleUpThreshold: 80
      scaleDownThreshold: 20
  health:
    unhealthyThreshold: 2
    autoReplace: true
```

### Step 2: Start the control plane

```bash
./control-plane --config navarch.yaml
```

The control plane starts and provisions two fake nodes (the `minReplicas` value).

### Step 3: List nodes

In a new terminal:

```bash
./navarch list
```

Output:

```
┌───────────┬──────────┬────────┬──────┬───────────────┬────────┬─────────┬────────────────┬──────┐
│ Node ID   │ Provider │ Region │ Zone │ Instance Type │ Status │ Health  │ Last Heartbeat │ GPUs │
├───────────┼──────────┼────────┼──────┼───────────────┼────────┼─────────┼────────────────┼──────┤
│ fake-1    │ fake     │ local  │      │ gpu_8x_h100   │ Active │ Healthy │ 5s ago         │ 8    │
│ fake-2    │ fake     │ local  │      │ gpu_8x_h100   │ Active │ Healthy │ 5s ago         │ 8    │
└───────────┴──────────┴────────┴──────┴───────────────┴────────┴─────────┴────────────────┴──────┘
```

### Step 4: Manage nodes

To cordon a node (prevent new workloads):

```bash
./navarch cordon fake-1
```

To drain a node (evict workloads and cordon):

```bash
./navarch drain fake-1
```

To view node details:

```bash
./navarch get fake-1
```

## Next steps

To connect real cloud providers, see the [configuration reference](configuration.md).

To learn about autoscaling strategies, see [pool management](pool-management.md).

To deploy in production, see the [deployment guide](deployment.md).

## Using the simulator

The simulator tests Navarch behavior without running the full system. It uses scenario files to define fleet configurations and events.

### Run a scenario

```bash
./simulator --scenario examples/scenarios/basic-fleet.yaml
```

### Interactive mode

Run the simulator in interactive mode to test CLI commands:

```bash
./simulator --scenario examples/scenarios/basic-fleet.yaml --interactive
```

Then use the CLI in another terminal:

```bash
./navarch -s http://localhost:8080 list
```

For more information, see [simulator documentation](simulator.md).

## Troubleshooting

### Connection refused

If `navarch list` returns "connection refused":

1. Verify the control plane is running.
2. Check the address matches (default is `http://localhost:50051`).
3. Use the `-s` flag to specify a different address.

### No nodes appear

If nodes do not appear after starting the control plane:

1. Check the control plane logs for errors.
2. Verify the pool configuration has `minReplicas` greater than zero.
3. Confirm the provider is configured correctly.

### Build errors

If the build fails:

1. Verify Go 1.21 or later is installed: `go version`
2. Run `go mod download` to fetch dependencies.
3. Check for missing NVML headers if building with GPU support.

