package pool

import (
	"context"
	"fmt"
	"time"
)

// Autoscaler decides how many nodes a pool should have.
type Autoscaler interface {
	// Recommend returns the recommended node count and reason.
	// Returns current count if no change is needed.
	Recommend(ctx context.Context, state PoolState) (ScaleRecommendation, error)
}

// ScaleRecommendation is the autoscaler's output.
type ScaleRecommendation struct {
	TargetNodes int    // Desired node count
	Reason      string // Human-readable explanation for logging/debugging
}

// PoolState provides current pool metrics to the autoscaler.
type PoolState struct {
	Name         string
	CurrentNodes int     // Total nodes in pool (healthy + unhealthy)
	HealthyNodes int     // Nodes passing health checks
	MinNodes     int     // Pool minimum node count
	MaxNodes     int     // Pool maximum node count
	Utilization  float64 // Average GPU utilization (0-100)
	PendingJobs  int     // Jobs waiting to be scheduled
	QueueDepth   int     // Total jobs in queue (pending + running)

	LastScaleTime  time.Time     // When the pool last scaled
	CooldownPeriod time.Duration // Minimum time between scaling actions

	UtilizationHistory []float64    // Recent utilization samples for trend analysis
	TimeOfDay          time.Time    // Current time for scheduled scaling
	DayOfWeek          time.Weekday // Current day for scheduled scaling
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

func (a *ReactiveAutoscaler) Recommend(ctx context.Context, state PoolState) (ScaleRecommendation, error) {
	if time.Since(state.LastScaleTime) < state.CooldownPeriod {
		return ScaleRecommendation{TargetNodes: state.CurrentNodes, Reason: "cooldown active"}, nil
	}

	// Ensure min_nodes are met
	if state.CurrentNodes < state.MinNodes {
		return ScaleRecommendation{
			TargetNodes: state.MinNodes,
			Reason:      fmt.Sprintf("below minimum: %d < %d", state.CurrentNodes, state.MinNodes),
		}, nil
	}

	if state.Utilization > a.ScaleUpThreshold && state.CurrentNodes < state.MaxNodes {
		target := min(state.CurrentNodes+a.ScaleUpStep, state.MaxNodes)
		return ScaleRecommendation{
			TargetNodes: target,
			Reason:      fmt.Sprintf("utilization %.1f%% > %.1f%% threshold", state.Utilization, a.ScaleUpThreshold),
		}, nil
	}

	if state.Utilization < a.ScaleDownThreshold && state.CurrentNodes > state.MinNodes {
		target := max(state.CurrentNodes-a.ScaleDownStep, state.MinNodes)
		return ScaleRecommendation{
			TargetNodes: target,
			Reason:      fmt.Sprintf("utilization %.1f%% < %.1f%% threshold", state.Utilization, a.ScaleDownThreshold),
		}, nil
	}

	return ScaleRecommendation{TargetNodes: state.CurrentNodes, Reason: "no scaling needed"}, nil
}

// QueueBasedAutoscaler scales based on pending job queue depth.
type QueueBasedAutoscaler struct {
	JobsPerNode int // Target jobs per node
}

func NewQueueBasedAutoscaler(jobsPerNode int) *QueueBasedAutoscaler {
	return &QueueBasedAutoscaler{JobsPerNode: jobsPerNode}
}

func (a *QueueBasedAutoscaler) Recommend(ctx context.Context, state PoolState) (ScaleRecommendation, error) {
	if time.Since(state.LastScaleTime) < state.CooldownPeriod {
		return ScaleRecommendation{TargetNodes: state.CurrentNodes, Reason: "cooldown active"}, nil
	}

	if a.JobsPerNode <= 0 {
		return ScaleRecommendation{TargetNodes: state.CurrentNodes, Reason: "jobs_per_node not configured"}, nil
	}

	needed := (state.QueueDepth + a.JobsPerNode - 1) / a.JobsPerNode
	target := max(needed, state.MinNodes)
	target = min(target, state.MaxNodes)

	return ScaleRecommendation{
		TargetNodes: target,
		Reason:      fmt.Sprintf("queue depth %d requires %d nodes (%d jobs/node)", state.QueueDepth, needed, a.JobsPerNode),
	}, nil
}

// ScheduledAutoscaler scales based on time-of-day patterns.
type ScheduledAutoscaler struct {
	Schedule []ScheduleEntry // Time-based scaling rules, evaluated in order
	Fallback Autoscaler      // Autoscaler to use for actual recommendations
}

// ScheduleEntry defines scaling limits for a time window.
type ScheduleEntry struct {
	DaysOfWeek []time.Weekday // Days this entry applies to; empty means all days
	StartHour  int            // Start hour (0-23, inclusive)
	EndHour    int            // End hour (0-23, exclusive)
	MinNodes   int            // Override pool MinNodes during this window
	MaxNodes   int            // Override pool MaxNodes during this window
}

func NewScheduledAutoscaler(schedule []ScheduleEntry, fallback Autoscaler) *ScheduledAutoscaler {
	return &ScheduledAutoscaler{
		Schedule: schedule,
		Fallback: fallback,
	}
}

func (a *ScheduledAutoscaler) Recommend(ctx context.Context, state PoolState) (ScaleRecommendation, error) {
	hour := state.TimeOfDay.Hour()
	day := state.DayOfWeek

	for _, entry := range a.Schedule {
		if !a.matchesDay(entry.DaysOfWeek, day) {
			continue
		}
		if hour >= entry.StartHour && hour < entry.EndHour {
			state.MinNodes = entry.MinNodes
			state.MaxNodes = entry.MaxNodes
			break
		}
	}

	if a.Fallback != nil {
		return a.Fallback.Recommend(ctx, state)
	}
	return ScaleRecommendation{TargetNodes: state.CurrentNodes, Reason: "no fallback autoscaler"}, nil
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
type PredictiveAutoscaler struct {
	Fallback       Autoscaler // Used when insufficient history is available
	LookbackWindow int        // Number of utilization samples to analyze
	GrowthFactor   float64    // Multiplier applied to predicted growth (e.g., 1.5 = 50% buffer)
}

func NewPredictiveAutoscaler(lookback int, growthFactor float64, fallback Autoscaler) *PredictiveAutoscaler {
	return &PredictiveAutoscaler{
		LookbackWindow: lookback,
		GrowthFactor:   growthFactor,
		Fallback:       fallback,
	}
}

func (a *PredictiveAutoscaler) Recommend(ctx context.Context, state PoolState) (ScaleRecommendation, error) {
	if len(state.UtilizationHistory) < a.LookbackWindow {
		if a.Fallback != nil {
			return a.Fallback.Recommend(ctx, state)
		}
		return ScaleRecommendation{TargetNodes: state.CurrentNodes, Reason: "insufficient history"}, nil
	}

	trend := a.calculateTrend(state.UtilizationHistory)
	predictedUtil := state.Utilization + (trend * a.GrowthFactor)

	if predictedUtil > 80 && state.CurrentNodes < state.MaxNodes {
		needed := int(float64(state.CurrentNodes) * (predictedUtil / 70))
		target := min(needed, state.MaxNodes)
		return ScaleRecommendation{
			TargetNodes: target,
			Reason:      fmt.Sprintf("predicted utilization %.1f%% (trend: %.1f)", predictedUtil, trend),
		}, nil
	}

	if a.Fallback != nil {
		return a.Fallback.Recommend(ctx, state)
	}
	return ScaleRecommendation{TargetNodes: state.CurrentNodes, Reason: "prediction below threshold"}, nil
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

// CompositeAutoscaler combines multiple autoscalers.
type CompositeAutoscaler struct {
	Autoscalers []Autoscaler  // Autoscalers to query for recommendations
	Mode        CompositeMode // How to combine recommendations
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

func (a *CompositeAutoscaler) Recommend(ctx context.Context, state PoolState) (ScaleRecommendation, error) {
	if len(a.Autoscalers) == 0 {
		return ScaleRecommendation{TargetNodes: state.CurrentNodes, Reason: "no autoscalers configured"}, nil
	}

	var recommendations []int
	var reasons []string
	for _, as := range a.Autoscalers {
		rec, err := as.Recommend(ctx, state)
		if err != nil {
			continue
		}
		recommendations = append(recommendations, rec.TargetNodes)
		reasons = append(reasons, rec.Reason)
	}

	if len(recommendations) == 0 {
		return ScaleRecommendation{TargetNodes: state.CurrentNodes, Reason: "all autoscalers failed"}, nil
	}

	var target int
	var modeDesc string
	switch a.Mode {
	case ModeMax:
		target = recommendations[0]
		for _, r := range recommendations[1:] {
			if r > target {
				target = r
			}
		}
		modeDesc = "max"
	case ModeMin:
		target = recommendations[0]
		for _, r := range recommendations[1:] {
			if r < target {
				target = r
			}
		}
		modeDesc = "min"
	case ModeAvg:
		sum := 0
		for _, r := range recommendations {
			sum += r
		}
		target = sum / len(recommendations)
		modeDesc = "avg"
	default:
		return ScaleRecommendation{TargetNodes: state.CurrentNodes, Reason: "unknown mode"}, nil
	}

	return ScaleRecommendation{
		TargetNodes: target,
		Reason:      fmt.Sprintf("%s of %d autoscalers: %v", modeDesc, len(recommendations), recommendations),
	}, nil
}
