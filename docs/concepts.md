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
pools:
  # Training pool: Large instances, conservative scaling
  training:
    provider: lambda
    instance_type: gpu_8x_h100_sxm5
    min_nodes: 2
    max_nodes: 20
    cooldown: 10m

  # Inference pool: Smaller instances, aggressive scaling
  inference:
    provider: lambda
    instance_type: gpu_1x_a100
    min_nodes: 5
    max_nodes: 100
    cooldown: 2m
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

### GPU check

Queries GPU metrics including temperature, power usage, and utilization. Detects communication failures and threshold violations.

### Health event check

Collects GPU health events and sends them to the control plane for evaluation. The control plane uses CEL (Common Expression Language) policies to classify events by severity.

Health event types:

| Type | Description |
|------|-------------|
| XID error | NVIDIA driver errors (hardware faults, driver issues) |
| Thermal | Temperature warnings and critical events |
| ECC SBE | Single-bit ECC errors (correctable) |
| ECC DBE | Double-bit ECC errors (uncorrectable) |
| NVLink | NVLink communication errors |
| PCIe | PCIe bus errors |

Common XID errors:

| XID | Severity | Description |
|-----|----------|-------------|
| 31 | Fatal | GPU memory page fault. |
| 43 | Fatal | GPU stopped processing. |
| 48 | Fatal | Double bit ECC error. |
| 63 | Fatal | ECC page retirement. |
| 79 | Fatal | GPU has fallen off the bus. |

When a node reports unhealthy status, Navarch can automatically cordon it to prevent new workloads.

## Health status vs node status

Navarch tracks two separate status types that serve different purposes.

### Health status

Health status reflects the hardware health reported by the node agent. It comes from health check results.

| Status | Meaning |
|--------|---------|
| Healthy | All health checks pass. GPUs are working normally. |
| Degraded | Partially functional. Some checks show warnings (high temperature, minor errors). |
| Unhealthy | Critical failure detected. One or more checks failed (XID error, GPU offline). |

Health status is computed from health check results. If any check reports unhealthy, the overall status is unhealthy. If any check reports degraded (and none are unhealthy), the overall status is degraded.

### Node status

Node status reflects the operational state of the node from the control plane's perspective.

| Status | Meaning |
|--------|---------|
| Active | Available for workloads. Receiving heartbeats and passing health checks. |
| Cordoned | Marked unschedulable by an administrator. Existing workloads continue. |
| Draining | Evicting workloads before termination. No new workloads scheduled. |
| Unhealthy | Failed health checks. Not usable for workloads. |
| Terminated | Instance has been shut down. |

### How they interact

Health status affects node status through these transitions:

- When a node reports **unhealthy health status**, its node status becomes **unhealthy**.
- When an unhealthy node reports **healthy health status**, its node status becomes **active**.
- When an unhealthy node reports **degraded health status**, its node status stays **unhealthy**. Partial recovery is not sufficient to restore the node to active.
- Administrative states (cordoned, draining) are preserved when health checks pass, but unhealthy health status overrides them.

This design ensures that nodes only return to active duty after fully recovering from failures.

## Instances vs Nodes

Navarch tracks **instances** and **nodes** as separate concepts:

- **Instance**: A cloud resource (what you pay for). Tracked from when you call `provider.Provision()` until termination.
- **Node**: A registered agent running on an instance. Created when the agent calls `RegisterNode`.

This separation matters because:

1. **Provisioning can fail** - Instance created but agent never boots.
2. **Registration can fail** - Instance running but agent crashes on startup.
3. **Costs accrue immediately** - You pay for instances, not nodes.

### Instance lifecycle

Instances progress through these states:

```
Provisioning → Pending Registration → Running → Terminating → Terminated
                        ↓
                      Failed
```

**Provisioning**: Cloud provider is creating the instance.

**Pending Registration**: Instance exists, waiting for node agent to register.

**Running**: Node agent has registered successfully.

**Failed**: Provisioning failed, or registration timed out (default: 10 minutes).

**Terminating**: Termination requested, in progress.

**Terminated**: Instance destroyed by cloud provider.

### Stale instance detection

If an instance stays in "Pending Registration" for too long (default: 10 minutes), Navarch marks it as failed. This catches:

- Boot failures (kernel panic, driver issues)
- Network issues (instance can't reach control plane)
- Agent crashes (segfault before registration)

Configure via `InstanceManagerConfig`:

```go
config := controlplane.InstanceManagerConfig{
    RegistrationTimeout: 10 * time.Minute,  // Time to wait for registration
    StaleCheckInterval:  1 * time.Minute,   // How often to check
}
```

## Node lifecycle

Nodes progress through these states:

```
Active → Cordoned → Draining → Terminated
   ↑         ↓
   └─────────┘
    (uncordon)
```

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
pools:
  training:
    provider: lambda
    instance_type: gpu_8x_h100
    min_nodes: 2
    max_nodes: 20
    labels:
      workload: training
      team: ml-platform
      environment: production
```

## Scaling limits

Each pool has minimum and maximum node counts:

- `min_nodes`: Nodes are never scaled below this count. Set to zero for pools that can be empty.
- `max_nodes`: Nodes are never scaled above this count. Protects against runaway scaling and cost overruns.

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

