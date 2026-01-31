# Config package

This package provides YAML-based configuration loading for Navarch.

## Overview

The config package parses a YAML configuration file that defines:

- Server settings (addresses, intervals).
- Cloud provider credentials and settings.
- GPU node pool configurations.
- Autoscaler policies.
- Default values.

## Configuration file structure

```yaml
server:
  address: ":50051"
  heartbeat_interval: 30s
  health_check_interval: 60s
  autoscale_interval: 30s

providers:
  lambda:
    type: lambda
    api_key_env: LAMBDA_API_KEY

  gcp-us:
    type: gcp
    project: my-project
    zone: us-central1-a

pools:
  training:
    provider: lambda
    instance_type: gpu_8x_h100_sxm5
    region: us-west-2
    min_nodes: 2
    max_nodes: 20
    autoscaler:
      type: reactive
      scale_up_threshold: 80
      scale_down_threshold: 20

  inference:
    providers:
      - name: lambda
        priority: 1
      - name: gcp-us
        priority: 2
    strategy: priority
    instance_type: gpu_1x_a100
    min_nodes: 1
    max_nodes: 10

defaults:
  cooldown_period: 5m
  unhealthy_threshold: 2
  auto_replace: true
```

## Loading configuration

```go
import "github.com/NavarchProject/navarch/pkg/config"

cfg, err := config.Load("navarch.yaml")
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Server address: %s\n", cfg.Server.Address)
fmt.Printf("Pools: %d\n", len(cfg.Pools))
```

## Provider types

| Type | Required fields |
|------|-----------------|
| `lambda` | `api_key_env` (environment variable name) |
| `gcp` | `project`, `zone` |
| `aws` | `region` |
| `fake` | `gpu_count` (optional, for testing) |

## Pool configuration

### Single provider

```yaml
pools:
  my-pool:
    provider: lambda
    instance_type: gpu_8x_h100_sxm5
    region: us-west-2
    min_nodes: 2
    max_nodes: 20
```

### Multi-provider (fungible compute)

```yaml
pools:
  fungible-pool:
    providers:
      - name: lambda
        priority: 1
      - name: gcp-us
        priority: 2
    strategy: priority  # or: cost, availability, round-robin
    instance_type: h100-8x
    min_nodes: 4
    max_nodes: 32
```

## Autoscaler configuration

```yaml
autoscaler:
  type: reactive  # or: queue, scheduled, predictive, composite
  scale_up_threshold: 80
  scale_down_threshold: 20
```

See `pkg/pool/README.md` for details on each autoscaler type.

## Default values

The `defaults` section applies to all pools unless overridden:

```yaml
defaults:
  cooldown_period: 5m
  unhealthy_threshold: 2
  auto_replace: true
  labels:
    managed-by: navarch
```

## Validation

The `Load` function validates configuration and returns errors for:

- Missing required fields.
- Unknown provider types.
- Invalid pool references.
- Conflicting settings.

## Environment variable substitution

Provider credentials should use environment variables rather than inline values:

```yaml
providers:
  lambda:
    type: lambda
    api_key_env: LAMBDA_API_KEY  # Reads from $LAMBDA_API_KEY
```

## Testing

```bash
go test ./pkg/config/... -v
```
