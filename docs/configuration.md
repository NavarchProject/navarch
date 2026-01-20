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
navarch-control-plane --config config.yaml
```

## Configuration reference

### Server

```yaml
server:
  address: ":50051"              # Listen address
  heartbeat_interval: 30s        # Node heartbeat frequency
  health_check_interval: 60s     # Health check frequency
  autoscale_interval: 30s        # Autoscaler evaluation frequency
```

All fields are optional and have sensible defaults.

### Providers

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

Supported provider types:

| Type | Description |
|------|-------------|
| `lambda` | Lambda Labs Cloud |
| `gcp` | Google Cloud Platform (coming soon) |
| `aws` | Amazon Web Services (coming soon) |
| `fake` | Fake provider for local development |

### Pools

Pools define groups of GPU nodes with scaling policies.

#### Single-provider pool

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

#### Multi-provider pool

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

#### Pool fields

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
| `autoscaling` | No | Autoscaler configuration |
| `health` | No | Health check configuration |

*Either `provider` or `providers` is required, but not both.

### Autoscaling

#### Reactive

Scale based on current utilization:

```yaml
autoscaling:
  type: reactive
  scale_up_at: 80      # Scale up when utilization exceeds 80%
  scale_down_at: 20    # Scale down when utilization drops below 20%
```

#### Queue-based

Scale based on pending jobs:

```yaml
autoscaling:
  type: queue
  jobs_per_node: 100   # Target jobs per node
```

#### Scheduled

Different limits by time:

```yaml
autoscaling:
  type: scheduled
  schedule:
    - days: [monday, tuesday, wednesday, thursday, friday]
      start: 9
      end: 18
      min_nodes: 10
      max_nodes: 100
    - days: [saturday, sunday]
      start: 0
      end: 24
      min_nodes: 0
      max_nodes: 10
  fallback:
    type: reactive
    scale_up_at: 80
    scale_down_at: 20
```

#### Predictive

Uses historical data to forecast demand:

```yaml
autoscaling:
  type: predictive
  lookback_window: 30    # Number of samples to consider
  growth_factor: 1.5     # Scale prediction by this factor
  fallback:
    type: reactive
    scale_up_at: 70
    scale_down_at: 30
```

#### Composite

Combine multiple strategies:

```yaml
autoscaling:
  type: composite
  mode: max              # max, min, or avg
  autoscalers:
    - type: reactive
      scale_up_at: 70
      scale_down_at: 30
    - type: queue
      jobs_per_node: 50
```

### Health

```yaml
health:
  unhealthy_after: 2     # Consecutive failures before unhealthy
  auto_replace: true     # Automatically replace unhealthy nodes
```

### Defaults

Apply defaults to all pools:

```yaml
defaults:
  ssh_keys:
    - ops-team
    - ml-team
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
| `LAMBDA_API_KEY` | Lambda Labs API key |
| `GOOGLE_APPLICATION_CREDENTIALS` | GCP credentials file path |
| `AWS_ACCESS_KEY_ID` | AWS access key |
| `AWS_SECRET_ACCESS_KEY` | AWS secret key |
