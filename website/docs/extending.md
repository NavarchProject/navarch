# Extending Navarch

Navarch is designed to be extended. You can add custom cloud providers, autoscaling strategies, and health check logic without modifying the core codebase.

## Custom providers

Providers handle instance lifecycle: provisioning, listing, and terminating GPU instances.

### Provider interface

```go
type Provider interface {
    // Name returns the provider identifier (e.g., "lambda", "gcp", "aws").
    Name() string

    // Provision creates a new instance with the given specification.
    Provision(ctx context.Context, req ProvisionRequest) (*Instance, error)

    // Terminate destroys an instance.
    Terminate(ctx context.Context, instanceID string) error

    // List returns all instances managed by this provider.
    List(ctx context.Context) ([]*Instance, error)
}
```

### ProvisionRequest

```go
type ProvisionRequest struct {
    InstanceType string            // Provider-specific instance type
    Region       string            // Region to provision in
    Zone         string            // Optional: specific zone
    SSHKeyNames  []string          // SSH keys to inject
    Labels       map[string]string // Labels to apply
    UserData     string            // Cloud-init or startup script
}
```

### Example: Custom provider

```go
package myprovider

import (
    "context"
    "github.com/NavarchProject/navarch/pkg/provider"
)

type MyCloudProvider struct {
    client *MyCloudClient
    region string
}

func New(apiKey, region string) (*MyCloudProvider, error) {
    client, err := NewMyCloudClient(apiKey)
    if err != nil {
        return nil, err
    }
    return &MyCloudProvider{client: client, region: region}, nil
}

func (p *MyCloudProvider) Name() string {
    return "mycloud"
}

func (p *MyCloudProvider) Provision(ctx context.Context, req provider.ProvisionRequest) (*provider.Instance, error) {
    resp, err := p.client.CreateInstance(ctx, CreateRequest{
        Type:     req.InstanceType,
        Region:   p.region,
        SSHKeys:  req.SSHKeyNames,
        UserData: req.UserData,
    })
    if err != nil {
        return nil, fmt.Errorf("create instance: %w", err)
    }

    return &provider.Instance{
        ID:           resp.ID,
        ProviderName: p.Name(),
        Status:       provider.StatusPending,
        Region:       p.region,
        InstanceType: req.InstanceType,
    }, nil
}

func (p *MyCloudProvider) Terminate(ctx context.Context, instanceID string) error {
    return p.client.DeleteInstance(ctx, instanceID)
}

func (p *MyCloudProvider) List(ctx context.Context) ([]*provider.Instance, error) {
    instances, err := p.client.ListInstances(ctx)
    if err != nil {
        return nil, err
    }

    result := make([]*provider.Instance, len(instances))
    for i, inst := range instances {
        result[i] = &provider.Instance{
            ID:           inst.ID,
            ProviderName: p.Name(),
            Status:       mapStatus(inst.State),
            PublicIP:     inst.PublicIP,
            PrivateIP:    inst.PrivateIP,
            Region:       inst.Region,
        }
    }
    return result, nil
}
```

See `pkg/provider/lambda/` for a production implementation. For testing, see the [Docker provider](testing.md#docker-provider).

### Registering your provider

```go
myCloud, err := myprovider.New(os.Getenv("MYCLOUD_API_KEY"), "us-east-1")
if err != nil {
    log.Fatal(err)
}

controlPlane.RegisterProvider(myCloud)
```

## Custom autoscalers

Autoscalers decide when to scale pools up or down based on metrics.

### Autoscaler interface

```go
type Autoscaler interface {
    // Name returns the autoscaler type identifier.
    Name() string

    // Recommend returns the target node count for the pool.
    Recommend(ctx context.Context, state PoolState) (ScaleRecommendation, error)
}

type PoolState struct {
    CurrentNodes int
    MinNodes     int
    MaxNodes     int
    Metrics      PoolMetrics
}

type PoolMetrics struct {
    Utilization        float64   // Average GPU utilization (0-100)
    PendingJobs        int       // Jobs waiting to be scheduled
    QueueDepth         int       // Total jobs in queue
    UtilizationHistory []float64 // Historical samples for trend analysis
}

type ScaleRecommendation struct {
    TargetNodes int
    Reason      string
}
```

### Example: Custom autoscaler

A cost-aware autoscaler that scales down aggressively outside business hours:

```go
package costscaler

import (
    "context"
    "time"
    "github.com/NavarchProject/navarch/pkg/pool"
)

type CostAwareScaler struct {
    peakStart     int     // Hour (0-23)
    peakEnd       int
    peakThreshold float64 // Scale up when utilization exceeds this
    offPeakMin    int     // Minimum nodes outside peak hours
}

func New(peakStart, peakEnd int, threshold float64, offPeakMin int) *CostAwareScaler {
    return &CostAwareScaler{
        peakStart:     peakStart,
        peakEnd:       peakEnd,
        peakThreshold: threshold,
        offPeakMin:    offPeakMin,
    }
}

func (s *CostAwareScaler) Name() string {
    return "cost-aware"
}

func (s *CostAwareScaler) Recommend(ctx context.Context, state pool.PoolState) (pool.ScaleRecommendation, error) {
    hour := time.Now().Hour()
    isPeak := hour >= s.peakStart && hour < s.peakEnd

    current := state.CurrentNodes
    util := state.Metrics.Utilization

    // Outside peak hours, scale to minimum
    if !isPeak {
        target := s.offPeakMin
        if target < state.MinNodes {
            target = state.MinNodes
        }
        return pool.ScaleRecommendation{
            TargetNodes: target,
            Reason:      "off-peak hours, scaling to minimum",
        }, nil
    }

    // During peak, scale based on utilization
    if util > s.peakThreshold && current < state.MaxNodes {
        return pool.ScaleRecommendation{
            TargetNodes: current + 1,
            Reason:      fmt.Sprintf("utilization %.1f%% exceeds threshold", util),
        }, nil
    }

    if util < s.peakThreshold/2 && current > state.MinNodes {
        return pool.ScaleRecommendation{
            TargetNodes: current - 1,
            Reason:      fmt.Sprintf("utilization %.1f%% below threshold", util),
        }, nil
    }

    return pool.ScaleRecommendation{
        TargetNodes: current,
        Reason:      "no change needed",
    }, nil
}
```

See `pkg/pool/autoscaler.go` for built-in implementations (reactive, queue, scheduled, predictive, composite).

## Custom metrics sources

For queue-based or custom autoscaling, provide metrics from your scheduler.

### MetricsSource interface

```go
type MetricsSource interface {
    GetPoolMetrics(ctx context.Context, poolName string) (*PoolMetrics, error)
}
```

### Example: Kubernetes integration

```go
type K8sMetricsSource struct {
    client    *kubernetes.Clientset
    namespace string
}

func (m *K8sMetricsSource) GetPoolMetrics(ctx context.Context, poolName string) (*PoolMetrics, error) {
    pods, err := m.client.CoreV1().Pods(m.namespace).List(ctx, metav1.ListOptions{
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

    return &PoolMetrics{
        PendingJobs: pending,
        QueueDepth:  pending + running,
    }, nil
}
```

### Example: Slurm integration

```go
type SlurmMetricsSource struct {
    partition string
}

func (m *SlurmMetricsSource) GetPoolMetrics(ctx context.Context, poolName string) (*PoolMetrics, error) {
    // Query pending jobs
    out, err := exec.CommandContext(ctx, "squeue", "-p", m.partition, "-t", "PENDING", "-h", "-o", "%i").Output()
    if err != nil {
        return nil, err
    }
    pending := len(strings.Split(strings.TrimSpace(string(out)), "\n"))

    // Query running jobs
    out, err = exec.CommandContext(ctx, "squeue", "-p", m.partition, "-t", "RUNNING", "-h", "-o", "%i").Output()
    if err != nil {
        return nil, err
    }
    running := len(strings.Split(strings.TrimSpace(string(out)), "\n"))

    return &PoolMetrics{
        PendingJobs: pending,
        QueueDepth:  pending + running,
    }, nil
}
```

## Testing extensions

### Unit tests

```go
func TestCostAwareScaler_OffPeak(t *testing.T) {
    scaler := costscaler.New(9, 18, 80.0, 2)

    state := pool.PoolState{
        CurrentNodes: 10,
        MinNodes:     1,
        MaxNodes:     20,
        Metrics:      pool.PoolMetrics{Utilization: 50.0},
    }

    // Test at 3am (off-peak)
    rec, err := scaler.Recommend(context.Background(), state)
    require.NoError(t, err)
    assert.Equal(t, 2, rec.TargetNodes) // Should scale to off-peak minimum
}
```

### Integration tests with simulator

Test your extensions with realistic scenarios:

```yaml
name: test-custom-autoscaler
fleet:
  - id: node-1
    provider: fake
    gpu_count: 8
  - id: node-2
    provider: fake
    gpu_count: 8

events:
  - at: 0s
    action: start_fleet
  - at: 30s
    action: assert
    target: node-1
    params:
      expected_status: active
```

## Best practices

1. **Handle context cancellation**: Check `ctx.Done()` in long-running operations.

2. **Return wrapped errors**: Use `fmt.Errorf("operation: %w", err)` for debuggable errors.

3. **Log at appropriate levels**: Debug for verbose details, Info for operations, Error for failures.

4. **Test with the simulator**: Validate behavior before deploying.

5. **Document configuration options**: Make your extension easy to configure.
