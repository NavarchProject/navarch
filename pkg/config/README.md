# Configuration

The `pkg/config` package provides configuration parsing and validation for Navarch.

## Format

Configuration files use YAML with versioned resources:

```yaml
apiVersion: navarch.io/v1alpha1
kind: Pool
metadata:
  name: training
  labels:
    workload: training
spec:
  providerRef: lambda
  instanceType: gpu_8x_h100
  # ...
```

### Fields

- **apiVersion**: Always `navarch.io/v1alpha1` (required)
- **kind**: Resource type: `ControlPlane`, `Pool`, or `Provider` (required)
- **metadata**: Resource metadata (name, labels, annotations)
- **spec**: Resource specification
- **status**: Observed state (read-only, set by system)

### Multi-document files

Multiple resources can be defined in a single file using `---` separators:

```yaml
apiVersion: navarch.io/v1alpha1
kind: Provider
metadata:
  name: lambda
spec:
  type: lambda
---
apiVersion: navarch.io/v1alpha1
kind: Pool
metadata:
  name: training
spec:
  providerRef: lambda
  # ...
```

## Resource types

### ControlPlane

Configures the Navarch control plane:

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

### Provider

Configures cloud provider credentials:

```yaml
# Lambda Labs
apiVersion: navarch.io/v1alpha1
kind: Provider
metadata:
  name: lambda
spec:
  type: lambda
  lambda:
    apiKeyEnvVar: LAMBDA_API_KEY
---
# GCP
apiVersion: navarch.io/v1alpha1
kind: Provider
metadata:
  name: gcp
spec:
  type: gcp
  gcp:
    project: my-project
    credentialsSecretRef:
      name: gcp-credentials
---
# Fake (for development)
apiVersion: navarch.io/v1alpha1
kind: Provider
metadata:
  name: fake
spec:
  type: fake
  fake:
    gpuCount: 8
```

### Pool

Configures a GPU node pool:

```yaml
apiVersion: navarch.io/v1alpha1
kind: Pool
metadata:
  name: training
  labels:
    workload: training
spec:
  providerRef: lambda          # References a Provider by name
  instanceType: gpu_8x_h100
  region: us-west-2
  zones:
    - us-west-2a
    - us-west-2b
  sshKeyNames:
    - ops-team
  labels:
    workload: training
  scaling:
    minReplicas: 2
    maxReplicas: 20
    cooldownPeriod: 5m
    autoscaler:
      type: reactive
      scaleUpThreshold: 80
      scaleDownThreshold: 20
  health:
    unhealthyThreshold: 2
    autoReplace: true
```

## Autoscaler types

### Reactive

Scales based on current utilization:

```yaml
autoscaler:
  type: reactive
  scaleUpThreshold: 80
  scaleDownThreshold: 20
```

### Queue-based

Scales based on job queue depth:

```yaml
autoscaler:
  type: queue
  jobsPerNode: 100
```

### Scheduled

Adjusts limits based on time:

```yaml
autoscaler:
  type: scheduled
  schedule:
    - daysOfWeek: [monday, tuesday, wednesday, thursday, friday]
      startHour: 9
      endHour: 18
      minReplicas: 10
      maxReplicas: 100
  fallback:
    type: reactive
    scaleUpThreshold: 80
    scaleDownThreshold: 20
```

### Predictive

Uses historical data to forecast demand:

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

### Composite

Combines multiple strategies:

```yaml
autoscaler:
  type: composite
  mode: max  # max, min, or avg
  autoscalers:
    - type: reactive
      scaleUpThreshold: 70
      scaleDownThreshold: 30
    - type: queue
      jobsPerNode: 50
```

## Usage

### Loading configuration

```go
import "github.com/NavarchProject/navarch/pkg/config"

cfg, err := config.Load("navarch.yaml")
if err != nil {
    log.Fatal(err)
}

// Apply defaults
cfg.Defaults()

// Validate
if err := cfg.Validate(); err != nil {
    log.Fatal(err)
}

// Access resources
if cfg.ControlPlane != nil {
    fmt.Println("Address:", cfg.ControlPlane.Spec.Address)
}

for _, pool := range cfg.Pools {
    fmt.Println("Pool:", pool.Metadata.Name)
}

provider := cfg.GetProvider("lambda")
```

### Parsing from bytes

```go
yaml := []byte(`
apiVersion: navarch.io/v1alpha1
kind: Pool
metadata:
  name: test
spec:
  providerRef: fake
  instanceType: gpu_1x
  region: local
  scaling:
    minReplicas: 1
    maxReplicas: 5
`)

cfg, err := config.Parse(yaml)
```

## Validation

The `Validate()` method checks:

- All Pools have a name
- All Pools reference an existing Provider
- minReplicas >= 0
- maxReplicas >= minReplicas
- All Providers have a type

## Defaults

The `Defaults()` method applies default values:

| Field | Default |
|-------|---------|
| ControlPlane.spec.address | `:50051` |
| ControlPlane.spec.healthCheckInterval | `60s` |
| ControlPlane.spec.heartbeatInterval | `30s` |
| ControlPlane.spec.enabledHealthChecks | `[boot, nvml, xid]` |
| ControlPlane.spec.autoscaleInterval | `30s` |
| Pool.spec.health.unhealthyThreshold | `3` |
| Provider.spec.fake.gpuCount | `8` |

## Example

See `examples/navarch.yaml` for a complete configuration example.

