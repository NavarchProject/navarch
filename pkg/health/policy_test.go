package health

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParsePolicy(t *testing.T) {
	yaml := `
rules:
  - name: fatal-xid
    condition: 'event.event_type == "xid" && event.metrics.xid_code == 79'
    result: unhealthy
    priority: 100
  - name: thermal-warning
    condition: 'event.metrics.temperature > 85'
    result: degraded
    priority: 50
  - name: default
    condition: 'true'
    result: healthy
    priority: 0
`
	policy, err := ParsePolicy([]byte(yaml))
	if err != nil {
		t.Fatalf("ParsePolicy() error = %v", err)
	}

	if len(policy.Rules) != 3 {
		t.Errorf("len(Rules) = %d, want 3", len(policy.Rules))
	}

	if policy.Rules[0].Name != "fatal-xid" {
		t.Errorf("Rules[0].Name = %q, want 'fatal-xid'", policy.Rules[0].Name)
	}
	if policy.Rules[0].Result != ResultUnhealthy {
		t.Errorf("Rules[0].Result = %q, want 'unhealthy'", policy.Rules[0].Result)
	}
	if policy.Rules[0].Priority != 100 {
		t.Errorf("Rules[0].Priority = %d, want 100", policy.Rules[0].Priority)
	}
}

func TestParsePolicy_Invalid(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{
			name: "empty rules",
			yaml: `rules: []`,
		},
		{
			name: "missing name",
			yaml: `
rules:
  - condition: 'true'
    result: healthy
`,
		},
		{
			name: "missing condition",
			yaml: `
rules:
  - name: test
    result: healthy
`,
		},
		{
			name: "invalid result",
			yaml: `
rules:
  - name: test
    condition: 'true'
    result: invalid
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParsePolicy([]byte(tt.yaml))
			if err == nil {
				t.Error("ParsePolicy() should fail")
			}
		})
	}
}

func TestLoadPolicy(t *testing.T) {
	yaml := `
rules:
  - name: test-rule
    condition: 'event.event_type == "xid"'
    result: unhealthy
    priority: 100
`
	// Create temp file
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	policy, err := LoadPolicy(path)
	if err != nil {
		t.Fatalf("LoadPolicy() error = %v", err)
	}

	if len(policy.Rules) != 1 {
		t.Errorf("len(Rules) = %d, want 1", len(policy.Rules))
	}
	if policy.Rules[0].Name != "test-rule" {
		t.Errorf("Rules[0].Name = %q, want 'test-rule'", policy.Rules[0].Name)
	}
}

func TestLoadPolicy_FileNotFound(t *testing.T) {
	_, err := LoadPolicy("/nonexistent/path/policy.yaml")
	if err == nil {
		t.Error("LoadPolicy() should fail for nonexistent file")
	}
}

func TestPolicy_SortedRules(t *testing.T) {
	policy := &Policy{
		Rules: []Rule{
			{Name: "low", Priority: 10},
			{Name: "high", Priority: 100},
			{Name: "medium", Priority: 50},
			{Name: "also-high", Priority: 100},
		},
	}

	sorted := policy.SortedRules()

	// Should be sorted by priority descending
	expected := []string{"high", "also-high", "medium", "low"}
	for i, name := range expected {
		if sorted[i].Name != name {
			t.Errorf("sorted[%d].Name = %q, want %q", i, sorted[i].Name, name)
		}
	}

	// Original should be unchanged
	if policy.Rules[0].Name != "low" {
		t.Error("Original rules should not be modified")
	}
}

func TestPolicy_SortedRules_StableSort(t *testing.T) {
	// Rules with same priority should maintain original order
	policy := &Policy{
		Rules: []Rule{
			{Name: "first", Priority: 100},
			{Name: "second", Priority: 100},
			{Name: "third", Priority: 100},
		},
	}

	sorted := policy.SortedRules()

	if sorted[0].Name != "first" || sorted[1].Name != "second" || sorted[2].Name != "third" {
		t.Errorf("Same-priority rules should maintain order: got %s, %s, %s",
			sorted[0].Name, sorted[1].Name, sorted[2].Name)
	}
}

func TestPolicy_Validate(t *testing.T) {
	tests := []struct {
		name    string
		policy  Policy
		wantErr bool
	}{
		{
			name: "valid policy",
			policy: Policy{
				Rules: []Rule{
					{Name: "test", Condition: "true", Result: ResultHealthy},
				},
			},
			wantErr: false,
		},
		{
			name: "all result types valid",
			policy: Policy{
				Rules: []Rule{
					{Name: "healthy", Condition: "true", Result: ResultHealthy},
					{Name: "degraded", Condition: "true", Result: ResultDegraded},
					{Name: "unhealthy", Condition: "true", Result: ResultUnhealthy},
				},
			},
			wantErr: false,
		},
		{
			name:    "empty rules",
			policy:  Policy{Rules: []Rule{}},
			wantErr: true,
		},
		{
			name: "missing name",
			policy: Policy{
				Rules: []Rule{
					{Condition: "true", Result: ResultHealthy},
				},
			},
			wantErr: true,
		},
		{
			name: "missing condition",
			policy: Policy{
				Rules: []Rule{
					{Name: "test", Result: ResultHealthy},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid result",
			policy: Policy{
				Rules: []Rule{
					{Name: "test", Condition: "true", Result: "invalid"},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.policy.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDefaultPolicy(t *testing.T) {
	policy := DefaultPolicy()

	if err := policy.Validate(); err != nil {
		t.Errorf("DefaultPolicy() is invalid: %v", err)
	}

	// Should have a default-healthy rule
	hasDefault := false
	for _, rule := range policy.Rules {
		if rule.Name == "default-healthy" {
			hasDefault = true
			if rule.Condition != "true" {
				t.Error("default-healthy rule should have 'true' condition")
			}
			if rule.Result != ResultHealthy {
				t.Error("default-healthy rule should return healthy")
			}
			if rule.Priority != 0 {
				t.Errorf("default-healthy priority = %d, want 0", rule.Priority)
			}
		}
	}
	if !hasDefault {
		t.Error("DefaultPolicy should have a default-healthy rule")
	}

	// Should have fatal-xid rule
	hasFatalXID := false
	for _, rule := range policy.Rules {
		if rule.Name == "fatal-xid" {
			hasFatalXID = true
			if rule.Result != ResultUnhealthy {
				t.Error("fatal-xid rule should return unhealthy")
			}
			if rule.Priority != 100 {
				t.Errorf("fatal-xid priority = %d, want 100", rule.Priority)
			}
		}
	}
	if !hasFatalXID {
		t.Error("DefaultPolicy should have a fatal-xid rule")
	}
}

func TestFatalXIDCodes(t *testing.T) {
	// Verify some well-known fatal XIDs are in the list
	known := map[int]bool{
		79:  true, // GPU has fallen off the bus
		48:  true, // Double Bit ECC Error
		119: true, // GSP RPC timeout
		74:  true, // NVLINK Error
	}

	for code := range known {
		found := false
		for _, c := range FatalXIDCodes {
			if c == code {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("FatalXIDCodes should contain %d", code)
		}
	}
}
