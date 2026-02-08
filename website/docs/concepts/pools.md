# Pools & Providers

Pools organize your GPU nodes. Providers connect to cloud platforms.

## Pools

A pool is a group of GPU nodes with shared configuration:

- Same cloud provider and region.
- Same instance type (GPU count and model).
- Common scaling limits and autoscaler configuration.
- Unified health and replacement policies.

Pools let you manage different workload types independently:

```yaml
pools:
  # Training pool: Large instances, conservative scaling
  training:
    provider: lambda
    instance_type: gpu_8x_h100_sxm5
    min_nodes: 2
    max_nodes: 20
    cooldown: 10m

  # Inference pool: Smaller instances, aggressive scaling
  inference:
    provider: lambda
    instance_type: gpu_1x_a100
    min_nodes: 5
    max_nodes: 100
    cooldown: 2m
```

### When to use multiple pools

- **Different instance types**: Training on 8xH100, inference on 1xA100.
- **Different regions**: US pool for US users, EU pool for EU users.
- **Different scaling behavior**: Batch jobs scale to zero, serving keeps minimum capacity.
- **Different teams**: Separate pools for separate cost tracking.

## Providers

A provider abstracts cloud-specific operations:

- Provisioning new instances.
- Terminating instances.
- Listing available instance types.
- Managing SSH keys and startup scripts.

### Supported providers

| Provider | Description |
|----------|-------------|
| `lambda` | Lambda Labs Cloud GPU instances. |
| `gcp` | Google Cloud Platform. |
| `aws` | Amazon Web Services. |
| `fake` | Simulated instances for development and testing. |

### Provider configuration

Providers are configured separately from pools:

```yaml
providers:
  lambda:
    type: lambda
    api_key_env: LAMBDA_API_KEY

  gcp:
    type: gcp
    project: my-project
    credentials_file: /path/to/credentials.json

pools:
  training:
    provider: lambda  # References the provider above
    # ...
```

This lets you:

- Use the same provider with different credentials.
- Switch providers without changing pool configuration.
- Test with the fake provider before using real clouds.

See [Configuration Reference](../configuration.md#providers) for all provider options.

## Labels

Labels are key-value pairs attached to pools and nodes. Use them for:

- Filtering nodes by workload type.
- Routing jobs to appropriate pools.
- Organizing resources by team or project.

```yaml
pools:
  training:
    provider: lambda
    instance_type: gpu_8x_h100
    labels:
      workload: training
      team: ml-platform
      environment: production
```

Labels propagate to nodes when they're provisioned. Query nodes by label:

```bash
navarch list --label workload=training
```

See [CLI Reference](../cli.md) for all filtering options.

## Multi-cloud setup

Navarch can manage nodes across multiple providers simultaneously:

```yaml
providers:
  lambda:
    type: lambda
    api_key_env: LAMBDA_API_KEY

  gcp:
    type: gcp
    project: my-project

pools:
  # Primary pool on Lambda
  training-primary:
    provider: lambda
    instance_type: gpu_8x_h100_sxm5
    region: us-west-1
    min_nodes: 4
    max_nodes: 20

  # Overflow pool on GCP
  training-overflow:
    provider: gcp
    instance_type: a3-highgpu-8g
    region: us-central1
    min_nodes: 0
    max_nodes: 10
```

This enables:

- **Failover**: If Lambda is out of capacity, use GCP.
- **Cost optimization**: Use the cheapest available provider.
- **Geographic distribution**: Run nodes closer to users.
