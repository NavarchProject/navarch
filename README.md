# Navarch

**Open-source GPU fleet management**

Navarch automates provisioning, health monitoring, and lifecycle management of GPU nodes across cloud providers. You bring your cloud credentials. Navarch provisions nodes, monitors hardware health, and automatically replaces failures.

📖 **[Documentation](https://navarchproject.github.io/navarch/)** · 🚀 **[Getting Started](https://navarchproject.github.io/navarch/getting-started/)**

## Why Navarch?

Managing GPU fleets across clouds is painful:

- **Every cloud is different.** GCP, AWS, Lambda Labs—each has its own API, instance types, and availability. You end up writing glue code instead of training models.

- **"Running" doesn't mean "healthy."** Cloud providers tell you a VM is up. They don't tell you about XID errors, ECC faults, or thermal throttling that silently corrupt your gradients.

- **Manual replacement doesn't scale.** When a node goes bad at 3am in a 256-GPU cluster, someone has to wake up, identify it, drain it, and provision a replacement. This should be automatic.

Navarch makes your GPU supply fungible across clouds. Request capacity, get healthy GPUs, and let Navarch handle the operational toil.

## Features

- **Multi-cloud provisioning** — Unified API across Lambda Labs, GCP, and AWS. Failover between providers when capacity is unavailable.
- **GPU health monitoring** — Detects XID errors, ECC faults, thermal issues, and NVLink failures via NVML before they crash your workloads.
- **Automatic replacement** — Unhealthy nodes are cordoned and replaced without manual intervention.
- **Autoscaling** — Scale based on GPU utilization, job queue depth, schedules, or custom logic.
- **Fleet simulator** — Test policies and failure scenarios locally with 1000+ simulated nodes.

## Quick Start

```bash
# Build
git clone https://github.com/NavarchProject/navarch.git
cd navarch
make build

# Create a config file
cat > config.yaml << 'EOF'
providers:
  fake:
    type: fake
    gpu_count: 8

pools:
  dev:
    provider: fake
    instance_type: gpu_8x_h100
    region: local
    min_nodes: 2
    max_nodes: 5
    health:
      auto_replace: true
EOF

# Start the control plane
./bin/control-plane --config config.yaml

# In another terminal, list nodes
./bin/navarch list
```

For real cloud providers, see the [configuration reference](https://navarchproject.github.io/navarch/configuration/).

## Architecture

```
┌─────────────────────────────────────────────┐
│  Workload Schedulers                        │
│  (Kubernetes, Slurm, Ray)                   │
└─────────────────────────────────────────────┘
                     ↓
┌─────────────────────────────────────────────┐
│  Navarch Control Plane                      │
│  - Provisions GPU VMs                       │
│  - Monitors hardware health                 │
│  - Autoscales pools                         │
│  - Replaces failures                        │
└─────────────────────────────────────────────┘
                     ↓
┌─────────────────────────────────────────────┐
│  Cloud Providers                            │
│  (Lambda Labs, GCP, AWS)                    │
└─────────────────────────────────────────────┘
```

The **control plane** manages pools, tracks node state, and provisions instances through cloud provider APIs.

The **node agent** runs on each GPU instance, reports health via NVML, and executes commands from the control plane.

## Development

```bash
# Run tests
make test

# Run the simulator
./bin/simulator run scenarios/gpu-failure.yaml -v

# Interactive mode
./bin/simulator interactive -v
```

See the [simulator documentation](https://navarchproject.github.io/navarch/simulator/) for stress testing with thousands of nodes.

## Repository Structure

```
navarch/
├── cmd/
│   ├── navarch/          # CLI
│   ├── control-plane/    # Control plane server
│   ├── node/             # Node agent
│   └── simulator/        # Fleet simulator
├── pkg/
│   ├── controlplane/     # Control plane logic
│   ├── provider/         # Cloud provider implementations
│   ├── pool/             # Pool management and autoscaling
│   ├── gpu/              # GPU monitoring (NVML)
│   └── simulator/        # Simulation framework
├── proto/                # Protobuf definitions
├── scenarios/            # Simulator scenarios
├── website/              # Documentation (MkDocs)
└── examples/             # Example configurations
```

## Extending

Navarch is designed to be extended. Implement custom cloud providers, autoscalers, or metrics sources without modifying the core.

```go
// Add a new cloud provider
type Provider interface {
    Name() string
    Provision(ctx context.Context, req ProvisionRequest) (*Instance, error)
    Terminate(ctx context.Context, instanceID string) error
    List(ctx context.Context) ([]*Instance, error)
}

// Add custom autoscaling logic
type Autoscaler interface {
    Recommend(ctx context.Context, state PoolState) (ScaleRecommendation, error)
}
```

See [Extending Navarch](https://navarchproject.github.io/navarch/extending/) for the full guide.

## Roadmap

- [x] Multi-cloud provisioning (Lambda Labs, GCP, AWS)
- [x] Node agent with NVML health monitoring
- [x] XID error detection and classification
- [x] Pool management with pluggable autoscaling
- [x] Fleet simulator with stress testing
- [ ] Spot instance support with preemption handling
- [ ] Active diagnostics (dcgmi diag, GPU burn tests)
- [ ] Web dashboard

## License

MIT
