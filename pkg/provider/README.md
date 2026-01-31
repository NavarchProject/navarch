# Provider package

This package defines the cloud provider interface and common types for GPU instance provisioning.

## Overview

The provider package provides:

- A `Provider` interface for cloud-agnostic provisioning.
- Common types for nodes, instance types, and requests.
- An optional `InstanceTypeLister` interface for querying available types.

## Provider interface

```go
type Provider interface {
    Name() string
    Provision(ctx context.Context, req ProvisionRequest) (*Node, error)
    Terminate(ctx context.Context, nodeID string) error
    List(ctx context.Context) ([]*Node, error)
}
```

| Method | Description |
|--------|-------------|
| `Name()` | Returns the provider name (e.g., "lambda", "gcp", "aws") |
| `Provision()` | Creates a new GPU instance |
| `Terminate()` | Terminates an instance by ID |
| `List()` | Lists all active instances |

## Types

### Node

Represents a provisioned GPU instance:

```go
type Node struct {
    ID           string            // Provider-assigned instance ID
    Provider     string            // Provider name
    Region       string            // Cloud region
    Zone         string            // Availability zone
    InstanceType string            // Instance type name
    Status       string            // provisioning, running, terminating, terminated
    IPAddress    string            // Public or private IP
    GPUCount     int               // Number of GPUs
    GPUType      string            // GPU model (e.g., "NVIDIA H100 80GB")
    Labels       map[string]string // User-defined labels
}
```

### ProvisionRequest

Parameters for provisioning:

```go
type ProvisionRequest struct {
    Name         string            // Instance name
    InstanceType string            // Instance type to launch
    Region       string            // Target region
    Zone         string            // Target zone (optional)
    SSHKeyNames  []string          // SSH keys to authorize
    Labels       map[string]string // Labels to apply
    UserData     string            // Startup script (cloud-init)
}
```

### InstanceType

Describes an available instance type:

```go
type InstanceType struct {
    Name       string   // Instance type name
    GPUCount   int      // Number of GPUs
    GPUType    string   // GPU model
    MemoryGB   int      // System memory
    VCPUs      int      // Virtual CPU count
    PricePerHr float64  // Price per hour in USD
    Regions    []string // Available regions
    Available  bool     // Current availability
}
```

## InstanceTypeLister

Optional interface for providers that can list available types:

```go
type InstanceTypeLister interface {
    ListInstanceTypes(ctx context.Context) ([]InstanceType, error)
}
```

## Implementations

| Provider | Package | Status |
|----------|---------|--------|
| Lambda Labs | `pkg/provider/lambda` | Implemented |
| Google Cloud | `pkg/provider/gcp` | Implemented |
| AWS | `pkg/provider/aws` | Placeholder |
| Fake | `pkg/provider/fake` | For testing |

## Usage

```go
import (
    "github.com/NavarchProject/navarch/pkg/provider"
    "github.com/NavarchProject/navarch/pkg/provider/lambda"
)

// Create a provider
lambdaProvider, err := lambda.New(lambda.Config{
    APIKey: os.Getenv("LAMBDA_API_KEY"),
})
if err != nil {
    log.Fatal(err)
}

// Provision a node
node, err := lambdaProvider.Provision(ctx, provider.ProvisionRequest{
    Name:         "training-node-1",
    InstanceType: "gpu_8x_h100_sxm5",
    Region:       "us-west-2",
    SSHKeyNames:  []string{"my-key"},
    Labels: map[string]string{
        "workload": "training",
    },
})
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Provisioned: %s (%s)\n", node.ID, node.Status)

// List instances
nodes, err := lambdaProvider.List(ctx)
for _, n := range nodes {
    fmt.Printf("%s: %s (%d x %s)\n", n.ID, n.Status, n.GPUCount, n.GPUType)
}

// Terminate when done
err = lambdaProvider.Terminate(ctx, node.ID)
```

## Multi-provider pools

Pools can use multiple providers for fungible compute. See `pkg/pool` for details on provider selection strategies (priority, cost, availability, round-robin).

```go
pool, err := pool.NewWithOptions(pool.NewPoolOptions{
    Config: pool.Config{Name: "fungible"},
    Providers: []pool.ProviderConfig{
        {Name: "lambda", Provider: lambdaProvider, Priority: 1},
        {Name: "gcp", Provider: gcpProvider, Priority: 2},
    },
    ProviderStrategy: "priority",
})
```

## Implementing a provider

To add a new cloud provider:

1. Create a package under `pkg/provider/` (e.g., `pkg/provider/azure`).
2. Implement the `Provider` interface.
3. Optionally implement `InstanceTypeLister` for instance type discovery.
4. Add configuration loading in `pkg/config`.

```go
package azure

type Provider struct {
    // ...
}

func New(cfg Config) (*Provider, error) {
    // Initialize Azure client
}

func (p *Provider) Name() string {
    return "azure"
}

func (p *Provider) Provision(ctx context.Context, req provider.ProvisionRequest) (*provider.Node, error) {
    // Call Azure API to create VM
}

func (p *Provider) Terminate(ctx context.Context, nodeID string) error {
    // Call Azure API to delete VM
}

func (p *Provider) List(ctx context.Context) ([]*provider.Node, error) {
    // Call Azure API to list VMs
}
```

## Testing

Use the fake provider for testing:

```go
import "github.com/NavarchProject/navarch/pkg/provider/fake"

fp := fake.New(fake.Config{GPUCount: 8})

node, _ := fp.Provision(ctx, provider.ProvisionRequest{
    Name:         "test-node",
    InstanceType: "fake-8gpu",
})
// node.Status == "running"
```
