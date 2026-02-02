# Navarch CLI reference

The Navarch CLI is a command-line tool for managing your GPU fleet across cloud providers.

## Installation

```bash
# From source
git clone https://github.com/NavarchProject/navarch.git
cd navarch
make build
sudo cp bin/navarch /usr/local/bin/

# Or using Go
go install github.com/NavarchProject/navarch/cmd/navarch@latest
```

## Configuration

The CLI communicates with the Navarch control plane via HTTP. You can configure the control plane address using any of these methods, in order of precedence:

1. **Command-line flag** (highest priority): `--server` or `-s`
2. **Environment variable**: `NAVARCH_SERVER`
3. **Default value** (lowest priority): `http://localhost:50051`

### Global flags

All commands support these flags:

```
-s, --server string      Control plane address (default "http://localhost:50051")
--insecure               Skip TLS certificate verification
-o, --output string      Output format: table, json (default "table")
--timeout duration       Request timeout (default 30s)
-h, --help               Show help for any command
```

### Examples

Connect to a remote control plane using the flag:

```bash
navarch -s https://navarch.example.com list
```

Set the control plane address using an environment variable:

```bash
export NAVARCH_SERVER=https://navarch.example.com
navarch list
```

Override the environment variable with a flag:

```bash
export NAVARCH_SERVER=https://prod.example.com
navarch -s https://staging.example.com list  # Uses staging
```

Get JSON output for scripting:

```bash
navarch list -o json | jq '.[] | select(.status == "ACTIVE")'
```

## Commands

### `navarch list`

List all nodes in your fleet.

Usage:

```bash
navarch list [flags]
```

Flags:

```
--provider string   Filter by cloud provider (gcp, aws, azure)
--region string     Filter by region (us-central1, us-east-1, etc.)
--status string     Filter by status (active, cordoned, draining, terminated)
```

Examples:

To list all nodes:
```bash
$ navarch list
┌─────────────┬──────────┬─────────────┬───────────────┬───────────────┬────────┬─────────┬────────────────┬──────┐
│ Node ID     │ Provider │ Region      │ Zone          │ Instance Type │ Status │ Health  │ Last Heartbeat │ GPUs │
│ node-gcp-1  │ gcp      │ us-central1 │ us-central1-a │ a3-highgpu-8g │ Active │ Healthy │ 30s ago        │ 8    │
│ node-gcp-2  │ gcp      │ us-west1    │ us-west1-b    │ a3-highgpu-8g │ Active │ Healthy │ 45s ago        │ 8    │
│ node-aws-1  │ aws      │ us-east-1   │ us-east-1a    │ p5.48xlarge   │ Active │ Healthy │ 1m ago         │ 8    │
└─────────────┴──────────┴─────────────┴───────────────┴───────────────┴────────┴─────────┴────────────────┴──────┘
```

To filter by provider:
```bash
$ navarch list --provider gcp
┌─────────────┬──────────┬─────────────┬───────────────┬───────────────┬────────┬─────────┬────────────────┬──────┐
│ Node ID     │ Provider │ Region      │ Zone          │ Instance Type │ Status │ Health  │ Last Heartbeat │ GPUs │
│ node-gcp-1  │ gcp      │ us-central1 │ us-central1-a │ a3-highgpu-8g │ Active │ Healthy │ 30s ago        │ 8    │
│ node-gcp-2  │ gcp      │ us-west1    │ us-west1-b    │ a3-highgpu-8g │ Active │ Healthy │ 45s ago        │ 8    │
└─────────────┴──────────┴─────────────┴───────────────┴───────────────┴────────┴─────────┴────────────────┴──────┘
```

To filter by region:

```bash
$ navarch list --region us-central1
```

To get JSON output:
```bash
$ navarch list -o json
[
  {
    "node_id": "node-gcp-1",
    "provider": "gcp",
    "region": "us-central1",
    "zone": "us-central1-a",
    "instance_type": "a3-highgpu-8g",
    "status": "NODE_STATUS_ACTIVE",
    "health_status": "HEALTH_STATUS_HEALTHY",
    "last_heartbeat": "2026-01-19T14:00:00Z",
    "gpus": [...]
  }
]
```

To combine filters:
```bash
$ navarch list --provider gcp --region us-central1 --status NODE_STATUS_ACTIVE
```

---

### `navarch get`

Returns detailed information about a specific node.

Usage:

```bash
navarch get <node-id> [flags]
```

Examples:

To get node details:
```bash
$ navarch get node-gcp-1
Node ID:       node-gcp-1
Provider:      gcp
Region:        us-central1
Zone:          us-central1-a
Instance Type: a3-highgpu-8g
Status:        Active
Health:        Healthy
Last Heartbeat: 30s ago

GPUs:
  GPU 0:
    UUID:       GPU-12345678-1234-1234-1234-123456789abc
    Name:       NVIDIA H100 80GB HBM3
    PCI Bus ID: 0000:00:04.0
  GPU 1:
    UUID:       GPU-87654321-4321-4321-4321-cba987654321
    Name:       NVIDIA H100 80GB HBM3
    PCI Bus ID: 0000:00:05.0
  ... (6 more GPUs)

Metadata:
  Hostname:    node-gcp-1.c.project.internal
  Internal IP: 10.128.0.2
  External IP: 34.123.45.67
```

To get JSON output:

```bash
$ navarch get node-gcp-1 -o json
{
  "node_id": "node-gcp-1",
  "provider": "gcp",
  "region": "us-central1",
  "zone": "us-central1-a",
  "instance_type": "a3-highgpu-8g",
  "status": "NODE_STATUS_ACTIVE",
  "health_status": "HEALTH_STATUS_HEALTHY",
  "last_heartbeat": "2026-01-19T14:00:00Z",
  "gpus": [...],
  "metadata": {...}
}
```

---

### `navarch cordon`

Marks a node as unschedulable. This prevents new workloads from being scheduled on the node but does not affect existing workloads.

Usage:

```bash
navarch cordon <node-id>
```

Examples:

To cordon a node:
```bash
$ navarch cordon node-gcp-1
Node node-gcp-1 cordoned successfully
Command ID: a1b2c3d4-e5f6-7890-abcd-ef1234567890
```

To verify the node is cordoned:
```bash
$ navarch get node-gcp-1
Node ID:       node-gcp-1
Provider:      gcp
Region:        us-central1
Zone:          us-central1-a
Instance Type: a3-highgpu-8g
Status:        Cordoned
Health:        Healthy
Last Heartbeat: 1m ago
```

When to use this command:

- Before you perform maintenance on a node.
- When you suspect a node may have issues but want to observe it.
- To prevent scheduling on a node without disrupting running workloads.

---

### `navarch drain`

Drains a node by evicting workloads and marking it unschedulable. This is a more forceful operation than cordoning.

Usage:

```bash
navarch drain <node-id>
```

Examples:

To drain a node:
```bash
$ navarch drain node-gcp-1
Node node-gcp-1 draining
Command ID: b2c3d4e5-f6a7-8901-bcde-f12345678901
```

When to use this command:

- Before you decommission a node.
- When a node is unhealthy and workloads need to be moved.
- For planned downtime or upgrades.

The drain operation performs the following steps:
1. Marks the node as unschedulable (like cordon).
2. Evicts all running workloads.
3. Transitions the node to `DRAINING` status.

---

### `navarch uncordon`

Marks a cordoned node as schedulable again. This reverses the effect of `cordon`.

Usage:

```bash
navarch uncordon <node-id>
```

Examples:

To uncordon a node:
```bash
$ navarch uncordon node-gcp-1
Node node-gcp-1 uncordoned successfully
Command ID: c3d4e5f6-a7b8-9012-cdef-234567890123
```

To verify the node is schedulable again:
```bash
$ navarch get node-gcp-1
Node ID:       node-gcp-1
Provider:      gcp
Region:        us-central1
Zone:          us-central1-a
Instance Type: a3-highgpu-8g
Status:        Active
Health:        Healthy
Last Heartbeat: 30s ago
```

When to use this command:

- After maintenance is complete and the node is ready for workloads.
- To bring a previously cordoned node back into service.

Note: You can only uncordon a node that is currently in the `Cordoned` status. Attempting to uncordon a node in any other status (Active, Draining, Unhealthy, Terminated) will result in an error.

---

## Common workflows

### Monitor fleet health

To check all nodes and their health status:

```bash
$ navarch list
```

To filter for unhealthy nodes:
```bash
$ navarch list -o json | jq '.[] | select(.health_status != "HEALTH_STATUS_HEALTHY")'
```

### Perform maintenance

1. Cordon the node to prevent new work:
   ```bash
   navarch cordon node-gcp-1
   ```

2. Verify that no new workloads are being scheduled. Check your workload scheduler.

3. Perform maintenance on the node.

4. When ready, uncordon the node:

   ```bash
   navarch uncordon node-gcp-1
   ```

### Decommission a node

1. Drain the node to evict workloads:
   ```bash
   navarch drain node-gcp-1
   ```

2. Wait for workloads to evacuate. Check your workload scheduler.

3. Terminate the node through your cloud provider, or let Navarch handle the termination.

### Investigate a problematic node

1. Get detailed information:

   ```bash
   navarch get node-gcp-1
   ```

2. Check the GPU details and health status.

3. Decide whether to cordon, drain, or leave the node as-is.

### Scripting and automation

To count active nodes per region:
```bash
navarch list -o json | jq 'group_by(.region) | map({region: .[0].region, count: length})'
```

To get all node IDs in a specific region:

```bash
navarch list --region us-central1 -o json | jq -r '.[].node_id'
```

To check if any nodes have been offline for over 5 minutes:

```bash
navarch list -o json | jq '.[] | select(.last_heartbeat < (now - 300))'
```

To cordon all nodes in a specific zone:
```bash
for node in $(navarch list --region us-central1 -o json | jq -r '.[] | select(.zone == "us-central1-a") | .node_id'); do
  navarch cordon $node
done
```

---

## Output formats

### Table (default)

The table format provides a human-readable table with aligned columns for interactive use.

```bash
navarch list
```

### JSON

The JSON format provides machine-readable output for scripting and automation.

```bash
navarch list -o json
```

You can combine JSON output with `jq` for filtering:
```bash
# Get all active GCP nodes
navarch list -o json | jq '.[] | select(.provider == "gcp" and .status == "NODE_STATUS_ACTIVE")'

# Count nodes by status
navarch list -o json | jq 'group_by(.status) | map({status: .[0].status, count: length})'

# Get nodes with more than 4 GPUs
navarch list -o json | jq '.[] | select((.gpus | length) > 4)'
```

---

## Exit codes

- `0` - Success.
- `1` - General error, such as connection failed or command failed.

---

## Troubleshooting

### Connection refused

Error message: `failed to list nodes: connection refused`

To resolve this issue, verify that the control plane is running:
```bash
# Check if control plane is running
curl http://localhost:50051/healthz

# Start control plane if needed
control-plane -addr :50051
```

### Invalid node ID

Error message: `failed to get node: node not found`

To resolve this issue, verify that the node ID exists:
```bash
navarch list
```

### Control plane not found

Error message: `failed to connect to control plane`

To resolve this issue, specify the correct control plane address using the `--server` flag:

```bash
navarch -s http://control-plane.example.com:50051 list
```

Or set the `NAVARCH_SERVER` environment variable:

```bash
export NAVARCH_SERVER=http://control-plane.example.com:50051
navarch list
```

---

## What's next

- For information about setting up the control plane, see [Control Plane documentation](control-plane.md).
- For information about node daemon configuration, see [Node daemon configuration](node.md).
- To learn about extending Navarch with custom providers and health checks, see [Extending Navarch](extending.md).

