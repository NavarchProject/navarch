package pool

import (
	"context"
	"time"
)

// Autoscaler decides how many nodes a pool should have.
type Autoscaler interface {
	// Recommend returns the recommended node count for the pool.
	// Returns current count if no change is needed.
	Recommend(ctx context.Context, state PoolState) (int, error)
}

// PoolState provides current pool metrics to the autoscaler.
type PoolState struct {
	Name           string
	CurrentNodes   int
	HealthyNodes   int
	MinNodes       int
	MaxNodes       int
	Utilization    float64 // 0-100
	PendingJobs    int
	QueueDepth     int
	LastScaleTime  time.Time
	CooldownPeriod time.Duration

	// Historical metrics for predictive scaling
	UtilizationHistory []float64   // Recent utilization samples
	TimeOfDay          time.Time   // For time-based patterns
	DayOfWeek          time.Weekday
}

// ScaleDecision represents the autoscaler's recommendation.
type ScaleDecision struct {
	TargetNodes int
	Reason      string
	Confidence  float64 // 0-1, for predictive models
}

// ReactiveAutoscaler scales based on current utilization thresholds.
type ReactiveAutoscaler struct {
	ScaleUpThreshold   float64 // Scale up when utilization exceeds this
	ScaleDownThreshold float64 // Scale down when utilization falls below this
	ScaleUpStep        int     // Nodes to add per scale-up
	ScaleDownStep      int     // Nodes to remove per scale-down
}

func NewReactiveAutoscaler(scaleUpThreshold, scaleDownThreshold float64) *ReactiveAutoscaler {
	return &ReactiveAutoscaler{
		ScaleUpThreshold:   scaleUpThreshold,
		ScaleDownThreshold: scaleDownThreshold,
		ScaleUpStep:        1,
		ScaleDownStep:      1,
	}
}

func (a *ReactiveAutoscaler) Recommend(ctx context.Context, state PoolState) (int, error) {
	if time.Since(state.LastScaleTime) < state.CooldownPeriod {
		return state.CurrentNodes, nil
	}

	target := state.CurrentNodes

	if state.Utilization > a.ScaleUpThreshold && state.CurrentNodes < state.MaxNodes {
		target = min(state.CurrentNodes+a.ScaleUpStep, state.MaxNodes)
	} else if state.Utilization < a.ScaleDownThreshold && state.CurrentNodes > state.MinNodes {
		target = max(state.CurrentNodes-a.ScaleDownStep, state.MinNodes)
	}

	return target, nil
}

// QueueBasedAutoscaler scales based on pending job queue depth.
type QueueBasedAutoscaler struct {
	JobsPerNode int // Target jobs per node
}

func NewQueueBasedAutoscaler(jobsPerNode int) *QueueBasedAutoscaler {
	return &QueueBasedAutoscaler{JobsPerNode: jobsPerNode}
}

func (a *QueueBasedAutoscaler) Recommend(ctx context.Context, state PoolState) (int, error) {
	if time.Since(state.LastScaleTime) < state.CooldownPeriod {
		return state.CurrentNodes, nil
	}

	if a.JobsPerNode <= 0 {
		return state.CurrentNodes, nil
	}

	// Calculate nodes needed for current queue
	needed := (state.QueueDepth + a.JobsPerNode - 1) / a.JobsPerNode
	target := max(needed, state.MinNodes)
	target = min(target, state.MaxNodes)

	return target, nil
}

// ScheduledAutoscaler scales based on time-of-day patterns.
type ScheduledAutoscaler struct {
	Schedule []ScheduleEntry
	Fallback Autoscaler
}

type ScheduleEntry struct {
	DaysOfWeek []time.Weekday
	StartHour  int // 0-23
	EndHour    int // 0-23
	MinNodes   int
	MaxNodes   int
}

func NewScheduledAutoscaler(schedule []ScheduleEntry, fallback Autoscaler) *ScheduledAutoscaler {
	return &ScheduledAutoscaler{
		Schedule: schedule,
		Fallback: fallback,
	}
}

func (a *ScheduledAutoscaler) Recommend(ctx context.Context, state PoolState) (int, error) {
	hour := state.TimeOfDay.Hour()
	day := state.DayOfWeek

	for _, entry := range a.Schedule {
		if !a.matchesDay(entry.DaysOfWeek, day) {
			continue
		}
		if hour >= entry.StartHour && hour < entry.EndHour {
			// Override pool limits with schedule limits
			state.MinNodes = entry.MinNodes
			state.MaxNodes = entry.MaxNodes
			break
		}
	}

	if a.Fallback != nil {
		return a.Fallback.Recommend(ctx, state)
	}
	return state.CurrentNodes, nil
}

func (a *ScheduledAutoscaler) matchesDay(days []time.Weekday, day time.Weekday) bool {
	if len(days) == 0 {
		return true
	}
	for _, d := range days {
		if d == day {
			return true
		}
	}
	return false
}

// PredictiveAutoscaler uses historical data to forecast demand.
// This is a placeholder for ML-based prediction models.
type PredictiveAutoscaler struct {
	Fallback       Autoscaler
	LookbackWindow int     // Number of historical samples to consider
	GrowthFactor   float64 // Multiplier for predicted growth
}

func NewPredictiveAutoscaler(lookback int, growthFactor float64, fallback Autoscaler) *PredictiveAutoscaler {
	return &PredictiveAutoscaler{
		LookbackWindow: lookback,
		GrowthFactor:   growthFactor,
		Fallback:       fallback,
	}
}

func (a *PredictiveAutoscaler) Recommend(ctx context.Context, state PoolState) (int, error) {
	if len(state.UtilizationHistory) < a.LookbackWindow {
		if a.Fallback != nil {
			return a.Fallback.Recommend(ctx, state)
		}
		return state.CurrentNodes, nil
	}

	// Simple linear trend prediction
	trend := a.calculateTrend(state.UtilizationHistory)
	predictedUtil := state.Utilization + (trend * a.GrowthFactor)

	// Estimate nodes needed for predicted utilization
	if predictedUtil > 80 && state.CurrentNodes < state.MaxNodes {
		// Scale up proactively
		needed := int(float64(state.CurrentNodes) * (predictedUtil / 70))
		return min(needed, state.MaxNodes), nil
	}

	if a.Fallback != nil {
		return a.Fallback.Recommend(ctx, state)
	}
	return state.CurrentNodes, nil
}

func (a *PredictiveAutoscaler) calculateTrend(history []float64) float64 {
	if len(history) < 2 {
		return 0
	}
	recent := history[len(history)-a.LookbackWindow:]
	if len(recent) < 2 {
		return 0
	}
	return recent[len(recent)-1] - recent[0]
}

// CompositeAutoscaler combines multiple autoscalers with priority.
type CompositeAutoscaler struct {
	Autoscalers []Autoscaler
	Mode        CompositeMode
}

type CompositeMode int

const (
	ModeMax CompositeMode = iota // Take highest recommendation
	ModeMin                      // Take lowest recommendation
	ModeAvg                      // Average all recommendations
)

func NewCompositeAutoscaler(mode CompositeMode, autoscalers ...Autoscaler) *CompositeAutoscaler {
	return &CompositeAutoscaler{
		Autoscalers: autoscalers,
		Mode:        mode,
	}
}

func (a *CompositeAutoscaler) Recommend(ctx context.Context, state PoolState) (int, error) {
	if len(a.Autoscalers) == 0 {
		return state.CurrentNodes, nil
	}

	var recommendations []int
	for _, as := range a.Autoscalers {
		rec, err := as.Recommend(ctx, state)
		if err != nil {
			continue
		}
		recommendations = append(recommendations, rec)
	}

	if len(recommendations) == 0 {
		return state.CurrentNodes, nil
	}

	switch a.Mode {
	case ModeMax:
		result := recommendations[0]
		for _, r := range recommendations[1:] {
			if r > result {
				result = r
			}
		}
		return result, nil
	case ModeMin:
		result := recommendations[0]
		for _, r := range recommendations[1:] {
			if r < result {
				result = r
			}
		}
		return result, nil
	case ModeAvg:
		sum := 0
		for _, r := range recommendations {
			sum += r
		}
		return sum / len(recommendations), nil
	}

	return state.CurrentNodes, nil
}

