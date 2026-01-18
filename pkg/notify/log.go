package notify

import (
	"context"
	"fmt"
	"log"
)

// LogNotifier writes notifications to stdout.
type LogNotifier struct{}

// NewLogNotifier creates a new log notifier.
func NewLogNotifier() *LogNotifier {
	return &LogNotifier{}
}

// Notify writes the event to stdout.
func (n *LogNotifier) Notify(ctx context.Context, event Event) error {
	log.Printf("[NOTIFY] %s: %s", event.Type, event.Message)
	return nil
}

