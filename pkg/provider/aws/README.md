# AWS provider

This package provides an AWS EC2 implementation of the provider interface.

## Status

This provider is a placeholder. The interface is defined but methods return "not implemented yet" errors.

## Planned implementation

When implemented, the AWS provider will support:

- EC2 GPU instances (P4d, P5, G5 families).
- Application Default Credentials via AWS SDK.
- Instance tagging with labels.
- User data for startup scripts.

## Configuration

```go
type Config struct {
    Region string // AWS region (e.g., "us-west-2")
}
```

## Usage

```go
import "github.com/NavarchProject/navarch/pkg/provider/aws"

provider := aws.New("us-west-2")
// Methods currently return "not implemented yet"
```

## Contributing

To implement this provider:

1. Add AWS SDK dependency.
2. Implement `Provision()` using EC2 RunInstances.
3. Implement `Terminate()` using EC2 TerminateInstances.
4. Implement `List()` using EC2 DescribeInstances.
5. Add `ListInstanceTypes()` using EC2 DescribeInstanceTypes.
6. Add integration tests with localstack or similar.

See `pkg/provider/gcp` for a reference implementation.
