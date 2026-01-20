# Core concepts

This guide explains the fundamental concepts in Navarch.

## Control plane

The control plane is the central management server for your GPU fleet. It:

- Receives health reports from node agents.
- Tracks node status and lifecycle state.
- Manages node pools and autoscaling.
- Issues commands to nodes (cordon, drain, terminate).
- Provides an API for the CLI and external integrations.

There is one control plane per Navarch cluster. All nodes connect to it.

## Node agent

The node agent runs on each GPU instance. It:

- Registers the node with the control plane at startup.
- Sends periodic heartbeats to prove liveness.
- Runs health checks and reports results.
- Receives and executes commands from the control plane.

The node agent does not manage its own lifecycle. It reports status and follows commands.

## Pools

A pool is a group of GPU nodes with shared configuration:

- Same cloud provider and region.
- Same instance type (GPU count and model).
- Common scaling limits and autoscaler configuration.
- Unified health and replacement policies.

Pools enable you to manage different workload types independently. For example:

```yaml
# Training pool: Large instances, conservative scaling
apiVersion: navarch.io/v1alpha1
kind: Pool
metadata:
  name: training
spec:
  providerRef: lambda
  instanceType: gpu_8x_h100_sxm5
  scaling:
    minReplicas: 2
    maxReplicas: 20
    cooldownPeriod: 10m

# Inference pool: Smaller instances, aggressive scaling
---
apiVersion: navarch.io/v1alpha1
kind: Pool
metadata:
  name: inference
spec:
  providerRef: lambda
  instanceType: gpu_1x_a100
  scaling:
    minReplicas: 5
    maxReplicas: 100
    cooldownPeriod: 2m
```

## Providers

A provider abstracts cloud-specific operations:

- Provisioning new instances.
- Terminating instances.
- Listing available instance types.
- Managing SSH keys and startup scripts.

Navarch supports multiple providers:

| Provider | Description |
|----------|-------------|
| `lambda` | Lambda Labs Cloud GPU instances. |
| `gcp` | Google Cloud Platform (planned). |
| `aws` | Amazon Web Services (planned). |
| `fake` | Simulated instances for development. |

Pools reference providers by name. This allows you to use different cloud accounts or providers for different pools.

## Health checks

Health checks detect GPU issues before they affect workloads.

### Boot check

Validates that the node started correctly and can communicate with the control plane.

### NVML check

Verifies communication with the NVIDIA Management Library. Detects driver issues and GPU initialization failures.

### XID error check

Monitors system logs for NVIDIA XID errors. These indicate hardware faults, driver issues, or thermal problems.

Common XID errors:

| XID | Severity | Description |
|-----|----------|-------------|
| 31 | Critical | GPU memory page fault. |
| 43 | Warning | GPU stopped processing. |
| 48 | Critical | Double bit ECC error. |
| 63 | Warning | ECC page retirement. |
| 79 | Critical | GPU has fallen off the bus. |

When a node reports unhealthy status, Navarch can automatically cordon it to prevent new workloads.

## Node lifecycle

Nodes progress through these states:

```
Provisioning → Active → Cordoned → Draining → Terminated
                  ↑         ↓
                  └─────────┘
                   (uncordon)
```

### Provisioning

The control plane requests a new instance from the provider. The node agent has not yet registered.

### Active

The node is registered, healthy, and available for workloads. It sends heartbeats and health check results.

### Cordoned

The node is marked unschedulable. New workloads cannot be placed on it, but existing workloads continue running.

Use cordon for:

- Scheduled maintenance.
- Investigating suspected issues.
- Preparing for decommission.

### Draining

The node is evicting workloads and will be terminated. No new workloads are scheduled.

Use drain for:

- Decommissioning nodes.
- Responding to hardware failures.
- Forced node replacement.

### Terminated

The instance has been terminated by the provider. The node record remains for historical reference.

## Autoscaling

Autoscaling adjusts pool size based on demand. Navarch supports multiple strategies:

### Reactive

Scales based on current GPU utilization. Use for steady workloads where current load predicts future load.

### Queue-based

Scales based on pending job count. Use for batch processing where queue depth indicates required capacity.

### Scheduled

Adjusts scaling limits based on time of day. Use for predictable patterns like business hours.

### Predictive

Uses historical data to anticipate demand. Use for workloads with gradual ramp-up patterns.

### Composite

Combines multiple strategies. Use for complex scenarios requiring multiple signals.

## Labels

Labels are key-value pairs attached to resources. Use them for:

- Filtering nodes by workload type.
- Routing jobs to appropriate pools.
- Organizing resources by team or project.

```yaml
metadata:
  labels:
    workload: training
    team: ml-platform
    environment: production
```

## Scaling limits

Each pool has minimum and maximum replica counts:

- `minReplicas`: Nodes are never scaled below this count. Set to zero for pools that can be empty.
- `maxReplicas`: Nodes are never scaled above this count. Protects against runaway scaling and cost overruns.

The autoscaler operates within these limits. Manual scaling commands also respect these limits.

## Cooldown period

The cooldown period is the minimum time between scaling actions. It prevents thrashing when metrics fluctuate.

During cooldown:

- The autoscaler still evaluates state.
- Recommendations are calculated but not acted upon.
- Manual scaling commands are still accepted.

## Health-based replacement

When `autoReplace` is enabled, Navarch automatically replaces unhealthy nodes:

1. Node fails health checks consecutively.
2. After `unhealthyThreshold` failures, node is marked unhealthy.
3. Navarch terminates the unhealthy node.
4. Navarch provisions a replacement node.

This maintains pool capacity even when GPU hardware fails.

