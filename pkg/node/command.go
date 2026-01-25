package node

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	pb "github.com/NavarchProject/navarch/proto"
)

// CommandHandler processes commands received from the control plane.
type CommandHandler interface {
	// Handle executes the command and returns an error if it fails.
	Handle(ctx context.Context, cmd *pb.NodeCommand) error
}

// CommandDispatcher routes commands to their appropriate handlers.
type CommandDispatcher struct {
	handlers map[pb.NodeCommandType]CommandHandler
	logger   *slog.Logger
	mu       sync.RWMutex

	// Node state that commands can modify
	cordoned bool
	draining bool
}

// NewCommandDispatcher creates a new command dispatcher with default handlers.
func NewCommandDispatcher(logger *slog.Logger) *CommandDispatcher {
	d := &CommandDispatcher{
		handlers: make(map[pb.NodeCommandType]CommandHandler),
		logger:   logger,
	}

	// Register default handlers
	d.RegisterHandler(pb.NodeCommandType_NODE_COMMAND_TYPE_CORDON, &CordonHandler{dispatcher: d})
	d.RegisterHandler(pb.NodeCommandType_NODE_COMMAND_TYPE_DRAIN, &DrainHandler{dispatcher: d})
	d.RegisterHandler(pb.NodeCommandType_NODE_COMMAND_TYPE_TERMINATE, &TerminateHandler{dispatcher: d})
	d.RegisterHandler(pb.NodeCommandType_NODE_COMMAND_TYPE_RUN_DIAGNOSTIC, &DiagnosticHandler{dispatcher: d})

	return d
}

// RegisterHandler registers a handler for a command type.
func (d *CommandDispatcher) RegisterHandler(cmdType pb.NodeCommandType, handler CommandHandler) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.handlers[cmdType] = handler
}

// Dispatch routes a command to its handler and returns the result.
func (d *CommandDispatcher) Dispatch(ctx context.Context, cmd *pb.NodeCommand) error {
	d.mu.RLock()
	handler, ok := d.handlers[cmd.Type]
	d.mu.RUnlock()

	if !ok {
		return fmt.Errorf("no handler registered for command type: %s", cmd.Type.String())
	}

	d.logger.InfoContext(ctx, "executing command",
		slog.String("command_id", cmd.CommandId),
		slog.String("type", cmd.Type.String()),
	)

	if err := handler.Handle(ctx, cmd); err != nil {
		d.logger.ErrorContext(ctx, "command failed",
			slog.String("command_id", cmd.CommandId),
			slog.String("type", cmd.Type.String()),
			slog.String("error", err.Error()),
		)
		return err
	}

	d.logger.InfoContext(ctx, "command completed",
		slog.String("command_id", cmd.CommandId),
		slog.String("type", cmd.Type.String()),
	)

	return nil
}

// IsCordoned returns whether the node is currently cordoned.
func (d *CommandDispatcher) IsCordoned() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.cordoned
}

// IsDraining returns whether the node is currently draining.
func (d *CommandDispatcher) IsDraining() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.draining
}

// CordonHandler handles cordon commands.
type CordonHandler struct {
	dispatcher *CommandDispatcher
}

func (h *CordonHandler) Handle(ctx context.Context, cmd *pb.NodeCommand) error {
	h.dispatcher.mu.Lock()
	defer h.dispatcher.mu.Unlock()

	// Check if we should uncordon (parameter "uncordon" = "true")
	if cmd.Parameters != nil && cmd.Parameters["uncordon"] == "true" {
		h.dispatcher.cordoned = false
		h.dispatcher.logger.InfoContext(ctx, "node uncordoned")
		return nil
	}

	h.dispatcher.cordoned = true
	h.dispatcher.logger.InfoContext(ctx, "node cordoned - no new workloads will be scheduled")
	return nil
}

// DrainHandler handles drain commands.
type DrainHandler struct {
	dispatcher *CommandDispatcher
}

func (h *DrainHandler) Handle(ctx context.Context, cmd *pb.NodeCommand) error {
	h.dispatcher.mu.Lock()
	h.dispatcher.draining = true
	h.dispatcher.cordoned = true // Draining implies cordoned
	h.dispatcher.mu.Unlock()

	h.dispatcher.logger.InfoContext(ctx, "node draining - waiting for workloads to complete")

	// In a real implementation, this would:
	// 1. Signal workload orchestrator to stop scheduling new work
	// 2. Wait for existing workloads to complete or timeout
	// 3. Report completion

	// For now, we just set the state. A real implementation would
	// integrate with whatever workload management system is in use.

	return nil
}

// TerminateHandler handles terminate commands.
type TerminateHandler struct {
	dispatcher *CommandDispatcher
}

func (h *TerminateHandler) Handle(ctx context.Context, cmd *pb.NodeCommand) error {
	h.dispatcher.mu.Lock()
	h.dispatcher.cordoned = true
	h.dispatcher.draining = true
	h.dispatcher.mu.Unlock()

	h.dispatcher.logger.InfoContext(ctx, "node preparing for termination")

	// In a real implementation, this would:
	// 1. Cordon the node
	// 2. Drain workloads
	// 3. Sync any local state
	// 4. Prepare for shutdown

	// The actual termination is typically handled by the cloud provider
	// This command just prepares the node gracefully

	return nil
}

// DiagnosticHandler handles diagnostic commands.
type DiagnosticHandler struct {
	dispatcher *CommandDispatcher
}

func (h *DiagnosticHandler) Handle(ctx context.Context, cmd *pb.NodeCommand) error {
	testName := "all"
	if cmd.Parameters != nil {
		if t, ok := cmd.Parameters["test"]; ok {
			testName = t
		}
	}

	h.dispatcher.logger.InfoContext(ctx, "running diagnostics",
		slog.String("test", testName),
	)

	// In a real implementation, this would run actual diagnostic tests
	// For now, we just log the request

	return nil
}
