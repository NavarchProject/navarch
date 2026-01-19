package scheduler

import "context"

// ProvisionOption represents a provisioning option to be scored.
type ProvisionOption struct {
	Provider string
	Region   string
	Type     string
	Cost     float64
}

// Scheduler scores provisioning options. Higher scores are preferred.
type Scheduler interface {
	Score(ctx context.Context, option ProvisionOption) (float64, error)
}