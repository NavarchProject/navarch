# Architecture

Navarch is an infrastructure layer that sits between cloud providers and workload schedulers.

## System layers

```
┌────────────────────────────────────────────┐
│ Workload Schedulers                        │
│ (Kubernetes, Slurm, Ray, custom)           │
└────────────────────────────────────────────┘
                    ↓ schedule jobs
┌────────────────────────────────────────────┐
│ Navarch                                    │
│ - Provisions GPU VMs                       │
│ - Monitors hardware health                 │
│ - Autoscales node pools                    │
│ - Auto-replaces failures                   │
└────────────────────────────────────────────┘
                    ↓ provision/terminate
┌────────────────────────────────────────────┐
│ Cloud Provider APIs                        │
│ (Lambda Labs, GCP, AWS)                    │
└────────────────────────────────────────────┘
```

Your scheduler places workloads. Navarch maintains healthy infrastructure.

## Components

See [Components](concepts/components.md) for details.

**Control plane**: gRPC server that manages pools, tracks node state, and issues commands.

**Node agent**: Lightweight process on each GPU instance that reports health and executes commands.

**Pool manager**: Orchestrates autoscaling and node replacement.

## Kubernetes integration

Navarch and Kubernetes operate at different layers.

| Layer | Kubernetes | Navarch |
|-------|------------|---------|
| Workloads | Schedules pods, scales replicas | - |
| Nodes | Cluster Autoscaler adds/removes nodes | Provisions VMs, monitors GPU health |
| Hardware | No visibility | Detects XID errors, thermal issues, ECC faults |

### Pattern 1: Navarch + Kubernetes together

Recommended for production GPU clusters:

```yaml
# navarch.yaml
pools:
  k8s-gpu-workers:
    providers:
      - name: lambda
        priority: 1
      - name: gcp
        priority: 2
    min_nodes: 2
    max_nodes: 50
    autoscaling:
      type: reactive
      scale_up_at: 80
    health:
      auto_replace: true
```

Navarch provisions nodes and replaces failures. Kubernetes schedules workloads.

**Benefits:**

- Multi-cloud with automatic failover
- GPU health monitoring and auto-replacement
- Kubernetes focuses on workload orchestration

### Pattern 2: Let Kubernetes control scaling

Use Kubernetes Cluster Autoscaler for scaling decisions:

```yaml
pools:
  k8s-gpu-workers:
    min_nodes: 0
    max_nodes: 100
    # No autoscaling config - Kubernetes handles it
    health:
      auto_replace: true  # Keep health monitoring
```

Navarch provisions nodes and replaces failures. Kubernetes handles all scaling.

### Pattern 3: Navarch without Kubernetes

Use with Slurm, Ray, or custom schedulers:

```yaml
pools:
  training:
    min_nodes: 5
    max_nodes: 100
    autoscaling:
      type: queue
      jobs_per_node: 10
```

Your scheduler reports job queue metrics via the [MetricsSource interface](extending.md#custom-metrics-sources).

## Deployment models

### Single control plane

One control plane for all pools. Suitable for most deployments.

```bash
control-plane --config navarch.yaml
```

### High availability

Multiple control planes behind a load balancer with external state store. See [Deployment](deployment.md) for details.

### Multi-region

Separate control planes per region with independent configurations. Use for latency-sensitive workloads or regulatory requirements.

## Learn more

- [Components](concepts/components.md) - Control plane and node agent details
- [Pools & Providers](concepts/pools.md) - Multi-cloud provisioning
- [Health Monitoring](concepts/health.md) - GPU failure detection
- [Autoscaling](concepts/autoscaling.md) - Scaling strategies
- [Extending](extending.md) - Custom providers, autoscalers, metrics sources
