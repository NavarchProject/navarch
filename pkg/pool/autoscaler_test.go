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
		name        string
		state       PoolState
		wantTarget  int
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := as.Recommend(ctx, tt.state)
			if err != nil {
				t.Fatalf("Recommend() error = %v", err)
			}
			if got != tt.wantTarget {
				t.Errorf("Recommend() = %d, want %d", got, tt.wantTarget)
			}
		})
	}
}

func TestQueueBasedAutoscaler(t *testing.T) {
	as := NewQueueBasedAutoscaler(10) // 10 jobs per node
	ctx := context.Background()

	tests := []struct {
		name       string
		state      PoolState
		wantTarget int
	}{
		{
			name: "scale up for queue",
			state: PoolState{
				CurrentNodes: 2,
				MinNodes:     1,
				MaxNodes:     10,
				QueueDepth:   50,
			},
			wantTarget: 5,
		},
		{
			name: "scale down for empty queue",
			state: PoolState{
				CurrentNodes: 5,
				MinNodes:     1,
				MaxNodes:     10,
				QueueDepth:   5,
			},
			wantTarget: 1,
		},
		{
			name: "respect min nodes",
			state: PoolState{
				CurrentNodes: 5,
				MinNodes:     3,
				MaxNodes:     10,
				QueueDepth:   0,
			},
			wantTarget: 3,
		},
		{
			name: "respect max nodes",
			state: PoolState{
				CurrentNodes: 5,
				MinNodes:     1,
				MaxNodes:     10,
				QueueDepth:   200,
			},
			wantTarget: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := as.Recommend(ctx, tt.state)
			if err != nil {
				t.Fatalf("Recommend() error = %v", err)
			}
			if got != tt.wantTarget {
				t.Errorf("Recommend() = %d, want %d", got, tt.wantTarget)
			}
		})
	}
}

func TestScheduledAutoscaler(t *testing.T) {
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
	as := NewScheduledAutoscaler(schedule, reactive)
	ctx := context.Background()

	tests := []struct {
		name       string
		state      PoolState
		wantTarget int
	}{
		{
			name: "business hours scale up",
			state: PoolState{
				CurrentNodes: 5,
				MinNodes:     1,
				MaxNodes:     10,
				Utilization:  90,
				TimeOfDay:    time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC), // Monday 10am
				DayOfWeek:    time.Monday,
			},
			wantTarget: 6,
		},
		{
			name: "weekend limited max",
			state: PoolState{
				CurrentNodes: 3,
				MinNodes:     1,
				MaxNodes:     100,
				Utilization:  90,
				TimeOfDay:    time.Date(2024, 1, 13, 10, 0, 0, 0, time.UTC), // Saturday
				DayOfWeek:    time.Saturday,
			},
			wantTarget: 4, // Would be more but schedule limits to 5
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := as.Recommend(ctx, tt.state)
			if err != nil {
				t.Fatalf("Recommend() error = %v", err)
			}
			if got != tt.wantTarget {
				t.Errorf("Recommend() = %d, want %d", got, tt.wantTarget)
			}
		})
	}
}

func TestPredictiveAutoscaler(t *testing.T) {
	reactive := NewReactiveAutoscaler(80, 20)
	as := NewPredictiveAutoscaler(5, 1.5, reactive)
	ctx := context.Background()

	tests := []struct {
		name       string
		state      PoolState
		wantTarget int
	}{
		{
			name: "insufficient history falls back",
			state: PoolState{
				CurrentNodes:       5,
				MinNodes:           1,
				MaxNodes:           10,
				Utilization:        50,
				UtilizationHistory: []float64{40, 45},
			},
			wantTarget: 5, // Falls back to reactive, no change
		},
		{
			name: "proactive scale up on rising trend",
			state: PoolState{
				CurrentNodes:       5,
				MinNodes:           1,
				MaxNodes:           10,
				Utilization:        70,
				UtilizationHistory: []float64{50, 55, 60, 65, 70},
			},
			wantTarget: 7, // Predicts growth and scales proactively
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := as.Recommend(ctx, tt.state)
			if err != nil {
				t.Fatalf("Recommend() error = %v", err)
			}
			if got != tt.wantTarget {
				t.Errorf("Recommend() = %d, want %d", got, tt.wantTarget)
			}
		})
	}
}

func TestCompositeAutoscaler(t *testing.T) {
	ctx := context.Background()

	// Two autoscalers with different thresholds
	aggressive := NewReactiveAutoscaler(70, 30) // Scales up earlier
	conservative := NewReactiveAutoscaler(90, 10) // Scales up later

	state := PoolState{
		CurrentNodes: 5,
		MinNodes:     1,
		MaxNodes:     10,
		Utilization:  75,
	}

	t.Run("max mode takes highest", func(t *testing.T) {
		as := NewCompositeAutoscaler(ModeMax, aggressive, conservative)
		got, _ := as.Recommend(ctx, state)
		if got != 6 { // Aggressive wants 6, conservative wants 5
			t.Errorf("Recommend() = %d, want 6", got)
		}
	})

	t.Run("min mode takes lowest", func(t *testing.T) {
		as := NewCompositeAutoscaler(ModeMin, aggressive, conservative)
		got, _ := as.Recommend(ctx, state)
		if got != 5 { // Conservative wants 5
			t.Errorf("Recommend() = %d, want 5", got)
		}
	})
}

func TestAutoscalerInterface(t *testing.T) {
	var _ Autoscaler = (*ReactiveAutoscaler)(nil)
	var _ Autoscaler = (*QueueBasedAutoscaler)(nil)
	var _ Autoscaler = (*ScheduledAutoscaler)(nil)
	var _ Autoscaler = (*PredictiveAutoscaler)(nil)
	var _ Autoscaler = (*CompositeAutoscaler)(nil)
}

