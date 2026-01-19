package gcp

import (
	"context"
	"fmt"

	"github.com/NavarchProject/navarch/pkg/provider"
)

// GCPProvider implements the Provider interface for Google Cloud Platform.
type GCPProvider struct {
	project string
}

// New creates a new GCP provider instance.
func New(project string) *GCPProvider {
	return &GCPProvider{project: project}
}

// Name returns the provider name.
func (p *GCPProvider) Name() string {
	return "gcp"
}

// Provision provisions a new node (placeholder).
func (p *GCPProvider) Provision(ctx context.Context, req provider.ProvisionRequest) (*provider.Node, error) {
	return nil, fmt.Errorf("not implemented yet")
}

// Terminate terminates a node (placeholder).
func (p *GCPProvider) Terminate(ctx context.Context, nodeID string) error {
	return fmt.Errorf("not implemented yet")
}

// List lists all nodes (placeholder).
func (p *GCPProvider) List(ctx context.Context) ([]*provider.Node, error) {
	return nil, fmt.Errorf("not implemented yet")
}