package notify

import (
	"context"
	"log/slog"
)

// LogNotifier writes notifications using structured logging.
type LogNotifier struct {
	logger *slog.Logger
}

// NewLogNotifier creates a new log notifier.
// If logger is nil, a default logger is used.
func NewLogNotifier(logger *slog.Logger) *LogNotifier {
	if logger == nil {
		logger = slog.Default()
	}
	return &LogNotifier{logger: logger}
}

// Notify writes the event using structured logging.
func (n *LogNotifier) Notify(ctx context.Context, event Event) error {
	n.logger.InfoContext(ctx, "notification",
		slog.String("type", event.Type),
		slog.String("message", event.Message),
	)
	return nil
}