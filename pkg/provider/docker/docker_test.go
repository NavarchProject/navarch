package docker

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/NavarchProject/navarch/pkg/provider"
)

func findSSHPublicKey() string {
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".ssh", "id_ed25519.pub"),
		filepath.Join(home, ".ssh", "id_rsa.pub"),
		filepath.Join(home, ".ssh", "navarch-test.pub"),
	}
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

func TestDockerProvider_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Find an SSH public key
	keyPath := findSSHPublicKey()
	if keyPath == "" {
		t.Skip("no SSH public key found")
	}

	p, err := New(Config{SSHPublicKeyPath: keyPath})
	if err != nil {
		t.Skipf("failed to create provider (Docker not available?): %v", err)
	}
	defer p.Close()
	defer p.TerminateAll()

	// Pull the image - skip if Docker daemon not running
	if err := p.EnsureImage(ctx); err != nil {
		t.Skipf("failed to ensure image (Docker not running?): %v", err)
	}

	// Provision a container
	node, err := p.Provision(ctx, provider.ProvisionRequest{
		Labels: map[string]string{"pool": "test"},
	})
	if err != nil {
		t.Fatalf("failed to provision: %v", err)
	}

	t.Logf("provisioned node %s on %s:%d", node.ID, node.IPAddress, node.SSHPort)

	if node.IPAddress != "127.0.0.1" {
		t.Errorf("expected IP 127.0.0.1, got %s", node.IPAddress)
	}
	if node.SSHPort == 0 {
		t.Error("expected non-zero SSH port")
	}

	// Verify the port is listening
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", node.SSHPort), 10*time.Second)
	if err != nil {
		t.Fatalf("failed to connect to SSH port: %v", err)
	}
	conn.Close()
	t.Log("SSH port is listening")

	// List should return the node
	nodes, err := p.List(ctx)
	if err != nil {
		t.Fatalf("failed to list: %v", err)
	}
	if len(nodes) != 1 {
		t.Errorf("expected 1 node, got %d", len(nodes))
	}

	// Terminate
	if err := p.Terminate(ctx, node.ID); err != nil {
		t.Fatalf("failed to terminate: %v", err)
	}

	// List should be empty
	nodes, err = p.List(ctx)
	if err != nil {
		t.Fatalf("failed to list after terminate: %v", err)
	}
	if len(nodes) != 0 {
		t.Errorf("expected 0 nodes after terminate, got %d", len(nodes))
	}
}
