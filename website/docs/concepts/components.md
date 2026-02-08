# Components

Navarch has two main components: the control plane and the node agent.

## Control plane

The control plane is the central management server for your GPU fleet. It:

- Receives health reports from node agents.
- Tracks node status and lifecycle state.
- Manages node pools and autoscaling.
- Issues commands to nodes (cordon, drain, terminate).
- Provides an API for the CLI and external integrations.

There is one control plane per Navarch deployment. All nodes connect to it.

### Running the control plane

```bash
control-plane -config navarch.yaml
```

The control plane exposes:

- **gRPC API** (default port 50051): For node agents and programmatic access.
- **HTTP API** (default port 8080): For the CLI and health checks.

See [Configuration Reference](../configuration.md) for all available options.

## Node agent

The node agent runs on each GPU instance. It:

- Registers the node with the control plane at startup.
- Sends periodic heartbeats to prove liveness.
- Runs health checks and reports results.
- Receives and executes commands from the control plane.

The node agent does not manage its own lifecycle. It reports status and follows commands. The control plane decides when to terminate nodes.

### Running the node agent

```bash
node-agent --server control-plane.example.com:50051
```

The agent needs:

- Network access to the control plane.
- Access to NVIDIA drivers (for GPU health checks).
- Permission to read GPU metrics via NVML.

See [Deployment](../deployment.md) for production setup including systemd configuration.

## Communication flow

```
┌─────────────┐                      ┌───────────────┐
│ Node Agent  │ ───── Register ────► │               │
│             │                      │ Control Plane │
│             │ ◄──── Commands ───── │               │
│             │                      │               │
│             │ ── Heartbeat/Health► │               │
└─────────────┘                      └───────────────┘
```

1. **Registration**: On startup, the node agent calls `RegisterNode` with its metadata (provider, region, GPU info).

2. **Heartbeats**: Every 30 seconds (configurable), the agent sends a heartbeat with current metrics.

3. **Health reports**: With each heartbeat, the agent includes health check results.

4. **Commands**: The control plane can send commands (cordon, drain) which the agent executes.

## High availability

For production deployments:

- Run multiple control plane replicas behind a load balancer.
- Use a shared database backend for state.
- Node agents reconnect automatically if the control plane restarts.

See [Deployment](../deployment.md) for production setup details.
