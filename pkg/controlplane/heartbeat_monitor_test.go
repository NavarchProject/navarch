package controlplane

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/NavarchProject/navarch/pkg/clock"
	"github.com/NavarchProject/navarch/pkg/controlplane/db"
	pb "github.com/NavarchProject/navarch/proto"
)

func TestHeartbeatMonitor_DetectsStaleNode(t *testing.T) {
	database := db.NewInMemDB()
	fakeClock := clock.NewFakeClock(time.Now())

	// Register a node with a heartbeat
	ctx := context.Background()
	database.RegisterNode(ctx, &db.NodeRecord{
		NodeID:        "node-1",
		Status:        pb.NodeStatus_NODE_STATUS_ACTIVE,
		LastHeartbeat: fakeClock.Now(),
	})

	observer := &testHealthObserver{}
	monitor := NewHeartbeatMonitor(database, HeartbeatMonitorConfig{
		HeartbeatTimeout: 1 * time.Minute,
		CheckInterval:    10 * time.Second,
		Clock:            fakeClock,
	}, nil)
	monitor.SetHealthObserver(observer)
	monitor.Start(ctx)
	defer monitor.Stop()

	// Give goroutine time to block on ticker
	time.Sleep(10 * time.Millisecond)

	// Advance time past the timeout
	fakeClock.Advance(2 * time.Minute)

	// Wait for the check to run
	time.Sleep(100 * time.Millisecond)

	// Verify node was marked unhealthy
	node, _ := database.GetNode(ctx, "node-1")
	if node.Status != pb.NodeStatus_NODE_STATUS_UNHEALTHY {
		t.Errorf("Expected node status UNHEALTHY, got %v", node.Status)
	}

	// Verify observer was notified
	calls := observer.getCalls()
	if len(calls) != 1 || calls[0] != "node-1" {
		t.Errorf("Expected observer called with node-1, got %v", calls)
	}
}

func TestHeartbeatMonitor_IgnoresHealthyNodes(t *testing.T) {
	database := db.NewInMemDB()
	fakeClock := clock.NewFakeClock(time.Now())

	ctx := context.Background()
	database.RegisterNode(ctx, &db.NodeRecord{
		NodeID:        "node-1",
		Status:        pb.NodeStatus_NODE_STATUS_ACTIVE,
		LastHeartbeat: fakeClock.Now(),
	})

	monitor := NewHeartbeatMonitor(database, HeartbeatMonitorConfig{
		HeartbeatTimeout: 1 * time.Minute,
		CheckInterval:    10 * time.Second,
		Clock:            fakeClock,
	}, nil)
	monitor.Start(ctx)
	defer monitor.Stop()

	// Give goroutine time to block on ticker
	time.Sleep(10 * time.Millisecond)

	// Advance time but not past timeout
	fakeClock.Advance(30 * time.Second)

	// Wait for check
	time.Sleep(100 * time.Millisecond)

	// Node should still be active
	node, _ := database.GetNode(ctx, "node-1")
	if node.Status != pb.NodeStatus_NODE_STATUS_ACTIVE {
		t.Errorf("Expected node status ACTIVE, got %v", node.Status)
	}
}

func TestHeartbeatMonitor_IgnoresAlreadyUnhealthy(t *testing.T) {
	database := db.NewInMemDB()
	fakeClock := clock.NewFakeClock(time.Now())

	ctx := context.Background()
	database.RegisterNode(ctx, &db.NodeRecord{
		NodeID:        "node-1",
		Status:        pb.NodeStatus_NODE_STATUS_UNHEALTHY,
		LastHeartbeat: fakeClock.Now().Add(-5 * time.Minute), // Very old
	})

	observer := &testHealthObserver{}
	monitor := NewHeartbeatMonitor(database, HeartbeatMonitorConfig{
		HeartbeatTimeout: 1 * time.Minute,
		CheckInterval:    10 * time.Second,
		Clock:            fakeClock,
	}, nil)
	monitor.SetHealthObserver(observer)
	monitor.Start(ctx)
	defer monitor.Stop()

	// Give goroutine time to block on ticker
	time.Sleep(10 * time.Millisecond)

	// Advance time and wait for check
	fakeClock.Advance(15 * time.Second)
	time.Sleep(100 * time.Millisecond)

	// Observer should NOT be called (node was already unhealthy)
	if len(observer.getCalls()) != 0 {
		t.Errorf("Expected no observer calls for already unhealthy node, got %v", observer.getCalls())
	}
}

func TestHeartbeatMonitor_IgnoresNoHeartbeat(t *testing.T) {
	database := db.NewInMemDB()
	fakeClock := clock.NewFakeClock(time.Now())

	ctx := context.Background()
	// Node with zero heartbeat (just registered, no heartbeat yet)
	database.RegisterNode(ctx, &db.NodeRecord{
		NodeID: "node-1",
		Status: pb.NodeStatus_NODE_STATUS_ACTIVE,
		// LastHeartbeat is zero
	})

	monitor := NewHeartbeatMonitor(database, HeartbeatMonitorConfig{
		HeartbeatTimeout: 1 * time.Minute,
		CheckInterval:    10 * time.Second,
		Clock:            fakeClock,
	}, nil)
	monitor.Start(ctx)
	defer monitor.Stop()

	// Give goroutine time to block on ticker
	time.Sleep(10 * time.Millisecond)

	fakeClock.Advance(5 * time.Minute)
	time.Sleep(100 * time.Millisecond)

	// Node should still be active (we don't mark nodes unhealthy before first heartbeat)
	node, _ := database.GetNode(ctx, "node-1")
	if node.Status != pb.NodeStatus_NODE_STATUS_ACTIVE {
		t.Errorf("Expected node status ACTIVE (no heartbeat yet), got %v", node.Status)
	}
}

func TestHeartbeatMonitor_MultipleNodes(t *testing.T) {
	database := db.NewInMemDB()
	fakeClock := clock.NewFakeClock(time.Now())

	ctx := context.Background()
	now := fakeClock.Now()

	// node-1: recent heartbeat (should stay healthy)
	database.RegisterNode(ctx, &db.NodeRecord{
		NodeID:        "node-1",
		Status:        pb.NodeStatus_NODE_STATUS_ACTIVE,
		LastHeartbeat: now,
	})

	// node-2: stale heartbeat (should become unhealthy)
	database.RegisterNode(ctx, &db.NodeRecord{
		NodeID:        "node-2",
		Status:        pb.NodeStatus_NODE_STATUS_ACTIVE,
		LastHeartbeat: now.Add(-3 * time.Minute),
	})

	// node-3: cordoned but stale (should become unhealthy)
	database.RegisterNode(ctx, &db.NodeRecord{
		NodeID:        "node-3",
		Status:        pb.NodeStatus_NODE_STATUS_CORDONED,
		LastHeartbeat: now.Add(-3 * time.Minute),
	})

	observer := &testHealthObserver{}
	monitor := NewHeartbeatMonitor(database, HeartbeatMonitorConfig{
		HeartbeatTimeout: 1 * time.Minute,
		CheckInterval:    10 * time.Second,
		Clock:            fakeClock,
	}, nil)
	monitor.SetHealthObserver(observer)
	monitor.Start(ctx)
	defer monitor.Stop()

	// Give goroutine time to block on ticker
	time.Sleep(10 * time.Millisecond)

	fakeClock.Advance(15 * time.Second)

	// Wait for check to process with retries
	var node1, node2, node3 *db.NodeRecord
	for i := 0; i < 20; i++ {
		time.Sleep(50 * time.Millisecond)
		node1, _ = database.GetNode(ctx, "node-1")
		node2, _ = database.GetNode(ctx, "node-2")
		node3, _ = database.GetNode(ctx, "node-3")
		if node2.Status == pb.NodeStatus_NODE_STATUS_UNHEALTHY &&
			node3.Status == pb.NodeStatus_NODE_STATUS_UNHEALTHY {
			break
		}
	}

	// Check results

	if node1.Status != pb.NodeStatus_NODE_STATUS_ACTIVE {
		t.Errorf("node-1: expected ACTIVE, got %v", node1.Status)
	}
	if node2.Status != pb.NodeStatus_NODE_STATUS_UNHEALTHY {
		t.Errorf("node-2: expected UNHEALTHY, got %v", node2.Status)
	}
	if node3.Status != pb.NodeStatus_NODE_STATUS_UNHEALTHY {
		t.Errorf("node-3: expected UNHEALTHY, got %v", node3.Status)
	}

	calls := observer.getCalls()
	if len(calls) != 2 {
		t.Errorf("Expected 2 observer calls, got %d", len(calls))
	}
}

type testHealthObserver struct {
	mu    sync.Mutex
	calls []string
}

func (o *testHealthObserver) OnNodeUnhealthy(ctx context.Context, nodeID string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.calls = append(o.calls, nodeID)
}

func (o *testHealthObserver) getCalls() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	result := make([]string, len(o.calls))
	copy(result, o.calls)
	return result
}
