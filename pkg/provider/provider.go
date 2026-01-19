package provider

import "context"

// Node represents a provisioned GPU instance.
type Node struct {
	ID       string
	Provider string
	Type     string
	Status   string
}

// ProvisionRequest contains parameters for provisioning a node.
type ProvisionRequest struct {
	GPUCount int
	Type     string
	Name     string
}

// Provider abstracts cloud-specific provisioning operations.
type Provider interface {
	Name() string
	Provision(ctx context.Context, req ProvisionRequest) (*Node, error)
	Terminate(ctx context.Context, nodeID string) error
	List(ctx context.Context) ([]*Node, error)
}