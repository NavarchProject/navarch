# Node Lifecycle

Navarch tracks instances and nodes as separate concepts with distinct lifecycles.

## Instances vs Nodes

- **Instance**: A cloud resource (what you pay for). Tracked from `Provision()` until termination.
- **Node**: A registered agent running on an instance. Created when the agent calls `RegisterNode`.

This separation matters because:

1. **Provisioning can fail** - Instance created but agent never boots.
2. **Registration can fail** - Instance running but agent crashes on startup.
3. **Costs accrue immediately** - You pay for instances, not nodes.

## Instance lifecycle

```
Provisioning → Pending Registration → Running → Terminating → Terminated
                        ↓
                      Failed
```

| State | Description |
|-------|-------------|
| **Provisioning** | Cloud provider is creating the instance. |
| **Pending Registration** | Instance exists, waiting for node agent to register. |
| **Running** | Node agent has registered successfully. |
| **Failed** | Provisioning failed, or registration timed out. |
| **Terminating** | Termination requested, in progress. |
| **Terminated** | Instance destroyed by cloud provider. |

### Registration timeout

If an instance stays in "Pending Registration" too long (default: 10 minutes), Navarch marks it as failed. This catches:

- Boot failures (kernel panic, driver issues)
- Network issues (instance can't reach control plane)
- Agent crashes (segfault before registration)

Configure the timeout:

```yaml
instance_manager:
  registration_timeout: 10m
  stale_check_interval: 1m
```

## Node lifecycle

```
Active → Cordoned → Draining → Terminated
   ↑         ↓
   └─────────┘
    (uncordon)
```

### Active

The node is registered, healthy, and available for workloads. It sends heartbeats and health check results.

### Cordoned

The node is marked unschedulable. New workloads cannot be placed on it, but existing workloads continue.

Use cordon for:

- Scheduled maintenance
- Investigating suspected issues
- Preparing for decommission

```bash
navarch cordon node-1
```

When a coordinator is configured, Navarch notifies your workload system (e.g., Kubernetes, Slurm) to mark the node unschedulable. See [Coordinator Configuration](../configuration.md#coordinator).

See [CLI Reference](../cli.md#navarch-cordon) for details.

### Draining

The node is evicting workloads and will be terminated. No new workloads scheduled.

Use drain for:

- Decommissioning nodes
- Responding to hardware failures
- Forced node replacement

```bash
navarch drain node-1
```

When a coordinator is configured, Navarch notifies your workload system to evacuate workloads from the node. You can poll drain status to wait for completion before termination. See [Coordinator Configuration](../configuration.md#coordinator).

See [CLI Reference](../cli.md#navarch-drain) for details.

### Terminated

The instance has been terminated by the provider. The node record remains for historical reference.

## State transitions

### Manual transitions

| Command | From | To |
|---------|------|-----|
| `cordon` | Active | Cordoned |
| `uncordon` | Cordoned | Active |
| `drain` | Active, Cordoned | Draining |

### Automatic transitions

| Trigger | From | To |
|---------|------|-----|
| Health check failure | Active, Cordoned | Unhealthy |
| Health recovery | Unhealthy | Active |
| Auto-replacement | Unhealthy | Terminated |
| Scale-down | Active, Cordoned | Terminated |

## Heartbeats and liveness

Nodes send heartbeats every 30 seconds (configurable). If heartbeats stop:

1. After `heartbeat_timeout` (default: 2 minutes), node is marked **stale**.
2. Stale nodes are considered unhealthy.
3. If auto-replace is enabled, stale nodes are terminated and replaced.

This handles cases where the node agent crashes or loses network connectivity.
