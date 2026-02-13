# Health Monitoring

Navarch detects GPU failures before they crash your workloads.

## Health checks

The node agent runs three types of health checks:

- **Boot check**: Validates that the node started correctly and can communicate with the control plane. Runs once at startup.

- **GPU check**: Queries GPU metrics via NVML (temperature, power, utilization, memory). Detects communication failures and threshold violations.

- **Health event check**: Collects GPU health events and sends them to the control plane. The control plane uses CEL policies to classify events by severity.

## Health event types

| Type | Description |
|------|-------------|
| XID error | NVIDIA driver errors (hardware faults, driver issues) |
| Thermal | Temperature warnings and critical events |
| ECC SBE | Single-bit ECC errors (correctable) |
| ECC DBE | Double-bit ECC errors (uncorrectable) |
| NVLink | NVLink communication errors |
| PCIe | PCIe bus errors |

### XID errors

XID errors are NVIDIA driver error codes. Some are fatal (require node replacement), others are recoverable.

**Fatal XID codes**:

| XID | Description |
|-----|-------------|
| 43 | GPU stopped processing |
| 48 | Double bit ECC error |
| 63 | ECC page retirement |
| 79 | GPU has fallen off the bus |

**Recoverable XID codes**:

| XID | Description |
|-----|-------------|
| 13 | Graphics engine exception |
| 31 | GPU memory page fault |
| 45 | Preemptive cleanup |
| 64 | ECC page retirement event |
| 92 | High single-bit ECC rate |
| 94 | Contained ECC error |

When a fatal XID occurs, the node is marked unhealthy and (if auto-replace is enabled) terminated and replaced.

## Health status

Health status reflects the hardware health reported by the node agent.

| Status | Meaning |
|--------|---------|
| **Healthy** | All health checks pass. GPUs working normally. |
| **Degraded** | Partially functional. Some warnings (high temp, minor errors). |
| **Unhealthy** | Critical failure. One or more checks failed. |

Health status is computed from check results:

- Any check unhealthy → overall unhealthy
- Any check degraded (none unhealthy) → overall degraded
- All checks healthy → overall healthy

## Node status

Node status reflects the operational state from the control plane's perspective.

| Status | Meaning |
|--------|---------|
| **Active** | Available for workloads. Receiving heartbeats, passing checks. |
| **Cordoned** | Marked unschedulable. Existing workloads continue. |
| **Draining** | Evicting workloads before termination. |
| **Unhealthy** | Failed health checks. Not usable. |
| **Terminated** | Instance shut down. |

## How health and node status interact

Health status affects node status through these rules:

| Health Status | Node Status Transition |
|---------------|------------------------|
| Unhealthy | Node becomes **Unhealthy** |
| Healthy | Node stays **Unhealthy** (no auto-recovery) |
| Degraded | Node stays **Unhealthy** (no auto-recovery) |

Unhealthy nodes do not automatically recover to Active. This prevents nodes with intermittent hardware failures from being returned to service. To bring an unhealthy node back:

- Use `navarch uncordon <node-id>` after manually verifying the hardware is healthy.
- Or let auto-replacement terminate and replace the node.

## Health-based replacement

When `auto_replace` is enabled, Navarch automatically replaces unhealthy nodes:

1. Node fails health checks consecutively.
2. After `unhealthy_threshold` failures, node is marked unhealthy.
3. Navarch terminates the unhealthy node.
4. Navarch provisions a replacement.

```yaml
pools:
  training:
    health:
      auto_replace: true
      unhealthy_threshold: 2  # Replace after 2 consecutive failures
```

This maintains pool capacity even when GPU hardware fails.

See [Pool Management](../pool-management.md) for detailed health policy configuration.
