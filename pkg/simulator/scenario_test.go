package simulator

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestDuration_UnmarshalYAML(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected time.Duration
		wantErr  bool
	}{
		{"seconds", "5s", 5 * time.Second, false},
		{"milliseconds", "500ms", 500 * time.Millisecond, false},
		{"minutes", "2m", 2 * time.Minute, false},
		{"complex", "1m30s", 90 * time.Second, false},
		{"zero", "0s", 0, false},
		{"invalid", "invalid", 0, true},
		{"missing_unit", "5", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			yaml := "at: " + tt.input
			var event struct {
				At Duration `yaml:"at"`
			}
			err := unmarshalYAML(yaml, &event)
			if (err != nil) != tt.wantErr {
				t.Errorf("UnmarshalYAML() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && event.At.Duration() != tt.expected {
				t.Errorf("UnmarshalYAML() = %v, want %v", event.At.Duration(), tt.expected)
			}
		})
	}
}

func TestDuration_MarshalYAML(t *testing.T) {
	d := Duration(5 * time.Second)
	result, err := d.MarshalYAML()
	if err != nil {
		t.Fatalf("MarshalYAML() error = %v", err)
	}
	if result != "5s" {
		t.Errorf("MarshalYAML() = %v, want 5s", result)
	}
}

func TestScenario_Validate(t *testing.T) {
	tests := []struct {
		name     string
		scenario Scenario
		wantErr  string
	}{
		{
			name:     "missing name",
			scenario: Scenario{Fleet: []NodeSpec{{ID: "node-1"}}},
			wantErr:  "scenario name is required",
		},
		{
			name:     "empty fleet",
			scenario: Scenario{Name: "test"},
			wantErr:  "fleet must have at least one node",
		},
		{
			name: "missing node ID",
			scenario: Scenario{
				Name:  "test",
				Fleet: []NodeSpec{{Provider: "gcp"}},
			},
			wantErr: "node ID is required",
		},
		{
			name: "duplicate node ID",
			scenario: Scenario{
				Name: "test",
				Fleet: []NodeSpec{
					{ID: "node-1"},
					{ID: "node-1"},
				},
			},
			wantErr: "duplicate node ID: node-1",
		},
		{
			name: "unknown action",
			scenario: Scenario{
				Name:  "test",
				Fleet: []NodeSpec{{ID: "node-1"}},
				Events: []Event{
					{Action: "unknown_action"},
				},
			},
			wantErr: "unknown action",
		},
		{
			name: "valid scenario",
			scenario: Scenario{
				Name:  "test",
				Fleet: []NodeSpec{{ID: "node-1"}},
				Events: []Event{
					{At: Duration(0), Action: "start_fleet"},
					{At: Duration(5 * time.Second), Action: "log", Params: EventParams{LogMessage: "test"}},
				},
			},
			wantErr: "",
		},
		{
			name: "all valid actions",
			scenario: Scenario{
				Name:  "test",
				Fleet: []NodeSpec{{ID: "node-1"}},
				Events: []Event{
					{Action: "start_fleet"},
					{Action: "stop_fleet"},
					{Action: "inject_failure"},
					{Action: "recover_failure"},
					{Action: "issue_command"},
					{Action: "wait_for_status"},
					{Action: "wait"},
					{Action: "log"},
					{Action: "assert"},
				},
			},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.scenario.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("Validate() unexpected error = %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("Validate() expected error containing %q, got nil", tt.wantErr)
				} else if !contains(err.Error(), tt.wantErr) {
					t.Errorf("Validate() error = %v, want error containing %q", err, tt.wantErr)
				}
			}
		})
	}
}

func TestLoadScenario(t *testing.T) {
	t.Run("valid scenario file", func(t *testing.T) {
		content := `
name: test-scenario
description: A test scenario

fleet:
  - id: node-1
    provider: gcp
    region: us-central1
    zone: us-central1-a
    instance_type: a3-highgpu-8g
    gpu_count: 8
    gpu_type: "NVIDIA H100"

events:
  - at: 0s
    action: start_fleet
  - at: 5s
    action: log
    params:
      log_message: "Test message"

assertions:
  - type: node_status
    target: node-1
    expected: active
`
		path := writeTempFile(t, content)
		scenario, err := LoadScenario(path)
		if err != nil {
			t.Fatalf("LoadScenario() error = %v", err)
		}

		if scenario.Name != "test-scenario" {
			t.Errorf("Name = %v, want test-scenario", scenario.Name)
		}
		if len(scenario.Fleet) != 1 {
			t.Errorf("Fleet count = %v, want 1", len(scenario.Fleet))
		}
		if len(scenario.Events) != 2 {
			t.Errorf("Events count = %v, want 2", len(scenario.Events))
		}
		if len(scenario.Assertions) != 1 {
			t.Errorf("Assertions count = %v, want 1", len(scenario.Assertions))
		}
	})

	t.Run("nonexistent file", func(t *testing.T) {
		_, err := LoadScenario("/nonexistent/path.yaml")
		if err == nil {
			t.Error("LoadScenario() expected error for nonexistent file")
		}
	})

	t.Run("invalid yaml", func(t *testing.T) {
		path := writeTempFile(t, "invalid: yaml: content: [")
		_, err := LoadScenario(path)
		if err == nil {
			t.Error("LoadScenario() expected error for invalid YAML")
		}
	})

	t.Run("invalid scenario", func(t *testing.T) {
		content := `
name: ""
fleet: []
`
		path := writeTempFile(t, content)
		_, err := LoadScenario(path)
		if err == nil {
			t.Error("LoadScenario() expected validation error")
		}
	})
}

func TestXIDCodes(t *testing.T) {
	// Verify fatal codes are marked correctly
	fatalCodes := []int{43, 48, 63, 74, 79, 95}
	for _, code := range fatalCodes {
		info, ok := XIDCodes[code]
		if !ok {
			t.Errorf("XIDCodes missing fatal code %d", code)
			continue
		}
		if !info.Fatal {
			t.Errorf("XID %d (%s) should be marked as fatal", code, info.Name)
		}
	}

	// Verify recoverable codes are marked correctly
	recoverableCodes := []int{13, 31, 32, 45, 64, 68, 92, 94}
	for _, code := range recoverableCodes {
		info, ok := XIDCodes[code]
		if !ok {
			t.Errorf("XIDCodes missing recoverable code %d", code)
			continue
		}
		if info.Fatal {
			t.Errorf("XID %d (%s) should not be marked as fatal", code, info.Name)
		}
	}
}

func TestNodeSpec(t *testing.T) {
	content := `
name: test
fleet:
  - id: node-1
    provider: gcp
    region: us-central1
    zone: us-central1-a
    instance_type: a3-highgpu-8g
    gpu_count: 8
    gpu_type: "NVIDIA H100"
    labels:
      env: test
      team: ml
events:
  - at: 0s
    action: start_fleet
`
	path := writeTempFile(t, content)
	scenario, err := LoadScenario(path)
	if err != nil {
		t.Fatalf("LoadScenario() error = %v", err)
	}

	node := scenario.Fleet[0]
	if node.ID != "node-1" {
		t.Errorf("ID = %v, want node-1", node.ID)
	}
	if node.Provider != "gcp" {
		t.Errorf("Provider = %v, want gcp", node.Provider)
	}
	if node.Region != "us-central1" {
		t.Errorf("Region = %v, want us-central1", node.Region)
	}
	if node.Zone != "us-central1-a" {
		t.Errorf("Zone = %v, want us-central1-a", node.Zone)
	}
	if node.InstanceType != "a3-highgpu-8g" {
		t.Errorf("InstanceType = %v, want a3-highgpu-8g", node.InstanceType)
	}
	if node.GPUCount != 8 {
		t.Errorf("GPUCount = %v, want 8", node.GPUCount)
	}
	if node.GPUType != "NVIDIA H100" {
		t.Errorf("GPUType = %v, want NVIDIA H100", node.GPUType)
	}
	if len(node.Labels) != 2 {
		t.Errorf("Labels count = %v, want 2", len(node.Labels))
	}
	if node.Labels["env"] != "test" {
		t.Errorf("Labels[env] = %v, want test", node.Labels["env"])
	}
}

func TestEventParams(t *testing.T) {
	content := `
name: test
fleet:
  - id: node-1
events:
  - at: 0s
    action: start_fleet
  - at: 5s
    action: inject_failure
    target: node-1
    params:
      failure_type: xid_error
      xid_code: 79
      gpu_index: 3
      message: "GPU failure"
  - at: 10s
    action: issue_command
    target: node-1
    params:
      command_type: cordon
      command_args:
        reason: "maintenance"
  - at: 15s
    action: wait_for_status
    target: node-1
    params:
      expected_status: unhealthy
      timeout: 30s
`
	path := writeTempFile(t, content)
	scenario, err := LoadScenario(path)
	if err != nil {
		t.Fatalf("LoadScenario() error = %v", err)
	}

	// Check inject_failure event
	failureEvent := scenario.Events[1]
	if failureEvent.Params.FailureType != "xid_error" {
		t.Errorf("FailureType = %v, want xid_error", failureEvent.Params.FailureType)
	}
	if failureEvent.Params.XIDCode != 79 {
		t.Errorf("XIDCode = %v, want 79", failureEvent.Params.XIDCode)
	}
	if failureEvent.Params.GPUIndex != 3 {
		t.Errorf("GPUIndex = %v, want 3", failureEvent.Params.GPUIndex)
	}

	// Check issue_command event
	cmdEvent := scenario.Events[2]
	if cmdEvent.Params.CommandType != "cordon" {
		t.Errorf("CommandType = %v, want cordon", cmdEvent.Params.CommandType)
	}
	if cmdEvent.Params.CommandArgs["reason"] != "maintenance" {
		t.Errorf("CommandArgs[reason] = %v, want maintenance", cmdEvent.Params.CommandArgs["reason"])
	}

	// Check wait_for_status event
	waitEvent := scenario.Events[3]
	if waitEvent.Params.ExpectedStatus != "unhealthy" {
		t.Errorf("ExpectedStatus = %v, want unhealthy", waitEvent.Params.ExpectedStatus)
	}
	if waitEvent.Params.Timeout.Duration() != 30*time.Second {
		t.Errorf("Timeout = %v, want 30s", waitEvent.Params.Timeout.Duration())
	}
}

// Helper functions

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "scenario.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	return path
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr, 0))
}

func containsAt(s, substr string, start int) bool {
	for i := start; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func unmarshalYAML(content string, v interface{}) error {
	return yaml.Unmarshal([]byte(content), v)
}

