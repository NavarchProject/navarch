package health

import (
	"context"
	"time"

	"github.com/NavarchProject/navarch/pkg/provider"
)

// NvmlCheck monitors GPU health via NVML.
type NvmlCheck struct{}

// NewNvmlCheck creates a new NVML health check.
func NewNvmlCheck() *NvmlCheck {
	return &NvmlCheck{}
}

// Name returns the check name.
func (c *NvmlCheck) Name() string {
	return "nvml"
}

// Run performs the NVML health check (placeholder).
func (c *NvmlCheck) Run(ctx context.Context, node *provider.Node) (*HealthResult, error) {
	return &HealthResult{
		CheckName: c.Name(),
		Status:    "ok",
		Message:   "NVML check not implemented yet",
		Timestamp: time.Now(),
	}, nil
}

// Interval returns how often this check should run.
func (c *NvmlCheck) Interval() time.Duration {
	return 30 * time.Second
}

