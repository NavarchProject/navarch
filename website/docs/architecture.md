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
