# Extending Navarch

Navarch is designed to be extended. You can add custom cloud providers, autoscaling strategies, workload notifiers, and metrics sources without modifying the core codebase.

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

## Custom notifiers

Notifiers integrate Navarch with your workload management system. When Navarch cordons, drains, or uncordons a node, the notifier tells your scheduler (Kubernetes, Slurm, Ray, or custom) to stop scheduling work and migrate existing workloads.

### Notifier interface

```go
type Notifier interface {
    // Cordon marks a node as unschedulable. The workload system should stop
    // placing new workloads on this node. Existing workloads continue.
    Cordon(ctx context.Context, nodeID string, reason string) error

    // Uncordon marks a node as schedulable again.
    Uncordon(ctx context.Context, nodeID string) error

    // Drain requests the workload system to migrate workloads off the node.
    Drain(ctx context.Context, nodeID string, reason string) error

    // IsDrained returns true if all workloads have been migrated off
    // the node and it's safe to terminate.
    IsDrained(ctx context.Context, nodeID string) (bool, error)

    // Name returns the notifier name for logging.
    Name() string
}
```

### Example: Kubernetes notifier

```go
package k8snotifier

import (
    "context"
    "fmt"

    corev1 "k8s.io/api/core/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/client-go/kubernetes"
)

type K8sNotifier struct {
    client *kubernetes.Clientset
}

func New(client *kubernetes.Clientset) *K8sNotifier {
    return &K8sNotifier{client: client}
}

func (n *K8sNotifier) Name() string {
    return "kubernetes"
}

func (n *K8sNotifier) Cordon(ctx context.Context, nodeID string, reason string) error {
    node, err := n.client.CoreV1().Nodes().Get(ctx, nodeID, metav1.GetOptions{})
    if err != nil {
        return fmt.Errorf("get node: %w", err)
    }

    node.Spec.Unschedulable = true
    _, err = n.client.CoreV1().Nodes().Update(ctx, node, metav1.UpdateOptions{})
    return err
}

func (n *K8sNotifier) Uncordon(ctx context.Context, nodeID string) error {
    node, err := n.client.CoreV1().Nodes().Get(ctx, nodeID, metav1.GetOptions{})
    if err != nil {
        return fmt.Errorf("get node: %w", err)
    }

    node.Spec.Unschedulable = false
    _, err = n.client.CoreV1().Nodes().Update(ctx, node, metav1.UpdateOptions{})
    return err
}

func (n *K8sNotifier) Drain(ctx context.Context, nodeID string, reason string) error {
    // List pods on the node
    pods, err := n.client.CoreV1().Pods("").List(ctx, metav1.ListOptions{
        FieldSelector: "spec.nodeName=" + nodeID,
    })
    if err != nil {
        return fmt.Errorf("list pods: %w", err)
    }

    // Evict each pod
    for _, pod := range pods.Items {
        if pod.Namespace == "kube-system" {
            continue // Skip system pods
        }
        eviction := &policyv1.Eviction{
            ObjectMeta: metav1.ObjectMeta{
                Name:      pod.Name,
                Namespace: pod.Namespace,
            },
        }
        n.client.CoreV1().Pods(pod.Namespace).Evict(ctx, eviction)
    }
    return nil
}

func (n *K8sNotifier) IsDrained(ctx context.Context, nodeID string) (bool, error) {
    pods, err := n.client.CoreV1().Pods("").List(ctx, metav1.ListOptions{
        FieldSelector: "spec.nodeName=" + nodeID,
    })
    if err != nil {
        return false, fmt.Errorf("list pods: %w", err)
    }

    // Check if only system pods remain
    for _, pod := range pods.Items {
        if pod.Namespace != "kube-system" && pod.Status.Phase == corev1.PodRunning {
            return false, nil
        }
    }
    return true, nil
}
```

### Example: Slurm notifier

```go
package slurmnotifier

import (
    "context"
    "fmt"
    "os/exec"
    "strings"
)

type SlurmNotifier struct{}

func New() *SlurmNotifier {
    return &SlurmNotifier{}
}

func (n *SlurmNotifier) Name() string {
    return "slurm"
}

func (n *SlurmNotifier) Cordon(ctx context.Context, nodeID string, reason string) error {
    // Set node to drain state (no new jobs)
    cmd := exec.CommandContext(ctx, "scontrol", "update", "nodename="+nodeID, "state=drain", "reason="+reason)
    return cmd.Run()
}

func (n *SlurmNotifier) Uncordon(ctx context.Context, nodeID string) error {
    cmd := exec.CommandContext(ctx, "scontrol", "update", "nodename="+nodeID, "state=resume")
    return cmd.Run()
}

func (n *SlurmNotifier) Drain(ctx context.Context, nodeID string, reason string) error {
    // Slurm drain state already prevents new jobs; jobs finish naturally
    return n.Cordon(ctx, nodeID, reason)
}

func (n *SlurmNotifier) IsDrained(ctx context.Context, nodeID string) (bool, error) {
    // Check if any jobs are running on this node
    cmd := exec.CommandContext(ctx, "squeue", "-w", nodeID, "-h", "-o", "%i")
    out, err := cmd.Output()
    if err != nil {
        return false, fmt.Errorf("squeue: %w", err)
    }
    return strings.TrimSpace(string(out)) == "", nil
}
```

### Built-in notifiers

Navarch includes two built-in notifiers:

| Notifier | Description |
|----------|-------------|
| `noop` | Logs operations but takes no action. Default when no notifier is configured. |
| `webhook` | Sends HTTP requests to your workload system. See [configuration](configuration.md#notifier). |

See `pkg/notifier/` for implementation details.

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
