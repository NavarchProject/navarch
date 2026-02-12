package coordinator

import (
	"context"
	"log/slog"
)

// Noop is a no-op coordinator that logs operations but takes no action.
// Use this when no external workload system integration is configured.
type Noop struct {
	logger *slog.Logger
}

// NewNoop creates a new no-op coordinator.
func NewNoop(logger *slog.Logger) *Noop {
	if logger == nil {
		logger = slog.Default()
	}
	return &Noop{logger: logger}
}

// Name returns the coordinator name.
func (n *Noop) Name() string {
	return "noop"
}

// Cordon logs the cordon request but takes no action.
func (n *Noop) Cordon(ctx context.Context, nodeID string, reason string) error {
	n.logger.Info("node cordoned (no coordinator configured)",
		slog.String("node_id", nodeID),
		slog.String("reason", reason),
	)
	return nil
}

// Uncordon logs the uncordon request but takes no action.
func (n *Noop) Uncordon(ctx context.Context, nodeID string) error {
	n.logger.Info("node uncordoned (no coordinator configured)",
		slog.String("node_id", nodeID),
	)
	return nil
}

// Drain logs the drain request but takes no action.
func (n *Noop) Drain(ctx context.Context, nodeID string, reason string) error {
	n.logger.Info("node drain requested (no coordinator configured)",
		slog.String("node_id", nodeID),
		slog.String("reason", reason),
	)
	return nil
}

// IsDrained always returns true since there's no external system to check.
func (n *Noop) IsDrained(ctx context.Context, nodeID string) (bool, error) {
	return true, nil
}

// Ensure Noop implements Coordinator.
var _ Coordinator = (*Noop)(nil)
