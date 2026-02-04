package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	yaml := `
providers:
  lambda:
    type: lambda
    api_key_env: LAMBDA_API_KEY

pools:
  training:
    provider: lambda
    instance_type: gpu_8x_h100
    region: us-west-2
    min_nodes: 2
    max_nodes: 10
`
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Server.Address != ":50051" {
		t.Errorf("expected default address :50051, got %s", cfg.Server.Address)
	}
	if len(cfg.Providers) != 1 {
		t.Errorf("expected 1 provider, got %d", len(cfg.Providers))
	}
	if len(cfg.Pools) != 1 {
		t.Errorf("expected 1 pool, got %d", len(cfg.Pools))
	}

	pool := cfg.Pools["training"]
	if pool.Provider != "lambda" {
		t.Errorf("expected provider lambda, got %s", pool.Provider)
	}
	if pool.MinNodes != 2 {
		t.Errorf("expected min_nodes 2, got %d", pool.MinNodes)
	}
	if pool.Cooldown != 5*time.Minute {
		t.Errorf("expected default cooldown 5m, got %s", pool.Cooldown)
	}
}

func TestLoad_MultiProvider(t *testing.T) {
	yaml := `
providers:
  lambda:
    type: lambda
  gcp:
    type: gcp
    project: test

pools:
  fungible:
    providers:
      - name: lambda
        priority: 1
      - name: gcp
        priority: 2
    strategy: priority
    instance_type: h100-8x
    min_nodes: 1
    max_nodes: 10
`
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	pool := cfg.Pools["fungible"]
	if len(pool.Providers) != 2 {
		t.Errorf("expected 2 providers, got %d", len(pool.Providers))
	}
	if pool.Strategy != "priority" {
		t.Errorf("expected strategy priority, got %s", pool.Strategy)
	}
}

func TestLoad_Autoscaling(t *testing.T) {
	yaml := `
providers:
  fake:
    type: fake

pools:
  test:
    provider: fake
    instance_type: gpu_8x
    min_nodes: 1
    max_nodes: 10
    autoscaling:
      type: reactive
      scale_up_at: 80
      scale_down_at: 20
`
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	pool := cfg.Pools["test"]
	if pool.Autoscaling == nil {
		t.Fatal("expected autoscaling config")
	}
	if pool.Autoscaling.Type != "reactive" {
		t.Errorf("expected type reactive, got %s", pool.Autoscaling.Type)
	}
	if *pool.Autoscaling.ScaleUpAt != 80 {
		t.Errorf("expected scale_up_at 80, got %d", *pool.Autoscaling.ScaleUpAt)
	}
}

func TestLoad_Defaults(t *testing.T) {
	yaml := `
providers:
  fake:
    type: fake

pools:
  test:
    provider: fake
    instance_type: gpu_8x
    min_nodes: 1
    max_nodes: 10

defaults:
  ssh_keys:
    - default-key
  health:
    unhealthy_after: 3
    auto_replace: true
`
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	pool := cfg.Pools["test"]
	if len(pool.SSHKeys) != 1 || pool.SSHKeys[0] != "default-key" {
		t.Errorf("expected ssh_keys from defaults, got %v", pool.SSHKeys)
	}
	if pool.Health == nil || pool.Health.UnhealthyAfter != 3 {
		t.Errorf("expected health from defaults")
	}
}

func TestValidate_MissingProvider(t *testing.T) {
	yaml := `
providers:
  lambda:
    type: lambda

pools:
  test:
    provider: unknown
    instance_type: gpu_8x
    min_nodes: 1
    max_nodes: 10
`
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

func TestValidate_BothProviderAndProviders(t *testing.T) {
	yaml := `
providers:
  lambda:
    type: lambda

pools:
  test:
    provider: lambda
    providers:
      - name: lambda
    instance_type: gpu_8x
    min_nodes: 1
    max_nodes: 10
`
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error when both provider and providers specified")
	}
}

func TestValidate_InvalidScaling(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{
			name: "min > max",
			yaml: `
providers:
  fake:
    type: fake
pools:
  test:
    provider: fake
    instance_type: gpu_8x
    min_nodes: 10
    max_nodes: 5
`,
		},
		{
			name: "max_nodes zero",
			yaml: `
providers:
  fake:
    type: fake
pools:
  test:
    provider: fake
    instance_type: gpu_8x
    min_nodes: 0
    max_nodes: 0
`,
		},
		{
			name: "negative min_nodes",
			yaml: `
providers:
  fake:
    type: fake
pools:
  test:
    provider: fake
    instance_type: gpu_8x
    min_nodes: -1
    max_nodes: 10
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(tt.yaml), 0644); err != nil {
				t.Fatal(err)
			}

			_, err := Load(path)
			if err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestValidate_SetupCommandsRequireSSHKey(t *testing.T) {
	yaml := `
providers:
  fake:
    type: fake
pools:
  test:
    provider: fake
    instance_type: gpu_8x
    min_nodes: 1
    max_nodes: 5
    setup_commands:
      - echo hello
`
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected validation error for setup_commands without ssh_private_key_path")
	}
	if !strings.Contains(err.Error(), "ssh_private_key_path") {
		t.Errorf("expected error about ssh_private_key_path, got: %v", err)
	}
}

func TestValidate_SetupCommandsWithDefaultSSHKey(t *testing.T) {
	yaml := `
providers:
  fake:
    type: fake
defaults:
  ssh_private_key_path: /path/to/key
pools:
  test:
    provider: fake
    instance_type: gpu_8x
    min_nodes: 1
    max_nodes: 5
    setup_commands:
      - echo hello
`
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}
