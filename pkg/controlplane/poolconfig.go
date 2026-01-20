package controlplane

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/NavarchProject/navarch/pkg/pool"
)

// PoolsConfig represents the top-level pools configuration file.
type PoolsConfig struct {
	Pools     []PoolConfigYAML          `yaml:"pools"`
	Global    GlobalConfig              `yaml:"global,omitempty"`
	Providers map[string]ProviderConfig `yaml:"providers,omitempty"`
}

// PoolConfigYAML represents a single pool configuration in YAML.
type PoolConfigYAML struct {
	Name         string            `yaml:"name"`
	Provider     string            `yaml:"provider"`
	InstanceType string            `yaml:"instance_type"`
	Region       string            `yaml:"region"`
	Zones        []string          `yaml:"zones,omitempty"`
	Scaling      ScalingConfig     `yaml:"scaling"`
	Health       HealthConfig      `yaml:"health,omitempty"`
	Labels       map[string]string `yaml:"labels,omitempty"`
}

// ScalingConfig holds scaling limits and autoscaler configuration.
type ScalingConfig struct {
	MinNodes       int              `yaml:"min_nodes"`
	MaxNodes       int              `yaml:"max_nodes"`
	CooldownPeriod string           `yaml:"cooldown_period,omitempty"`
	Autoscaler     AutoscalerConfig `yaml:"autoscaler,omitempty"`
}

// HealthConfig holds health check settings.
type HealthConfig struct {
	UnhealthyThreshold int  `yaml:"unhealthy_threshold"`
	AutoReplace        bool `yaml:"auto_replace"`
}

// AutoscalerConfig holds autoscaler type and parameters.
type AutoscalerConfig struct {
	Type string `yaml:"type"`

	// Reactive
	ScaleUpThreshold   float64 `yaml:"scale_up_threshold,omitempty"`
	ScaleDownThreshold float64 `yaml:"scale_down_threshold,omitempty"`

	// Queue-based
	JobsPerNode int `yaml:"jobs_per_node,omitempty"`

	// Scheduled
	Schedule []ScheduleEntryYAML `yaml:"schedule,omitempty"`
	Fallback *AutoscalerConfig   `yaml:"fallback,omitempty"`

	// Predictive
	LookbackWindow int     `yaml:"lookback_window,omitempty"`
	GrowthFactor   float64 `yaml:"growth_factor,omitempty"`

	// Composite
	Mode        string             `yaml:"mode,omitempty"`
	Autoscalers []AutoscalerConfig `yaml:"autoscalers,omitempty"`
}

// ScheduleEntryYAML represents a schedule entry in YAML.
type ScheduleEntryYAML struct {
	Days      []string `yaml:"days,omitempty"`
	StartHour int      `yaml:"start_hour"`
	EndHour   int      `yaml:"end_hour"`
	MinNodes  int      `yaml:"min_nodes"`
	MaxNodes  int      `yaml:"max_nodes"`
}

// GlobalConfig holds global settings.
type GlobalConfig struct {
	SSHKeyNames []string    `yaml:"ssh_key_names,omitempty"`
	Agent       AgentConfig `yaml:"agent,omitempty"`
}

// AgentConfig holds agent settings.
type AgentConfig struct {
	Server              string `yaml:"server,omitempty"`
	HeartbeatInterval   string `yaml:"heartbeat_interval,omitempty"`
	HealthCheckInterval string `yaml:"health_check_interval,omitempty"`
}

// ProviderConfig holds provider credentials and settings.
type ProviderConfig struct {
	APIKeySecret      string `yaml:"api_key_secret,omitempty"`
	Project           string `yaml:"project,omitempty"`
	Region            string `yaml:"region,omitempty"`
	CredentialsSecret string `yaml:"credentials_secret,omitempty"`
	GPUCount          int    `yaml:"gpu_count,omitempty"` // For fake provider: GPUs per instance
}

// LoadPoolsConfig loads pool configuration from a YAML file.
func LoadPoolsConfig(path string) (*PoolsConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg PoolsConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	return &cfg, nil
}

// ToPoolConfig converts YAML config to pool.Config.
func (p *PoolConfigYAML) ToPoolConfig(global GlobalConfig) (pool.Config, error) {
	cfg := pool.Config{
		Name:               p.Name,
		InstanceType:       p.InstanceType,
		Region:             p.Region,
		Zones:              p.Zones,
		MinNodes:           p.Scaling.MinNodes,
		MaxNodes:           p.Scaling.MaxNodes,
		UnhealthyThreshold: p.Health.UnhealthyThreshold,
		AutoReplace:        p.Health.AutoReplace,
		Labels:             p.Labels,
	}

	if p.Scaling.CooldownPeriod != "" {
		d, err := time.ParseDuration(p.Scaling.CooldownPeriod)
		if err != nil {
			return cfg, fmt.Errorf("invalid cooldown_period: %w", err)
		}
		cfg.CooldownPeriod = d
	}

	if len(global.SSHKeyNames) > 0 {
		cfg.SSHKeyNames = global.SSHKeyNames
	}

	return cfg, nil
}

// ProviderName returns the provider name for this pool configuration.
func (p *PoolConfigYAML) ProviderName() string {
	return p.Provider
}

// BuildAutoscaler creates an Autoscaler from YAML config.
func BuildAutoscaler(cfg AutoscalerConfig) (pool.Autoscaler, error) {
	switch cfg.Type {
	case "reactive":
		return pool.NewReactiveAutoscaler(cfg.ScaleUpThreshold, cfg.ScaleDownThreshold), nil

	case "queue":
		return pool.NewQueueBasedAutoscaler(cfg.JobsPerNode), nil

	case "scheduled":
		schedule := make([]pool.ScheduleEntry, len(cfg.Schedule))
		for i, s := range cfg.Schedule {
			days, err := parseDays(s.Days)
			if err != nil {
				return nil, err
			}
			schedule[i] = pool.ScheduleEntry{
				DaysOfWeek: days,
				StartHour:  s.StartHour,
				EndHour:    s.EndHour,
				MinNodes:   s.MinNodes,
				MaxNodes:   s.MaxNodes,
			}
		}
		var fallback pool.Autoscaler
		if cfg.Fallback != nil {
			var err error
			fallback, err = BuildAutoscaler(*cfg.Fallback)
			if err != nil {
				return nil, fmt.Errorf("failed to build fallback autoscaler: %w", err)
			}
		}
		return pool.NewScheduledAutoscaler(schedule, fallback), nil

	case "predictive":
		var fallback pool.Autoscaler
		if cfg.Fallback != nil {
			var err error
			fallback, err = BuildAutoscaler(*cfg.Fallback)
			if err != nil {
				return nil, fmt.Errorf("failed to build fallback autoscaler: %w", err)
			}
		}
		return pool.NewPredictiveAutoscaler(cfg.LookbackWindow, cfg.GrowthFactor, fallback), nil

	case "composite":
		mode := pool.ModeMax
		switch strings.ToLower(cfg.Mode) {
		case "min":
			mode = pool.ModeMin
		case "avg":
			mode = pool.ModeAvg
		}
		var autoscalers []pool.Autoscaler
		for _, ac := range cfg.Autoscalers {
			a, err := BuildAutoscaler(ac)
			if err != nil {
				return nil, err
			}
			autoscalers = append(autoscalers, a)
		}
		return pool.NewCompositeAutoscaler(mode, autoscalers...), nil

	case "":
		return nil, nil

	default:
		return nil, fmt.Errorf("unknown autoscaler type: %s", cfg.Type)
	}
}

func parseDays(days []string) ([]time.Weekday, error) {
	result := make([]time.Weekday, 0, len(days))
	for _, d := range days {
		switch strings.ToLower(d) {
		case "sunday":
			result = append(result, time.Sunday)
		case "monday":
			result = append(result, time.Monday)
		case "tuesday":
			result = append(result, time.Tuesday)
		case "wednesday":
			result = append(result, time.Wednesday)
		case "thursday":
			result = append(result, time.Thursday)
		case "friday":
			result = append(result, time.Friday)
		case "saturday":
			result = append(result, time.Saturday)
		default:
			return nil, fmt.Errorf("unknown day: %s", d)
		}
	}
	return result, nil
}
