# Configuration

Navarch uses a single YAML configuration file to define providers, pools, and server settings.

## Quick start

```yaml
providers:
  lambda:
    type: lambda
    api_key_env: LAMBDA_API_KEY

pools:
  training:
    provider: lambda
    instance_type: gpu_8x_h100_sxm5
    region: us-west-2
    min_nodes: 2
    max_nodes: 20
    autoscaling:
      type: reactive
      scale_up_at: 80
      scale_down_at: 20
```

Run the control plane with:

```bash
control-plane --config config.yaml
```

## Server

```yaml
server:
  address: ":50051"              # Listen address
  heartbeat_interval: 30s        # Node heartbeat frequency
  health_check_interval: 60s     # Health check frequency
  autoscale_interval: 30s        # Autoscaler evaluation frequency
  health_policy: ./health-policy.yaml  # Custom health policy file
  coordinator:                   # Workload system integration
    type: webhook
    webhook:
      cordon_url: https://scheduler.example.com/api/cordon
      drain_url: https://scheduler.example.com/api/drain
```

All fields are optional with sensible defaults.

| Field | Default | Description |
|-------|---------|-------------|
| `address` | `:50051` | gRPC/HTTP listen address |
| `heartbeat_interval` | `30s` | How often nodes send heartbeats |
| `health_check_interval` | `60s` | How often health checks run |
| `autoscale_interval` | `30s` | How often autoscaler evaluates |
| `health_policy` | (none) | Path to [health policy](health-policy.md) file |
| `coordinator` | (none) | [Coordinator configuration](#coordinator) for workload system integration |

## Authentication

The control plane supports bearer token authentication:

```bash
export NAVARCH_AUTH_TOKEN="your-secret-token"
control-plane --config config.yaml
```

See [Authentication](authentication.md) for client configuration and custom methods.

## Providers

Providers define cloud platforms where GPU nodes are provisioned.

```yaml
providers:
  lambda:
    type: lambda
    api_key_env: LAMBDA_API_KEY    # Environment variable containing API key

  gcp:
    type: gcp
    project: my-gcp-project

  fake:
    type: fake
    gpu_count: 8                   # GPUs per fake instance (for testing)
```

| Type | Description |
|------|-------------|
| `lambda` | Lambda Labs Cloud |
| `gcp` | Google Cloud Platform (coming soon) |
| `aws` | Amazon Web Services (coming soon) |
| `fake` | Fake provider for local development |

## Pools

Pools define groups of GPU nodes with scaling policies.

### Single-provider pool

```yaml
pools:
  training:
    provider: lambda
    instance_type: gpu_8x_h100_sxm5
    region: us-west-2
    min_nodes: 2
    max_nodes: 20
    cooldown: 5m
    ssh_keys:
      - ops-team
    labels:
      workload: training
    autoscaling:
      type: reactive
      scale_up_at: 80
      scale_down_at: 20
    health:
      unhealthy_after: 2
      auto_replace: true
```

### Multi-provider pool

For fungible compute across multiple providers:

```yaml
pools:
  fungible:
    providers:
      - name: lambda
        priority: 1
        regions: [us-west-2, us-east-1]
      - name: gcp
        priority: 2
        regions: [us-central1]
        instance_type: a3-highgpu-8g    # Provider-specific override
    strategy: priority
    instance_type: h100-8x              # Abstract type
    min_nodes: 4
    max_nodes: 32
```

Provider selection strategies:

| Strategy | Description |
|----------|-------------|
| `priority` | Try providers in priority order (lowest first) |
| `cost` | Select cheapest available provider |
| `availability` | Select first provider with capacity |
| `round-robin` | Distribute evenly across providers |

### Pool fields

| Field | Required | Description |
|-------|----------|-------------|
| `provider` | Yes* | Single provider name |
| `providers` | Yes* | List of provider entries (multi-provider) |
| `strategy` | No | Provider selection strategy (multi-provider) |
| `instance_type` | Yes | Instance type (provider-specific or abstract) |
| `region` | No | Default region |
| `zones` | No | Availability zones |
| `ssh_keys` | No | SSH key names to install |
| `min_nodes` | Yes | Minimum nodes to maintain |
| `max_nodes` | Yes | Maximum nodes allowed |
| `cooldown` | No | Time between scaling actions (default: 5m) |
| `labels` | No | Key-value labels for workload routing |
| `autoscaling` | No | [Autoscaler configuration](#autoscaling) |
| `health` | No | [Health check configuration](#health) |
| `setup_commands` | No | [Bootstrap commands](bootstrap.md) |
| `ssh_user` | No | SSH username for bootstrap (default: `ubuntu`) |
| `ssh_private_key_path` | No | Path to SSH private key for bootstrap |

*Either `provider` or `providers` is required, but not both.

## Autoscaling

Configure how pools scale based on demand. See [Autoscaling Concepts](concepts/autoscaling.md) for details on each strategy.

```yaml
autoscaling:
  type: reactive          # reactive, queue, scheduled, predictive, composite
  scale_up_at: 80         # Scale up when utilization > 80%
  scale_down_at: 20       # Scale down when utilization < 20%
```

| Type | Use case |
|------|----------|
| `reactive` | Scale on current GPU utilization |
| `queue` | Scale on pending job count |
| `scheduled` | Time-based scaling limits |
| `predictive` | Forecast-based proactive scaling |
| `composite` | Combine multiple strategies |

## Health

Configure health checking and auto-replacement:

```yaml
health:
  unhealthy_after: 2     # Consecutive failures before unhealthy
  auto_replace: true     # Automatically replace unhealthy nodes
```

See [Health Monitoring](concepts/health.md) for details on health events and XID errors.

For custom health evaluation logic, see [Health Policy](health-policy.md).

## Coordinator

The coordinator integrates Navarch with external workload systems (job schedulers, Kubernetes, etc.). When nodes are cordoned or drained, the coordinator notifies your workload system so it can stop scheduling new work and migrate existing workloads.

### Webhook coordinator

Send HTTP notifications to your workload system:

```yaml
server:
  coordinator:
    type: webhook
    webhook:
      cordon_url: https://scheduler.example.com/api/v1/nodes/cordon
      uncordon_url: https://scheduler.example.com/api/v1/nodes/uncordon
      drain_url: https://scheduler.example.com/api/v1/nodes/drain
      drain_status_url: https://scheduler.example.com/api/v1/nodes/drain-status
      timeout: 30s
      headers:
        Authorization: Bearer ${SCHEDULER_TOKEN}
```

| Field | Description |
|-------|-------------|
| `cordon_url` | Called when a node is cordoned (POST) |
| `uncordon_url` | Called when a node is uncordoned (POST) |
| `drain_url` | Called when a node should be drained (POST) |
| `drain_status_url` | Polled to check if drain is complete (GET) |
| `timeout` | Request timeout (default: 30s) |
| `headers` | Custom headers for authentication |

### Webhook payloads

**POST requests** (cordon, uncordon, drain):

```json
{
  "event": "cordon",
  "node_id": "node-abc123",
  "reason": "GPU failure detected",
  "timestamp": "2024-01-15T10:30:00Z"
}
```

**GET drain status** request includes `?node_id=node-abc123` query parameter.

**Expected response**:

```json
{
  "drained": true,
  "message": "All workloads evicted"
}
```

### No coordinator (default)

Without a coordinator configured, cordon/drain/uncordon operations only update Navarch's internal state. Use this when:

- Running standalone without external schedulers
- Your workload system doesn't need notifications
- You're testing or developing locally

```yaml
server:
  coordinator:
    type: noop
```

## Defaults

Apply defaults to all pools:

```yaml
defaults:
  ssh_keys:
    - ops-team
    - ml-team
  ssh_user: ubuntu
  ssh_private_key_path: ~/.ssh/navarch-key
  health:
    unhealthy_after: 2
    auto_replace: true
```

## Abstract instance types

Use abstract types to provision equivalent hardware across providers:

| Abstract | Lambda | GCP | AWS |
|----------|--------|-----|-----|
| `h100-8x` | `gpu_8x_h100_sxm5` | `a3-highgpu-8g` | `p5.48xlarge` |
| `h100-1x` | `gpu_1x_h100_pcie` | `a3-highgpu-1g` | - |
| `a100-8x` | `gpu_8x_a100` | `a2-highgpu-8g` | `p4d.24xlarge` |
| `a100-4x` | `gpu_4x_a100` | `a2-highgpu-4g` | `p4de.24xlarge` |
| `a100-1x` | `gpu_1x_a100` | `a2-highgpu-1g` | - |

## Environment variables

| Variable | Description |
|----------|-------------|
| `NAVARCH_AUTH_TOKEN` | Authentication token for control plane |
| `LAMBDA_API_KEY` | Lambda Labs API key |
| `GOOGLE_APPLICATION_CREDENTIALS` | GCP credentials file path |
| `AWS_ACCESS_KEY_ID` | AWS access key |
| `AWS_SECRET_ACCESS_KEY` | AWS secret key |
