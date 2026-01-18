package health

import (
	"context"
	"fmt"
	"time"

	"github.com/NavarchProject/navarch/pkg/provider"
)

// XidCheck monitors XID errors via dmesg.
type XidCheck struct{}

// NewXidCheck creates a new XID health check.
func NewXidCheck() *XidCheck {
	return &XidCheck{}
}

// Name returns the check name.
func (c *XidCheck) Name() string {
	return "xid"
}

// Run performs the XID health check (placeholder).
func (c *XidCheck) Run(ctx context.Context, node *provider.Node) (*HealthResult, error) {
	return &HealthResult{
		CheckName: c.Name(),
		Status:    "ok",
		Message:   "XID check not implemented yet",
		Timestamp: time.Now(),
	}, nil
}

// Interval returns how often this check should run.
func (c *XidCheck) Interval() time.Duration {
	return 60 * time.Second
}

