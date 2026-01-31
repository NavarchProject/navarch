package health

import (
	"context"
	"testing"

	"github.com/NavarchProject/navarch/pkg/gpu"
	pb "github.com/NavarchProject/navarch/proto"
)

// TestCELPolicyE2E tests the full CEL policy evaluation pipeline with various
// policy configurations and event types.
func TestCELPolicyE2E(t *testing.T) {
	ctx := context.Background()

	// Test different policy configurations
	testCases := []struct {
		name           string
		policyYAML     string
		events         []gpu.HealthEvent
		expectedStatus Result
		expectedRule   string
	}{
		{
			name: "default_policy_fatal_xid_79",
			policyYAML: `
rules:
  - name: fatal-xid
    condition: 'event.event_type == "xid" && event.metrics.xid_code in [79, 48, 63]'
    result: unhealthy
    priority: 100
  - name: default
    condition: "true"
    result: healthy
    priority: 0
`,
			events: []gpu.HealthEvent{
				gpu.NewXIDEvent(0, "GPU-12345", 79, "GPU fallen off bus"),
			},
			expectedStatus: ResultUnhealthy,
			expectedRule:   "fatal-xid",
		},
		{
			name: "default_policy_recoverable_xid",
			policyYAML: `
rules:
  - name: fatal-xid
    condition: 'event.event_type == "xid" && event.metrics.xid_code in [79, 48, 63]'
    result: unhealthy
    priority: 100
  - name: recoverable-xid
    condition: 'event.event_type == "xid"'
    result: degraded
    priority: 50
  - name: default
    condition: "true"
    result: healthy
    priority: 0
`,
			events: []gpu.HealthEvent{
				gpu.NewXIDEvent(0, "GPU-12345", 42, "Unknown XID"),
			},
			expectedStatus: ResultDegraded,
			expectedRule:   "recoverable-xid",
		},
		{
			name: "strict_policy_any_xid_unhealthy",
			policyYAML: `
rules:
  - name: any-xid-fatal
    condition: 'event.event_type == "xid"'
    result: unhealthy
    priority: 100
  - name: default
    condition: "true"
    result: healthy
    priority: 0
`,
			events: []gpu.HealthEvent{
				gpu.NewXIDEvent(0, "GPU-12345", 13, "Graphics engine exception"),
			},
			expectedStatus: ResultUnhealthy,
			expectedRule:   "any-xid-fatal",
		},
		{
			name: "permissive_policy_ignore_xid",
			policyYAML: `
rules:
  - name: ignore-xid
    condition: 'event.event_type == "xid"'
    result: healthy
    priority: 100
  - name: default
    condition: "true"
    result: healthy
    priority: 0
`,
			events: []gpu.HealthEvent{
				gpu.NewXIDEvent(0, "GPU-12345", 79, "GPU fallen off bus"),
			},
			expectedStatus: ResultHealthy,
			expectedRule:   "", // Healthy results don't update MatchedRule (stays empty)
		},
		{
			name: "thermal_critical_threshold",
			policyYAML: `
rules:
  - name: thermal-critical
    condition: 'event.event_type == "thermal" && event.metrics.temperature >= 95'
    result: unhealthy
    priority: 100
  - name: thermal-warning
    condition: 'event.event_type == "thermal" && event.metrics.temperature >= 85'
    result: degraded
    priority: 50
  - name: default
    condition: "true"
    result: healthy
    priority: 0
`,
			events: []gpu.HealthEvent{
				gpu.NewThermalEvent(0, "GPU-12345", 96, "Temperature critical"),
			},
			expectedStatus: ResultUnhealthy,
			expectedRule:   "thermal-critical",
		},
		{
			name: "thermal_warning_threshold",
			policyYAML: `
rules:
  - name: thermal-critical
    condition: 'event.event_type == "thermal" && event.metrics.temperature >= 95'
    result: unhealthy
    priority: 100
  - name: thermal-warning
    condition: 'event.event_type == "thermal" && event.metrics.temperature >= 85'
    result: degraded
    priority: 50
  - name: default
    condition: "true"
    result: healthy
    priority: 0
`,
			events: []gpu.HealthEvent{
				gpu.NewThermalEvent(0, "GPU-12345", 88, "Temperature elevated"),
			},
			expectedStatus: ResultDegraded,
			expectedRule:   "thermal-warning",
		},
		{
			name: "thermal_normal_temperature",
			policyYAML: `
rules:
  - name: thermal-critical
    condition: 'event.event_type == "thermal" && event.metrics.temperature >= 95'
    result: unhealthy
    priority: 100
  - name: thermal-warning
    condition: 'event.event_type == "thermal" && event.metrics.temperature >= 85'
    result: degraded
    priority: 50
  - name: default
    condition: "true"
    result: healthy
    priority: 0
`,
			events: []gpu.HealthEvent{
				gpu.NewThermalEvent(0, "GPU-12345", 65, "Normal temperature"),
			},
			expectedStatus: ResultHealthy,
			expectedRule:   "", // Healthy results don't update MatchedRule
		},
		{
			name: "ecc_dbe_unhealthy",
			policyYAML: `
rules:
  - name: ecc-dbe
    condition: 'event.event_type == "ecc_dbe"'
    result: unhealthy
    priority: 100
  - name: ecc-sbe
    condition: 'event.event_type == "ecc_sbe"'
    result: degraded
    priority: 50
  - name: default
    condition: "true"
    result: healthy
    priority: 0
`,
			events: []gpu.HealthEvent{
				gpu.NewMemoryEvent(0, "GPU-12345", pb.HealthEventType_HEALTH_EVENT_TYPE_ECC_DBE, 0, 1, "Double-bit ECC error"),
			},
			expectedStatus: ResultUnhealthy,
			expectedRule:   "ecc-dbe",
		},
		{
			name: "nvlink_error_degraded",
			policyYAML: `
rules:
  - name: nvlink-error
    condition: 'event.event_type == "nvlink"'
    result: degraded
    priority: 50
  - name: default
    condition: "true"
    result: healthy
    priority: 0
`,
			events: []gpu.HealthEvent{
				gpu.NewNVLinkEvent(0, "GPU-12345", 2, "NVLink CRC error"),
			},
			expectedStatus: ResultDegraded,
			expectedRule:   "nvlink-error",
		},
		{
			name: "multiple_events_worst_wins",
			policyYAML: `
rules:
  - name: fatal-xid
    condition: 'event.event_type == "xid" && event.metrics.xid_code == 79'
    result: unhealthy
    priority: 100
  - name: thermal-warning
    condition: 'event.event_type == "thermal"'
    result: degraded
    priority: 50
  - name: default
    condition: "true"
    result: healthy
    priority: 0
`,
			events: []gpu.HealthEvent{
				gpu.NewThermalEvent(0, "GPU-12345", 88, "Temperature elevated"),
				gpu.NewXIDEvent(1, "GPU-67890", 79, "GPU fallen off bus"),
			},
			expectedStatus: ResultUnhealthy,
			expectedRule:   "fatal-xid",
		},
		{
			name: "no_events_healthy",
			policyYAML: `
rules:
  - name: fatal-xid
    condition: 'event.event_type == "xid"'
    result: unhealthy
    priority: 100
  - name: default
    condition: "true"
    result: healthy
    priority: 0
`,
			events:         []gpu.HealthEvent{},
			expectedStatus: ResultHealthy,
			expectedRule:   "",
		},
		{
			name: "custom_xid_thresholds",
			policyYAML: `
rules:
  - name: critical-xid-codes
    condition: 'event.event_type == "xid" && event.metrics.xid_code in [48, 79, 95]'
    result: unhealthy
    priority: 100
  - name: warning-xid-codes
    condition: 'event.event_type == "xid" && event.metrics.xid_code in [13, 31, 43]'
    result: degraded
    priority: 90
  - name: info-xid-codes
    condition: 'event.event_type == "xid"'
    result: healthy
    priority: 80
  - name: default
    condition: "true"
    result: healthy
    priority: 0
`,
			events: []gpu.HealthEvent{
				gpu.NewXIDEvent(0, "GPU-12345", 31, "GPU memory page fault"),
			},
			expectedStatus: ResultDegraded,
			expectedRule:   "warning-xid-codes",
		},
		{
			name: "gpu_specific_rules",
			policyYAML: `
rules:
  - name: gpu0-strict
    condition: 'event.gpu_index == 0 && event.event_type == "xid"'
    result: unhealthy
    priority: 100
  - name: other-gpu-lenient
    condition: 'event.event_type == "xid"'
    result: degraded
    priority: 50
  - name: default
    condition: "true"
    result: healthy
    priority: 0
`,
			events: []gpu.HealthEvent{
				gpu.NewXIDEvent(0, "GPU-12345", 31, "Error on GPU 0"),
			},
			expectedStatus: ResultUnhealthy,
			expectedRule:   "gpu0-strict",
		},
		{
			name: "gpu_specific_rules_other_gpu",
			policyYAML: `
rules:
  - name: gpu0-strict
    condition: 'event.gpu_index == 0 && event.event_type == "xid"'
    result: unhealthy
    priority: 100
  - name: other-gpu-lenient
    condition: 'event.event_type == "xid"'
    result: degraded
    priority: 50
  - name: default
    condition: "true"
    result: healthy
    priority: 0
`,
			events: []gpu.HealthEvent{
				gpu.NewXIDEvent(3, "GPU-ABCDE", 31, "Error on GPU 3"),
			},
			expectedStatus: ResultDegraded,
			expectedRule:   "other-gpu-lenient",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Parse the policy
			policy, err := ParsePolicy([]byte(tc.policyYAML))
			if err != nil {
				t.Fatalf("Failed to parse policy: %v", err)
			}

			// Create evaluator
			evaluator, err := NewEvaluator(policy)
			if err != nil {
				t.Fatalf("Failed to create evaluator: %v", err)
			}

			// Evaluate events
			result, err := evaluator.Evaluate(ctx, tc.events)
			if err != nil {
				t.Fatalf("Evaluation failed: %v", err)
			}

			// Check results
			if result.Status != tc.expectedStatus {
				t.Errorf("Status = %s, want %s", result.Status, tc.expectedStatus)
			}
			if result.MatchedRule != tc.expectedRule {
				t.Errorf("MatchedRule = %q, want %q", result.MatchedRule, tc.expectedRule)
			}
		})
	}
}

// TestDefaultPolicyComprehensive verifies the default policy handles all
// expected scenarios correctly.
func TestDefaultPolicyComprehensive(t *testing.T) {
	ctx := context.Background()

	policy := DefaultPolicy()
	evaluator, err := NewEvaluator(policy)
	if err != nil {
		t.Fatalf("Failed to create evaluator: %v", err)
	}

	testCases := []struct {
		name           string
		event          gpu.HealthEvent
		expectedStatus Result
	}{
		// Fatal XID codes
		{"xid_79_gpu_fallen_off_bus", gpu.NewXIDEvent(0, "GPU-1", 79, ""), ResultUnhealthy},
		{"xid_48_double_bit_ecc", gpu.NewXIDEvent(0, "GPU-1", 48, ""), ResultUnhealthy},
		{"xid_63_ecc_page_retirement", gpu.NewXIDEvent(0, "GPU-1", 63, ""), ResultUnhealthy},
		{"xid_74_nvlink_error", gpu.NewXIDEvent(0, "GPU-1", 74, ""), ResultUnhealthy},
		{"xid_95_uncontained_ecc", gpu.NewXIDEvent(0, "GPU-1", 95, ""), ResultUnhealthy},
		{"xid_13_graphics_exception", gpu.NewXIDEvent(0, "GPU-1", 13, ""), ResultUnhealthy},
		{"xid_31_memory_page_fault", gpu.NewXIDEvent(0, "GPU-1", 31, ""), ResultUnhealthy},
		{"xid_43_gpu_stopped", gpu.NewXIDEvent(0, "GPU-1", 43, ""), ResultUnhealthy},

		// Recoverable XID codes (not in fatal list)
		{"xid_8_unknown", gpu.NewXIDEvent(0, "GPU-1", 8, ""), ResultDegraded},
		{"xid_999_unknown", gpu.NewXIDEvent(0, "GPU-1", 999, ""), ResultDegraded},

		// Thermal events
		{"thermal_critical_95", gpu.NewThermalEvent(0, "GPU-1", 95, ""), ResultUnhealthy},
		{"thermal_critical_100", gpu.NewThermalEvent(0, "GPU-1", 100, ""), ResultUnhealthy},
		{"thermal_warning_85", gpu.NewThermalEvent(0, "GPU-1", 85, ""), ResultDegraded},
		{"thermal_warning_90", gpu.NewThermalEvent(0, "GPU-1", 90, ""), ResultDegraded},
		{"thermal_normal_70", gpu.NewThermalEvent(0, "GPU-1", 70, ""), ResultHealthy},
		{"thermal_normal_84", gpu.NewThermalEvent(0, "GPU-1", 84, ""), ResultHealthy},

		// ECC errors
		{"ecc_dbe", gpu.NewMemoryEvent(0, "GPU-1", pb.HealthEventType_HEALTH_EVENT_TYPE_ECC_DBE, 0, 1, ""), ResultUnhealthy},

		// NVLink errors
		{"nvlink_error", gpu.NewNVLinkEvent(0, "GPU-1", 0, ""), ResultDegraded},

		// Power events
		{"power_event", gpu.NewPowerEvent(0, "GPU-1", 500, "Power spike"), ResultDegraded},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := evaluator.Evaluate(ctx, []gpu.HealthEvent{tc.event})
			if err != nil {
				t.Fatalf("Evaluation failed: %v", err)
			}
			if result.Status != tc.expectedStatus {
				t.Errorf("Status = %s, want %s (rule: %s)", result.Status, tc.expectedStatus, result.MatchedRule)
			}
		})
	}
}

// TestPolicyHotReload verifies that policies can be updated at runtime.
func TestPolicyHotReload(t *testing.T) {
	ctx := context.Background()

	// Start with a permissive policy
	permissivePolicy := `
rules:
  - name: allow-all
    condition: "true"
    result: healthy
    priority: 0
`
	policy, _ := ParsePolicy([]byte(permissivePolicy))
	evaluator, _ := NewEvaluator(policy)

	// XID 79 should be healthy with permissive policy
	xidEvent := gpu.NewXIDEvent(0, "GPU-1", 79, "GPU fallen off bus")
	result, _ := evaluator.Evaluate(ctx, []gpu.HealthEvent{xidEvent})
	if result.Status != ResultHealthy {
		t.Errorf("Before reload: Status = %s, want healthy", result.Status)
	}

	// Hot-reload to a strict policy
	strictPolicy := `
rules:
  - name: fatal-xid
    condition: 'event.event_type == "xid"'
    result: unhealthy
    priority: 100
  - name: default
    condition: "true"
    result: healthy
    priority: 0
`
	newPolicy, _ := ParsePolicy([]byte(strictPolicy))
	if err := evaluator.UpdatePolicy(newPolicy); err != nil {
		t.Fatalf("Failed to update policy: %v", err)
	}

	// Same event should now be unhealthy
	result, _ = evaluator.Evaluate(ctx, []gpu.HealthEvent{xidEvent})
	if result.Status != ResultUnhealthy {
		t.Errorf("After reload: Status = %s, want unhealthy", result.Status)
	}
}

// TestMultipleEventsAggregation verifies that multiple events are evaluated
// and the worst status is returned.
func TestMultipleEventsAggregation(t *testing.T) {
	ctx := context.Background()

	policy := DefaultPolicy()
	evaluator, _ := NewEvaluator(policy)

	testCases := []struct {
		name           string
		events         []gpu.HealthEvent
		expectedStatus Result
	}{
		{
			name: "all_healthy",
			events: []gpu.HealthEvent{
				gpu.NewThermalEvent(0, "GPU-1", 60, "Normal"),
				gpu.NewThermalEvent(1, "GPU-2", 65, "Normal"),
			},
			expectedStatus: ResultHealthy,
		},
		{
			name: "one_degraded",
			events: []gpu.HealthEvent{
				gpu.NewThermalEvent(0, "GPU-1", 60, "Normal"),
				gpu.NewThermalEvent(1, "GPU-2", 88, "Elevated"),
			},
			expectedStatus: ResultDegraded,
		},
		{
			name: "one_unhealthy_overrides_degraded",
			events: []gpu.HealthEvent{
				gpu.NewThermalEvent(0, "GPU-1", 88, "Elevated"),
				gpu.NewXIDEvent(1, "GPU-2", 79, "Fatal"),
			},
			expectedStatus: ResultUnhealthy,
		},
		{
			name: "multiple_unhealthy",
			events: []gpu.HealthEvent{
				gpu.NewXIDEvent(0, "GPU-1", 79, "Fatal 1"),
				gpu.NewXIDEvent(1, "GPU-2", 48, "Fatal 2"),
				gpu.NewXIDEvent(2, "GPU-3", 63, "Fatal 3"),
			},
			expectedStatus: ResultUnhealthy,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, _ := evaluator.Evaluate(ctx, tc.events)
			if result.Status != tc.expectedStatus {
				t.Errorf("Status = %s, want %s", result.Status, tc.expectedStatus)
			}
			// Verify all matches are recorded
			if len(tc.events) > 0 && len(result.AllMatches) == 0 {
				t.Error("Expected AllMatches to be populated")
			}
		})
	}
}
