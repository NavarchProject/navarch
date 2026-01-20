# Extending Navarch

Navarch can be extended via two main interfaces: providers and autoscalers.

## Custom providers

Implement `provider.Provider` to add support for new cloud platforms:

```go
type Provider interface {
    Name() string
    Provision(ctx context.Context, req ProvisionRequest) (*Node, error)
    Terminate(ctx context.Context, nodeID string) error
    List(ctx context.Context) ([]*Node, error)
}
```

See `pkg/provider/lambda/` for a complete example.

## Custom autoscalers

Implement `pool.Autoscaler` for custom scaling logic:

```go
type Autoscaler interface {
    Recommend(ctx context.Context, state PoolState) (ScaleRecommendation, error)
}
```

See `pkg/pool/autoscaler.go` for built-in implementations.

