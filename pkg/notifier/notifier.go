// Package notifier provides integration with workload management systems.
//
// Navarch manages GPU infrastructure but doesn't schedule workloads.
// When Navarch needs to take a node out of service (for maintenance,
// health issues, or scaling down), it notifies external systems
// so workloads can be migrated gracefully.
package notifier

import (
	"context"
)

// Notifier defines the interface for workload system notifications.
// Implementations notify external systems (Kubernetes, Slurm, custom schedulers)
// about node lifecycle events and query drain status.
type Notifier interface {
	// Cordon marks a node as unschedulable. The workload system should stop
	// placing new workloads on this node. Existing workloads continue.
	Cordon(ctx context.Context, nodeID string, reason string) error

	// Uncordon marks a node as schedulable again, reversing a cordon.
	Uncordon(ctx context.Context, nodeID string) error

	// Drain requests the workload system to migrate workloads off the node.
	// This is called before termination to allow graceful shutdown.
	Drain(ctx context.Context, nodeID string, reason string) error

	// IsDrained returns true if all workloads have been migrated off
	// the node and it's safe to terminate.
	IsDrained(ctx context.Context, nodeID string) (bool, error)

	// Name returns the notifier name for logging.
	Name() string
}

// DrainWaiter provides a helper to wait for a node to be drained.
type DrainWaiter interface {
	// WaitForDrain blocks until the node is drained or context is canceled.
	WaitForDrain(ctx context.Context, nodeID string) error
}
