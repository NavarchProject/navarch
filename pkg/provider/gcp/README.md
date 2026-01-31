# GCP provider

This package provides a Google Cloud Platform implementation of the provider interface.

## Overview

The GCP provider uses the Compute Engine API to manage GPU instances:

- Provisions instances with Deep Learning VM images.
- Supports A2, A3, and G2 machine type families.
- Uses Application Default Credentials for authentication.
- Lists instances across all zones in a project.

## Configuration

```go
type Config struct {
    Project string        // GCP project ID (required)
    Zone    string        // Default zone (required, e.g., "us-central1-a")
    BaseURL string        // API base URL (optional, for testing)
    Timeout time.Duration // HTTP client timeout (default: 60s)
}
```

## Usage

```go
import "github.com/NavarchProject/navarch/pkg/provider/gcp"

provider, err := gcp.New(gcp.Config{
    Project: "my-project",
    Zone:    "us-central1-a",
})
if err != nil {
    log.Fatal(err)
}

// Provision an A3 instance with 8 H100 GPUs
node, err := provider.Provision(ctx, provider.ProvisionRequest{
    Name:         "training-node-1",
    InstanceType: "a3-highgpu-8g",
    Labels: map[string]string{
        "workload": "training",
    },
})
```

## Supported machine types

### A3 family (H100)

| Type | GPUs | GPU Model |
|------|------|-----------|
| `a3-highgpu-8g` | 8 | NVIDIA H100 80GB |
| `a3-megagpu-8g` | 8 | NVIDIA H100 80GB |

### A2 family (A100)

| Type | GPUs | GPU Model |
|------|------|-----------|
| `a2-highgpu-1g` | 1 | NVIDIA A100 40GB |
| `a2-highgpu-2g` | 2 | NVIDIA A100 40GB |
| `a2-highgpu-4g` | 4 | NVIDIA A100 40GB |
| `a2-highgpu-8g` | 8 | NVIDIA A100 40GB |
| `a2-ultragpu-1g` | 1 | NVIDIA A100 80GB |
| `a2-ultragpu-2g` | 2 | NVIDIA A100 80GB |
| `a2-ultragpu-4g` | 4 | NVIDIA A100 80GB |
| `a2-ultragpu-8g` | 8 | NVIDIA A100 80GB |
| `a2-megagpu-16g` | 16 | NVIDIA A100 80GB |

### G2 family (L4)

| Type | GPUs | GPU Model |
|------|------|-----------|
| `g2-standard-4` | 1 | NVIDIA L4 |
| `g2-standard-8` | 1 | NVIDIA L4 |
| `g2-standard-24` | 2 | NVIDIA L4 |
| `g2-standard-48` | 4 | NVIDIA L4 |
| `g2-standard-96` | 8 | NVIDIA L4 |

## Authentication

The provider uses Application Default Credentials (ADC). Set up credentials using one of:

```bash
# Option 1: gcloud CLI
gcloud auth application-default login

# Option 2: Service account key
export GOOGLE_APPLICATION_CREDENTIALS=/path/to/key.json

# Option 3: Workload Identity (GKE)
# Automatic when running on GKE with Workload Identity configured
```

## Instance configuration

Provisioned instances are configured with:

- **Boot disk**: Deep Learning VM image (Debian 11, Python 3.10, CUDA 12.1), 200GB SSD.
- **Network**: Default VPC with external IP (ONE_TO_ONE_NAT).
- **Metadata**: OS Login enabled by default.
- **Scheduling**: TERMINATE on host maintenance (required for GPU instances).

## Testing

The provider supports a custom HTTP client for testing:

```go
// Use httptest server for integration tests
ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    // Mock GCP API responses
}))

provider := gcp.NewWithClient(gcp.Config{
    Project: "test-project",
    Zone:    "us-central1-a",
    BaseURL: ts.URL,
}, ts.Client())
```

## Listing instance types

The provider implements `InstanceTypeLister`:

```go
types, err := provider.ListInstanceTypes(ctx)
for _, t := range types {
    fmt.Printf("%s: %d x %s\n", t.Name, t.GPUCount, t.GPUType)
}
```

Note: Pricing is not available via the Compute API. Use the Cloud Billing API for accurate pricing.

## Running tests

```bash
go test ./pkg/provider/gcp/... -v
```
