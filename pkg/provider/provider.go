package provider

import "context"

// Node represents a provisioned GPU instance.
type Node struct {
	ID           string
	Provider     string
	Region       string
	Zone         string
	InstanceType string
	Status       string // provisioning, running, terminating, terminated
	IPAddress    string
	GPUCount     int
	GPUType      string
	Labels       map[string]string
}

// ProvisionRequest contains parameters for provisioning a node.
type ProvisionRequest struct {
	Name         string
	InstanceType string
	Region       string
	Zone         string
	SSHKeyNames  []string
	Labels       map[string]string
	// UserData is a script to run on instance startup (cloud-init).
	UserData string
}

// Provider abstracts cloud-specific provisioning operations.
type Provider interface {
	Name() string
	Provision(ctx context.Context, req ProvisionRequest) (*Node, error)
	Terminate(ctx context.Context, nodeID string) error
	List(ctx context.Context) ([]*Node, error)
}

// InstanceType describes an available instance type from a provider.
type InstanceType struct {
	Name        string
	GPUCount    int
	GPUType     string
	MemoryGB    int
	VCPUs       int
	PricePerHr  float64
	Regions     []string
	Available   bool
}

// InstanceTypeLister is an optional interface for providers that can list available types.
type InstanceTypeLister interface {
	ListInstanceTypes(ctx context.Context) ([]InstanceType, error)
}
