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
  health_policy: ./health-policy.yaml  # Custom health policy file
```

All fields are optional and have sensible defaults.

| Field | Default | Description |
|-------|---------|-------------|
| `address` | `:50051` | gRPC/HTTP listen address |
| `heartbeat_interval` | `30s` | How often nodes send heartbeats |
| `health_check_interval` | `60s` | How often health checks run |
| `autoscale_interval` | `30s` | How often autoscaler evaluates |
| `health_policy` | (none) | Path to custom health policy YAML file |

### Authentication

The control plane supports bearer token authentication. Set the `NAVARCH_AUTH_TOKEN` environment variable or use the `--auth-token` flag to enable it.

```bash
export NAVARCH_AUTH_TOKEN="your-secret-token"
control-plane --config config.yaml
```

For details on client configuration and custom authentication methods, see [authentication](authentication.md).

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

## Health policy

Navarch uses [CEL (Common Expression Language)](https://github.com/google/cel-spec) to evaluate GPU health events and determine node health status. You can customize this logic by providing a health policy file.

If no policy is specified, Navarch uses a built-in default policy that classifies fatal XID errors (like XID 79 "GPU has fallen off the bus") as unhealthy and recoverable errors as degraded.

### Policy file format

```yaml
version: v1

metadata:
  name: my-policy
  description: Custom health policy for my fleet

rules:
  # More specific rules first
  - name: fatal-xid
    description: XID errors indicating unrecoverable GPU failure
    condition: |
      event.event_type == "xid" && event.metrics.xid_code in [48, 79, 95]
    result: unhealthy

  - name: recoverable-xid
    description: XID errors that may recover
    condition: event.event_type == "xid"
    result: degraded

  - name: thermal-critical
    condition: |
      event.event_type == "thermal" &&
      event.metrics.temperature >= 95
    result: unhealthy

  # Default rule must be last
  - name: default
    condition: "true"
    result: healthy
```

Rules are evaluated in order; the first matching rule determines the result. Place more specific rules before general ones, and always include a default rule at the end.

### Rule fields

| Field | Required | Description |
|-------|----------|-------------|
| `name` | Yes | Unique rule identifier |
| `description` | No | Human-readable description |
| `condition` | Yes | CEL expression that returns true when rule matches |
| `result` | Yes | Result when rule matches: `healthy`, `degraded`, or `unhealthy` |

### CEL event fields

The following fields are available in CEL expressions:

| Field | Type | Description |
|-------|------|-------------|
| `event.event_type` | string | Event type: `xid`, `thermal`, `ecc_dbe`, `ecc_sbe`, `nvlink`, `pcie`, `power` |
| `event.system` | string | DCGM health watch system identifier |
| `event.gpu_index` | int | GPU index (0-based, -1 for node-level) |
| `event.metrics` | map | Event-specific metrics |
| `event.message` | string | Human-readable description |

Common metrics by event type:

| Event Type | Metric | Type | Description |
|------------|--------|------|-------------|
| `xid` | `xid_code` | int | NVIDIA XID error code |
| `thermal` | `temperature` | int | GPU temperature in Celsius |
| `ecc_dbe` | `ecc_dbe_count` | int | Double-bit ECC error count |
| `ecc_sbe` | `ecc_sbe_count` | int | Single-bit ECC error count |

### Example policies

#### Strict policy

Treat all XID errors as fatal:

```yaml
rules:
  - name: any-xid-fatal
    condition: event.event_type == "xid"
    result: unhealthy
  - name: default
    condition: "true"
    result: healthy
```

#### Permissive policy

Only fail on the most severe errors:

```yaml
rules:
  - name: bus-error-only
    condition: |
      event.event_type == "xid" && event.metrics.xid_code == 79
    result: unhealthy
  - name: default
    condition: "true"
    result: healthy
```

#### GPU-specific policy

Different thresholds for different GPUs:

```yaml
rules:
  - name: gpu0-strict
    description: GPU 0 is critical, any error is fatal
    condition: event.gpu_index == 0 && event.event_type == "xid"
    result: unhealthy
  - name: other-gpu-permissive
    condition: event.event_type == "xid"
    result: degraded
  - name: default
    condition: "true"
    result: healthy
```

### Testing policies

Use the [simulator](simulator.md) to test health policies before deploying to production. The simulator HTML report includes a "Policy Rules" section showing which rules matched for each failure.

```bash
./bin/simulator run scenarios/xid-classification.yaml -v
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
