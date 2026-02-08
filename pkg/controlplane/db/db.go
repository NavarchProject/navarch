package db

import (
	"context"
	"time"

	pb "github.com/NavarchProject/navarch/proto"
)

// NodeRecord represents a registered node with its metadata and status.
type NodeRecord struct {
	NodeID       string
	Provider     string
	Region       string
	Zone         string
	InstanceType string
	GPUs         []*pb.GPUInfo
	Metadata     *pb.NodeMetadata
	
	// Runtime state
	Status              pb.NodeStatus
	LastHeartbeat       time.Time
	LastHealthCheck     time.Time
	HealthStatus        pb.HealthStatus
	RegisteredAt        time.Time
	
	// Configuration
	Config *pb.NodeConfig
}

// HealthCheckRecord represents a historical health check result.
type HealthCheckRecord struct {
	NodeID    string
	Timestamp time.Time
	Results   []*pb.HealthCheckResult
}

// CommandRecord represents a command issued to a node.
type CommandRecord struct {
	CommandID  string
	NodeID     string
	Type       pb.NodeCommandType
	Parameters map[string]string
	IssuedAt   time.Time
	Status     string // "pending", "acknowledged", "completed", "failed"
}

// MetricsRecord represents metrics collected from a node at a point in time.
type MetricsRecord struct {
	NodeID    string
	Timestamp time.Time
	Metrics   *pb.NodeMetrics
}

// BootstrapLogRecord represents a bootstrap execution attempt for an instance.
type BootstrapLogRecord struct {
	ID          string                  // Unique ID for this bootstrap attempt
	NodeID      string                  // Node ID being bootstrapped
	InstanceID  string                  // Cloud instance ID
	Pool        string                  // Pool name
	StartedAt   time.Time               // When bootstrap started
	Duration    time.Duration           // Total duration
	SSHWaitTime time.Duration           // Time waiting for SSH
	Success     bool                    // Whether bootstrap succeeded
	Error       string                  // Error message if failed
	Commands    []BootstrapCommandLog   // Per-command results
}

// BootstrapCommandLog captures output from a single bootstrap command.
type BootstrapCommandLog struct {
	Command  string        // The command template (unexpanded)
	Stdout   string        // Command stdout (truncated if too large)
	Stderr   string        // Command stderr (truncated if too large)
	ExitCode int           // Exit code (0 = success)
	Duration time.Duration // How long the command took
}

// InstanceRecord represents a cloud instance tracked by the control plane.
// This is separate from NodeRecord to capture the full instance lifecycle,
// including cases where nodes fail to register or boot properly.
type InstanceRecord struct {
	// InstanceID is the provider-assigned instance ID (e.g., GCP instance name).
	InstanceID string

	// Provider is the cloud provider name (e.g., "gcp", "aws", "lambda").
	Provider string

	// Region is the cloud region where the instance is running.
	Region string

	// Zone is the availability zone within the region.
	Zone string

	// InstanceType is the instance type (e.g., "a3-highgpu-8g").
	InstanceType string

	// State is the current lifecycle state of the instance.
	State pb.InstanceState

	// PoolName is the name of the pool that provisioned this instance (empty if standalone).
	PoolName string

	// CreatedAt is when the instance provisioning was requested.
	CreatedAt time.Time

	// ReadyAt is when the node registered (instance became ready), if applicable.
	ReadyAt time.Time

	// TerminatedAt is when the instance was terminated, if applicable.
	TerminatedAt time.Time

	// NodeID is the ID of the registered node, if the node has registered.
	// Empty if the instance is still pending registration or failed.
	NodeID string

	// StatusMessage is a human-readable status message (e.g., error details).
	StatusMessage string

	// Labels are user-defined labels copied from the pool configuration.
	Labels map[string]string
}

// DB is the interface for control plane data storage.
type DB interface {
	// Node operations
	RegisterNode(ctx context.Context, record *NodeRecord) error
	GetNode(ctx context.Context, nodeID string) (*NodeRecord, error)
	UpdateNodeStatus(ctx context.Context, nodeID string, status pb.NodeStatus) error
	UpdateNodeHealthStatus(ctx context.Context, nodeID string, health pb.HealthStatus) error
	UpdateNodeHeartbeat(ctx context.Context, nodeID string, timestamp time.Time) error
	ListNodes(ctx context.Context) ([]*NodeRecord, error)
	DeleteNode(ctx context.Context, nodeID string) error
	
	// Health check operations
	RecordHealthCheck(ctx context.Context, record *HealthCheckRecord) error
	GetLatestHealthCheck(ctx context.Context, nodeID string) (*HealthCheckRecord, error)
	
	// Command operations
	CreateCommand(ctx context.Context, record *CommandRecord) error
	GetPendingCommands(ctx context.Context, nodeID string) ([]*CommandRecord, error)
	UpdateCommandStatus(ctx context.Context, commandID, status string) error
	
	// Metrics operations
	RecordMetrics(ctx context.Context, record *MetricsRecord) error
	GetRecentMetrics(ctx context.Context, nodeID string, duration time.Duration) ([]*MetricsRecord, error)

	// Instance operations (cloud resource lifecycle tracking)
	CreateInstance(ctx context.Context, record *InstanceRecord) error
	GetInstance(ctx context.Context, instanceID string) (*InstanceRecord, error)
	UpdateInstanceState(ctx context.Context, instanceID string, state pb.InstanceState, message string) error
	UpdateInstanceNodeID(ctx context.Context, instanceID string, nodeID string) error
	ListInstances(ctx context.Context) ([]*InstanceRecord, error)
	ListInstancesByState(ctx context.Context, state pb.InstanceState) ([]*InstanceRecord, error)
	ListInstancesByPool(ctx context.Context, poolName string) ([]*InstanceRecord, error)
	DeleteInstance(ctx context.Context, instanceID string) error

	// Bootstrap log operations
	RecordBootstrapLog(ctx context.Context, record *BootstrapLogRecord) error
	GetBootstrapLogs(ctx context.Context, nodeID string) ([]*BootstrapLogRecord, error)
	ListBootstrapLogsByPool(ctx context.Context, pool string, limit int) ([]*BootstrapLogRecord, error)

	// Cleanup
	Close() error
}