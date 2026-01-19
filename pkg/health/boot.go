package health

import (
	"context"
	"time"

	"github.com/NavarchProject/navarch/pkg/provider"
)

// BootCheck performs basic GPU validation on startup.
type BootCheck struct{}

// NewBootCheck creates a new boot health check.
func NewBootCheck() *BootCheck {
	return &BootCheck{}
}

// Name returns the check name.
func (c *BootCheck) Name() string {
	return "boot"
}

// Run performs the boot health check (placeholder).
func (c *BootCheck) Run(ctx context.Context, node *provider.Node) (*HealthResult, error) {
	return &HealthResult{
		CheckName: c.Name(),
		Status:    "ok",
		Message:   "Boot check not implemented yet",
		Timestamp: time.Now(),
	}, nil
}

// Interval returns 0 for boot-only checks.
func (c *BootCheck) Interval() time.Duration {
	return 0
}