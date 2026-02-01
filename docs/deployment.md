# Deployment architecture

This document describes how to deploy Navarch in production, including agent installation, custom images, and autoscaling.

## Architecture overview

The control plane is the single source of truth. It provisions instances through provider adapters and receives health reports from node agents.

```
┌─────────────────────────────────────────────────────────────────────┐
│                         Control Plane                                │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌───────────┐  │
│  │ API Server  │  │ Pool        │  │ Health      │  │ Provider  │  │
│  │             │  │ Manager     │  │ Monitor     │  │ Registry  │  │
│  └─────────────┘  └──────┬──────┘  └─────────────┘  └─────┬─────┘  │
└──────────────────────────┼────────────────────────────────┼────────┘
                           │                                │
              Provision/   │                                │ Routes to
              Terminate    │                                │ provider
                           ▼                                ▼
                  ┌─────────────────────────────────────────────────┐
                  │              Provider Adapters                   │
                  │  [GCP]  [AWS]  [Lambda]  [CoreWeave]  [Custom]  │
                  └────────────────────┬────────────────────────────┘
                                       │
                                       │ Cloud APIs
                                       ▼
┌─────────────────────────────────────────────────────────────────────┐
│                         GPU Node Pool                                │
│                                                                      │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐              │
│  │  Node 1      │  │  Node 2      │  │  Node N      │              │
│  │  ┌────────┐  │  │  ┌────────┐  │  │  ┌────────┐  │              │
│  │  │ Agent  │──┼──┼──│ Agent  │──┼──┼──│ Agent  │──┼─── Health    │
│  │  └────────┘  │  │  └────────┘  │  │  └────────┘  │    reports   │
│  │  8x H100     │  │  8x H100     │  │  8x H100     │    to CP     │
│  └──────────────┘  └──────────────┘  └──────────────┘              │
└─────────────────────────────────────────────────────────────────────┘
```

**Key principle**: Nodes do not manage their own lifecycle. The control plane decides when to provision and terminate instances. Node agents only report health.

## Node agent deployment

### Option 1: Custom machine images (recommended)

Create custom images with the Navarch agent pre-installed.

Benefits:

- Agent starts automatically on boot.
- Consistent configuration across all nodes.
- Faster instance startup (no runtime installation).
- Version control for agent updates.

GCP custom image:

```bash
# 1. Create a base instance
gcloud compute instances create navarch-base \
  --zone=us-central1-a \
  --machine-type=n1-standard-4 \
  --image-family=ubuntu-2204-lts \
  --image-project=ubuntu-os-cloud

# 2. SSH and install agent
gcloud compute ssh navarch-base --zone=us-central1-a

# Install NVIDIA driver, Go, Navarch agent
sudo apt-get update
sudo apt-get install -y nvidia-driver-535
# ... install navarch-node binary and systemd service

# 3. Create image
gcloud compute instances stop navarch-base --zone=us-central1-a
gcloud compute images create navarch-gpu-v1 \
  --source-disk=navarch-base \
  --source-disk-zone=us-central1-a \
  --family=navarch-gpu
```

AWS AMI:

```bash
# Use Packer or AWS Image Builder
# See examples/packer/ for Packer templates
```

### Option 2: Cloud-init / user-data

Install agent at instance startup using cloud-init.

GCP startup script:

```bash
gcloud compute instances create gpu-node-1 \
  --metadata-from-file startup-script=scripts/install-agent.sh
```

install-agent.sh:

```bash
#!/bin/bash
set -e

# Download and install agent
curl -L https://github.com/NavarchProject/navarch/releases/latest/download/navarch-node-linux-amd64 \
  -o /usr/local/bin/navarch-node
chmod +x /usr/local/bin/navarch-node

# Get instance metadata
INSTANCE_ID=$(curl -s http://metadata.google.internal/computeMetadata/v1/instance/id -H "Metadata-Flavor: Google")
ZONE=$(curl -s http://metadata.google.internal/computeMetadata/v1/instance/zone -H "Metadata-Flavor: Google" | cut -d/ -f4)
REGION=$(echo $ZONE | rev | cut -d- -f2- | rev)

# Create systemd service
cat > /etc/systemd/system/navarch-node.service << EOF
[Unit]
Description=Navarch Node Agent
After=network.target nvidia-persistenced.service

[Service]
Type=simple
ExecStart=/usr/local/bin/navarch-node \
  --server https://control-plane.example.com \
  --node-id ${INSTANCE_ID} \
  --provider gcp \
  --region ${REGION} \
  --zone ${ZONE}
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable navarch-node
systemctl start navarch-node
```

### Option 3: Container deployment

Run the agent as a container (useful for Kubernetes).

Docker:

```bash
docker run -d \
  --name navarch-node \
  --privileged \
  --gpus all \
  -v /var/log:/var/log:ro \
  navarch/node:latest \
  --server https://control-plane.example.com \
  --node-id $(hostname)
```

Kubernetes DaemonSet:

```yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: navarch-node
spec:
  selector:
    matchLabels:
      app: navarch-node
  template:
    spec:
      containers:
      - name: navarch-node
        image: navarch/node:latest
        args:
        - --server=https://control-plane.example.com
        securityContext:
          privileged: true
        volumeMounts:
        - name: dev
          mountPath: /dev
      volumes:
      - name: dev
        hostPath:
          path: /dev
      nodeSelector:
        nvidia.com/gpu: "true"
```

## Systemd service configuration

For production deployments, run the agent as a systemd service.

**/etc/systemd/system/navarch-node.service:**

```ini
[Unit]
Description=Navarch Node Agent
Documentation=https://github.com/NavarchProject/navarch
After=network-online.target nvidia-persistenced.service
Wants=network-online.target

[Service]
Type=simple
User=root
ExecStart=/usr/local/bin/navarch-node \
  --server https://control-plane.example.com \
  --node-id %H
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal

# Security hardening
NoNewPrivileges=no
ProtectSystem=strict
ReadWritePaths=/var/log

[Install]
WantedBy=multi-user.target
```

Managing the service:

```bash
# Enable on boot
sudo systemctl enable navarch-node

# Start/stop/restart
sudo systemctl start navarch-node
sudo systemctl stop navarch-node
sudo systemctl restart navarch-node

# View logs
journalctl -u navarch-node -f
```

## Pool management

The control plane manages GPU pools directly through provider adapters. There is no separate autoscaler component.

### Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                         Control Plane                                │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐                 │
│  │ Pool        │  │ Provider    │  │ Health      │                 │
│  │ Manager     │──│ Registry    │  │ Monitor     │                 │
│  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘                 │
│         │                │                │                         │
│         │ Decides        │ Routes to      │ Reports                 │
│         │ scale actions  │ correct adapter│ unhealthy nodes        │
└─────────┼────────────────┼────────────────┼─────────────────────────┘
          │                │                │
          ▼                ▼                ▼
   ┌─────────────────────────────────────────────────────────────┐
   │                    Provider Adapters                         │
   │  ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────┐        │
   │  │  GCP    │  │  AWS    │  │ Lambda  │  │ Custom  │        │
   │  │ Adapter │  │ Adapter │  │ Adapter │  │ Adapter │        │
   │  └────┬────┘  └────┬────┘  └────┬────┘  └────┬────┘        │
   └───────┼────────────┼────────────┼────────────┼──────────────┘
           │            │            │            │
           ▼            ▼            ▼            ▼
      [GCP API]    [AWS API]   [Lambda API]   [Your API]
```

### Provider interface

Each cloud provider implements a simple interface:

```go
type Provider interface {
    Name() string
    Provision(ctx context.Context, req ProvisionRequest) (*Node, error)
    Terminate(ctx context.Context, nodeID string) error
    List(ctx context.Context) ([]*Node, error)
}
```

The control plane calls these methods to manage instances. The node agent just reports health; it does not manage its own lifecycle.

### Pool manager responsibilities

The pool manager (part of control plane) handles:

1. **Scaling decisions**: When to add or remove nodes.
2. **Health-based replacement**: Terminate unhealthy, provision replacement.
3. **Pool constraints**: Min/max nodes, instance types, zones.
4. **Provider selection**: Multi-cloud routing based on availability and cost.

### Scaling triggers

Scale up when:
- Pool size below minimum.
- All healthy nodes at capacity.
- Pending provision requests.

Scale down when:
- Pool size above maximum.
- Low utilization for extended period.
- Node reported unhealthy (terminate and replace).

### Health-based replacement

When a node becomes unhealthy, the control plane handles it:

```
Health Monitor detects unhealthy node
                │
                ▼
┌───────────────────────────────┐
│ Is failure fatal?             │
│ (XID 79, 48, 94, 95, etc.)   │
└───────────────┬───────────────┘
                │
       Yes      │      No
        ▼       │       ▼
┌───────────────┐  ┌─────────────────┐
│ 1. Cordon     │  │ Log warning     │
│ 2. Drain      │  │ Continue        │
│ 3. Terminate  │  │ monitoring      │
│ 4. Provision  │  └─────────────────┘
│    replacement│
└───────────────┘
```

The control plane orchestrates the entire flow:

```go
// Pseudocode for health-based replacement
func (pm *PoolManager) handleUnhealthyNode(ctx context.Context, node *Node) error {
    // Cordon: prevent new workloads
    if err := pm.cordon(ctx, node.ID); err != nil {
        return err
    }
    
    // Drain: wait for workloads to complete or migrate
    if err := pm.drain(ctx, node.ID); err != nil {
        return err
    }
    
    // Terminate through provider adapter
    provider := pm.registry.Get(node.Provider)
    if err := provider.Terminate(ctx, node.ID); err != nil {
        return err
    }
    
    // Provision replacement
    _, err := provider.Provision(ctx, ProvisionRequest{
        Type:     node.InstanceType,
        GPUCount: node.GPUCount,
    })
    return err
}
```

### Pool configuration

```yaml
providers:
  gcp:
    type: gcp
    project: my-gcp-project

  aws:
    type: aws
    region: us-east-1

pools:
  training:
    provider: gcp
    instance_type: a3-highgpu-8g
    region: us-central1
    zones: [us-central1-a, us-central1-b]
    min_nodes: 2
    max_nodes: 100
    cooldown: 10m
    autoscaling:
      type: reactive
      scale_up_at: 80
      scale_down_at: 20
    health:
      unhealthy_after: 2
      auto_replace: true

  inference:
    provider: aws
    instance_type: p4d.24xlarge
    region: us-east-1
    min_nodes: 4
    max_nodes: 50
```

### Multi-cloud support

The control plane can manage pools across multiple providers:

```
┌──────────────────────────────────────────────────────────────┐
│                      Control Plane                            │
│                                                               │
│  Pool: training-pool (GCP)    Pool: inference-pool (AWS)    │
│  ├── node-gcp-1              ├── node-aws-1                 │
│  ├── node-gcp-2              ├── node-aws-2                 │
│  └── node-gcp-3              └── node-aws-3                 │
│                                                               │
│  Pool: burst-pool (Lambda)                                   │
│  └── (scales 0 to N on demand)                              │
└──────────────────────────────────────────────────────────────┘
```

### Provider adapter implementation

To add a new cloud provider, implement the interface:

```go
package lambda

type LambdaProvider struct {
    apiKey string
    client *lambda.Client
}

func (p *LambdaProvider) Name() string {
    return "lambda"
}

func (p *LambdaProvider) Provision(ctx context.Context, req provider.ProvisionRequest) (*provider.Node, error) {
    // Call Lambda Labs API to create instance
    instance, err := p.client.LaunchInstance(ctx, &lambda.LaunchRequest{
        InstanceType: req.Type,
        Name:         req.Name,
    })
    if err != nil {
        return nil, err
    }
    
    return &provider.Node{
        ID:       instance.ID,
        Provider: "lambda",
        Type:     instance.Type,
        Status:   "provisioning",
    }, nil
}

func (p *LambdaProvider) Terminate(ctx context.Context, nodeID string) error {
    return p.client.TerminateInstance(ctx, nodeID)
}

func (p *LambdaProvider) List(ctx context.Context) ([]*provider.Node, error) {
    instances, err := p.client.ListInstances(ctx)
    // ... convert to provider.Node
}
```

## High availability

### Control plane HA

For production, run multiple control plane replicas:

```
                    ┌─────────────────┐
                    │  Load Balancer  │
                    └────────┬────────┘
                             │
         ┌───────────────────┼───────────────────┐
         ▼                   ▼                   ▼
┌─────────────────┐ ┌─────────────────┐ ┌─────────────────┐
│ Control Plane 1 │ │ Control Plane 2 │ │ Control Plane 3 │
└────────┬────────┘ └────────┬────────┘ └────────┬────────┘
         │                   │                   │
         └───────────────────┴───────────────────┘
                             │
                    ┌────────┴────────┐
                    │    Database     │
                    │   (Postgres)    │
                    └─────────────────┘
```

### Node agent resilience

The node agent is designed to be resilient:

- Automatic reconnection on control plane failure.
- Local health check caching during disconnection.
- Graceful degradation when control plane is unavailable.

## Monitoring and observability

### Metrics to export

```
# Node metrics
navarch_node_gpu_count
navarch_node_gpu_temperature_celsius
navarch_node_gpu_utilization_percent
navarch_node_gpu_memory_used_bytes
navarch_node_gpu_power_watts
navarch_node_health_check_status

# Control plane metrics
navarch_nodes_total
navarch_nodes_healthy
navarch_nodes_unhealthy
navarch_commands_issued_total
navarch_xid_errors_total
```

### Alerting recommendations

| Alert | Condition | Severity |
|-------|-----------|----------|
| GPU temperature high | > 83°C for 5 min | Warning |
| GPU fallen off bus | XID 79 | Critical |
| Node unhealthy | Health != Healthy for 10 min | Warning |
| No heartbeat | Last heartbeat > 5 min | Critical |
| Pool capacity low | Available nodes < 10% | Warning |

## Security considerations

### Network security

- Control plane should be behind a load balancer with TLS.
- Node agents should authenticate with the control plane.
- Consider using private networks for node-to-control-plane traffic.

### Authentication

Enable bearer token authentication by setting `NAVARCH_AUTH_TOKEN` on both the control plane and node agents:

```bash
# Control plane
export NAVARCH_AUTH_TOKEN="your-secret-token"
control-plane --config config.yaml

# Node agent
export NAVARCH_AUTH_TOKEN="your-secret-token"
node-agent --server https://control-plane.example.com
```

For token generation, client configuration, and custom authentication methods, see [authentication](authentication.md).

### Secrets management

- Use cloud provider secret managers for sensitive configuration.
- Rotate credentials regularly.
- Avoid hardcoding secrets in images or scripts.

