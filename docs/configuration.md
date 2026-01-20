# Configuration reference

Navarch uses a YAML configuration format with versioned resources.

## Format overview

Each resource has four sections:

```yaml
apiVersion: navarch.io/v1alpha1  # API version
kind: Pool                        # Resource type
metadata:                         # Resource metadata
  name: training
  labels:
    workload: training
spec:                             # Resource specification
  providerRef: lambda
  instanceType: gpu_8x_h100
```

Multiple resources can be defined in a single file using `---` separators.

## Resource types

Navarch supports three resource types:

| Kind | Description |
|------|-------------|
| `ControlPlane` | Control plane configuration (one per cluster). |
| `Provider` | Cloud provider credentials and settings. |
| `Pool` | GPU node pool with scaling and health policies. |

## ControlPlane

Configures the Navarch control plane server.

```yaml
apiVersion: navarch.io/v1alpha1
kind: ControlPlane
metadata:
  name: production
spec:
  address: ":50051"
  healthCheckInterval: 60s
  heartbeatInterval: 30s
  enabledHealthChecks:
    - boot
    - nvml
    - xid
  autoscaleInterval: 30s
  tls:
    enabled: true
    certFile: /etc/navarch/tls.crt
    keyFile: /etc/navarch/tls.key
```

### ControlPlane spec fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `address` | string | `:50051` | Listen address for the HTTP server. |
| `healthCheckInterval` | duration | `60s` | How often nodes run health checks. |
| `heartbeatInterval` | duration | `30s` | How often nodes send heartbeats. |
| `enabledHealthChecks` | []string | `[boot, nvml, xid]` | Health checks to enable on nodes. |
| `autoscaleInterval` | duration | `30s` | How often the autoscaler evaluates pools. |
| `tls.enabled` | bool | `false` | Enable TLS for the HTTP server. |
| `tls.certFile` | string | | Path to TLS certificate file. |
| `tls.keyFile` | string | | Path to TLS private key file. |

## Provider

Configures cloud provider credentials and settings. Pools reference providers by name.

### Lambda Labs

```yaml
apiVersion: navarch.io/v1alpha1
kind: Provider
metadata:
  name: lambda
spec:
  type: lambda
  lambda:
    apiKeyEnvVar: LAMBDA_API_KEY
```

Set the API key as an environment variable:

```bash
export LAMBDA_API_KEY=your-api-key
```

### GCP

```yaml
apiVersion: navarch.io/v1alpha1
kind: Provider
metadata:
  name: gcp
spec:
  type: gcp
  gcp:
    project: my-gcp-project
    credentialsSecretRef:
      name: gcp-credentials
      key: credentials.json
```

### AWS

```yaml
apiVersion: navarch.io/v1alpha1
kind: Provider
metadata:
  name: aws
spec:
  type: aws
  aws:
    region: us-east-1
    credentialsSecretRef:
      name: aws-credentials
```

### Fake provider

For development and testing without cloud costs:

```yaml
apiVersion: navarch.io/v1alpha1
kind: Provider
metadata:
  name: fake
spec:
  type: fake
  fake:
    gpuCount: 8
```

The fake provider simulates GPU instances by running node agents as goroutines.

### Provider spec fields

| Field | Type | Description |
|-------|------|-------------|
| `type` | string | Provider type: `lambda`, `gcp`, `aws`, or `fake`. |
| `lambda.apiKeyEnvVar` | string | Environment variable containing the Lambda API key. |
| `gcp.project` | string | GCP project ID. |
| `gcp.credentialsSecretRef` | SecretRef | Reference to GCP credentials. |
| `aws.region` | string | AWS region. |
| `aws.credentialsSecretRef` | SecretRef | Reference to AWS credentials. |
| `fake.gpuCount` | int | Number of fake GPUs per instance (default: 8). |

## Pool

Configures a GPU node pool with scaling and health policies. A pool can use a single provider or multiple providers for fungible compute.

### Single-provider pool

```yaml
apiVersion: navarch.io/v1alpha1
kind: Pool
metadata:
  name: training
spec:
  providerRef: lambda
  instanceType: gpu_8x_h100_sxm5
  region: us-west-2
  scaling:
    minReplicas: 2
    maxReplicas: 20
    autoscaler:
      type: reactive
      scaleUpThreshold: 80
      scaleDownThreshold: 20
  health:
    unhealthyThreshold: 2
    autoReplace: true
```

### Multi-provider pool (fungible compute)

Multi-provider pools treat GPUs as fungible compute across cloud providers. If Lambda Labs has no capacity, Navarch automatically provisions from GCP. This is the core promise of Navarch.

```yaml
apiVersion: navarch.io/v1alpha1
kind: Pool
metadata:
  name: fungible
spec:
  providers:
    - name: lambda
      priority: 1
      regions: [us-west-2, us-east-1]
    - name: gcp
      priority: 2
      regions: [us-central1]
      instanceType: a3-highgpu-8g  # Provider-specific override
  providerStrategy: priority
  instanceType: h100-8x  # Abstract type
  scaling:
    minReplicas: 4
    maxReplicas: 32
    autoscaler:
      type: reactive
      scaleUpThreshold: 75
      scaleDownThreshold: 25
  health:
    unhealthyThreshold: 2
    autoReplace: true
```

### Provider selection strategies

When using multiple providers, `providerStrategy` determines how Navarch chooses between them:

| Strategy | Description |
|----------|-------------|
| `priority` | Tries providers in priority order (lowest number first). Falls back on failure. Default. |
| `cost` | Queries providers for pricing and selects the cheapest with available capacity. |
| `availability` | Queries providers for capacity and selects the first with availability. |
| `round-robin` | Distributes provisioning evenly across providers using weights. |

### Abstract instance types

Abstract types let you request hardware without knowing provider-specific names:

| Abstract type | Lambda | GCP | AWS |
|---------------|--------|-----|-----|
| `h100-8x` | `gpu_8x_h100_sxm5` | `a3-highgpu-8g` | `p5.48xlarge` |
| `h100-1x` | `gpu_1x_h100_pcie` | `a3-highgpu-1g` | `p5.xlarge` |
| `a100-8x` | `gpu_8x_a100` | `a2-highgpu-8g` | `p4d.24xlarge` |
| `a100-4x` | `gpu_4x_a100` | `a2-highgpu-4g` | `p4de.24xlarge` |
| `a100-1x` | `gpu_1x_a100` | `a2-highgpu-1g` | - |
| `a10-1x` | `gpu_1x_a10` | `g2-standard-4` | `g5.xlarge` |

You can also use provider-specific instance types directly.

### Provider reference fields

When using `providers` (multi-provider):

| Field | Type | Description |
|-------|------|-------------|
| `providers[].name` | string | Name of the Provider resource. |
| `providers[].priority` | int | Selection priority (lower = preferred). |
| `providers[].weight` | int | Weight for round-robin distribution. |
| `providers[].regions` | []string | Regions to use with this provider. |
| `providers[].instanceType` | string | Provider-specific instance type override. |

### Pool spec fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `providerRef` | string | * | Name of a single Provider. Mutually exclusive with `providers`. |
| `providers` | []ProviderRef | * | Multiple providers for fungible compute. Mutually exclusive with `providerRef`. |
| `providerStrategy` | string | No | Selection strategy: `priority`, `cost`, `availability`, or `round-robin`. |
| `instanceType` | string | Yes | Instance type (abstract or provider-specific). |
| `region` | string | No | Default cloud region for instances. |
| `zones` | []string | No | Availability zones for multi-zone pools. |
| `sshKeyNames` | []string | No | SSH key names to install on instances. |
| `labels` | map | No | Labels applied to provisioned nodes. |
| `scaling.minReplicas` | int | Yes | Minimum number of nodes (hard floor). |
| `scaling.maxReplicas` | int | Yes | Maximum number of nodes (hard ceiling). |
| `scaling.cooldownPeriod` | duration | No | Minimum time between scaling actions. |
| `scaling.autoscaler` | AutoscalerSpec | No | Autoscaler configuration. |
| `health.unhealthyThreshold` | int | No | Consecutive failures before unhealthy (default: 3). |
| `health.autoReplace` | bool | No | Automatically replace unhealthy nodes. |

\* Either `providerRef` or `providers` is required, but not both.

## Autoscaler configuration

The autoscaler determines when to add or remove nodes from a pool.

### Reactive autoscaler

Scales based on current GPU utilization.

```yaml
autoscaler:
  type: reactive
  scaleUpThreshold: 80
  scaleDownThreshold: 20
```

| Field | Type | Description |
|-------|------|-------------|
| `scaleUpThreshold` | float | Scale up when utilization exceeds this percentage. |
| `scaleDownThreshold` | float | Scale down when utilization falls below this percentage. |

### Queue-based autoscaler

Scales based on pending job count.

```yaml
autoscaler:
  type: queue
  jobsPerNode: 100
```

| Field | Type | Description |
|-------|------|-------------|
| `jobsPerNode` | int | Target number of jobs per node. |

### Scheduled autoscaler

Adjusts scaling limits based on time of day.

```yaml
autoscaler:
  type: scheduled
  schedule:
    - daysOfWeek: [monday, tuesday, wednesday, thursday, friday]
      startHour: 9
      endHour: 18
      minReplicas: 10
      maxReplicas: 100
    - daysOfWeek: [saturday, sunday]
      startHour: 0
      endHour: 24
      minReplicas: 0
      maxReplicas: 10
  fallback:
    type: reactive
    scaleUpThreshold: 80
    scaleDownThreshold: 20
```

| Field | Type | Description |
|-------|------|-------------|
| `schedule[].daysOfWeek` | []string | Days this entry applies to (empty means all days). |
| `schedule[].startHour` | int | Start hour (0-23, inclusive). |
| `schedule[].endHour` | int | End hour (0-23, exclusive). |
| `schedule[].minReplicas` | int | Override minReplicas during this window. |
| `schedule[].maxReplicas` | int | Override maxReplicas during this window. |
| `fallback` | AutoscalerSpec | Autoscaler to use for actual scaling decisions. |

### Predictive autoscaler

Uses historical data to anticipate demand.

```yaml
autoscaler:
  type: predictive
  lookbackWindow: 30
  growthFactor: 1.5
  fallback:
    type: reactive
    scaleUpThreshold: 70
    scaleDownThreshold: 30
```

| Field | Type | Description |
|-------|------|-------------|
| `lookbackWindow` | int | Number of utilization samples to analyze. |
| `growthFactor` | float | Multiplier for predicted growth (1.5 = 50% buffer). |
| `fallback` | AutoscalerSpec | Autoscaler to use when insufficient history. |

### Composite autoscaler

Combines multiple autoscaling strategies.

```yaml
autoscaler:
  type: composite
  mode: max
  autoscalers:
    - type: reactive
      scaleUpThreshold: 70
      scaleDownThreshold: 30
    - type: queue
      jobsPerNode: 50
```

| Field | Type | Description |
|-------|------|-------------|
| `mode` | string | How to combine recommendations: `max`, `min`, or `avg`. |
| `autoscalers` | []AutoscalerSpec | List of autoscalers to query. |

## Metadata fields

All resources support standard metadata fields:

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Unique identifier for the resource. |
| `namespace` | string | Optional namespace (reserved for future use). |
| `labels` | map | Key-value labels for filtering and selection. |
| `annotations` | map | Key-value annotations for metadata. |

## Duration format

Duration fields accept Go duration strings:

- `30s` - 30 seconds.
- `5m` - 5 minutes.
- `1h30m` - 1 hour and 30 minutes.
- `2h` - 2 hours.

## Complete example

See `examples/navarch.yaml` for a complete configuration with multiple providers and pools.

```yaml
apiVersion: navarch.io/v1alpha1
kind: ControlPlane
metadata:
  name: production
spec:
  address: ":50051"
  healthCheckInterval: 60s
  heartbeatInterval: 30s
---
apiVersion: navarch.io/v1alpha1
kind: Provider
metadata:
  name: lambda
spec:
  type: lambda
  lambda:
    apiKeyEnvVar: LAMBDA_API_KEY
---
apiVersion: navarch.io/v1alpha1
kind: Pool
metadata:
  name: training
spec:
  providerRef: lambda
  instanceType: gpu_8x_h100_sxm5
  region: us-west-2
  scaling:
    minReplicas: 2
    maxReplicas: 20
    autoscaler:
      type: reactive
      scaleUpThreshold: 80
      scaleDownThreshold: 20
  health:
    autoReplace: true
```

