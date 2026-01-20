package controlplane

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/NavarchProject/navarch/pkg/pool"
)

func TestLoadPoolsConfig(t *testing.T) {
	yaml := `
pools:
  - name: test-pool
    provider: lambda
    instance_type: gpu_1x_a100
    region: us-west-2
    scaling:
      min_nodes: 2
      max_nodes: 10
      cooldown_period: 5m
      autoscaler:
        type: reactive
        scale_up_threshold: 80
        scale_down_threshold: 20
    health:
      unhealthy_threshold: 3
      auto_replace: true
    labels:
      env: test
global:
  ssh_key_names:
    - team-key
providers:
  lambda:
    api_key_secret: LAMBDA_KEY
`

	dir := t.TempDir()
	path := filepath.Join(dir, "pools.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadPoolsConfig(path)
	if err != nil {
		t.Fatal(err)
	}

	if len(cfg.Pools) != 1 {
		t.Fatalf("expected 1 pool, got %d", len(cfg.Pools))
	}

	p := cfg.Pools[0]
	if p.Name != "test-pool" {
		t.Errorf("expected name test-pool, got %s", p.Name)
	}
	if p.Provider != "lambda" {
		t.Errorf("expected provider lambda, got %s", p.Provider)
	}
	if p.Scaling.MinNodes != 2 {
		t.Errorf("expected min_nodes 2, got %d", p.Scaling.MinNodes)
	}
	if p.Scaling.MaxNodes != 10 {
		t.Errorf("expected max_nodes 10, got %d", p.Scaling.MaxNodes)
	}
	if p.Health.UnhealthyThreshold != 3 {
		t.Errorf("expected unhealthy_threshold 3, got %d", p.Health.UnhealthyThreshold)
	}
	if !p.Health.AutoReplace {
		t.Error("expected auto_replace true")
	}
	if p.Labels["env"] != "test" {
		t.Errorf("expected label env=test, got %s", p.Labels["env"])
	}

	if len(cfg.Global.SSHKeyNames) != 1 || cfg.Global.SSHKeyNames[0] != "team-key" {
		t.Errorf("expected ssh_key_names [team-key], got %v", cfg.Global.SSHKeyNames)
	}
}

func TestLoadPoolsConfig_FileNotFound(t *testing.T) {
	_, err := LoadPoolsConfig("/nonexistent/path.yaml")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestLoadPoolsConfig_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "invalid.yaml")
	if err := os.WriteFile(path, []byte("invalid: yaml: content:"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadPoolsConfig(path)
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestPoolConfigYAML_ToPoolConfig(t *testing.T) {
	cfg := PoolConfigYAML{
		Name:         "convert-test",
		Provider:     "lambda",
		InstanceType: "gpu_8x_h100",
		Region:       "us-east-1",
		Zones:        []string{"us-east-1a", "us-east-1b"},
		Scaling: ScalingConfig{
			MinNodes:       5,
			MaxNodes:       50,
			CooldownPeriod: "10m",
		},
		Health: HealthConfig{
			UnhealthyThreshold: 2,
			AutoReplace:        true,
		},
		Labels: map[string]string{"tier": "premium"},
	}

	global := GlobalConfig{
		SSHKeyNames: []string{"ops-key"},
	}

	poolCfg, err := cfg.ToPoolConfig(global)
	if err != nil {
		t.Fatal(err)
	}

	if poolCfg.Name != "convert-test" {
		t.Errorf("expected name convert-test, got %s", poolCfg.Name)
	}
	if poolCfg.CooldownPeriod != 10*time.Minute {
		t.Errorf("expected cooldown 10m, got %s", poolCfg.CooldownPeriod)
	}
	if len(poolCfg.SSHKeyNames) != 1 || poolCfg.SSHKeyNames[0] != "ops-key" {
		t.Errorf("expected ssh keys from global, got %v", poolCfg.SSHKeyNames)
	}
	if len(poolCfg.Zones) != 2 {
		t.Errorf("expected 2 zones, got %d", len(poolCfg.Zones))
	}
}

func TestPoolConfigYAML_ToPoolConfig_InvalidDuration(t *testing.T) {
	cfg := PoolConfigYAML{
		Name: "bad-duration",
		Scaling: ScalingConfig{
			CooldownPeriod: "not-a-duration",
		},
	}

	_, err := cfg.ToPoolConfig(GlobalConfig{})
	if err == nil {
		t.Error("expected error for invalid duration")
	}
}

func TestBuildAutoscaler_Reactive(t *testing.T) {
	cfg := AutoscalerConfig{
		Type:               "reactive",
		ScaleUpThreshold:   85,
		ScaleDownThreshold: 15,
	}

	a, err := BuildAutoscaler(cfg)
	if err != nil {
		t.Fatal(err)
	}

	ra, ok := a.(*pool.ReactiveAutoscaler)
	if !ok {
		t.Fatal("expected ReactiveAutoscaler")
	}
	if ra.ScaleUpThreshold != 85 {
		t.Errorf("expected scale_up 85, got %f", ra.ScaleUpThreshold)
	}
}

func TestBuildAutoscaler_Queue(t *testing.T) {
	cfg := AutoscalerConfig{
		Type:        "queue",
		JobsPerNode: 50,
	}

	a, err := BuildAutoscaler(cfg)
	if err != nil {
		t.Fatal(err)
	}

	qa, ok := a.(*pool.QueueBasedAutoscaler)
	if !ok {
		t.Fatal("expected QueueBasedAutoscaler")
	}
	if qa.JobsPerNode != 50 {
		t.Errorf("expected jobs_per_node 50, got %d", qa.JobsPerNode)
	}
}

func TestBuildAutoscaler_Scheduled(t *testing.T) {
	cfg := AutoscalerConfig{
		Type: "scheduled",
		Schedule: []ScheduleEntryYAML{
			{
				Days:      []string{"monday", "tuesday"},
				StartHour: 9,
				EndHour:   17,
				MinNodes:  5,
				MaxNodes:  20,
			},
		},
		Fallback: &AutoscalerConfig{
			Type:               "reactive",
			ScaleUpThreshold:   80,
			ScaleDownThreshold: 20,
		},
	}

	a, err := BuildAutoscaler(cfg)
	if err != nil {
		t.Fatal(err)
	}

	sa, ok := a.(*pool.ScheduledAutoscaler)
	if !ok {
		t.Fatal("expected ScheduledAutoscaler")
	}
	if len(sa.Schedule) != 1 {
		t.Errorf("expected 1 schedule entry, got %d", len(sa.Schedule))
	}
	if sa.Fallback == nil {
		t.Error("expected fallback autoscaler")
	}
}

func TestBuildAutoscaler_Predictive(t *testing.T) {
	cfg := AutoscalerConfig{
		Type:           "predictive",
		LookbackWindow: 30,
		GrowthFactor:   1.5,
	}

	a, err := BuildAutoscaler(cfg)
	if err != nil {
		t.Fatal(err)
	}

	pa, ok := a.(*pool.PredictiveAutoscaler)
	if !ok {
		t.Fatal("expected PredictiveAutoscaler")
	}
	if pa.LookbackWindow != 30 {
		t.Errorf("expected lookback 30, got %d", pa.LookbackWindow)
	}
	if pa.GrowthFactor != 1.5 {
		t.Errorf("expected growth 1.5, got %f", pa.GrowthFactor)
	}
}

func TestBuildAutoscaler_Composite(t *testing.T) {
	cfg := AutoscalerConfig{
		Type: "composite",
		Mode: "min",
		Autoscalers: []AutoscalerConfig{
			{Type: "reactive", ScaleUpThreshold: 80, ScaleDownThreshold: 20},
			{Type: "queue", JobsPerNode: 100},
		},
	}

	a, err := BuildAutoscaler(cfg)
	if err != nil {
		t.Fatal(err)
	}

	ca, ok := a.(*pool.CompositeAutoscaler)
	if !ok {
		t.Fatal("expected CompositeAutoscaler")
	}
	if len(ca.Autoscalers) != 2 {
		t.Errorf("expected 2 autoscalers, got %d", len(ca.Autoscalers))
	}
	if ca.Mode != pool.ModeMin {
		t.Errorf("expected ModeMin, got %d", ca.Mode)
	}
}

func TestBuildAutoscaler_Empty(t *testing.T) {
	a, err := BuildAutoscaler(AutoscalerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if a != nil {
		t.Error("expected nil autoscaler for empty config")
	}
}

func TestBuildAutoscaler_Unknown(t *testing.T) {
	_, err := BuildAutoscaler(AutoscalerConfig{Type: "unknown"})
	if err == nil {
		t.Error("expected error for unknown type")
	}
}

func TestParseDays(t *testing.T) {
	tests := []struct {
		input    []string
		expected []time.Weekday
	}{
		{[]string{"monday", "friday"}, []time.Weekday{time.Monday, time.Friday}},
		{[]string{"Sunday"}, []time.Weekday{time.Sunday}},
		{[]string{}, []time.Weekday{}},
	}

	for _, tt := range tests {
		days, err := parseDays(tt.input)
		if err != nil {
			t.Errorf("parseDays(%v) error: %v", tt.input, err)
			continue
		}
		if len(days) != len(tt.expected) {
			t.Errorf("parseDays(%v) = %v, want %v", tt.input, days, tt.expected)
		}
	}
}

func TestParseDays_Invalid(t *testing.T) {
	_, err := parseDays([]string{"notaday"})
	if err == nil {
		t.Error("expected error for invalid day")
	}
}

