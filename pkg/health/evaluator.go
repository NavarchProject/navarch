package health

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"

	"github.com/NavarchProject/navarch/pkg/gpu"
)

// Evaluator evaluates health events against a policy using CEL.
type Evaluator struct {
	policy   *Policy
	env      *cel.Env
	programs map[string]cel.Program
	mu       sync.RWMutex
}

// EvaluationResult contains the outcome of evaluating events against a policy.
type EvaluationResult struct {
	// Status is the overall health status after evaluating all events.
	Status Result

	// MatchedRule is the name of the rule that determined the status.
	// Empty if no events were provided (defaults to healthy).
	MatchedRule string

	// MatchedEvent is the event that triggered the matched rule.
	// Nil if no events were provided.
	MatchedEvent *gpu.HealthEvent

	// AllMatches contains all rule matches for debugging/logging.
	AllMatches []RuleMatch
}

// RuleMatch records a single rule matching an event.
type RuleMatch struct {
	Rule  string
	Event gpu.HealthEvent
}

// NewEvaluator creates a new health policy evaluator.
func NewEvaluator(policy *Policy) (*Evaluator, error) {
	// Create CEL environment with the event variable
	env, err := cel.NewEnv(
		cel.Variable("event", cel.MapType(cel.StringType, cel.DynType)),
	)
	if err != nil {
		return nil, fmt.Errorf("create CEL environment: %w", err)
	}

	// Compile all rule conditions
	programs := make(map[string]cel.Program)
	for _, rule := range policy.Rules {
		ast, issues := env.Compile(rule.Condition)
		if issues != nil && issues.Err() != nil {
			return nil, fmt.Errorf("compile rule %q: %w", rule.Name, issues.Err())
		}

		program, err := env.Program(ast)
		if err != nil {
			return nil, fmt.Errorf("create program for rule %q: %w", rule.Name, err)
		}

		programs[rule.Name] = program
	}

	return &Evaluator{
		policy:   policy,
		env:      env,
		programs: programs,
	}, nil
}

// Evaluate evaluates a set of health events against the policy.
// Returns the worst health status among all events.
func (e *Evaluator) Evaluate(ctx context.Context, events []gpu.HealthEvent) (*EvaluationResult, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	result := &EvaluationResult{
		Status:     ResultHealthy,
		AllMatches: make([]RuleMatch, 0),
	}

	if len(events) == 0 {
		return result, nil
	}

	// Get rules sorted by priority
	sortedRules := e.policy.SortedRules()

	// Track the worst status seen
	worstStatus := ResultHealthy
	var worstRule string
	var worstEvent *gpu.HealthEvent

	// Evaluate each event against all rules
	for i := range events {
		event := &events[i]
		eventMap := eventToMap(event)

		// Find the first matching rule for this event
		for _, rule := range sortedRules {
			program := e.programs[rule.Name]

			out, _, err := program.Eval(map[string]any{
				"event": eventMap,
			})
			if err != nil {
				// Log but continue - don't fail evaluation on single rule error
				continue
			}

			if out.Type() == types.BoolType && out.Value().(bool) {
				result.AllMatches = append(result.AllMatches, RuleMatch{
					Rule:  rule.Name,
					Event: *event,
				})

				// Update worst status if this is worse
				if isWorse(rule.Result, worstStatus) {
					worstStatus = rule.Result
					worstRule = rule.Name
					worstEvent = event
				}

				// First matching rule wins for this event
				break
			}
		}
	}

	result.Status = worstStatus
	result.MatchedRule = worstRule
	result.MatchedEvent = worstEvent

	return result, nil
}

// EvaluateSingle evaluates a single event and returns the matching rule's result.
func (e *Evaluator) EvaluateSingle(ctx context.Context, event gpu.HealthEvent) (Result, string, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	eventMap := eventToMap(&event)
	sortedRules := e.policy.SortedRules()

	for _, rule := range sortedRules {
		program := e.programs[rule.Name]

		out, _, err := program.Eval(map[string]any{
			"event": eventMap,
		})
		if err != nil {
			continue
		}

		if out.Type() == types.BoolType && out.Value().(bool) {
			return rule.Result, rule.Name, nil
		}
	}

	// No rule matched (shouldn't happen with a default rule)
	return ResultHealthy, "", nil
}

// UpdatePolicy replaces the current policy with a new one.
func (e *Evaluator) UpdatePolicy(policy *Policy) error {
	// Compile the new policy first
	programs := make(map[string]cel.Program)
	for _, rule := range policy.Rules {
		ast, issues := e.env.Compile(rule.Condition)
		if issues != nil && issues.Err() != nil {
			return fmt.Errorf("compile rule %q: %w", rule.Name, issues.Err())
		}

		program, err := e.env.Program(ast)
		if err != nil {
			return fmt.Errorf("create program for rule %q: %w", rule.Name, err)
		}

		programs[rule.Name] = program
	}

	// Swap atomically
	e.mu.Lock()
	e.policy = policy
	e.programs = programs
	e.mu.Unlock()

	return nil
}

// Policy returns the current policy.
func (e *Evaluator) Policy() *Policy {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.policy
}

// eventToMap converts a HealthEvent to a map for CEL evaluation.
// Proto enums are converted to strings for user-friendly policy conditions.
func eventToMap(event *gpu.HealthEvent) map[string]any {
	return map[string]any{
		"timestamp":  event.Timestamp.Format(time.RFC3339),
		"gpu_index":  event.GPUIndex,
		"gpu_uuid":   event.GPUUUID,
		"system":     gpu.SystemString(event.System),
		"event_type": gpu.EventTypeString(event.EventType),
		"message":    event.Message,
		"metrics":    convertMetrics(event.Metrics),
	}
}

// convertMetrics converts the metrics map to CEL-compatible types.
func convertMetrics(metrics map[string]any) map[string]any {
	if metrics == nil {
		return map[string]any{}
	}

	result := make(map[string]any, len(metrics))
	for k, v := range metrics {
		result[k] = convertValue(v)
	}
	return result
}

// convertValue converts a value to a CEL-compatible type.
func convertValue(v any) any {
	switch val := v.(type) {
	case int:
		return int64(val)
	case int32:
		return int64(val)
	case int64:
		return val
	case uint:
		return int64(val)
	case uint32:
		return int64(val)
	case uint64:
		return int64(val) // May overflow for very large values
	case float32:
		return float64(val)
	case float64:
		return val
	case string:
		return val
	case bool:
		return val
	case []int:
		result := make([]ref.Val, len(val))
		for i, item := range val {
			result[i] = types.Int(item)
		}
		return result
	default:
		return val
	}
}

// isWorse returns true if a is worse than b.
// unhealthy > degraded > healthy
func isWorse(a, b Result) bool {
	return resultSeverity(a) > resultSeverity(b)
}

func resultSeverity(r Result) int {
	switch r {
	case ResultUnhealthy:
		return 2
	case ResultDegraded:
		return 1
	case ResultHealthy:
		return 0
	default:
		return 0
	}
}
