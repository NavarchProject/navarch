package fake

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/NavarchProject/navarch/pkg/controlplane"
	"github.com/NavarchProject/navarch/pkg/controlplane/db"
	"github.com/NavarchProject/navarch/pkg/provider"
	"github.com/NavarchProject/navarch/proto/protoconnect"
)

func startTestControlPlane(t *testing.T) (string, func()) {
	database := db.NewInMemDB()
	im := controlplane.NewInstanceManager(database, controlplane.DefaultInstanceManagerConfig(), nil)
	srv := controlplane.NewServer(database, controlplane.DefaultConfig(), im, nil)

	mux := http.NewServeMux()
	path, handler := protoconnect.NewControlPlaneServiceHandler(srv)
	mux.Handle(path, handler)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	httpServer := &http.Server{
		Handler: h2c.NewHandler(mux, &http2.Server{}),
	}

	go httpServer.Serve(listener)

	addr := "http://" + listener.Addr().String()
	cleanup := func() {
		httpServer.Close()
		database.Close()
	}

	return addr, cleanup
}

func TestFakeProvider_New(t *testing.T) {
	_, err := New(Config{})
	if err == nil {
		t.Error("expected error for missing control plane address")
	}

	p, err := New(Config{ControlPlaneAddr: "http://localhost:50051"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "fake" {
		t.Errorf("expected name 'fake', got %s", p.Name())
	}
}

func TestFakeProvider_ProvisionAndTerminate(t *testing.T) {
	addr, cleanup := startTestControlPlane(t)
	defer cleanup()

	p, err := New(Config{
		ControlPlaneAddr: addr,
		GPUCount:         4,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer p.TerminateAll()

	ctx := context.Background()

	node, err := p.Provision(ctx, provider.ProvisionRequest{
		Name:         "test-node",
		InstanceType: "gpu_4x_h100",
		Region:       "us-west-2",
	})
	if err != nil {
		t.Fatal(err)
	}

	if node.ID == "" {
		t.Error("expected node ID")
	}
	if node.Provider != "fake" {
		t.Errorf("expected provider 'fake', got %s", node.Provider)
	}
	if node.GPUCount != 4 {
		t.Errorf("expected 4 GPUs, got %d", node.GPUCount)
	}
	if node.Status != "running" {
		t.Errorf("expected status 'running', got %s", node.Status)
	}

	// Give the agent time to register
	time.Sleep(100 * time.Millisecond)

	if p.RunningCount() != 1 {
		t.Errorf("expected 1 running instance, got %d", p.RunningCount())
	}

	if err := p.Terminate(ctx, node.ID); err != nil {
		t.Fatal(err)
	}

	// Give the agent time to stop
	time.Sleep(100 * time.Millisecond)

	if p.RunningCount() != 0 {
		t.Errorf("expected 0 running instances, got %d", p.RunningCount())
	}
}

func TestFakeProvider_List(t *testing.T) {
	addr, cleanup := startTestControlPlane(t)
	defer cleanup()

	p, err := New(Config{
		ControlPlaneAddr: addr,
		GPUCount:         2,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer p.TerminateAll()

	ctx := context.Background()

	nodes, err := p.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 0 {
		t.Errorf("expected 0 nodes, got %d", len(nodes))
	}

	p.Provision(ctx, provider.ProvisionRequest{Name: "node-1", InstanceType: "gpu_2x"})
	p.Provision(ctx, provider.ProvisionRequest{Name: "node-2", InstanceType: "gpu_2x"})

	time.Sleep(100 * time.Millisecond)

	nodes, err = p.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(nodes))
	}
}

func TestFakeProvider_TerminateAll(t *testing.T) {
	addr, cleanup := startTestControlPlane(t)
	defer cleanup()

	p, err := New(Config{
		ControlPlaneAddr: addr,
		GPUCount:         1,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	p.Provision(ctx, provider.ProvisionRequest{Name: "node-1", InstanceType: "gpu_1x"})
	p.Provision(ctx, provider.ProvisionRequest{Name: "node-2", InstanceType: "gpu_1x"})
	p.Provision(ctx, provider.ProvisionRequest{Name: "node-3", InstanceType: "gpu_1x"})

	time.Sleep(100 * time.Millisecond)

	if p.RunningCount() != 3 {
		t.Errorf("expected 3 running instances, got %d", p.RunningCount())
	}

	p.TerminateAll()

	time.Sleep(100 * time.Millisecond)

	if p.RunningCount() != 0 {
		t.Errorf("expected 0 running instances, got %d", p.RunningCount())
	}
}

func TestFakeProvider_TerminateNotFound(t *testing.T) {
	p, _ := New(Config{ControlPlaneAddr: "http://localhost:50051"})
	err := p.Terminate(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent node")
	}
}

func TestFakeProvider_MultipleNodes(t *testing.T) {
	addr, cleanup := startTestControlPlane(t)
	defer cleanup()

	p, err := New(Config{
		ControlPlaneAddr: addr,
		GPUCount:         8,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer p.TerminateAll()

	ctx := context.Background()

	var nodes []*provider.Node
	for i := 0; i < 5; i++ {
		n, err := p.Provision(ctx, provider.ProvisionRequest{
			Name:         "node",
			InstanceType: "gpu_8x_h100",
			Region:       "us-east-1",
			Labels:       map[string]string{"pool": "training"},
		})
		if err != nil {
			t.Fatal(err)
		}
		nodes = append(nodes, n)
	}

	// Each node should have a unique ID
	ids := make(map[string]bool)
	for _, n := range nodes {
		if ids[n.ID] {
			t.Errorf("duplicate node ID: %s", n.ID)
		}
		ids[n.ID] = true
	}

	time.Sleep(200 * time.Millisecond)

	if p.RunningCount() != 5 {
		t.Errorf("expected 5 running instances, got %d", p.RunningCount())
	}
}

