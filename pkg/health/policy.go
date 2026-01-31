package health

import (
	_ "embed"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

//go:embed default_policy.yaml
var defaultPolicyYAML []byte

// Result represents the outcome of a health policy evaluation.
type Result string

const (
	ResultHealthy   Result = "healthy"
	ResultDegraded  Result = "degraded"
	ResultUnhealthy Result = "unhealthy"
)

// PolicyFile represents the on-disk format for a health policy configuration.
type PolicyFile struct {
	// Version of the policy file format.
	Version string `yaml:"version"`

	// Metadata contains optional descriptive information.
	Metadata PolicyMetadata `yaml:"metadata,omitempty"`

	// Rules define health classification logic. Rules are evaluated in order;
	// the first matching rule determines the result.
	Rules []Rule `yaml:"rules"`
}

// PolicyMetadata contains optional policy metadata.
type PolicyMetadata struct {
	Name        string `yaml:"name,omitempty"`
	Description string `yaml:"description,omitempty"`
}

// Policy defines a set of rules for evaluating GPU health events.
type Policy struct {
	// Rules are evaluated in definition order (first match wins).
	Rules []Rule `yaml:"rules"`
}

// Rule defines a single health evaluation rule.
type Rule struct {
	// Name identifies the rule for logging and debugging.
	Name string `yaml:"name"`

	// Description provides human-readable context for the rule.
	Description string `yaml:"description,omitempty"`

	// Condition is a CEL expression evaluated against each health event.
	// The expression has access to an 'event' variable with fields:
	//   - event.event_type (string): xid, thermal, memory, nvlink, etc.
	//   - event.system (string): DCGM health watch system
	//   - event.gpu_index (int): GPU index (-1 for node-level)
	//   - event.metrics (map): event-specific metrics
	//   - event.message (string): human-readable message
	Condition string `yaml:"condition"`

	// Result is the health status when this rule matches.
	Result Result `yaml:"result"`
}

// LoadPolicy loads a health policy from a YAML file.
func LoadPolicy(path string) (*Policy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read policy file: %w", err)
	}

	return ParsePolicy(data)
}

// LoadDefaultPolicy returns the built-in default health policy.
func LoadDefaultPolicy() (*Policy, error) {
	return ParsePolicy(defaultPolicyYAML)
}

// ParsePolicy parses a health policy from YAML data.
func ParsePolicy(data []byte) (*Policy, error) {
	var pf PolicyFile
	if err := yaml.Unmarshal(data, &pf); err != nil {
		return nil, fmt.Errorf("parse policy YAML: %w", err)
	}

	policy := &Policy{Rules: pf.Rules}

	if err := policy.Validate(); err != nil {
		return nil, fmt.Errorf("validate policy: %w", err)
	}

	return policy, nil
}

// Validate checks that the policy is well-formed.
func (p *Policy) Validate() error {
	if len(p.Rules) == 0 {
		return fmt.Errorf("policy must have at least one rule")
	}

	seen := make(map[string]bool)
	for i, rule := range p.Rules {
		if rule.Name == "" {
			return fmt.Errorf("rule %d: name is required", i)
		}
		if seen[rule.Name] {
			return fmt.Errorf("rule %q: duplicate rule name", rule.Name)
		}
		seen[rule.Name] = true
		if rule.Condition == "" {
			return fmt.Errorf("rule %q: condition is required", rule.Name)
		}
		switch rule.Result {
		case ResultHealthy, ResultDegraded, ResultUnhealthy:
			// valid
		default:
			return fmt.Errorf("rule %q: invalid result %q (must be healthy, degraded, or unhealthy)", rule.Name, rule.Result)
		}
	}

	return nil
}

// SortedRules returns rules in evaluation order (definition order).
// The first matching rule wins.
func (p *Policy) SortedRules() []Rule {
	return p.Rules
}
