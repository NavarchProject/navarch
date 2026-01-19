package health

import (
	"context"
	"time"

	"github.com/NavarchProject/navarch/pkg/provider"
)

// HealthResult represents the result of a health check.
type HealthResult struct {
	CheckName string
	Status    string
	Message   string
	Timestamp time.Time
}

// HealthCheck runs a diagnostic and returns node health status.
type HealthCheck interface {
	Name() string
	Run(ctx context.Context, node *provider.Node) (*HealthResult, error)
	// Interval returns how often this check should run.
	// Return 0 for boot-only checks.
	Interval() time.Duration
}