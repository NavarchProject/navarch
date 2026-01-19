package aws

import (
	"context"
	"fmt"

	"github.com/NavarchProject/navarch/pkg/provider"
)

// AWSProvider implements the Provider interface for Amazon Web Services.
type AWSProvider struct {
	region string
}

// New creates a new AWS provider instance.
func New(region string) *AWSProvider {
	return &AWSProvider{region: region}
}

// Name returns the provider name.
func (p *AWSProvider) Name() string {
	return "aws"
}

// Provision provisions a new node (placeholder).
func (p *AWSProvider) Provision(ctx context.Context, req provider.ProvisionRequest) (*provider.Node, error) {
	return nil, fmt.Errorf("not implemented yet")
}

// Terminate terminates a node (placeholder).
func (p *AWSProvider) Terminate(ctx context.Context, nodeID string) error {
	return fmt.Errorf("not implemented yet")
}

// List lists all nodes (placeholder).
func (p *AWSProvider) List(ctx context.Context) ([]*provider.Node, error) {
	return nil, fmt.Errorf("not implemented yet")
}