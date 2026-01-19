package scheduler

import "context"

// FirstAvailable is a simple scheduler that returns a constant score.
type FirstAvailable struct{}

// NewFirstAvailable creates a new FirstAvailable scheduler.
func NewFirstAvailable() *FirstAvailable {
	return &FirstAvailable{}
}

// Score returns a constant score for any option.
func (s *FirstAvailable) Score(ctx context.Context, option ProvisionOption) (float64, error) {
	return 1.0, nil
}