package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestParse_SingleDocument(t *testing.T) {
	yaml := `
apiVersion: navarch.io/v1alpha1
kind: Provider
metadata:
  name: lambda-prod
spec:
  type: lambda
  lambda:
    apiKeyEnvVar: LAMBDA_API_KEY
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}

	if len(cfg.Providers) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(cfg.Providers))
	}

	provider := cfg.GetProvider("lambda-prod")
	if provider == nil {
		t.Fatal("provider lambda-prod not found")
	}
	if provider.Spec.Type != "lambda" {
		t.Errorf("expected type lambda, got %s", provider.Spec.Type)
	}
}

func TestParse_MultiDocument(t *testing.T) {
	yaml := `
apiVersion: navarch.io/v1alpha1
kind: ControlPlane
metadata:
  name: prod
spec:
  address: ":8080"
  healthCheckInterval: 2m
---
apiVersion: navarch.io/v1alpha1
kind: Provider
metadata:
  name: fake-dev
spec:
  type: fake
  fake:
    gpuCount: 4
---
apiVersion: navarch.io/v1alpha1
kind: Pool
metadata:
  name: training
  labels:
    workload: training
spec:
  providerRef: fake-dev
  instanceType: gpu_8x_h100
  region: us-west-2
  scaling:
    minReplicas: 2
    maxReplicas: 10
    cooldownPeriod: 5m
    autoscaler:
      type: reactive
      scaleUpThreshold: 80
      scaleDownThreshold: 20
  health:
    unhealthyThreshold: 2
    autoReplace: true
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}

	if cfg.ControlPlane == nil {
		t.Fatal("expected ControlPlane")
	}
	if cfg.ControlPlane.Spec.Address != ":8080" {
		t.Errorf("expected address :8080, got %s", cfg.ControlPlane.Spec.Address)
	}
	if cfg.ControlPlane.Spec.HealthCheckInterval != Duration(2*time.Minute) {
		t.Errorf("expected 2m, got %v", cfg.ControlPlane.Spec.HealthCheckInterval)
	}

	if len(cfg.Providers) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(cfg.Providers))
	}
	if cfg.Providers["fake-dev"].Spec.Fake.GPUCount != 4 {
		t.Errorf("expected 4 GPUs, got %d", cfg.Providers["fake-dev"].Spec.Fake.GPUCount)
	}

	if len(cfg.Pools) != 1 {
		t.Fatalf("expected 1 pool, got %d", len(cfg.Pools))
	}
	pool := cfg.GetPool("training")
	if pool == nil {
		t.Fatal("pool training not found")
	}
	if pool.Spec.ProviderRef != "fake-dev" {
		t.Errorf("expected providerRef fake-dev, got %s", pool.Spec.ProviderRef)
	}
	if pool.Spec.Scaling.MinReplicas != 2 {
		t.Errorf("expected minReplicas 2, got %d", pool.Spec.Scaling.MinReplicas)
	}
	if pool.Spec.Scaling.Autoscaler.Type != "reactive" {
		t.Errorf("expected autoscaler type reactive, got %s", pool.Spec.Scaling.Autoscaler.Type)
	}
}

func TestParse_UnsupportedAPIVersion(t *testing.T) {
	yaml := `
apiVersion: navarch.io/v2
kind: Pool
metadata:
  name: test
spec: {}
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Error("expected error for unsupported apiVersion")
	}
}

func TestParse_UnknownKind(t *testing.T) {
	yaml := `
apiVersion: navarch.io/v1alpha1
kind: Unknown
metadata:
  name: test
spec: {}
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Error("expected error for unknown kind")
	}
}

func TestParse_MissingKind(t *testing.T) {
	yaml := `
apiVersion: navarch.io/v1alpha1
metadata:
  name: test
spec: {}
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Error("expected error for missing kind")
	}
}

func TestParse_DuplicateProvider(t *testing.T) {
	yaml := `
apiVersion: navarch.io/v1alpha1
kind: Provider
metadata:
  name: dupe
spec:
  type: fake
---
apiVersion: navarch.io/v1alpha1
kind: Provider
metadata:
  name: dupe
spec:
  type: lambda
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Error("expected error for duplicate provider")
	}
}

func TestParse_MultipleControlPlanes(t *testing.T) {
	yaml := `
apiVersion: navarch.io/v1alpha1
kind: ControlPlane
metadata:
  name: one
spec: {}
---
apiVersion: navarch.io/v1alpha1
kind: ControlPlane
metadata:
  name: two
spec: {}
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Error("expected error for multiple ControlPlanes")
	}
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *Config
		wantErr bool
	}{
		{
			name: "valid single provider",
			cfg: &Config{
				Providers: map[string]*Provider{
					"fake": {Spec: ProviderSpec{Type: "fake"}},
				},
				Pools: []*Pool{
					{
						Metadata: ObjectMeta{Name: "pool-1"},
						Spec: PoolSpec{
							ProviderRef: "fake",
							Scaling:     ScalingSpec{MinReplicas: 0, MaxReplicas: 10},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid multi-provider",
			cfg: &Config{
				Providers: map[string]*Provider{
					"lambda": {Spec: ProviderSpec{Type: "lambda"}},
					"gcp":    {Spec: ProviderSpec{Type: "gcp"}},
				},
				Pools: []*Pool{
					{
						Metadata: ObjectMeta{Name: "pool-1"},
						Spec: PoolSpec{
							Providers: []PoolProviderRef{
								{Name: "lambda", Priority: 1},
								{Name: "gcp", Priority: 2},
							},
							ProviderStrategy: "priority",
							Scaling:          ScalingSpec{MinReplicas: 0, MaxReplicas: 10},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "missing pool name",
			cfg: &Config{
				Providers: map[string]*Provider{"fake": {Spec: ProviderSpec{Type: "fake"}}},
				Pools:     []*Pool{{Spec: PoolSpec{ProviderRef: "fake", Scaling: ScalingSpec{MaxReplicas: 10}}}},
			},
			wantErr: true,
		},
		{
			name: "missing both providerRef and providers",
			cfg: &Config{
				Providers: map[string]*Provider{"fake": {Spec: ProviderSpec{Type: "fake"}}},
				Pools:     []*Pool{{Metadata: ObjectMeta{Name: "pool"}, Spec: PoolSpec{Scaling: ScalingSpec{MaxReplicas: 10}}}},
			},
			wantErr: true,
		},
		{
			name: "has both providerRef and providers",
			cfg: &Config{
				Providers: map[string]*Provider{"fake": {Spec: ProviderSpec{Type: "fake"}}},
				Pools: []*Pool{{
					Metadata: ObjectMeta{Name: "pool"},
					Spec: PoolSpec{
						ProviderRef: "fake",
						Providers:   []PoolProviderRef{{Name: "fake"}},
						Scaling:     ScalingSpec{MaxReplicas: 10},
					},
				}},
			},
			wantErr: true,
		},
		{
			name: "unknown providerRef",
			cfg: &Config{
				Providers: map[string]*Provider{"fake": {Spec: ProviderSpec{Type: "fake"}}},
				Pools: []*Pool{
					{Metadata: ObjectMeta{Name: "pool"}, Spec: PoolSpec{ProviderRef: "unknown", Scaling: ScalingSpec{MaxReplicas: 10}}},
				},
			},
			wantErr: true,
		},
		{
			name: "unknown provider in providers list",
			cfg: &Config{
				Providers: map[string]*Provider{"fake": {Spec: ProviderSpec{Type: "fake"}}},
				Pools: []*Pool{{
					Metadata: ObjectMeta{Name: "pool"},
					Spec: PoolSpec{
						Providers: []PoolProviderRef{{Name: "unknown"}},
						Scaling:   ScalingSpec{MaxReplicas: 10},
					},
				}},
			},
			wantErr: true,
		},
		{
			name: "empty provider name in providers list",
			cfg: &Config{
				Providers: map[string]*Provider{"fake": {Spec: ProviderSpec{Type: "fake"}}},
				Pools: []*Pool{{
					Metadata: ObjectMeta{Name: "pool"},
					Spec: PoolSpec{
						Providers: []PoolProviderRef{{Name: ""}},
						Scaling:   ScalingSpec{MaxReplicas: 10},
					},
				}},
			},
			wantErr: true,
		},
		{
			name: "invalid provider strategy",
			cfg: &Config{
				Providers: map[string]*Provider{"fake": {Spec: ProviderSpec{Type: "fake"}}},
				Pools: []*Pool{{
					Metadata: ObjectMeta{Name: "pool"},
					Spec: PoolSpec{
						Providers:        []PoolProviderRef{{Name: "fake"}},
						ProviderStrategy: "invalid-strategy",
						Scaling:          ScalingSpec{MaxReplicas: 10},
					},
				}},
			},
			wantErr: true,
		},
		{
			name: "negative minReplicas",
			cfg: &Config{
				Providers: map[string]*Provider{"fake": {Spec: ProviderSpec{Type: "fake"}}},
				Pools: []*Pool{
					{Metadata: ObjectMeta{Name: "pool"}, Spec: PoolSpec{ProviderRef: "fake", Scaling: ScalingSpec{MinReplicas: -1, MaxReplicas: 10}}},
				},
			},
			wantErr: true,
		},
		{
			name: "maxReplicas < minReplicas",
			cfg: &Config{
				Providers: map[string]*Provider{"fake": {Spec: ProviderSpec{Type: "fake"}}},
				Pools: []*Pool{
					{Metadata: ObjectMeta{Name: "pool"}, Spec: PoolSpec{ProviderRef: "fake", Scaling: ScalingSpec{MinReplicas: 10, MaxReplicas: 5}}},
				},
			},
			wantErr: true,
		},
		{
			name: "provider missing type",
			cfg: &Config{
				Providers: map[string]*Provider{"fake": {Spec: ProviderSpec{}}},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConfig_Defaults(t *testing.T) {
	cfg := &Config{
		ControlPlane: &ControlPlane{Spec: ControlPlaneSpec{}},
		Providers: map[string]*Provider{
			"fake": {Spec: ProviderSpec{Type: "fake", Fake: &FakeProviderSpec{}}},
		},
		Pools: []*Pool{
			{Spec: PoolSpec{Health: HealthSpec{}}},
		},
	}

	cfg.Defaults()

	if cfg.ControlPlane.Spec.Address != ":50051" {
		t.Errorf("expected default address :50051, got %s", cfg.ControlPlane.Spec.Address)
	}
	if cfg.ControlPlane.Spec.HealthCheckInterval != Duration(60*time.Second) {
		t.Errorf("expected default healthCheckInterval 60s, got %v", cfg.ControlPlane.Spec.HealthCheckInterval)
	}
	if cfg.Providers["fake"].Spec.Fake.GPUCount != 8 {
		t.Errorf("expected default gpuCount 8, got %d", cfg.Providers["fake"].Spec.Fake.GPUCount)
	}
	if cfg.Pools[0].Spec.Health.UnhealthyThreshold != 3 {
		t.Errorf("expected default unhealthyThreshold 3, got %d", cfg.Pools[0].Spec.Health.UnhealthyThreshold)
	}
}

func TestLoad_File(t *testing.T) {
	yaml := `
apiVersion: navarch.io/v1alpha1
kind: Provider
metadata:
  name: test
spec:
  type: fake
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if len(cfg.Providers) != 1 {
		t.Errorf("expected 1 provider, got %d", len(cfg.Providers))
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/path.yaml")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestDuration_UnmarshalYAML(t *testing.T) {
	tests := []struct {
		input    string
		expected time.Duration
	}{
		{`duration: 5m`, 5 * time.Minute},
		{`duration: 30s`, 30 * time.Second},
		{`duration: 1h30m`, 90 * time.Minute},
		{`duration: ""`, 0},
	}

	for _, tt := range tests {
		var obj struct {
			Duration Duration `yaml:"duration"`
		}
		if err := parseYAML([]byte(tt.input), &obj); err != nil {
			t.Errorf("failed to parse %q: %v", tt.input, err)
			continue
		}
		if obj.Duration.Duration() != tt.expected {
			t.Errorf("input %q: expected %v, got %v", tt.input, tt.expected, obj.Duration.Duration())
		}
	}
}

func parseYAML(data []byte, v any) error {
	return yaml.Unmarshal(data, v)
}

