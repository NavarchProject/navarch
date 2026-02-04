package bootstrap

import (
	"testing"
)

func TestRenderCommand(t *testing.T) {
	b := &Bootstrapper{}

	vars := TemplateVars{
		ControlPlane: "http://localhost:50051",
		Pool:         "gpu-pool",
		NodeID:       "node-123",
		Provider:     "lambda",
		Region:       "us-east-1",
		InstanceType: "gpu_1x_h100",
	}

	tests := []struct {
		name     string
		cmd      string
		expected string
	}{
		{
			name:     "no template",
			cmd:      "echo hello",
			expected: "echo hello",
		},
		{
			name:     "control plane",
			cmd:      "curl {{.ControlPlane}}/health",
			expected: "curl http://localhost:50051/health",
		},
		{
			name:     "pool name",
			cmd:      "navarch-node -pool {{.Pool}}",
			expected: "navarch-node -pool gpu-pool",
		},
		{
			name:     "node id",
			cmd:      "navarch-node -node-id {{.NodeID}}",
			expected: "navarch-node -node-id node-123",
		},
		{
			name:     "multiple vars",
			cmd:      "navarch-node -server {{.ControlPlane}} -pool {{.Pool}} -node-id {{.NodeID}}",
			expected: "navarch-node -server http://localhost:50051 -pool gpu-pool -node-id node-123",
		},
		{
			name:     "all vars",
			cmd:      "echo {{.Provider}} {{.Region}} {{.InstanceType}}",
			expected: "echo lambda us-east-1 gpu_1x_h100",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := b.renderCommand(tt.cmd, vars)
			if err != nil {
				t.Fatalf("renderCommand failed: %v", err)
			}
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestRenderCommand_InvalidTemplate(t *testing.T) {
	b := &Bootstrapper{}

	vars := TemplateVars{}

	_, err := b.renderCommand("{{.Invalid", vars)
	if err == nil {
		t.Error("expected error for invalid template")
	}
}

func TestNew(t *testing.T) {
	cfg := Config{
		SetupCommands:     []string{"echo hello"},
		SSHUser:           "ubuntu",
		SSHPrivateKeyPath: "/path/to/key",
	}

	b := New(cfg, nil)
	if b == nil {
		t.Fatal("expected non-nil bootstrapper")
	}
	if b.config.SSHUser != "ubuntu" {
		t.Errorf("expected SSHUser 'ubuntu', got %q", b.config.SSHUser)
	}
}
