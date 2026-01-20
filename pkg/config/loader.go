package config

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds all loaded configuration resources.
type Config struct {
	ControlPlane *ControlPlane
	Pools        []*Pool
	Providers    map[string]*Provider
}

// Load reads configuration from a file path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}
	return Parse(data)
}

// Parse parses configuration from YAML bytes.
// Supports multi-document YAML (separated by ---).
func Parse(data []byte) (*Config, error) {
	cfg := &Config{
		Providers: make(map[string]*Provider),
	}

	decoder := yaml.NewDecoder(bytes.NewReader(data))

	for {
		var raw map[string]any
		if err := decoder.Decode(&raw); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("failed to decode YAML document: %w", err)
		}

		if raw == nil {
			continue
		}

		kind, _ := raw["kind"].(string)
		apiVersion, _ := raw["apiVersion"].(string)

		if apiVersion != "" && apiVersion != APIVersion {
			return nil, fmt.Errorf("unsupported apiVersion: %s (expected %s)", apiVersion, APIVersion)
		}

		docBytes, err := yaml.Marshal(raw)
		if err != nil {
			return nil, fmt.Errorf("failed to re-marshal document: %w", err)
		}

		switch kind {
		case KindControlPlane:
			var cp ControlPlane
			if err := yaml.Unmarshal(docBytes, &cp); err != nil {
				return nil, fmt.Errorf("failed to parse ControlPlane: %w", err)
			}
			if cfg.ControlPlane != nil {
				return nil, fmt.Errorf("multiple ControlPlane resources found")
			}
			cfg.ControlPlane = &cp

		case KindPool:
			var pool Pool
			if err := yaml.Unmarshal(docBytes, &pool); err != nil {
				return nil, fmt.Errorf("failed to parse Pool %q: %w", pool.Metadata.Name, err)
			}
			cfg.Pools = append(cfg.Pools, &pool)

		case KindProvider:
			var provider Provider
			if err := yaml.Unmarshal(docBytes, &provider); err != nil {
				return nil, fmt.Errorf("failed to parse Provider: %w", err)
			}
			name := provider.Metadata.Name
			if name == "" {
				return nil, fmt.Errorf("Provider must have metadata.name")
			}
			if _, exists := cfg.Providers[name]; exists {
				return nil, fmt.Errorf("duplicate Provider name: %s", name)
			}
			cfg.Providers[name] = &provider

		case "":
			return nil, fmt.Errorf("document missing 'kind' field")

		default:
			return nil, fmt.Errorf("unknown kind: %s", kind)
		}
	}

	return cfg, nil
}

// Validate checks the configuration for errors.
func (c *Config) Validate() error {
	for _, pool := range c.Pools {
		if pool.Metadata.Name == "" {
			return fmt.Errorf("Pool must have metadata.name")
		}

		hasProviderRef := pool.Spec.ProviderRef != ""
		hasProviders := len(pool.Spec.Providers) > 0

		if !hasProviderRef && !hasProviders {
			return fmt.Errorf("Pool %q must have either spec.providerRef or spec.providers", pool.Metadata.Name)
		}
		if hasProviderRef && hasProviders {
			return fmt.Errorf("Pool %q cannot have both spec.providerRef and spec.providers", pool.Metadata.Name)
		}

		if hasProviderRef {
			if _, ok := c.Providers[pool.Spec.ProviderRef]; !ok {
				return fmt.Errorf("Pool %q references unknown provider %q", pool.Metadata.Name, pool.Spec.ProviderRef)
			}
		}

		for _, pref := range pool.Spec.Providers {
			if pref.Name == "" {
				return fmt.Errorf("Pool %q has provider reference with empty name", pool.Metadata.Name)
			}
			if _, ok := c.Providers[pref.Name]; !ok {
				return fmt.Errorf("Pool %q references unknown provider %q", pool.Metadata.Name, pref.Name)
			}
		}

		validStrategies := map[string]bool{"": true, "priority": true, "cost": true, "availability": true, "round-robin": true}
		if !validStrategies[pool.Spec.ProviderStrategy] {
			return fmt.Errorf("Pool %q has invalid providerStrategy %q", pool.Metadata.Name, pool.Spec.ProviderStrategy)
		}

		if pool.Spec.Scaling.MinReplicas < 0 {
			return fmt.Errorf("Pool %q: minReplicas must be >= 0", pool.Metadata.Name)
		}
		if pool.Spec.Scaling.MaxReplicas < pool.Spec.Scaling.MinReplicas {
			return fmt.Errorf("Pool %q: maxReplicas must be >= minReplicas", pool.Metadata.Name)
		}
	}

	for name, provider := range c.Providers {
		if provider.Spec.Type == "" {
			return fmt.Errorf("Provider %q must have spec.type", name)
		}
	}

	return nil
}

// GetPool returns a pool by name.
func (c *Config) GetPool(name string) *Pool {
	for _, p := range c.Pools {
		if p.Metadata.Name == name {
			return p
		}
	}
	return nil
}

// GetProvider returns a provider by name.
func (c *Config) GetProvider(name string) *Provider {
	return c.Providers[name]
}

// UnmarshalYAML implements custom YAML unmarshaling for Duration.
func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	var s string
	if err := node.Decode(&s); err != nil {
		return err
	}
	if s == "" {
		*d = 0
		return nil
	}
	dur, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(dur)
	return nil
}

// MarshalYAML implements custom YAML marshaling for Duration.
func (d Duration) MarshalYAML() (interface{}, error) {
	if d == 0 {
		return "", nil
	}
	return time.Duration(d).String(), nil
}

// Defaults applies default values to the configuration.
func (c *Config) Defaults() {
	if c.ControlPlane != nil {
		spec := &c.ControlPlane.Spec
		if spec.Address == "" {
			spec.Address = ":50051"
		}
		if spec.HealthCheckInterval == 0 {
			spec.HealthCheckInterval = Duration(60 * time.Second)
		}
		if spec.HeartbeatInterval == 0 {
			spec.HeartbeatInterval = Duration(30 * time.Second)
		}
		if len(spec.EnabledHealthChecks) == 0 {
			spec.EnabledHealthChecks = []string{"boot", "nvml", "xid"}
		}
		if spec.AutoscaleInterval == 0 {
			spec.AutoscaleInterval = Duration(30 * time.Second)
		}
	}

	for _, pool := range c.Pools {
		if pool.Spec.Health.UnhealthyThreshold == 0 {
			pool.Spec.Health.UnhealthyThreshold = 3
		}
	}

	for _, provider := range c.Providers {
		if provider.Spec.Fake != nil && provider.Spec.Fake.GPUCount == 0 {
			provider.Spec.Fake.GPUCount = 8
		}
	}
}

