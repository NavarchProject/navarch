package pool

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/NavarchProject/navarch/pkg/provider"
)

type bootstrapTestProvider struct {
	provisions atomic.Int64
	nodeIP     string
}

func (p *bootstrapTestProvider) Name() string { return "bootstrap-test" }

func (p *bootstrapTestProvider) Provision(ctx context.Context, req provider.ProvisionRequest) (*provider.Node, error) {
	p.provisions.Add(1)
	return &provider.Node{
		ID:           "test-node-1",
		Provider:     "bootstrap-test",
		Region:       req.Region,
		InstanceType: req.InstanceType,
		Status:       "running",
		IPAddress:    p.nodeIP,
	}, nil
}

func (p *bootstrapTestProvider) Terminate(ctx context.Context, id string) error {
	return nil
}

func (p *bootstrapTestProvider) List(ctx context.Context) ([]*provider.Node, error) {
	if p.provisions.Load() > 0 {
		return []*provider.Node{
			{
				ID:        "test-node-1",
				Provider:  "bootstrap-test",
				IPAddress: p.nodeIP,
			},
		}, nil
	}
	return nil, nil
}

func TestPool_BootstrapTriggered(t *testing.T) {
	prov := &bootstrapTestProvider{nodeIP: "10.0.0.1"}

	p, err := NewWithOptions(NewPoolOptions{
		Config: Config{
			Name:              "bootstrap-test",
			MinNodes:          0,
			MaxNodes:          5,
			SetupCommands:     []string{"echo hello {{.NodeID}}", "echo pool={{.Pool}}"},
			SSHUser:           "ubuntu",
			SSHPrivateKeyPath: "/nonexistent/key",
			ControlPlaneAddr:  "http://localhost:50051",
		},
		Providers: []ProviderConfig{
			{Name: "bootstrap-test", Provider: prov, Priority: 1},
		},
		ProviderStrategy: "priority",
	})
	if err != nil {
		t.Fatal(err)
	}

	nodes, err := p.ScaleUp(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	if nodes[0].ID != "test-node-1" {
		t.Errorf("expected node ID 'test-node-1', got %q", nodes[0].ID)
	}

	// Let bootstrap goroutine attempt SSH (will fail due to missing key)
	time.Sleep(500 * time.Millisecond)

	status := p.Status()
	if status.TotalNodes != 1 {
		t.Errorf("expected 1 total node, got %d", status.TotalNodes)
	}
}

func TestPool_NoBootstrapWithoutCommands(t *testing.T) {
	prov := &bootstrapTestProvider{nodeIP: "10.0.0.1"}

	p, err := NewWithOptions(NewPoolOptions{
		Config: Config{
			Name:     "no-bootstrap-test",
			MinNodes: 0,
			MaxNodes: 5,
		},
		Providers: []ProviderConfig{
			{Name: "bootstrap-test", Provider: prov, Priority: 1},
		},
		ProviderStrategy: "priority",
	})
	if err != nil {
		t.Fatal(err)
	}

	nodes, err := p.ScaleUp(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}

	time.Sleep(100 * time.Millisecond)

	status := p.Status()
	if status.TotalNodes != 1 {
		t.Errorf("expected 1 total node, got %d", status.TotalNodes)
	}
	// Without setup commands, bootstrap status should be skipped (not counted)
	if status.BootstrapFailed != 0 || status.BootstrapPending != 0 {
		t.Errorf("expected no bootstrap tracking for skipped, got failed=%d pending=%d",
			status.BootstrapFailed, status.BootstrapPending)
	}
}

func TestPool_BootstrapStatusTracking(t *testing.T) {
	prov := &bootstrapTestProvider{nodeIP: "10.0.0.1"}

	p, err := NewWithOptions(NewPoolOptions{
		Config: Config{
			Name:              "bootstrap-status-test",
			MinNodes:          0,
			MaxNodes:          5,
			SetupCommands:     []string{"echo hello"},
			SSHUser:           "ubuntu",
			SSHPrivateKeyPath: "/nonexistent/key",
			ControlPlaneAddr:  "http://localhost:50051",
		},
		Providers: []ProviderConfig{
			{Name: "bootstrap-test", Provider: prov, Priority: 1},
		},
		ProviderStrategy: "priority",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = p.ScaleUp(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}

	// Give bootstrap goroutine time to fail (missing SSH key)
	time.Sleep(500 * time.Millisecond)

	status := p.Status()
	if status.TotalNodes != 1 {
		t.Errorf("expected 1 total node, got %d", status.TotalNodes)
	}
	// Bootstrap should have failed due to missing SSH key
	if status.BootstrapFailed != 1 {
		t.Errorf("expected 1 bootstrap failed node, got %d (pending=%d)",
			status.BootstrapFailed, status.BootstrapPending)
	}
}
