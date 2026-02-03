package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the root configuration for Navarch.
type Config struct {
	Server    ServerConfig            `yaml:"server,omitempty"`
	Providers map[string]ProviderCfg  `yaml:"providers"`
	Pools     map[string]PoolCfg      `yaml:"pools"`
	Defaults  DefaultsCfg             `yaml:"defaults,omitempty"`
}

// ServerConfig configures the control plane server.
type ServerConfig struct {
	Address              string        `yaml:"address,omitempty"` // Default: ":50051"
	HeartbeatInterval    time.Duration `yaml:"heartbeat_interval,omitempty"`
	HeartbeatTimeout     time.Duration `yaml:"heartbeat_timeout,omitempty"` // Mark node unhealthy after this. Default: 3x heartbeat_interval
	HealthCheckInterval  time.Duration `yaml:"health_check_interval,omitempty"`
	AutoscaleInterval    time.Duration `yaml:"autoscale_interval,omitempty"`
	HealthPolicy         string        `yaml:"health_policy,omitempty"` // Path to health policy YAML file
}

// ProviderCfg configures a cloud provider.
type ProviderCfg struct {
	Type string `yaml:"type"` // lambda, gcp, aws, fake

	// Lambda
	APIKeyEnv string `yaml:"api_key_env,omitempty"` // Environment variable name

	// GCP
	Project string `yaml:"project,omitempty"`
	Zone    string `yaml:"zone,omitempty"`

	// AWS
	Region string `yaml:"region,omitempty"`

	// Fake
	GPUCount int `yaml:"gpu_count,omitempty"`
}

// PoolCfg configures a GPU node pool.
type PoolCfg struct {
	// Provider configuration - use "provider" for single, "providers" for multi
	Provider  string              `yaml:"provider,omitempty"`
	Providers []PoolProviderEntry `yaml:"providers,omitempty"`
	Strategy  string              `yaml:"strategy,omitempty"` // priority, cost, availability, round-robin

	InstanceType string   `yaml:"instance_type"`
	Region       string   `yaml:"region,omitempty"`
	Zones        []string `yaml:"zones,omitempty"`
	SSHKeys      []string `yaml:"ssh_keys,omitempty"`

	MinNodes int           `yaml:"min_nodes"`
	MaxNodes int           `yaml:"max_nodes"`
	Cooldown time.Duration `yaml:"cooldown,omitempty"`

	Autoscaling *AutoscalingCfg `yaml:"autoscaling,omitempty"`
	Health      *HealthCfg      `yaml:"health,omitempty"`

	Labels map[string]string `yaml:"labels,omitempty"`
}

// PoolProviderEntry configures a provider within a multi-provider pool.
type PoolProviderEntry struct {
	Name         string   `yaml:"name"`
	Priority     int      `yaml:"priority,omitempty"`
	Weight       int      `yaml:"weight,omitempty"`
	Regions      []string `yaml:"regions,omitempty"`
	InstanceType string   `yaml:"instance_type,omitempty"` // Override for this provider
}

// AutoscalingCfg configures autoscaling behavior.
type AutoscalingCfg struct {
	Type string `yaml:"type"` // reactive, queue, scheduled, predictive, composite

	// Reactive
	ScaleUpAt   *int `yaml:"scale_up_at,omitempty"`   // Percentage
	ScaleDownAt *int `yaml:"scale_down_at,omitempty"` // Percentage

	// Queue
	JobsPerNode *int `yaml:"jobs_per_node,omitempty"`

	// Scheduled
	Schedule []ScheduleWindow  `yaml:"schedule,omitempty"`
	Fallback *AutoscalingCfg   `yaml:"fallback,omitempty"`

	// Predictive
	LookbackWindow *int     `yaml:"lookback_window,omitempty"`
	GrowthFactor   *float64 `yaml:"growth_factor,omitempty"`

	// Composite
	Mode        string            `yaml:"mode,omitempty"` // max, min, avg
	Autoscalers []AutoscalingCfg  `yaml:"autoscalers,omitempty"`
}

// ScheduleWindow defines scaling parameters for a time window.
type ScheduleWindow struct {
	Days     []string `yaml:"days,omitempty"` // monday, tuesday, etc.
	Start    int      `yaml:"start"`          // Hour (0-23)
	End      int      `yaml:"end"`            // Hour (0-23)
	MinNodes int      `yaml:"min_nodes"`
	MaxNodes int      `yaml:"max_nodes"`
}

// HealthCfg configures health checking.
type HealthCfg struct {
	UnhealthyAfter int  `yaml:"unhealthy_after,omitempty"` // Consecutive failures
	AutoReplace    bool `yaml:"auto_replace,omitempty"`
}

// DefaultsCfg holds default values applied to all pools.
type DefaultsCfg struct {
	SSHKeys []string   `yaml:"ssh_keys,omitempty"`
	Health  *HealthCfg `yaml:"health,omitempty"`
}

// Load reads configuration from a YAML file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	cfg.applyDefaults()
	return &cfg, nil
}

// Validate checks the configuration for errors.
func (c *Config) Validate() error {
	if len(c.Pools) == 0 {
		return fmt.Errorf("at least one pool is required")
	}

	for name, pool := range c.Pools {
		if pool.Provider == "" && len(pool.Providers) == 0 {
			return fmt.Errorf("pool %q: must specify provider or providers", name)
		}
		if pool.Provider != "" && len(pool.Providers) > 0 {
			return fmt.Errorf("pool %q: cannot specify both provider and providers", name)
		}
		if pool.InstanceType == "" {
			return fmt.Errorf("pool %q: instance_type is required", name)
		}
		if pool.MaxNodes <= 0 {
			return fmt.Errorf("pool %q: max_nodes must be > 0", name)
		}
		if pool.MinNodes < 0 {
			return fmt.Errorf("pool %q: min_nodes must be >= 0", name)
		}
		if pool.MinNodes > pool.MaxNodes {
			return fmt.Errorf("pool %q: min_nodes cannot exceed max_nodes", name)
		}

		// Validate provider references
		if pool.Provider != "" {
			if _, ok := c.Providers[pool.Provider]; !ok {
				return fmt.Errorf("pool %q: unknown provider %q", name, pool.Provider)
			}
		}
		for _, p := range pool.Providers {
			if _, ok := c.Providers[p.Name]; !ok {
				return fmt.Errorf("pool %q: unknown provider %q", name, p.Name)
			}
		}
	}

	for name, prov := range c.Providers {
		if prov.Type == "" {
			return fmt.Errorf("provider %q: type is required", name)
		}
	}

	return nil
}

func (c *Config) applyDefaults() {
	if c.Server.Address == "" {
		c.Server.Address = ":50051"
	}
	if c.Server.HeartbeatInterval == 0 {
		c.Server.HeartbeatInterval = 30 * time.Second
	}
	if c.Server.HealthCheckInterval == 0 {
		c.Server.HealthCheckInterval = 60 * time.Second
	}
	if c.Server.AutoscaleInterval == 0 {
		c.Server.AutoscaleInterval = 30 * time.Second
	}

	for name, pool := range c.Pools {
		if pool.Cooldown == 0 {
			pool.Cooldown = 5 * time.Minute
		}
		if len(pool.SSHKeys) == 0 && len(c.Defaults.SSHKeys) > 0 {
			pool.SSHKeys = c.Defaults.SSHKeys
		}
		if pool.Health == nil && c.Defaults.Health != nil {
			pool.Health = c.Defaults.Health
		}
		c.Pools[name] = pool
	}
}


