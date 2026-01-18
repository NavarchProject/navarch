package remediate

import (
	"context"

	"github.com/NavarchProject/navarch/pkg/health"
	"github.com/NavarchProject/navarch/pkg/provider"
)

// Action represents a remediation action.
type Action string

const (
	ActionCordon   Action = "cordon"
	ActionReplace  Action = "replace"
	ActionNoAction Action = "no_action"
)

// Remediator decides what to do with an unhealthy node.
type Remediator interface {
	Remediate(ctx context.Context, node *provider.Node, result *health.HealthResult) (Action, error)
}

