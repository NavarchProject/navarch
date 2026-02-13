# notifier

Package notifier provides integration with workload management systems.

For user-facing documentation, see [docs/extending.md](../../website/docs/extending.md#custom-notifiers) and [docs/configuration.md](../../website/docs/configuration.md#notifier).

## Overview

Navarch manages GPU infrastructure but does not schedule workloads. When Navarch needs to take a node out of service (for maintenance, health issues, or scaling down), it notifies external systems so workloads can be migrated gracefully.

## Notifier interface

```go
type Notifier interface {
    Cordon(ctx context.Context, nodeID string, reason string) error
    Uncordon(ctx context.Context, nodeID string) error
    Drain(ctx context.Context, nodeID string, reason string) error
    IsDrained(ctx context.Context, nodeID string) (bool, error)
    Name() string
}
```

## Built-in notifiers

### Noop

Logs operations but takes no action. Use this when no external workload system integration is needed.

```go
notifier := notifier.NewNoop(logger)
```

### Webhook

Sends HTTP requests to your workload system. Configurable endpoints for cordon, uncordon, drain, and drain status.

```go
notifier := notifier.NewWebhook(notifier.WebhookConfig{
    CordonURL:      "https://scheduler.example.com/api/cordon",
    UncordonURL:    "https://scheduler.example.com/api/uncordon",
    DrainURL:       "https://scheduler.example.com/api/drain",
    DrainStatusURL: "https://scheduler.example.com/api/drain-status",
    Timeout:        30 * time.Second,
    Headers: map[string]string{
        "Authorization": "Bearer " + token,
    },
}, logger)
```

## Implementing a custom notifier

To integrate with Kubernetes, Slurm, Ray, or a custom scheduler, implement the `Notifier` interface.

See [extending.md](../../website/docs/extending.md#custom-notifiers) for Kubernetes and Slurm examples.
