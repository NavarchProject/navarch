package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/NavarchProject/navarch/pkg/pool"
)

// BuildAutoscaler creates an Autoscaler from configuration.
func BuildAutoscaler(cfg *AutoscalingCfg) (pool.Autoscaler, error) {
	if cfg == nil {
		return nil, nil
	}

	switch cfg.Type {
	case "reactive":
		return buildReactiveAutoscaler(cfg), nil

	case "queue":
		return buildQueueAutoscaler(cfg), nil

	case "scheduled":
		return buildScheduledAutoscaler(cfg)

	case "predictive":
		return buildPredictiveAutoscaler(cfg)

	case "composite":
		return buildCompositeAutoscaler(cfg)

	default:
		return nil, fmt.Errorf("unknown autoscaler type: %s", cfg.Type)
	}
}

func buildReactiveAutoscaler(cfg *AutoscalingCfg) *pool.ReactiveAutoscaler {
	scaleUp := 80.0
	scaleDown := 20.0
	if cfg.ScaleUpAt != nil {
		scaleUp = float64(*cfg.ScaleUpAt)
	}
	if cfg.ScaleDownAt != nil {
		scaleDown = float64(*cfg.ScaleDownAt)
	}
	return pool.NewReactiveAutoscaler(scaleUp, scaleDown)
}

func buildQueueAutoscaler(cfg *AutoscalingCfg) *pool.QueueBasedAutoscaler {
	jobsPerNode := 10
	if cfg.JobsPerNode != nil {
		jobsPerNode = *cfg.JobsPerNode
	}
	return pool.NewQueueBasedAutoscaler(jobsPerNode)
}

func buildScheduledAutoscaler(cfg *AutoscalingCfg) (*pool.ScheduledAutoscaler, error) {
	var entries []pool.ScheduleEntry
	for _, s := range cfg.Schedule {
		entries = append(entries, pool.ScheduleEntry{
			DaysOfWeek: ParseDaysOfWeek(s.Days),
			StartHour:  s.Start,
			EndHour:    s.End,
			MinNodes:   s.MinNodes,
			MaxNodes:   s.MaxNodes,
		})
	}

	var fallback pool.Autoscaler
	if cfg.Fallback != nil {
		var err error
		fallback, err = BuildAutoscaler(cfg.Fallback)
		if err != nil {
			return nil, fmt.Errorf("building fallback autoscaler: %w", err)
		}
	}

	return pool.NewScheduledAutoscaler(entries, fallback), nil
}

func buildPredictiveAutoscaler(cfg *AutoscalingCfg) (*pool.PredictiveAutoscaler, error) {
	lookback := 10
	growth := 1.2
	if cfg.LookbackWindow != nil {
		lookback = *cfg.LookbackWindow
	}
	if cfg.GrowthFactor != nil {
		growth = *cfg.GrowthFactor
	}

	var fallback pool.Autoscaler
	if cfg.Fallback != nil {
		var err error
		fallback, err = BuildAutoscaler(cfg.Fallback)
		if err != nil {
			return nil, fmt.Errorf("building fallback autoscaler: %w", err)
		}
	}

	return pool.NewPredictiveAutoscaler(lookback, growth, fallback), nil
}

func buildCompositeAutoscaler(cfg *AutoscalingCfg) (*pool.CompositeAutoscaler, error) {
	var autoscalers []pool.Autoscaler
	for i, a := range cfg.Autoscalers {
		as, err := BuildAutoscaler(&a)
		if err != nil {
			return nil, fmt.Errorf("building autoscaler %d: %w", i, err)
		}
		autoscalers = append(autoscalers, as)
	}

	mode := pool.ModeMax
	switch cfg.Mode {
	case "min":
		mode = pool.ModeMin
	case "avg":
		mode = pool.ModeAvg
	}

	return pool.NewCompositeAutoscaler(mode, autoscalers...), nil
}

// ParseDaysOfWeek converts day name strings to time.Weekday values.
func ParseDaysOfWeek(days []string) []time.Weekday {
	dayMap := map[string]time.Weekday{
		"sunday":    time.Sunday,
		"sun":       time.Sunday,
		"monday":    time.Monday,
		"mon":       time.Monday,
		"tuesday":   time.Tuesday,
		"tue":       time.Tuesday,
		"wednesday": time.Wednesday,
		"wed":       time.Wednesday,
		"thursday":  time.Thursday,
		"thu":       time.Thursday,
		"friday":    time.Friday,
		"fri":       time.Friday,
		"saturday":  time.Saturday,
		"sat":       time.Saturday,
	}

	var result []time.Weekday
	for _, d := range days {
		if wd, ok := dayMap[strings.ToLower(d)]; ok {
			result = append(result, wd)
		}
	}
	return result
}

// GetUnhealthyThreshold returns the unhealthy threshold from health config, or default.
func GetUnhealthyThreshold(h *HealthCfg) int {
	if h == nil || h.UnhealthyAfter == 0 {
		return 2
	}
	return h.UnhealthyAfter
}

// GetAutoReplace returns the auto-replace setting from health config, or default.
func GetAutoReplace(h *HealthCfg) bool {
	if h == nil {
		return true
	}
	return h.AutoReplace
}
