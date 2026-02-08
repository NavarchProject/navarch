package provider

import "context"

// Node represents a provisioned GPU instance.
type Node struct {
	ID           string            // Provider-assigned instance ID
	Provider     string            // Provider name (e.g., "lambda", "gcp")
	Region       string            // Cloud region
	Zone         string            // Availability zone
	InstanceType string            // Instance type name
	Status       string            // Instance state: provisioning, running, terminating, terminated
	IPAddress    string            // Public or private IP address
	SSHPort      int               // SSH port (default: 22). Used by docker provider for port mapping.
	GPUCount     int               // Number of GPUs attached
	GPUType      string            // GPU model description (e.g., "NVIDIA H100 80GB")
	Labels       map[string]string // User-defined key-value labels
}

// ProvisionRequest contains parameters for provisioning a node.
type ProvisionRequest struct {
	Name         string            // Instance name (may be used as hostname)
	InstanceType string            // Instance type to launch
	Region       string            // Target region
	Zone         string            // Target availability zone (optional)
	SSHKeyNames  []string          // SSH key names to authorize
	Labels       map[string]string // Labels to apply to the instance
	UserData     string            // Startup script (cloud-init format)
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
	Name       string   // Instance type name (e.g., "gpu_8x_h100_sxm5")
	GPUCount   int      // Number of GPUs
	GPUType    string   // GPU model description
	MemoryGB   int      // System memory in gigabytes
	VCPUs      int      // Virtual CPU count
	PricePerHr float64  // Price per hour in USD
	Regions    []string // Regions where this type is offered
	Available  bool     // True if capacity is currently available
}

// InstanceTypeLister is an optional interface for providers that can list available types.
type InstanceTypeLister interface {
	ListInstanceTypes(ctx context.Context) ([]InstanceType, error)
}

// SelfBootstrapping is an optional interface for providers that manage node setup internally.
// When a provider implements this and returns true, the pool skips SSH bootstrap.
// Examples: fake provider (spawns agents directly), Kubernetes (uses init containers).
type SelfBootstrapping interface {
	// SelfBootstraps returns true if the provider handles node agent installation.
	SelfBootstraps() bool
}
