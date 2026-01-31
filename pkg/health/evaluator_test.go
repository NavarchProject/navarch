package health

import (
	"context"
	"testing"

	"github.com/NavarchProject/navarch/pkg/gpu"
	pb "github.com/NavarchProject/navarch/proto"
)

func TestNewEvaluator(t *testing.T) {
	policy := DefaultPolicy()
	eval, err := NewEvaluator(policy)
	if err != nil {
		t.Fatalf("NewEvaluator() error = %v", err)
	}
	if eval == nil {
		t.Fatal("NewEvaluator() returned nil")
	}
}

func TestNewEvaluator_InvalidCondition(t *testing.T) {
	policy := &Policy{
		Rules: []Rule{
			{
				Name:      "bad-syntax",
				Condition: "event.metrics[", // Invalid CEL
				Result:    ResultHealthy,
			},
		},
	}

	_, err := NewEvaluator(policy)
	if err == nil {
		t.Error("NewEvaluator() should fail for invalid CEL syntax")
	}
}

func TestEvaluator_Evaluate_Empty(t *testing.T) {
	eval, _ := NewEvaluator(DefaultPolicy())
	ctx := context.Background()

	result, err := eval.Evaluate(ctx, []gpu.HealthEvent{})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}

	if result.Status != ResultHealthy {
		t.Errorf("Status = %v, want healthy", result.Status)
	}
	if result.MatchedRule != "" {
		t.Errorf("MatchedRule = %q, want empty", result.MatchedRule)
	}
}

func TestEvaluator_Evaluate_FatalXID(t *testing.T) {
	eval, _ := NewEvaluator(DefaultPolicy())
	ctx := context.Background()

	events := []gpu.HealthEvent{
		gpu.NewXIDEvent(0, "GPU-0", 79, "GPU has fallen off the bus"),
	}

	result, err := eval.Evaluate(ctx, events)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}

	if result.Status != ResultUnhealthy {
		t.Errorf("Status = %v, want unhealthy", result.Status)
	}
	if result.MatchedRule != "fatal-xid" {
		t.Errorf("MatchedRule = %q, want 'fatal-xid'", result.MatchedRule)
	}
	if result.MatchedEvent == nil {
		t.Error("MatchedEvent should not be nil")
	}
}

func TestEvaluator_Evaluate_RecoverableXID(t *testing.T) {
	eval, _ := NewEvaluator(DefaultPolicy())
	ctx := context.Background()

	// XID 8 is not in the fatal list
	events := []gpu.HealthEvent{
		gpu.NewXIDEvent(0, "GPU-0", 8, "GPU shader exception"),
	}

	result, err := eval.Evaluate(ctx, events)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}

	if result.Status != ResultDegraded {
		t.Errorf("Status = %v, want degraded", result.Status)
	}
	if result.MatchedRule != "recoverable-xid" {
		t.Errorf("MatchedRule = %q, want 'recoverable-xid'", result.MatchedRule)
	}
}

func TestEvaluator_Evaluate_ThermalCritical(t *testing.T) {
	eval, _ := NewEvaluator(DefaultPolicy())
	ctx := context.Background()

	events := []gpu.HealthEvent{
		gpu.NewThermalEvent(0, "GPU-0", 98, "Temperature critical"),
	}

	result, err := eval.Evaluate(ctx, events)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}

	if result.Status != ResultUnhealthy {
		t.Errorf("Status = %v, want unhealthy", result.Status)
	}
	if result.MatchedRule != "thermal-critical" {
		t.Errorf("MatchedRule = %q, want 'thermal-critical'", result.MatchedRule)
	}
}

func TestEvaluator_Evaluate_ThermalWarning(t *testing.T) {
	eval, _ := NewEvaluator(DefaultPolicy())
	ctx := context.Background()

	events := []gpu.HealthEvent{
		gpu.NewThermalEvent(0, "GPU-0", 87, "Temperature warning"),
	}

	result, err := eval.Evaluate(ctx, events)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}

	if result.Status != ResultDegraded {
		t.Errorf("Status = %v, want degraded", result.Status)
	}
	if result.MatchedRule != "thermal-warning" {
		t.Errorf("MatchedRule = %q, want 'thermal-warning'", result.MatchedRule)
	}
}

func TestEvaluator_Evaluate_MultipleEvents_WorstWins(t *testing.T) {
	eval, _ := NewEvaluator(DefaultPolicy())
	ctx := context.Background()

	events := []gpu.HealthEvent{
		gpu.NewThermalEvent(0, "GPU-0", 87, "Thermal warning"),       // degraded
		gpu.NewXIDEvent(1, "GPU-1", 79, "GPU fell off bus"),          // unhealthy
		gpu.NewNVLinkEvent(2, "GPU-2", 0, "NVLink error"),            // degraded
	}

	result, err := eval.Evaluate(ctx, events)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}

	// Should return unhealthy (worst case)
	if result.Status != ResultUnhealthy {
		t.Errorf("Status = %v, want unhealthy", result.Status)
	}

	// Should track all matches
	if len(result.AllMatches) != 3 {
		t.Errorf("len(AllMatches) = %d, want 3", len(result.AllMatches))
	}
}

func TestEvaluator_Evaluate_ECCErrors(t *testing.T) {
	eval, _ := NewEvaluator(DefaultPolicy())
	ctx := context.Background()

	t.Run("double-bit ECC is unhealthy", func(t *testing.T) {
		events := []gpu.HealthEvent{
			gpu.NewMemoryEvent(0, "GPU-0", pb.HealthEventType_HEALTH_EVENT_TYPE_ECC_DBE, 0, 1, "DBE error"),
		}

		result, err := eval.Evaluate(ctx, events)
		if err != nil {
			t.Fatalf("Evaluate() error = %v", err)
		}

		if result.Status != ResultUnhealthy {
			t.Errorf("Status = %v, want unhealthy", result.Status)
		}
	})
}

func TestEvaluator_EvaluateSingle(t *testing.T) {
	eval, _ := NewEvaluator(DefaultPolicy())
	ctx := context.Background()

	event := gpu.NewXIDEvent(0, "GPU-0", 79, "Fatal XID")

	result, rule, err := eval.EvaluateSingle(ctx, event)
	if err != nil {
		t.Fatalf("EvaluateSingle() error = %v", err)
	}

	if result != ResultUnhealthy {
		t.Errorf("result = %v, want unhealthy", result)
	}
	if rule != "fatal-xid" {
		t.Errorf("rule = %q, want 'fatal-xid'", rule)
	}
}

func TestEvaluator_UpdatePolicy(t *testing.T) {
	// Start with default policy
	eval, _ := NewEvaluator(DefaultPolicy())
	ctx := context.Background()

	// XID 79 should be unhealthy
	event := gpu.NewXIDEvent(0, "GPU-0", 79, "Fatal XID")
	result, _, _ := eval.EvaluateSingle(ctx, event)
	if result != ResultUnhealthy {
		t.Errorf("Before update: result = %v, want unhealthy", result)
	}

	// Update policy to treat XID 79 as degraded
	newPolicy := &Policy{
		Rules: []Rule{
			{
				Name:      "xid-79-degraded",
				Condition: `event.event_type == "xid" && event.metrics.xid_code == 79`,
				Result:    ResultDegraded,
			},
			{
				Name:      "default",
				Condition: "true",
				Result:    ResultHealthy,
			},
		},
	}

	if err := eval.UpdatePolicy(newPolicy); err != nil {
		t.Fatalf("UpdatePolicy() error = %v", err)
	}

	// Now XID 79 should be degraded
	result, rule, _ := eval.EvaluateSingle(ctx, event)
	if result != ResultDegraded {
		t.Errorf("After update: result = %v, want degraded", result)
	}
	if rule != "xid-79-degraded" {
		t.Errorf("After update: rule = %q, want 'xid-79-degraded'", rule)
	}
}

func TestEvaluator_UpdatePolicy_InvalidCEL(t *testing.T) {
	eval, _ := NewEvaluator(DefaultPolicy())

	badPolicy := &Policy{
		Rules: []Rule{
			{
				Name:      "bad",
				Condition: "event.[invalid",
				Result:    ResultHealthy,
			},
		},
	}

	err := eval.UpdatePolicy(badPolicy)
	if err == nil {
		t.Error("UpdatePolicy() should fail for invalid CEL")
	}

	// Original policy should still work
	ctx := context.Background()
	event := gpu.NewXIDEvent(0, "GPU-0", 79, "Fatal XID")
	result, _, _ := eval.EvaluateSingle(ctx, event)
	if result != ResultUnhealthy {
		t.Error("Original policy should still be in effect")
	}
}

func TestEvaluator_Policy(t *testing.T) {
	policy := DefaultPolicy()
	eval, _ := NewEvaluator(policy)

	got := eval.Policy()
	if got != policy {
		t.Error("Policy() should return the current policy")
	}
}

func TestEvaluator_CustomPolicy(t *testing.T) {
	// Test a custom policy with application-specific rules
	// Rules are evaluated in definition order (first match wins)
	policy := &Policy{
		Rules: []Rule{
			{
				Name:      "any-xid-is-fatal",
				Condition: `event.event_type == "xid"`,
				Result:    ResultUnhealthy,
			},
			{
				Name:      "temperature-over-80",
				Condition: `event.event_type == "thermal" && event.metrics.temperature > 80`,
				Result:    ResultDegraded,
			},
			{
				Name:      "default",
				Condition: "true",
				Result:    ResultHealthy,
			},
		},
	}

	eval, err := NewEvaluator(policy)
	if err != nil {
		t.Fatalf("NewEvaluator() error = %v", err)
	}

	ctx := context.Background()

	// Any XID (even non-fatal) should be unhealthy
	result, rule, _ := eval.EvaluateSingle(ctx, gpu.NewXIDEvent(0, "GPU-0", 8, "Minor XID"))
	if result != ResultUnhealthy {
		t.Errorf("XID 8: result = %v, want unhealthy", result)
	}
	if rule != "any-xid-is-fatal" {
		t.Errorf("XID 8: rule = %q, want 'any-xid-is-fatal'", rule)
	}

	// Temperature 82 should be degraded
	result, rule, _ = eval.EvaluateSingle(ctx, gpu.NewThermalEvent(0, "GPU-0", 82, "Warm"))
	if result != ResultDegraded {
		t.Errorf("Temp 82: result = %v, want degraded", result)
	}

	// Temperature 75 should be healthy
	result, rule, _ = eval.EvaluateSingle(ctx, gpu.NewThermalEvent(0, "GPU-0", 75, "Normal"))
	if result != ResultHealthy {
		t.Errorf("Temp 75: result = %v, want healthy", result)
	}
}

func TestIsWorse(t *testing.T) {
	tests := []struct {
		a, b   Result
		expect bool
	}{
		{ResultUnhealthy, ResultHealthy, true},
		{ResultUnhealthy, ResultDegraded, true},
		{ResultDegraded, ResultHealthy, true},
		{ResultHealthy, ResultHealthy, false},
		{ResultDegraded, ResultDegraded, false},
		{ResultUnhealthy, ResultUnhealthy, false},
		{ResultHealthy, ResultDegraded, false},
		{ResultHealthy, ResultUnhealthy, false},
		{ResultDegraded, ResultUnhealthy, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.a)+"_vs_"+string(tt.b), func(t *testing.T) {
			if got := isWorse(tt.a, tt.b); got != tt.expect {
				t.Errorf("isWorse(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.expect)
			}
		})
	}
}
