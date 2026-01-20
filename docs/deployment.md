# Deployment architecture

This document describes how to deploy Navarch in production, including agent installation, custom images, and autoscaling.

## Architecture overview

```
┌─────────────────────────────────────────────────────────────────────┐
│                         Control Plane                                │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐                 │
│  │   API       │  │  Database   │  │  Scheduler  │                 │
│  │  Server     │  │  (Postgres) │  │             │                 │
│  └─────────────┘  └─────────────┘  └─────────────┘                 │
└─────────────────────────────────────────────────────────────────────┘
                              │
                              │ HTTPS/gRPC
                              ▼
┌─────────────────────────────────────────────────────────────────────┐
│                         GPU Node Pool                                │
│                                                                      │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐              │
│  │  Node 1      │  │  Node 2      │  │  Node N      │              │
│  │  ┌────────┐  │  │  ┌────────┐  │  │  ┌────────┐  │              │
│  │  │ Agent  │  │  │  │ Agent  │  │  │  │ Agent  │  │              │
│  │  └────────┘  │  │  └────────┘  │  │  └────────┘  │              │
│  │  8x H100     │  │  8x H100     │  │  8x H100     │              │
│  └──────────────┘  └──────────────┘  └──────────────┘              │
└─────────────────────────────────────────────────────────────────────┘
```

## Node agent deployment

### Option 1: Custom machine images (recommended)

Create custom images with the Navarch agent pre-installed.

**Benefits:**
- Agent starts automatically on boot.
- Consistent configuration across all nodes.
- Faster instance startup (no runtime installation).
- Version control for agent updates.

**GCP custom image:**

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

**AWS AMI:**

```bash
# Use Packer or AWS Image Builder
# See examples/packer/ for Packer templates
```

### Option 2: Cloud-init / user-data

Install agent at instance startup using cloud-init.

**GCP startup script:**

```bash
gcloud compute instances create gpu-node-1 \
  --metadata-from-file startup-script=scripts/install-agent.sh
```

**install-agent.sh:**

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

**Docker:**

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

**Kubernetes DaemonSet:**

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

**Managing the service:**

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

## Pool autoscaling

Navarch can integrate with cloud provider autoscaling to manage GPU pools.

### Autoscaling architecture

```
┌─────────────────┐
│  Autoscaler     │
│  Controller     │
└────────┬────────┘
         │
         │ Monitors pool health
         │ Triggers scale up/down
         ▼
┌─────────────────┐     ┌─────────────────┐
│  Control Plane  │◄───►│  Cloud Provider │
│                 │     │  API            │
└─────────────────┘     └─────────────────┘
         │
         │ Node status
         ▼
┌─────────────────────────────────────────┐
│           GPU Node Pool                  │
│  [Node 1] [Node 2] [Node 3] ... [Node N]│
└─────────────────────────────────────────┘
```

### Scale-up triggers

The autoscaler should scale up when:

1. **Capacity shortage**: All healthy nodes are at capacity.
2. **Pending workloads**: Jobs waiting for GPU resources.
3. **Scheduled scaling**: Time-based scaling for predictable loads.

### Scale-down triggers

The autoscaler should scale down when:

1. **Low utilization**: GPU utilization below threshold for extended period.
2. **Unhealthy nodes**: Nodes with fatal XID errors (replace, don't repair).
3. **Cost optimization**: Spot/preemptible instance interruptions.

### Replacement vs. repair

For GPU failures, replacement is usually better than repair:

```
XID Error Detected
        │
        ▼
┌───────────────────┐
│ Is XID fatal?     │
│ (79, 48, 94, 95)  │
└────────┬──────────┘
         │
    Yes  │  No
    ▼    │  ▼
┌────────┴──┐  ┌─────────────┐
│ Cordon    │  │ Log warning │
│ Drain     │  │ Monitor     │
│ Terminate │  └─────────────┘
│ Replace   │
└───────────┘
```

### Pool configuration (future)

```yaml
# Example pool configuration
apiVersion: navarch.io/v1
kind: GPUPool
metadata:
  name: training-pool
spec:
  provider: gcp
  region: us-central1
  zones:
    - us-central1-a
    - us-central1-b
  
  instanceType: a3-highgpu-8g
  gpuType: nvidia-h100-80gb
  gpusPerNode: 8
  
  scaling:
    minNodes: 2
    maxNodes: 100
    targetUtilization: 80
    
  health:
    unhealthyThreshold: 2  # consecutive failures
    replacementPolicy: auto
    
  image:
    family: navarch-gpu
    project: my-project
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

### Agent authentication (future)

```yaml
# Node agent config with authentication
server: https://control-plane.example.com
auth:
  method: mtls
  certFile: /etc/navarch/node.crt
  keyFile: /etc/navarch/node.key
  caFile: /etc/navarch/ca.crt
```

### Secrets management

- Use cloud provider secret managers for sensitive configuration.
- Rotate credentials regularly.
- Avoid hardcoding secrets in images or scripts.

