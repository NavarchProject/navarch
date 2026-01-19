# Control Plane Database Interface

This package defines the database interface for the Navarch control plane and provides an in-memory implementation for testing and development.

## Interface

The `DB` interface provides methods for:

- **Node Management**: Register, update, list, and delete nodes
- **Health Checks**: Record and retrieve health check results
- **Commands**: Issue and track commands sent to nodes

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
)

// Create in-memory database
database := db.NewInMemDB()
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
```

## Future Implementations

Planned database implementations:

- **PostgreSQL**: For production deployments requiring persistence
- **etcd**: For distributed control plane setups
- **SQLite**: For simple single-node persistent storage

To add a new implementation, create a new type that implements the `DB` interface.

