package health

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Result represents the outcome of a health policy evaluation.
type Result string

const (
	ResultHealthy   Result = "healthy"
	ResultDegraded  Result = "degraded"
	ResultUnhealthy Result = "unhealthy"
)

// Policy defines a set of rules for evaluating GPU health events.
type Policy struct {
	// Rules are evaluated in priority order (highest first).
	// The first matching rule determines the result.
	Rules []Rule `yaml:"rules"`
}

// Rule defines a single health evaluation rule.
type Rule struct {
	// Name identifies the rule for logging and debugging.
	Name string `yaml:"name"`

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

	// Priority determines evaluation order. Higher priority rules are
	// evaluated first. Rules with the same priority are evaluated in
	// definition order.
	Priority int `yaml:"priority"`
}

// LoadPolicy loads a health policy from a YAML file.
func LoadPolicy(path string) (*Policy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read policy file: %w", err)
	}

	return ParsePolicy(data)
}

// ParsePolicy parses a health policy from YAML data.
func ParsePolicy(data []byte) (*Policy, error) {
	var policy Policy
	if err := yaml.Unmarshal(data, &policy); err != nil {
		return nil, fmt.Errorf("parse policy YAML: %w", err)
	}

	if err := policy.Validate(); err != nil {
		return nil, fmt.Errorf("validate policy: %w", err)
	}

	return &policy, nil
}

// Validate checks that the policy is well-formed.
func (p *Policy) Validate() error {
	if len(p.Rules) == 0 {
		return fmt.Errorf("policy must have at least one rule")
	}

	for i, rule := range p.Rules {
		if rule.Name == "" {
			return fmt.Errorf("rule %d: name is required", i)
		}
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

// SortedRules returns the rules sorted by priority (highest first),
// with stable ordering for rules with the same priority.
func (p *Policy) SortedRules() []Rule {
	// Copy rules to avoid mutating the original
	sorted := make([]Rule, len(p.Rules))
	copy(sorted, p.Rules)

	// Stable sort by priority (descending)
	// Using insertion sort for stability and simplicity (small n)
	for i := 1; i < len(sorted); i++ {
		j := i
		for j > 0 && sorted[j].Priority > sorted[j-1].Priority {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
			j--
		}
	}

	return sorted
}
