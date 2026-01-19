# Navarch control plane

The Navarch control plane is the central coordination service that manages the GPU fleet. It handles node registration, tracks node health, stores state, and issues commands to nodes.

## Overview

The control plane provides the following functionality:

- Node registration and lifecycle management.
- Health check result aggregation and status tracking.
- Heartbeat monitoring to detect unresponsive nodes.
- Command issuance (cordon, drain, uncordon).
- Fleet-wide node listing and filtering.
- RESTful API for CLI and external integrations.

## Installation

Build the control plane binary:

```bash
go build -o control-plane ./cmd/control-plane
```

## Configuration

The control plane accepts the following command-line flags:

- `--addr`: HTTP server address (default: `:50051`).
- `--health-check-interval`: Default health check interval in seconds (default: `60`).
- `--heartbeat-interval`: Default heartbeat interval in seconds (default: `30`).
- `--shutdown-timeout`: Graceful shutdown timeout in seconds (default: `30`).

Example:

```bash
./control-plane --addr :8080 --health-check-interval 120 --heartbeat-interval 45
```

## Running the control plane

Start the control plane with default settings:

```bash
./control-plane
```

The server listens on `:50051` and is ready to accept node registrations.

Start with custom configuration:

```bash
./control-plane \
  --addr :8080 \
  --health-check-interval 90 \
  --heartbeat-interval 30 \
  --shutdown-timeout 60
```

## API endpoints

The control plane exposes the following HTTP endpoints:

### Health endpoints

- `GET /healthz`: Basic health check, returns `200 OK` if running.
- `GET /readyz`: Readiness check, returns `200 OK` if database is operational.

These endpoints support both GET and HEAD requests for use with load balancers and orchestration systems.

### gRPC/Connect API

The control plane uses Connect (gRPC-compatible HTTP/2) for the main API. All RPC methods are available at `/navarch.controlplane.v1.ControlPlaneService/`.

Supported operations:

- `RegisterNode`: Register a node with the fleet.
- `SendHeartbeat`: Report node liveness.
- `ReportHealth`: Submit health check results.
- `GetNodeCommands`: Retrieve pending commands for a node.
- `ListNodes`: List all nodes with optional filtering.
- `GetNode`: Get detailed information about a specific node.
- `IssueCommand`: Issue a command (cordon, drain) to a node.

## Database

The control plane currently uses an in-memory database (`pkg/controlplane/db/inmem.go`) suitable for development and testing. The database stores:

- Node registration information.
- GPU device details.
- Node metadata (hostname, IP addresses).
- Health check results and history.
- Issued commands and their status.

The in-memory database does not persist data across restarts. For production deployments, implement the `db.DB` interface with a persistent storage backend.

## Node lifecycle

The control plane manages nodes through the following lifecycle:

1. **Registration**: Node sends registration request with GPU and metadata.
2. **Active**: Node sends periodic heartbeats and health check results.
3. **Cordoned**: Node marked unschedulable (manual or automated).
4. **Draining**: Node evicting workloads before termination.
5. **Terminated**: Node removed from the fleet.

The control plane automatically updates node health status based on reported health check results.

## Health status calculation

The control plane aggregates individual health check results to determine overall node health:

- **Healthy**: All health checks pass.
- **Degraded**: One or more checks report degraded status.
- **Unhealthy**: One or more checks fail (node automatically cordoned).
- **Unknown**: No health checks reported yet.

When a node becomes unhealthy, the control plane automatically updates the node status to `UNHEALTHY`.

## Configuration delivery

When a node registers, the control plane returns configuration:

- Health check interval (how often to run checks).
- Heartbeat interval (how often to report liveness).
- Enabled health checks (boot, nvml, xid).

This allows centralized control of monitoring behavior across the fleet.

## Command system

The control plane implements a command queue for each node:

1. CLI or automation issues a command via `IssueCommand`.
2. Control plane creates a command record with status `pending`.
3. Node polls via `GetNodeCommands` and receives pending commands.
4. Command status updates to `acknowledged`.
5. Node executes command and reports completion (future work).

Commands include:

- **Cordon**: Prevent new workload scheduling on the node.
- **Drain**: Evict existing workloads and cordon the node.
- **Uncordon**: Allow workload scheduling again (not implemented).

## Logging

The control plane uses structured logging (slog) in JSON format:

- **Info**: Node registrations, command issuance, startup/shutdown.
- **Warn**: Unexpected conditions (unregistered node heartbeat).
- **Error**: Operational failures (database errors, RPC failures).
- **Debug**: Detailed operational information (heartbeat acknowledgments, command polling).

All logs are written to stdout for easy ingestion by log aggregation systems.

## Graceful shutdown

The control plane handles shutdown signals (SIGINT, SIGTERM) gracefully:

1. Stops accepting new HTTP requests.
2. Waits for in-flight requests to complete (up to shutdown timeout).
3. Closes database connections.
4. Exits cleanly.

This ensures no data loss or incomplete operations during shutdown.

## HTTP/2 and TLS

The control plane uses HTTP/2 via the h2c (HTTP/2 Cleartext) protocol, which provides the performance benefits of HTTP/2 without requiring TLS.

For production deployments, configure TLS by:

1. Modifying the HTTP server configuration to enable TLS.
2. Providing TLS certificates and keys.
3. Updating node daemon `--server` URLs to use `https://`.

## Example deployment

Basic deployment:

```bash
./control-plane --addr :50051
```

Production deployment with custom settings:

```bash
./control-plane \
  --addr :443 \
  --health-check-interval 120 \
  --heartbeat-interval 60 \
  --shutdown-timeout 30
```

## Monitoring

Monitor control plane health using the health endpoints:

```bash
# Basic health check
curl http://localhost:50051/healthz

# Readiness check (verifies database)
curl http://localhost:50051/readyz
```

Integrate these endpoints with:

- Kubernetes liveness and readiness probes.
- Load balancer health checks.
- External monitoring systems.

## Troubleshooting

### Control plane fails to start

Check for port conflicts:

```bash
lsof -i :50051
```

Verify the address format is correct (`:port` or `host:port`).

### Nodes fail to register

Verify the control plane is reachable:

```bash
curl http://localhost:50051/healthz
```

Check control plane logs for connection errors or rejected registrations.

### Readiness check fails

The readiness check queries the database. Verify database initialization succeeded by checking control plane logs for database errors.

### High memory usage

The in-memory database stores all node and health check data. For large fleets, implement a persistent database backend that can paginate and archive historical data.

## Development

Run tests:

```bash
go test ./cmd/control-plane/...
```

Run the control plane locally:

```bash
go run ./cmd/control-plane
```

The control plane is ready for connections when it logs:

```
{"level":"INFO","msg":"control plane ready","addr":":50051"}
```

