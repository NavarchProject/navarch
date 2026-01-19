package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// SimulatorHandler is a human-friendly log handler for the simulator.
type SimulatorHandler struct {
	mu     sync.Mutex
	out    io.Writer
	level  slog.Level
	attrs  []slog.Attr
	groups []string
}

// NewSimulatorHandler creates a new human-friendly log handler.
func NewSimulatorHandler(out io.Writer, level slog.Level) *SimulatorHandler {
	return &SimulatorHandler{
		out:   out,
		level: level,
	}
}

func (h *SimulatorHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *SimulatorHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	var buf strings.Builder

	buf.WriteString(r.Time.Format("15:04:05"))
	buf.WriteString(" ")

	emoji := getEmoji(r.Level, r.Message)
	buf.WriteString(emoji)
	buf.WriteString(" ")

	buf.WriteString(r.Message)

	var attrs []string
	for _, a := range h.attrs {
		if s := formatAttr(a); s != "" {
			attrs = append(attrs, s)
		}
	}
	r.Attrs(func(a slog.Attr) bool {
		if s := formatAttr(a); s != "" {
			attrs = append(attrs, s)
		}
		return true
	})

	if len(attrs) > 0 {
		buf.WriteString(" (")
		buf.WriteString(strings.Join(attrs, ", "))
		buf.WriteString(")")
	}

	buf.WriteString("\n")

	_, err := h.out.Write([]byte(buf.String()))
	return err
}

func (h *SimulatorHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	h2 := &SimulatorHandler{
		out:    h.out,
		level:  h.level,
		attrs:  make([]slog.Attr, len(h.attrs)+len(attrs)),
		groups: h.groups,
	}
	copy(h2.attrs, h.attrs)
	copy(h2.attrs[len(h.attrs):], attrs)
	return h2
}

func (h *SimulatorHandler) WithGroup(name string) slog.Handler {
	h2 := &SimulatorHandler{
		out:    h.out,
		level:  h.level,
		attrs:  h.attrs,
		groups: append(h.groups, name),
	}
	return h2
}

func getEmoji(level slog.Level, msg string) string {
	if level == slog.LevelError {
		return "❌"
	}
	if level == slog.LevelWarn {
		return "⚠️ "
	}

	msgLower := strings.ToLower(msg)

	switch {
	case strings.Contains(msgLower, "completed successfully"),
		strings.Contains(msgLower, "passed"),
		strings.Contains(msgLower, "registered"):
		return "✅"
	case strings.Contains(msgLower, "started"):
		return "🚀"
	case strings.Contains(msgLower, "healthy"):
		return "💚"
	case strings.Contains(msgLower, "testing"):
		return "🧪"
	case strings.Contains(msgLower, "recovered"):
		return "🔧"
	case strings.Contains(msgLower, "failed"),
		strings.Contains(msgLower, "error"):
		return "❌"
	case strings.Contains(msgLower, "unhealthy"):
		return "🔴"
	case strings.Contains(msgLower, "inject"):
		return "💉"
	case strings.Contains(msgLower, "failure"):
		return "💥"
	case strings.Contains(msgLower, "cordon"):
		return "🚧"
	case strings.Contains(msgLower, "drain"):
		return "🔄"
	case strings.Contains(msgLower, "command"):
		return "📤"
	case strings.Contains(msgLower, "executing event"):
		return "▶️ "
	case strings.Contains(msgLower, "waiting"):
		return "⏳"
	case strings.Contains(msgLower, "reached"):
		return "🎯"
	case strings.Contains(msgLower, "scenario"):
		return "📋"
	case strings.Contains(msgLower, "control plane"),
		strings.Contains(msgLower, "fleet"):
		return "🖥️ "
	case strings.Contains(msgLower, "node"):
		return "📦"
	default:
		if level == slog.LevelDebug {
			return "🔍"
		}
		return "ℹ️ "
	}
}

func formatAttr(a slog.Attr) string {
	key := a.Key
	val := a.Value

	if val.Kind() == slog.KindString && val.String() == "" {
		return ""
	}

	switch val.Kind() {
	case slog.KindDuration:
		d := val.Duration()
		if d < time.Second {
			return fmt.Sprintf("%s=%dms", key, d.Milliseconds())
		}
		return fmt.Sprintf("%s=%s", key, d.Round(time.Millisecond))
	case slog.KindTime:
		return fmt.Sprintf("%s=%s", key, val.Time().Format("15:04:05"))
	case slog.KindInt64:
		return fmt.Sprintf("%s=%d", key, val.Int64())
	case slog.KindString:
		s := val.String()
		if !strings.Contains(s, " ") && !strings.Contains(s, ",") {
			return fmt.Sprintf("%s=%s", key, s)
		}
		return fmt.Sprintf("%s=%q", key, s)
	default:
		return fmt.Sprintf("%s=%v", key, val.Any())
	}
}
