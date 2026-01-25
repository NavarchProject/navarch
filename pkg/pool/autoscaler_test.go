package pool

import (
	"context"
	"testing"
	"time"
)

func TestReactiveAutoscaler(t *testing.T) {
	as := NewReactiveAutoscaler(80, 20)
	ctx := context.Background()

	tests := []struct {
		name       string
		state      PoolState
		wantTarget int
	}{
		{
			name: "scale up when utilization high",
			state: PoolState{
				CurrentNodes: 5,
				MinNodes:     1,
				MaxNodes:     10,
				Utilization:  85,
			},
			wantTarget: 6,
		},
		{
			name: "scale down when utilization low",
			state: PoolState{
				CurrentNodes: 5,
				MinNodes:     1,
				MaxNodes:     10,
				Utilization:  15,
			},
			wantTarget: 4,
		},
		{
			name: "no change in normal range",
			state: PoolState{
				CurrentNodes: 5,
				MinNodes:     1,
				MaxNodes:     10,
				Utilization:  50,
			},
			wantTarget: 5,
		},
		{
			name: "respect max limit",
			state: PoolState{
				CurrentNodes: 10,
				MinNodes:     1,
				MaxNodes:     10,
				Utilization:  90,
			},
			wantTarget: 10,
		},
		{
			name: "respect min limit",
			state: PoolState{
				CurrentNodes: 1,
				MinNodes:     1,
				MaxNodes:     10,
				Utilization:  10,
			},
			wantTarget: 1,
		},
		{
			name: "respect cooldown",
			state: PoolState{
				CurrentNodes:   5,
				MinNodes:       1,
				MaxNodes:       10,
				Utilization:    90,
				LastScaleTime:  time.Now(),
				CooldownPeriod: time.Hour,
			},
			wantTarget: 5,
		},
		{
			name: "at threshold boundary - no scale up",
			state: PoolState{
				CurrentNodes: 5,
				MinNodes:     1,
				MaxNodes:     10,
				Utilization:  80, // Exactly at threshold, not above
			},
			wantTarget: 5,
		},
		{
			name: "at threshold boundary - no scale down",
			state: PoolState{
				CurrentNodes: 5,
				MinNodes:     1,
				MaxNodes:     10,
				Utilization:  20, // Exactly at threshold, not below
			},
			wantTarget: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, err := as.Recommend(ctx, tt.state)
			if err != nil {
				t.Fatalf("Recommend() error = %v", err)
			}
			if rec.TargetNodes != tt.wantTarget {
				t.Errorf("Recommend() = %d, want %d (reason: %s)", rec.TargetNodes, tt.wantTarget, rec.Reason)
			}
		})
	}
}

func TestQueueBasedAutoscaler(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		jobsPerNode int
		state       PoolState
		wantTarget  int
	}{
		{
			name:        "scale up for queue",
			jobsPerNode: 10,
			state: PoolState{
				CurrentNodes: 2,
				MinNodes:     1,
				MaxNodes:     10,
				QueueDepth:   50,
			},
			wantTarget: 5,
		},
		{
			name:        "scale down for empty queue",
			jobsPerNode: 10,
			state: PoolState{
				CurrentNodes: 5,
				MinNodes:     1,
				MaxNodes:     10,
				QueueDepth:   5,
			},
			wantTarget: 1,
		},
		{
			name:        "respect min nodes",
			jobsPerNode: 10,
			state: PoolState{
				CurrentNodes: 5,
				MinNodes:     3,
				MaxNodes:     10,
				QueueDepth:   0,
			},
			wantTarget: 3,
		},
		{
			name:        "respect max nodes",
			jobsPerNode: 10,
			state: PoolState{
				CurrentNodes: 5,
				MinNodes:     1,
				MaxNodes:     10,
				QueueDepth:   200,
			},
			wantTarget: 10,
		},
		{
			name:        "zero jobs per node returns current",
			jobsPerNode: 0,
			state: PoolState{
				CurrentNodes: 5,
				MinNodes:     1,
				MaxNodes:     10,
				QueueDepth:   100,
			},
			wantTarget: 5,
		},
		{
			name:        "respect cooldown",
			jobsPerNode: 10,
			state: PoolState{
				CurrentNodes:   5,
				MinNodes:       1,
				MaxNodes:       10,
				QueueDepth:     100,
				LastScaleTime:  time.Now(),
				CooldownPeriod: time.Hour,
			},
			wantTarget: 5,
		},
		{
			name:        "partial queue rounds up",
			jobsPerNode: 10,
			state: PoolState{
				CurrentNodes: 1,
				MinNodes:     1,
				MaxNodes:     10,
				QueueDepth:   11, // Needs 2 nodes (ceil(11/10))
			},
			wantTarget: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			as := NewQueueBasedAutoscaler(tt.jobsPerNode)
			rec, err := as.Recommend(ctx, tt.state)
			if err != nil {
				t.Fatalf("Recommend() error = %v", err)
			}
			if rec.TargetNodes != tt.wantTarget {
				t.Errorf("Recommend() = %d, want %d (reason: %s)", rec.TargetNodes, tt.wantTarget, rec.Reason)
			}
		})
	}
}

func TestScheduledAutoscaler(t *testing.T) {
	ctx := context.Background()
	reactive := NewReactiveAutoscaler(80, 20)
	schedule := []ScheduleEntry{
		{
			DaysOfWeek: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday},
			StartHour:  9,
			EndHour:    17,
			MinNodes:   5,
			MaxNodes:   20,
		},
		{
			DaysOfWeek: []time.Weekday{time.Saturday, time.Sunday},
			StartHour:  0,
			EndHour:    24,
			MinNodes:   1,
			MaxNodes:   5,
		},
	}

	tests := []struct {
		name       string
		fallback   Autoscaler
		state      PoolState
		wantTarget int
	}{
		{
			name:     "business hours scale up",
			fallback: reactive,
			state: PoolState{
				CurrentNodes: 5,
				MinNodes:     1,
				MaxNodes:     10,
				Utilization:  90,
				TimeOfDay:    time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
				DayOfWeek:    time.Monday,
			},
			wantTarget: 6,
		},
		{
			name:     "weekend limited max",
			fallback: reactive,
			state: PoolState{
				CurrentNodes: 3,
				MinNodes:     1,
				MaxNodes:     100,
				Utilization:  90,
				TimeOfDay:    time.Date(2024, 1, 13, 10, 0, 0, 0, time.UTC),
				DayOfWeek:    time.Saturday,
			},
			wantTarget: 4,
		},
		{
			name:     "outside schedule uses original limits",
			fallback: reactive,
			state: PoolState{
				CurrentNodes: 5,
				MinNodes:     1,
				MaxNodes:     50,
				Utilization:  90,
				TimeOfDay:    time.Date(2024, 1, 15, 20, 0, 0, 0, time.UTC), // Monday 8pm
				DayOfWeek:    time.Monday,
			},
			wantTarget: 6, // Uses original MaxNodes=50, scales up
		},
		{
			name:     "no fallback returns current",
			fallback: nil,
			state: PoolState{
				CurrentNodes: 5,
				MinNodes:     1,
				MaxNodes:     10,
				Utilization:  90,
				TimeOfDay:    time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
				DayOfWeek:    time.Monday,
			},
			wantTarget: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			as := NewScheduledAutoscaler(schedule, tt.fallback)
			rec, err := as.Recommend(ctx, tt.state)
			if err != nil {
				t.Fatalf("Recommend() error = %v", err)
			}
			if rec.TargetNodes != tt.wantTarget {
				t.Errorf("Recommend() = %d, want %d (reason: %s)", rec.TargetNodes, tt.wantTarget, rec.Reason)
			}
		})
	}
}

func TestScheduledAutoscaler_EmptyDaysMatchesAll(t *testing.T) {
	ctx := context.Background()
	reactive := NewReactiveAutoscaler(80, 20)
	schedule := []ScheduleEntry{
		{
			DaysOfWeek: []time.Weekday{}, // Empty means all days
			StartHour:  9,
			EndHour:    17,
			MinNodes:   10,
			MaxNodes:   50,
		},
	}
	as := NewScheduledAutoscaler(schedule, reactive)

	state := PoolState{
		CurrentNodes: 5,
		MinNodes:     1,
		MaxNodes:     100,
		Utilization:  90,
		TimeOfDay:    time.Date(2024, 1, 13, 10, 0, 0, 0, time.UTC), // Saturday
		DayOfWeek:    time.Saturday,
	}

	rec, _ := as.Recommend(ctx, state)
	if rec.TargetNodes != 10 { // MinNodes=10 enforced, scales directly to min
		t.Errorf("Recommend() = %d, want 10", rec.TargetNodes)
	}
}

func TestPredictiveAutoscaler(t *testing.T) {
	ctx := context.Background()
	reactive := NewReactiveAutoscaler(80, 20)

	tests := []struct {
		name       string
		lookback   int
		growth     float64
		fallback   Autoscaler
		state      PoolState
		wantTarget int
	}{
		{
			name:     "insufficient history with fallback",
			lookback: 5,
			growth:   1.5,
			fallback: reactive,
			state: PoolState{
				CurrentNodes:       5,
				MinNodes:           1,
				MaxNodes:           10,
				Utilization:        50,
				UtilizationHistory: []float64{40, 45},
			},
			wantTarget: 5,
		},
		{
			name:     "insufficient history without fallback",
			lookback: 5,
			growth:   1.5,
			fallback: nil,
			state: PoolState{
				CurrentNodes:       5,
				MinNodes:           1,
				MaxNodes:           10,
				Utilization:        50,
				UtilizationHistory: []float64{40, 45},
			},
			wantTarget: 5,
		},
		{
			name:     "rising trend proactive scale up",
			lookback: 5,
			growth:   1.5,
			fallback: reactive,
			state: PoolState{
				CurrentNodes:       5,
				MinNodes:           1,
				MaxNodes:           10,
				Utilization:        70,
				UtilizationHistory: []float64{50, 55, 60, 65, 70},
			},
			wantTarget: 7,
		},
		{
			name:     "respect max limit on prediction",
			lookback: 5,
			growth:   2.0,
			fallback: reactive,
			state: PoolState{
				CurrentNodes:       8,
				MinNodes:           1,
				MaxNodes:           10,
				Utilization:        75,
				UtilizationHistory: []float64{50, 55, 60, 65, 75},
			},
			wantTarget: 10,
		},
		{
			name:     "stable trend uses fallback",
			lookback: 5,
			growth:   1.5,
			fallback: reactive,
			state: PoolState{
				CurrentNodes:       5,
				MinNodes:           1,
				MaxNodes:           10,
				Utilization:        50,
				UtilizationHistory: []float64{50, 50, 50, 50, 50},
			},
			wantTarget: 5, // No trend, fallback reactive sees 50% utilization
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			as := NewPredictiveAutoscaler(tt.lookback, tt.growth, tt.fallback)
			rec, err := as.Recommend(ctx, tt.state)
			if err != nil {
				t.Fatalf("Recommend() error = %v", err)
			}
			if rec.TargetNodes != tt.wantTarget {
				t.Errorf("Recommend() = %d, want %d (reason: %s)", rec.TargetNodes, tt.wantTarget, rec.Reason)
			}
		})
	}
}

func TestCompositeAutoscaler(t *testing.T) {
	ctx := context.Background()
	aggressive := NewReactiveAutoscaler(70, 30)
	conservative := NewReactiveAutoscaler(90, 10)

	state := PoolState{
		CurrentNodes: 5,
		MinNodes:     1,
		MaxNodes:     10,
		Utilization:  75,
	}

	tests := []struct {
		name       string
		mode       CompositeMode
		scalers    []Autoscaler
		wantTarget int
	}{
		{
			name:       "max mode takes highest",
			mode:       ModeMax,
			scalers:    []Autoscaler{aggressive, conservative},
			wantTarget: 6,
		},
		{
			name:       "min mode takes lowest",
			mode:       ModeMin,
			scalers:    []Autoscaler{aggressive, conservative},
			wantTarget: 5,
		},
		{
			name:       "avg mode averages",
			mode:       ModeAvg,
			scalers:    []Autoscaler{aggressive, conservative},
			wantTarget: 5, // (6+5)/2 = 5
		},
		{
			name:       "empty autoscalers returns current",
			mode:       ModeMax,
			scalers:    []Autoscaler{},
			wantTarget: 5,
		},
		{
			name:       "single autoscaler",
			mode:       ModeMax,
			scalers:    []Autoscaler{aggressive},
			wantTarget: 6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			as := NewCompositeAutoscaler(tt.mode, tt.scalers...)
			rec, err := as.Recommend(ctx, state)
			if err != nil {
				t.Fatalf("Recommend() error = %v", err)
			}
			if rec.TargetNodes != tt.wantTarget {
				t.Errorf("Recommend() = %d, want %d (reason: %s)", rec.TargetNodes, tt.wantTarget, rec.Reason)
			}
		})
	}
}

func TestAutoscalerInterface(t *testing.T) {
	var _ Autoscaler = (*ReactiveAutoscaler)(nil)
	var _ Autoscaler = (*QueueBasedAutoscaler)(nil)
	var _ Autoscaler = (*ScheduledAutoscaler)(nil)
	var _ Autoscaler = (*PredictiveAutoscaler)(nil)
	var _ Autoscaler = (*CompositeAutoscaler)(nil)
}
