# Fake provider

The fake provider simulates cloud instances by running node agents as goroutines. This enables full system testing without cloud costs or network dependencies.

## Overview

When you provision a "fake" instance, the provider:

1. Creates a fake GPU manager with configurable GPU count.
2. Spawns a node agent in a goroutine.
3. The agent connects to the control plane and registers.
4. The agent sends heartbeats and health check results.
5. The agent responds to commands (cordon, drain).

When you terminate the instance, the goroutine stops cleanly.

## Usage

### Programmatic

```go
import "github.com/NavarchProject/navarch/pkg/provider/fake"

provider, err := fake.New(fake.Config{
    ControlPlaneAddr: "http://localhost:50051",
    GPUCount:         8,
})
if err != nil {
    log.Fatal(err)
}

// Provision spawns a node agent goroutine
node, err := provider.Provision(ctx, provider.ProvisionRequest{
    Name:         "test-node",
    InstanceType: "gpu_8x_h100",
    Region:       "us-west-2",
})

// Node agent is now running and registered with control plane

// Terminate stops the goroutine
provider.Terminate(ctx, node.ID)

// Clean up all instances
provider.TerminateAll()
```

### With pools config

Create a pools configuration that uses the fake provider:

```yaml
pools:
  - name: dev-pool
    provider: fake
    instance_type: gpu_8x_h100
    region: local
    
    scaling:
      min_nodes: 2
      max_nodes: 10
      cooldown_period: 10s
      
      autoscaler:
        type: reactive
        scale_up_threshold: 80
        scale_down_threshold: 20

providers:
  fake:
    gpu_count: 8
```

Start the control plane with this config:

```bash
./control-plane --pools-config pools-dev.yaml
```

The control plane will:

1. Create a fake provider pointing to itself.
2. Create the pool with the specified configuration.
3. When the autoscaler recommends scaling up, fake node agents spawn as goroutines.
4. Each fake agent registers, sends heartbeats, and reports health.
5. When scaling down, agents terminate cleanly.

## Configuration

| Field | Description | Default |
|-------|-------------|---------|
| `ControlPlaneAddr` | Address of the control plane to connect to | Required |
| `GPUCount` | Number of fake GPUs per instance | 8 |
| `Logger` | Logger for provider and node agents | slog.Default() |

## Use cases

### Local development

Test the full system on your laptop:

```bash
# Start control plane with fake provider
./control-plane --pools-config pools-dev.yaml

# Watch nodes register
./navarch list --watch
```

### Integration testing

Test autoscaling, health checks, and commands without cloud dependencies:

```go
func TestAutoscaling(t *testing.T) {
    // Start control plane
    cp := startTestControlPlane(t)
    
    // Create fake provider
    prov, _ := fake.New(fake.Config{
        ControlPlaneAddr: cp.Addr(),
        GPUCount:         4,
    })
    defer prov.TerminateAll()
    
    // Create pool with autoscaler
    pool, _ := pool.New(poolConfig, prov)
    
    // Trigger scale up
    pool.ScaleUp(ctx, 3)
    
    // Verify nodes registered
    nodes := listNodes(cp)
    assert.Len(t, nodes, 3)
}
```

### CI/CD pipelines

Run full system tests in CI without cloud credentials:

```yaml
- name: Run integration tests
  run: |
    go build ./cmd/control-plane
    ./control-plane --pools-config testdata/pools-ci.yaml &
    sleep 2
    go test ./integration/... -v
```

## Behavior

### Health checks

Fake nodes report healthy status for all checks (boot, NVML, XID) by default. To simulate failures, inject XID errors into the fake GPU:

```go
fakeGPU := node.GPU.(*gpu.Fake)
fakeGPU.InjectXIDError(79, "GPU memory error", true)
```

### Commands

Fake nodes acknowledge commands immediately. The cordon and drain commands update internal state but do not perform real workload eviction.

### Metrics

Fake nodes report simulated GPU metrics:

- Temperature: 30-60°C (random)
- Power: 100-200W (random)
- Memory utilization: 0-50% (random)
- GPU utilization: 0-50% (random)

## Limitations

- No actual GPU workloads can run.
- Network simulation is instant (no latency).
- All instances succeed (no provisioning failures).
- No resource constraints (can spawn unlimited instances).

For testing failure scenarios, consider using the simulator instead.

