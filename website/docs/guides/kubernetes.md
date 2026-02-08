# Kubernetes integration

This guide covers how to integrate Navarch with Kubernetes to get GPU health monitoring, multi-cloud provisioning, and automated node replacement alongside Kubernetes workload scheduling.

## Overview

Navarch and Kubernetes operate at different layers of the stack. Kubernetes schedules workloads onto nodes. Navarch provisions GPU nodes, monitors hardware health, and replaces failures. Together they provide a production-grade GPU cluster.

```
┌─────────────────────────────────────────────────────┐
│                  Kubernetes                          │
│  Schedules pods, manages deployments, scales         │
│  replicas, handles networking and storage            │
└──────────────────────┬──────────────────────────────┘
                       │ schedules onto
┌──────────────────────▼──────────────────────────────┐
│                    Navarch                            │
│  Provisions GPU VMs, monitors hardware health,       │
│  auto-replaces failures, manages node pools          │
└──────────────────────┬──────────────────────────────┘
                       │ provisions via
┌──────────────────────▼──────────────────────────────┐
│               Cloud Provider APIs                    │
│  (Lambda Labs, GCP, AWS, CoreWeave)                  │
└─────────────────────────────────────────────────────┘
```

| Layer | Kubernetes | Navarch |
|-------|------------|---------|
| Workloads | Schedules pods, scales replicas | - |
| Nodes | Cluster Autoscaler adds/removes nodes | Provisions VMs, monitors GPU health |
| Hardware | No visibility | Detects XID errors, thermal issues, ECC faults |

## Prerequisites

- A running Kubernetes cluster (v1.24+)
- `kubectl` configured with cluster access
- [NVIDIA GPU Operator](https://docs.nvidia.com/datacenter/cloud-native/gpu-operator/overview.html) or manually installed GPU drivers and device plugin
- Navarch control plane binary or container image
- Cloud provider credentials for at least one supported provider

## Integration patterns

Choose the pattern that best fits your operational model.

### Pattern 1: Navarch manages nodes, Kubernetes schedules workloads

Recommended for production GPU clusters. Navarch handles all node lifecycle operations while Kubernetes focuses on workload orchestration.

```
┌─────────────────────────────────────────────────────────────┐
│                    Kubernetes Cluster                         │
│                                                              │
│  ┌────────────┐  ┌────────────┐  ┌────────────┐             │
│  │  GPU Pod 1 │  │  GPU Pod 2 │  │  GPU Pod 3 │  ...        │
│  └─────┬──────┘  └─────┬──────┘  └─────┬──────┘             │
│        │               │               │                     │
│  ┌─────▼──────┐  ┌─────▼──────┐  ┌─────▼──────┐             │
│  │  Worker 1  │  │  Worker 2  │  │  Worker 3  │             │
│  │  Navarch   │  │  Navarch   │  │  Navarch   │             │
│  │  Agent     │  │  Agent     │  │  Agent     │             │
│  └─────┬──────┘  └─────┬──────┘  └─────┬──────┘             │
└────────┼───────────────┼───────────────┼─────────────────────┘
         │               │               │
         └───────────────┼───────────────┘
                         │ health reports
                         ▼
               ┌──────────────────┐
               │ Navarch Control  │
               │ Plane            │──── provisions via cloud APIs
               └──────────────────┘
```

**Benefits:**

- Multi-cloud provisioning with automatic failover
- GPU-level health monitoring (XID errors, thermal, ECC)
- Automatic replacement of nodes with failing hardware
- Kubernetes focuses solely on workload orchestration

**Pool configuration:**

```yaml
pools:
  - name: k8s-gpu-workers
    provider: lambda
    instance_type: gpu_8x_h100_sxm5
    region: us-west-2

    scaling:
      min_nodes: 2
      max_nodes: 50
      cooldown_period: 5m

      autoscaler:
        type: reactive
        scale_up_threshold: 80
        scale_down_threshold: 20

    health:
      unhealthy_threshold: 2
      auto_replace: true

    labels:
      workload: training
      cluster: prod
```

### Pattern 2: Kubernetes controls scaling, Navarch monitors health

Use this when you already have Kubernetes Cluster Autoscaler configured and only need Navarch for GPU health monitoring and replacement.

```yaml
pools:
  - name: k8s-gpu-workers
    provider: gcp
    instance_type: a3-highgpu-8g
    region: us-central1

    scaling:
      min_nodes: 0
      max_nodes: 100
      # No autoscaler configured - Kubernetes handles scaling decisions

    health:
      unhealthy_threshold: 2
      auto_replace: true  # Still replace unhealthy GPU nodes
```

In this pattern, Navarch does not make scaling decisions. It only provisions nodes when explicitly requested and replaces nodes that fail GPU health checks.

## Deploying the control plane on Kubernetes

Run the Navarch control plane as a Kubernetes Deployment.

### Namespace and configuration

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: navarch
---
apiVersion: v1
kind: Secret
metadata:
  name: navarch-credentials
  namespace: navarch
type: Opaque
stringData:
  lambda-api-key: "your-lambda-api-key"
  auth-token: "your-navarch-auth-token"
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: navarch-config
  namespace: navarch
data:
  config.yaml: |
    server:
      address: 0.0.0.0:50051
      heartbeat_interval: 30s
      health_check_interval: 60s
      autoscale_interval: 30s
      health_policy_path: /etc/navarch/health-policy.yaml

    providers:
      lambda:
        api_key_env: LAMBDA_API_KEY

    pools:
      - name: k8s-gpu-workers
        provider: lambda
        instance_type: gpu_8x_h100_sxm5
        region: us-west-2

        scaling:
          min_nodes: 2
          max_nodes: 50
          cooldown_period: 5m

          autoscaler:
            type: reactive
            scale_up_threshold: 80
            scale_down_threshold: 20

        health:
          unhealthy_threshold: 2
          auto_replace: true
```

### Control plane Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: navarch-control-plane
  namespace: navarch
spec:
  replicas: 1
  selector:
    matchLabels:
      app: navarch-control-plane
  template:
    metadata:
      labels:
        app: navarch-control-plane
    spec:
      containers:
      - name: control-plane
        image: navarch/control-plane:latest
        args:
        - --config=/etc/navarch/config.yaml
        ports:
        - containerPort: 50051
          name: grpc
        env:
        - name: LAMBDA_API_KEY
          valueFrom:
            secretKeyRef:
              name: navarch-credentials
              key: lambda-api-key
        - name: NAVARCH_AUTH_TOKEN
          valueFrom:
            secretKeyRef:
              name: navarch-credentials
              key: auth-token
        volumeMounts:
        - name: config
          mountPath: /etc/navarch
        livenessProbe:
          httpGet:
            path: /healthz
            port: 50051
          initialDelaySeconds: 10
          periodSeconds: 30
        readinessProbe:
          httpGet:
            path: /readyz
            port: 50051
          initialDelaySeconds: 5
          periodSeconds: 10
      volumes:
      - name: config
        configMap:
          name: navarch-config
---
apiVersion: v1
kind: Service
metadata:
  name: navarch-control-plane
  namespace: navarch
spec:
  selector:
    app: navarch-control-plane
  ports:
  - port: 50051
    targetPort: grpc
    name: grpc
```

For high availability, see [Deployment](../deployment.md#high-availability).

## Deploying the node agent

Deploy the Navarch node agent as a DaemonSet on GPU nodes. The agent reports hardware health to the control plane.

### DaemonSet

```yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: navarch-node-agent
  namespace: navarch
spec:
  selector:
    matchLabels:
      app: navarch-node-agent
  template:
    metadata:
      labels:
        app: navarch-node-agent
    spec:
      hostPID: true
      containers:
      - name: node-agent
        image: navarch/node:latest
        args:
        - --server=navarch-control-plane.navarch.svc.cluster.local:50051
        - --node-id=$(NODE_NAME)
        env:
        - name: NODE_NAME
          valueFrom:
            fieldRef:
              fieldPath: spec.nodeName
        - name: NAVARCH_AUTH_TOKEN
          valueFrom:
            secretKeyRef:
              name: navarch-credentials
              key: auth-token
        securityContext:
          privileged: true
        volumeMounts:
        - name: dev
          mountPath: /dev
        - name: proc
          mountPath: /host/proc
          readOnly: true
        resources:
          requests:
            cpu: 50m
            memory: 64Mi
          limits:
            cpu: 200m
            memory: 128Mi
      volumes:
      - name: dev
        hostPath:
          path: /dev
      - name: proc
        hostPath:
          path: /proc
      nodeSelector:
        nvidia.com/gpu.present: "true"
      tolerations:
      - key: nvidia.com/gpu
        operator: Exists
        effect: NoSchedule
```

The `nodeSelector` ensures agents only run on GPU nodes. The `privileged` security context is required for GPU hardware access.

### Alternative: SSH bootstrap

Instead of a DaemonSet, you can have the control plane install the agent via SSH when provisioning nodes. This is configured in the pool definition:

```yaml
pools:
  - name: k8s-gpu-workers
    provider: lambda
    instance_type: gpu_8x_h100_sxm5
    region: us-west-2
    ssh_user: ubuntu
    ssh_private_key_path: /etc/navarch/ssh-key
    setup_commands:
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
```

See [Node Bootstrap](../bootstrap.md) for all available template variables.

## Queue-based autoscaling with Kubernetes

Use Kubernetes pod status as a signal for Navarch's queue-based autoscaler. This scales GPU nodes based on the number of pending and running GPU workloads.

### Metrics source implementation

Implement the `MetricsSource` interface to feed Kubernetes pod metrics into Navarch:

```go
package k8smetrics

import (
    "context"

    "github.com/NavarchProject/navarch/pkg/controlplane"
    corev1 "k8s.io/api/core/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/client-go/kubernetes"
)

type K8sMetricsSource struct {
    clientset *kubernetes.Clientset
    dbMetrics *controlplane.DBMetricsSource
}

func New(clientset *kubernetes.Clientset, dbMetrics *controlplane.DBMetricsSource) *K8sMetricsSource {
    return &K8sMetricsSource{clientset: clientset, dbMetrics: dbMetrics}
}

func (m *K8sMetricsSource) GetPoolMetrics(ctx context.Context, poolName string) (*controlplane.PoolMetrics, error) {
    // Get GPU utilization from Navarch's built-in metrics
    baseMetrics, err := m.dbMetrics.GetPoolMetrics(ctx, poolName)
    if err != nil {
        return nil, err
    }

    // Query Kubernetes for GPU pods matching this pool
    pods, err := m.clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{
        LabelSelector: "navarch.dev/pool=" + poolName,
    })
    if err != nil {
        return nil, err
    }

    var pending, running int
    for _, pod := range pods.Items {
        switch pod.Status.Phase {
        case corev1.PodPending:
            pending++
        case corev1.PodRunning:
            running++
        }
    }

    baseMetrics.PendingJobs = pending
    baseMetrics.QueueDepth = pending + running
    return baseMetrics, nil
}
```

### Pool configuration for queue-based scaling

```yaml
pools:
  - name: k8s-gpu-workers
    provider: lambda
    instance_type: gpu_8x_h100_sxm5
    region: us-west-2

    scaling:
      min_nodes: 2
      max_nodes: 50
      cooldown_period: 5m

      autoscaler:
        type: queue
        jobs_per_node: 8  # One job per GPU
```

### Labeling workloads

Add the `navarch.dev/pool` label to GPU workloads so the metrics source can count them:

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: training-job
spec:
  template:
    metadata:
      labels:
        navarch.dev/pool: k8s-gpu-workers
    spec:
      containers:
      - name: train
        image: my-training:latest
        resources:
          limits:
            nvidia.com/gpu: 1
      restartPolicy: Never
```

## Node cordoning and draining

When Navarch detects a GPU hardware failure, it cordons and drains the node before terminating it. In a Kubernetes environment, you can extend this to also cordon the node in Kubernetes so the scheduler stops placing new pods on it.

### How replacement works

1. Navarch detects unhealthy GPU (XID error, thermal event, ECC fault)
2. Node is marked unhealthy after exceeding `unhealthy_threshold`
3. Navarch cordons the node (stops accepting new workloads)
4. Navarch drains the node (waits for existing workloads to finish)
5. The cloud instance is terminated
6. A replacement node is provisioned

### Kubernetes-aware draining

To also cordon the Kubernetes node during this process, you can run a sidecar or controller that watches Navarch node state and mirrors it to Kubernetes:

```bash
# Cordon a node in Kubernetes when Navarch reports it unhealthy
kubectl cordon <node-name>

# Drain pods gracefully
kubectl drain <node-name> --ignore-daemonsets --delete-emptydir-data --grace-period=300
```

This can be automated by watching the Navarch API for node state changes.

## Monitoring

### Health endpoints

The control plane exposes standard health endpoints compatible with Kubernetes probes:

| Endpoint | Purpose | Use |
|----------|---------|-----|
| `GET /healthz` | Liveness | Container restart check |
| `GET /readyz` | Readiness | Load balancer routing |
| `GET /metrics` | Prometheus metrics | Monitoring (future) |

### Prometheus integration

Export Navarch metrics alongside your Kubernetes monitoring stack:

```yaml
# ServiceMonitor for Prometheus Operator
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: navarch-control-plane
  namespace: navarch
spec:
  selector:
    matchLabels:
      app: navarch-control-plane
  endpoints:
  - port: grpc
    path: /metrics
    interval: 30s
```

Key metrics to monitor:

| Metric | Description |
|--------|-------------|
| `navarch_nodes_total` | Total nodes per pool |
| `navarch_nodes_healthy` | Healthy nodes per pool |
| `navarch_nodes_unhealthy` | Unhealthy nodes per pool |
| `navarch_xid_errors_total` | GPU XID errors detected |
| `navarch_node_gpu_temperature_celsius` | GPU temperature per node |
| `navarch_node_gpu_utilization_percent` | GPU utilization per node |

### Alerting

Example Prometheus alert rules for GPU health:

```yaml
groups:
- name: navarch
  rules:
  - alert: NavarchNodeUnhealthy
    expr: navarch_nodes_unhealthy > 0
    for: 5m
    labels:
      severity: warning
    annotations:
      summary: "Unhealthy GPU nodes detected"
  - alert: NavarchPoolCapacityLow
    expr: navarch_nodes_healthy / navarch_nodes_total < 0.8
    for: 10m
    labels:
      severity: critical
    annotations:
      summary: "Pool has less than 80% healthy nodes"
```

## Complete example

A full working configuration for a Kubernetes cluster with Navarch-managed GPU nodes:

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
  gcp:
    project: my-gcp-project

pools:
  # Training pool: large H100 instances, conservative scaling
  - name: training
    provider: lambda
    instance_type: gpu_8x_h100_sxm5
    region: us-west-2

    scaling:
      min_nodes: 2
      max_nodes: 20
      cooldown_period: 10m

      autoscaler:
        type: composite
        mode: max
        autoscalers:
          - type: reactive
            scale_up_threshold: 80
            scale_down_threshold: 20
          - type: queue
            jobs_per_node: 8

    health:
      unhealthy_threshold: 2
      auto_replace: true

    labels:
      workload: training

  # Inference pool: smaller instances, aggressive scaling
  - name: inference
    provider: gcp
    instance_type: a2-highgpu-1g
    region: us-central1

    scaling:
      min_nodes: 4
      max_nodes: 100
      cooldown_period: 2m

      autoscaler:
        type: reactive
        scale_up_threshold: 60
        scale_down_threshold: 30

    health:
      unhealthy_threshold: 1
      auto_replace: true

    labels:
      workload: inference
```

## Next steps

- [Pool Management](../pool-management.md) - Detailed pool configuration and autoscaling strategies
- [Deployment](../deployment.md) - Production deployment patterns and high availability
- [Health Monitoring](../concepts/health.md) - GPU failure detection and health policies
- [Extending Navarch](../extending.md) - Custom metrics sources and providers
