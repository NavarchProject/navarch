package notify

import "context"

// Event represents a notification event.
type Event struct {
	Type      string
	Message   string
	Timestamp int64
}

// Notifier sends alerts when notable events occur.
type Notifier interface {
	Notify(ctx context.Context, event Event) error
}