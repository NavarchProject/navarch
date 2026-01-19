package remediate

import (
	"context"

	"github.com/NavarchProject/navarch/pkg/health"
	"github.com/NavarchProject/navarch/pkg/provider"
)

// CordonAndReplace is a remediator that cordons unhealthy nodes and provisions replacements.
type CordonAndReplace struct{}

// NewCordonAndReplace creates a new CordonAndReplace remediator.
func NewCordonAndReplace() *CordonAndReplace {
	return &CordonAndReplace{}
}

// Remediate decides to cordon and replace unhealthy nodes (placeholder).
func (r *CordonAndReplace) Remediate(ctx context.Context, node *provider.Node, result *health.HealthResult) (Action, error) {
	return ActionCordon, nil
}