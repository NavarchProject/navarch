# Control Plane Database Interface

This package defines the database interface for the Navarch control plane and provides an in-memory implementation for testing and development.

## Interface

The `DB` interface provides methods for:

- **Node Management**: Register, update, list, and delete nodes
- **Health Checks**: Record and retrieve health check results
- **Commands**: Issue and track commands sent to nodes
- **Metrics**: Store and retrieve node metrics
- **Instance Tracking**: Track cloud instance lifecycle from provisioning through termination

## Implementations

### In-Memory Database

The `InMemDB` implementation stores all data in memory with proper synchronization. It's suitable for:

- Testing
- Development
- Single-instance deployments
- Demos

**Note**: All data is lost when the process stops.

### Usage

```go
import (
    "github.com/NavarchProject/navarch/pkg/controlplane/db"
    "github.com/NavarchProject/navarch/pkg/clock"
)

// Create in-memory database
database := db.NewInMemDB()
defer database.Close()

// Or with clock injection for deterministic tests
fakeClock := clock.NewFakeClock(time.Now())
database := db.NewInMemDBWithClock(fakeClock)
defer database.Close()

// Register a node
record := &db.NodeRecord{
    NodeID:   "node-1",
    Provider: "gcp",
    Region:   "us-central1",
    Zone:     "us-central1-a",
    Status:   pb.NodeStatus_NODE_STATUS_ACTIVE,
}
err := database.RegisterNode(ctx, record)

// Get a node
node, err := database.GetNode(ctx, "node-1")

// List all nodes
nodes, err := database.ListNodes(ctx)

// Track an instance
instance := &db.InstanceRecord{
    InstanceID:   "i-12345",
    Provider:     "gcp",
    Region:       "us-central1",
    Zone:         "us-central1-a",
    InstanceType: "a3-highgpu-8g",
    State:        pb.InstanceState_INSTANCE_STATE_PROVISIONING,
    PoolName:     "gpu-pool",
    CreatedAt:    time.Now(),
}
err = database.CreateInstance(ctx, instance)

// Update instance state when node registers
err = database.UpdateInstanceState(ctx, "i-12345", pb.InstanceState_INSTANCE_STATE_RUNNING, "node registered")
err = database.UpdateInstanceNodeID(ctx, "i-12345", "node-1")

// List instances by state
pending, err := database.ListInstancesByState(ctx, pb.InstanceState_INSTANCE_STATE_PENDING_REGISTRATION)
```

## Future Implementations

Planned database implementations:

- **PostgreSQL**: For production deployments requiring persistence
- **etcd**: For distributed control plane setups
- **SQLite**: For simple single-node persistent storage

To add a new implementation, create a new type that implements the `DB` interface.

