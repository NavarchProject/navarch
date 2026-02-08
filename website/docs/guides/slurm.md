# Slurm integration

This guide covers how to integrate Navarch with Slurm to get GPU health monitoring, elastic cloud provisioning, and automated node replacement for Slurm-managed HPC clusters.

## Overview

Slurm is a widely used workload manager for HPC and GPU training clusters. Navarch complements Slurm by handling the infrastructure layer: provisioning cloud GPU nodes, monitoring hardware health, and automatically replacing failed nodes.

```
┌─────────────────────────────────────────────────────┐
│                     Slurm                            │
│  Manages partitions, schedules jobs, tracks          │
│  resources, handles job queues and priorities         │
└──────────────────────┬──────────────────────────────┘
                       │ schedules onto
┌──────────────────────▼──────────────────────────────┐
│                    Navarch                            │
│  Provisions GPU VMs, monitors hardware health,       │
│  auto-replaces failures, scales node pools           │
└──────────────────────┬──────────────────────────────┘
                       │ provisions via
┌──────────────────────▼──────────────────────────────┐
│               Cloud Provider APIs                    │
│  (Lambda Labs, GCP, AWS, CoreWeave)                  │
└─────────────────────────────────────────────────────┘
```

| Layer | Slurm | Navarch |
|-------|-------|---------|
| Job scheduling | Manages job queues, priorities, resource allocation | - |
| Node management | Tracks node state (idle, allocated, drained) | Provisions VMs, monitors GPU health |
| Hardware | Limited visibility (via GRES) | Detects XID errors, thermal issues, ECC faults |
| Scaling | Static partitions | Elastic cloud provisioning based on queue depth |

## Prerequisites

- Slurm controller (`slurmctld`) version 21.08+
- `slurmd` configured on compute nodes or included in node images
- Navarch control plane binary or container image
- Cloud provider credentials for at least one supported provider
- Network connectivity between the Slurm controller and Navarch-provisioned nodes

## Architecture

In a typical Slurm + Navarch deployment, the Slurm controller runs on a persistent management node while Navarch provisions and manages the GPU compute nodes.

```
┌──────────────────────────────────────────────────────────────┐
│                   Management Node                             │
│                                                               │
│  ┌──────────────────┐    ┌─────────────────────────┐          │
│  │  Slurm Controller│    │  Navarch Control Plane  │          │
│  │  (slurmctld)     │    │                         │          │
│  │                  │    │  - Pool manager          │          │
│  │  - Job scheduler │    │  - Health monitor        │          │
│  │  - Partition mgmt│    │  - Provider adapters     │          │
│  └────────┬─────────┘    └────────────┬────────────┘          │
└───────────┼───────────────────────────┼───────────────────────┘
            │                           │
            │ slurmd                    │ health reports
            │                           │
┌───────────▼───────────────────────────▼───────────────────────┐
│                      GPU Compute Nodes                         │
│                                                                │
│  ┌──────────────────┐  ┌──────────────────┐                    │
│  │  Node 1          │  │  Node 2          │  ...               │
│  │  ┌──────┐        │  │  ┌──────┐        │                    │
│  │  │slurmd│        │  │  │slurmd│        │                    │
│  │  └──────┘        │  │  └──────┘        │                    │
│  │  ┌──────────────┐│  │  ┌──────────────┐│                    │
│  │  │Navarch Agent ││  │  │Navarch Agent ││                    │
│  │  └──────────────┘│  │  └──────────────┘│                    │
│  │  8x H100         │  │  8x H100         │                    │
│  └──────────────────┘  └──────────────────┘                    │
└────────────────────────────────────────────────────────────────┘
```

Each compute node runs both `slurmd` (for Slurm job execution) and the Navarch node agent (for GPU health reporting).

## Deploying the control plane

Run the Navarch control plane on your Slurm management node or a dedicated host.

### Configuration

```yaml
# navarch-config.yaml
server:
  address: 0.0.0.0:50051
  heartbeat_interval: 30s
  health_check_interval: 60s
  autoscale_interval: 30s

providers:
  lambda:
    api_key_env: LAMBDA_API_KEY

pools:
  - name: gpu-partition
    provider: lambda
    instance_type: gpu_8x_h100_sxm5
    region: us-west-2

    scaling:
      min_nodes: 4
      max_nodes: 64
      cooldown_period: 10m

      autoscaler:
        type: queue
        jobs_per_node: 1  # One multi-GPU job per node

    health:
      unhealthy_threshold: 2
      auto_replace: true
```

### Running the control plane

```bash
# Start as a systemd service
sudo systemctl start navarch-control-plane

# Or run directly
navarch-control-plane --config navarch-config.yaml
```

See [Deployment](../deployment.md) for systemd service configuration and high availability options.

## Deploying the node agent

Each GPU compute node needs both the Navarch agent and `slurmd`. The recommended approach is to use SSH bootstrap so Navarch installs everything when provisioning a node.

### SSH bootstrap with Slurm

Configure setup commands that install both `slurmd` and the Navarch agent:

```yaml
pools:
  - name: gpu-partition
    provider: lambda
    instance_type: gpu_8x_h100_sxm5
    region: us-west-2
    ssh_user: ubuntu
    ssh_private_key_path: /etc/navarch/ssh-key
    setup_commands:
      # Install Navarch agent
      - |
        curl -L https://github.com/NavarchProject/navarch/releases/latest/download/navarch-node-linux-amd64 \
          -o /usr/local/bin/navarch-node && chmod +x /usr/local/bin/navarch-node
      - |
        cat > /etc/systemd/system/navarch-node.service << EOF
        [Unit]
        Description=Navarch Node Agent
        After=network.target
        [Service]
        ExecStart=/usr/local/bin/navarch-node --server {{.ControlPlane}} --node-id {{.NodeID}}
        Restart=always
        [Install]
        WantedBy=multi-user.target
        EOF
      - systemctl daemon-reload && systemctl enable navarch-node && systemctl start navarch-node
      # Install and configure slurmd
      - apt-get update && apt-get install -y slurmd
      - |
        cat > /etc/slurm/slurm.conf << EOF
        # Minimal slurm.conf - point to your slurmctld
        ClusterName=gpu-cluster
        SlurmctldHost=slurmctld.example.com
        PartitionName=gpu Nodes={{.NodeID}} Default=YES MaxTime=INFINITE State=UP
        NodeName={{.NodeID}} CPUs=96 Gres=gpu:8 RealMemory=1500000 State=UNKNOWN
        GresTypes=gpu
        EOF
      - systemctl enable slurmd && systemctl start slurmd

    scaling:
      min_nodes: 4
      max_nodes: 64
      cooldown_period: 10m

      autoscaler:
        type: queue
        jobs_per_node: 1

    health:
      unhealthy_threshold: 2
      auto_replace: true
```

See [Node Bootstrap](../bootstrap.md) for all available template variables (`{{.ControlPlane}}`, `{{.NodeID}}`, `{{.Pool}}`, `{{.Provider}}`, `{{.Region}}`, `{{.InstanceType}}`).

### Custom machine image

For faster startup, build a custom image with both agents pre-installed:

```bash
# Pre-install on base image:
# 1. NVIDIA drivers
# 2. Slurm (slurmd, munge)
# 3. Navarch node agent
# 4. Systemd services for both

# Then reference the image in your provider config
```

See [Deployment - Custom machine images](../deployment.md#option-1-custom-machine-images-recommended) for details on building custom images.

## Queue-based autoscaling with Slurm

Navarch can scale GPU nodes based on the Slurm job queue. When jobs are waiting, Navarch provisions more nodes. When the queue is empty, it scales down.

### Metrics source implementation

Implement the `MetricsSource` interface to query Slurm job queue state:

```go
package slurmmetrics

import (
    "context"
    "os/exec"
    "strings"

    "github.com/NavarchProject/navarch/pkg/controlplane"
)

type SlurmMetricsSource struct {
    partition string
    dbMetrics *controlplane.DBMetricsSource
}

func New(partition string, dbMetrics *controlplane.DBMetricsSource) *SlurmMetricsSource {
    return &SlurmMetricsSource{partition: partition, dbMetrics: dbMetrics}
}

func (m *SlurmMetricsSource) GetPoolMetrics(ctx context.Context, poolName string) (*controlplane.PoolMetrics, error) {
    // Get GPU utilization from Navarch's built-in metrics
    baseMetrics, err := m.dbMetrics.GetPoolMetrics(ctx, poolName)
    if err != nil {
        return nil, err
    }

    // Count pending jobs in the Slurm partition
    pendingOut, err := exec.CommandContext(ctx,
        "squeue", "-p", m.partition, "-t", "PENDING", "-h", "-o", "%i",
    ).Output()
    if err != nil {
        return nil, fmt.Errorf("query pending jobs: %w", err)
    }
    pending := countLines(string(pendingOut))

    // Count running jobs
    runningOut, err := exec.CommandContext(ctx,
        "squeue", "-p", m.partition, "-t", "RUNNING", "-h", "-o", "%i",
    ).Output()
    if err != nil {
        return nil, fmt.Errorf("query running jobs: %w", err)
    }
    running := countLines(string(runningOut))

    baseMetrics.PendingJobs = pending
    baseMetrics.QueueDepth = pending + running
    return baseMetrics, nil
}

func countLines(s string) int {
    s = strings.TrimSpace(s)
    if s == "" {
        return 0
    }
    return len(strings.Split(s, "\n"))
}
```

### Using scontrol for richer metrics

For more detailed job information, you can query `scontrol` instead of `squeue`:

```go
func (m *SlurmMetricsSource) GetPoolMetrics(ctx context.Context, poolName string) (*controlplane.PoolMetrics, error) {
    baseMetrics, err := m.dbMetrics.GetPoolMetrics(ctx, poolName)
    if err != nil {
        return nil, err
    }

    // Get partition info including pending/running counts
    out, err := exec.CommandContext(ctx,
        "scontrol", "show", "partition", m.partition, "--oneliner",
    ).Output()
    if err != nil {
        return nil, fmt.Errorf("query partition: %w", err)
    }

    // Parse TotalJobs, PendingJobs from scontrol output
    pending, running := parseScontrolOutput(string(out))

    baseMetrics.PendingJobs = pending
    baseMetrics.QueueDepth = pending + running
    return baseMetrics, nil
}
```

### Pool configuration for Slurm queue scaling

```yaml
pools:
  - name: gpu-partition
    provider: lambda
    instance_type: gpu_8x_h100_sxm5
    region: us-west-2

    scaling:
      min_nodes: 4
      max_nodes: 64
      cooldown_period: 10m

      autoscaler:
        type: queue
        jobs_per_node: 1  # Each job uses an entire 8-GPU node
```

With this configuration, if 20 jobs are pending and 4 nodes are running, Navarch will scale up to 24 nodes (capped at `max_nodes`).

### Composite autoscaler for Slurm

Combine queue-based scaling with reactive GPU utilization monitoring:

```yaml
pools:
  - name: gpu-partition
    provider: lambda
    instance_type: gpu_8x_h100_sxm5
    region: us-west-2

    scaling:
      min_nodes: 4
      max_nodes: 64
      cooldown_period: 10m

      autoscaler:
        type: composite
        mode: max
        autoscalers:
          - type: queue
            jobs_per_node: 1
          - type: reactive
            scale_up_threshold: 90
            scale_down_threshold: 10
```

This scales up when either the job queue is deep or GPU utilization is high, whichever demands more capacity.

## Node registration with Slurm

When Navarch provisions a new node, it must be registered with the Slurm controller so that jobs can be scheduled on it.

### Dynamic node registration

Use Slurm's dynamic node feature (Slurm 22.05+) to register nodes as they come online:

```bash
# On the Slurm controller, enable dynamic nodes in slurm.conf
# MaxNodeCount=1000
```

New nodes register themselves when `slurmd` starts and connects to `slurmctld`.

### Static node definitions

For clusters with known node counts, pre-define nodes in `slurm.conf`:

```bash
# slurm.conf on slurmctld
NodeName=gpu-node-[001-064] CPUs=96 Gres=gpu:8 RealMemory=1500000 State=CLOUD
PartitionName=gpu Nodes=gpu-node-[001-064] Default=YES MaxTime=INFINITE State=UP
```

The `State=CLOUD` flag tells Slurm these nodes are cloud-provisioned and may not always be available.

### GRES configuration

Configure Slurm to recognize GPUs on each node:

```bash
# /etc/slurm/gres.conf on each compute node
AutoDetect=nvml
```

Or manually:

```bash
# /etc/slurm/gres.conf
Name=gpu Type=h100 File=/dev/nvidia[0-7]
```

This allows Slurm to schedule GPU-aware jobs:

```bash
# Submit a job requesting 8 GPUs
sbatch --gres=gpu:8 --partition=gpu training_job.sh
```

## Health monitoring and node replacement

When Navarch detects a GPU failure on a Slurm compute node, it coordinates with Slurm to safely replace the node.

### Replacement flow

```
Navarch detects GPU failure (XID error, thermal, ECC)
                    │
                    ▼
    Mark node unhealthy in Navarch
                    │
                    ▼
    Drain the node in Slurm
    (scontrol update NodeName=<node> State=DRAIN Reason="GPU failure")
                    │
                    ▼
    Wait for running jobs to complete
                    │
                    ▼
    Terminate the cloud instance
                    │
                    ▼
    Provision replacement node
                    │
                    ▼
    New node starts slurmd + navarch-node
                    │
                    ▼
    Node becomes available for jobs
```

### Automating Slurm drain on health failure

You can automate the Slurm drain step by running a process that watches Navarch node state and drains unhealthy nodes in Slurm:

```bash
#!/bin/bash
# watch-navarch-health.sh
# Run on the Slurm controller node

while true; do
    # Query Navarch for unhealthy nodes
    unhealthy=$(navarch list --output json | jq -r '.[] | select(.health == "unhealthy") | .id')

    for node in $unhealthy; do
        state=$(scontrol show node "$node" 2>/dev/null | grep -oP 'State=\K\S+')
        if [[ "$state" != *"DRAIN"* ]]; then
            echo "Draining unhealthy node: $node"
            scontrol update NodeName="$node" State=DRAIN Reason="Navarch: GPU health failure"
        fi
    done

    sleep 30
done
```

### Prolog/epilog scripts

Use Slurm prolog and epilog scripts to validate GPU health before and after each job:

```bash
# /etc/slurm/prolog.sh - runs before each job
#!/bin/bash
# Check if Navarch reports this node as healthy
status=$(navarch get "$(hostname)" --output json 2>/dev/null | jq -r '.health')
if [ "$status" = "unhealthy" ]; then
    echo "Node GPU health check failed" >&2
    exit 1  # Job will be requeued
fi
```

```bash
# /etc/slurm/epilog.sh - runs after each job
#!/bin/bash
# Log GPU health status after job completion
navarch get "$(hostname)" --output json | jq '{node: .id, health: .health, gpus: .gpu_count}'
```

Configure in `slurm.conf`:

```bash
Prolog=/etc/slurm/prolog.sh
Epilog=/etc/slurm/epilog.sh
```

## Multi-partition configuration

Map Navarch pools to Slurm partitions for different workload types:

```yaml
# navarch-config.yaml
pools:
  # Large training jobs - full 8-GPU nodes
  - name: training
    provider: lambda
    instance_type: gpu_8x_h100_sxm5
    region: us-west-2

    scaling:
      min_nodes: 4
      max_nodes: 32
      cooldown_period: 10m

      autoscaler:
        type: queue
        jobs_per_node: 1

    health:
      unhealthy_threshold: 2
      auto_replace: true

  # Inference and development - smaller instances
  - name: dev
    provider: gcp
    instance_type: a2-highgpu-1g
    region: us-central1

    scaling:
      min_nodes: 2
      max_nodes: 16
      cooldown_period: 5m

      autoscaler:
        type: reactive
        scale_up_threshold: 70
        scale_down_threshold: 20

    health:
      unhealthy_threshold: 2
      auto_replace: true
```

Corresponding Slurm partitions:

```bash
# slurm.conf
PartitionName=training Nodes=training-[001-032] Default=NO MaxTime=7-00:00:00 State=UP
PartitionName=dev Nodes=dev-[001-016] Default=YES MaxTime=1-00:00:00 State=UP
```

## Monitoring

### Structured logs

The control plane emits structured JSON logs for all scaling and health events:

```json
{"time": "2026-01-19T22:00:15Z", "level": "INFO", "msg": "scaling up", "pool": "gpu-partition", "from": 4, "to": 8, "reason": "pending jobs 8 > 4 nodes * 1 jobs_per_node"}
{"time": "2026-01-19T22:01:30Z", "level": "WARN", "msg": "node unhealthy", "pool": "gpu-partition", "node": "gpu-node-003", "reason": "XID 79: GPU fallen off bus"}
{"time": "2026-01-19T22:01:31Z", "level": "INFO", "msg": "replacing node", "pool": "gpu-partition", "node": "gpu-node-003"}
```

### Slurm accounting integration

Use Slurm's `sacct` to correlate job failures with Navarch health events:

```bash
# Find failed jobs on a specific node
sacct -N gpu-node-003 --state=FAILED --format=JobID,JobName,Start,End,ExitCode,NodeList

# Compare with Navarch health events
navarch get gpu-node-003 --output json | jq '.health_events'
```

### Key metrics

| Metric | Source | Description |
|--------|--------|-------------|
| Pending jobs | `squeue -t PENDING` | Jobs waiting for resources |
| GPU utilization | Navarch agent | Per-GPU utilization percentage |
| XID errors | Navarch agent | GPU hardware errors detected |
| Node health | Navarch control plane | Healthy vs unhealthy node count |
| Job throughput | `sacct` | Jobs completed per hour |

## Complete example

A full configuration for a Slurm cluster with Navarch-managed GPU nodes:

```yaml
# navarch-config.yaml
server:
  address: 0.0.0.0:50051
  heartbeat_interval: 30s
  health_check_interval: 60s
  autoscale_interval: 60s

providers:
  lambda:
    api_key_env: LAMBDA_API_KEY

pools:
  - name: gpu-partition
    provider: lambda
    instance_type: gpu_8x_h100_sxm5
    region: us-west-2
    ssh_user: ubuntu
    ssh_private_key_path: /etc/navarch/ssh-key
    setup_commands:
      # Install Navarch agent
      - |
        curl -L https://github.com/NavarchProject/navarch/releases/latest/download/navarch-node-linux-amd64 \
          -o /usr/local/bin/navarch-node && chmod +x /usr/local/bin/navarch-node
      - |
        cat > /etc/systemd/system/navarch-node.service << EOF
        [Unit]
        Description=Navarch Node Agent
        After=network.target
        [Service]
        ExecStart=/usr/local/bin/navarch-node --server {{.ControlPlane}} --node-id {{.NodeID}}
        Restart=always
        [Install]
        WantedBy=multi-user.target
        EOF
      - systemctl daemon-reload && systemctl enable navarch-node && systemctl start navarch-node
      # Install and start slurmd
      - apt-get update && apt-get install -y slurmd munge
      - systemctl enable slurmd && systemctl start slurmd

    scaling:
      min_nodes: 4
      max_nodes: 64
      cooldown_period: 10m

      autoscaler:
        type: composite
        mode: max
        autoscalers:
          - type: queue
            jobs_per_node: 1
          - type: reactive
            scale_up_threshold: 90
            scale_down_threshold: 10

    health:
      unhealthy_threshold: 2
      auto_replace: true

    labels:
      scheduler: slurm
      partition: gpu
```

## Next steps

- [Pool Management](../pool-management.md) - Detailed pool configuration and autoscaling strategies
- [Deployment](../deployment.md) - Production deployment patterns and high availability
- [Health Monitoring](../concepts/health.md) - GPU failure detection and health policies
- [Extending Navarch](../extending.md) - Custom metrics sources and providers
