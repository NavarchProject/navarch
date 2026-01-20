# Lambda Labs provider

This package implements the `provider.Provider` interface for [Lambda Labs Cloud](https://lambdalabs.com/service/gpu-cloud).

## Configuration

```go
import "github.com/NavarchProject/navarch/pkg/provider/lambda"

provider, err := lambda.New(lambda.Config{
    APIKey: os.Getenv("LAMBDA_API_KEY"),
})
```

### Config options

| Field | Description | Required |
|-------|-------------|----------|
| `APIKey` | Lambda Labs API key | Yes |
| `BaseURL` | API base URL (default: `https://cloud.lambdalabs.com/api/v1`) | No |
| `Timeout` | HTTP timeout (default: 30s) | No |

## Getting an API key

1. Log in to [Lambda Labs Cloud](https://cloud.lambdalabs.com).
2. Go to **Settings** → **API Keys**.
3. Click **Generate API Key**.
4. Store the key securely (e.g., environment variable or secret manager).

## Usage

### Provision an instance

```go
node, err := provider.Provision(ctx, provider.ProvisionRequest{
    Name:         "my-gpu-node",
    InstanceType: "gpu_1x_a100_sxm4",
    Region:       "us-west-2",
    SSHKeyNames:  []string{"my-ssh-key"},
})
```

### List instances

```go
nodes, err := provider.List(ctx)
for _, node := range nodes {
    fmt.Printf("%s: %s (%s)\n", node.ID, node.Status, node.InstanceType)
}
```

### Terminate an instance

```go
err := provider.Terminate(ctx, "instance-id")
```

### List available instance types

```go
types, err := provider.ListInstanceTypes(ctx)
for _, t := range types {
    fmt.Printf("%s: %d x %s ($%.2f/hr) - Available: %v\n",
        t.Name, t.GPUCount, t.GPUType, t.PricePerHr, t.Available)
}
```

## Instance types

Lambda Labs offers various GPU configurations:

| Instance Type | GPUs | GPU Model | Memory |
|---------------|------|-----------|--------|
| `gpu_1x_a100_sxm4` | 1 | A100 SXM4 40GB | 200 GB |
| `gpu_1x_a100_80gb_sxm4` | 1 | A100 SXM4 80GB | 200 GB |
| `gpu_8x_a100_80gb_sxm4` | 8 | A100 SXM4 80GB | 1800 GB |
| `gpu_1x_h100_pcie` | 1 | H100 PCIe 80GB | 200 GB |
| `gpu_8x_h100_sxm5` | 8 | H100 SXM5 80GB | 1800 GB |

Check `ListInstanceTypes()` for current availability and pricing.

## Bootstrap script

To automatically start the Navarch agent on new instances, include a bootstrap script:

```go
bootstrapScript := `#!/bin/bash
curl -L https://github.com/NavarchProject/navarch/releases/latest/download/navarch-node-linux-amd64 \
  -o /usr/local/bin/navarch-node
chmod +x /usr/local/bin/navarch-node
navarch-node --server https://control-plane.example.com --node-id $(hostname)
`

// Note: Lambda does not support user-data directly.
// SSH into the instance after creation to run the script,
// or use a custom image with the agent pre-installed.
```

## API reference

This provider uses the [Lambda Labs Cloud API](https://docs-api.lambda.ai/api/cloud):

- `POST /instance-operations/launch` - Create instances
- `POST /instance-operations/terminate` - Terminate instances
- `GET /instances` - List running instances
- `GET /instance-types` - List available instance types

## Error handling

The provider returns descriptive errors from the Lambda API:

```go
node, err := provider.Provision(ctx, req)
if err != nil {
    // err.Error() == "lambda API error: Instance type not available (code: invalid_request)"
}
```

Common error codes:

| Code | Description |
|------|-------------|
| `invalid_request` | Invalid parameters |
| `instance_not_found` | Instance does not exist |
| `capacity_unavailable` | No capacity for requested instance type |
| `unauthorized` | Invalid API key |

