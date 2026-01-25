package node

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	pb "github.com/NavarchProject/navarch/proto"
)

// CommandHandler processes commands received from the control plane.
type CommandHandler interface {
	// Handle executes the command and returns an error if it fails.
	Handle(ctx context.Context, cmd *pb.NodeCommand) error
}

// ShutdownFunc is called to initiate node shutdown.
type ShutdownFunc func(ctx context.Context, force bool) error

// WorkloadDrainFunc is called to drain workloads from the node.
// It should return when all workloads have completed or been terminated.
type WorkloadDrainFunc func(ctx context.Context, timeout time.Duration, force bool) error

// CommandDispatcher routes commands to their appropriate handlers.
type CommandDispatcher struct {
	handlers map[pb.NodeCommandType]CommandHandler
	logger   *slog.Logger
	mu       sync.RWMutex

	// Node state that commands can modify
	cordoned bool
	draining bool

	// Callbacks for node lifecycle operations
	shutdownFunc      ShutdownFunc
	workloadDrainFunc WorkloadDrainFunc
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

// SetShutdownFunc sets the callback for node shutdown.
func (d *CommandDispatcher) SetShutdownFunc(fn ShutdownFunc) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.shutdownFunc = fn
}

// SetWorkloadDrainFunc sets the callback for draining workloads.
func (d *CommandDispatcher) SetWorkloadDrainFunc(fn WorkloadDrainFunc) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.workloadDrainFunc = fn
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
// Parameters:
//   - timeout: drain timeout in seconds (default: 300)
//   - force: if "true", forcefully terminate workloads after timeout
type DrainHandler struct {
	dispatcher *CommandDispatcher
}

func (h *DrainHandler) Handle(ctx context.Context, cmd *pb.NodeCommand) error {
	h.dispatcher.mu.Lock()
	h.dispatcher.draining = true
	h.dispatcher.cordoned = true // Draining implies cordoned
	drainFunc := h.dispatcher.workloadDrainFunc
	h.dispatcher.mu.Unlock()

	// Parse parameters
	timeout := 5 * time.Minute
	force := false
	if cmd.Parameters != nil {
		if t, ok := cmd.Parameters["timeout"]; ok {
			if d, err := time.ParseDuration(t + "s"); err == nil {
				timeout = d
			}
		}
		force = cmd.Parameters["force"] == "true"
	}

	h.dispatcher.logger.InfoContext(ctx, "node draining - waiting for workloads to complete",
		slog.Duration("timeout", timeout),
		slog.Bool("force", force),
	)

	// Call the workload drain function if registered
	if drainFunc != nil {
		if err := drainFunc(ctx, timeout, force); err != nil {
			return fmt.Errorf("draining workloads: %w", err)
		}
	}

	h.dispatcher.logger.InfoContext(ctx, "drain completed")
	return nil
}

// TerminateHandler handles terminate commands.
// Parameters:
//   - force: if "true", skip graceful drain and terminate immediately
//   - timeout: drain timeout in seconds before forcing (default: 300)
//   - exit: if "true", call os.Exit after termination (default: "true")
type TerminateHandler struct {
	dispatcher *CommandDispatcher
}

func (h *TerminateHandler) Handle(ctx context.Context, cmd *pb.NodeCommand) error {
	// Parse parameters
	force := false
	timeout := 5 * time.Minute
	shouldExit := true
	if cmd.Parameters != nil {
		force = cmd.Parameters["force"] == "true"
		if t, ok := cmd.Parameters["timeout"]; ok {
			if d, err := time.ParseDuration(t + "s"); err == nil {
				timeout = d
			}
		}
		if cmd.Parameters["exit"] == "false" {
			shouldExit = false
		}
	}

	h.dispatcher.mu.Lock()
	h.dispatcher.cordoned = true
	h.dispatcher.draining = true
	drainFunc := h.dispatcher.workloadDrainFunc
	shutdownFunc := h.dispatcher.shutdownFunc
	h.dispatcher.mu.Unlock()

	h.dispatcher.logger.InfoContext(ctx, "node preparing for termination",
		slog.Bool("force", force),
		slog.Duration("timeout", timeout),
	)

	// Step 1: Cordon the node (already done above)
	h.dispatcher.logger.InfoContext(ctx, "step 1/4: node cordoned")

	// Step 2: Drain workloads (unless force)
	if !force && drainFunc != nil {
		h.dispatcher.logger.InfoContext(ctx, "step 2/4: draining workloads")
		if err := drainFunc(ctx, timeout, false); err != nil {
			h.dispatcher.logger.WarnContext(ctx, "graceful drain failed, forcing",
				slog.String("error", err.Error()),
			)
			// Force drain on failure
			if err := drainFunc(ctx, 30*time.Second, true); err != nil {
				h.dispatcher.logger.ErrorContext(ctx, "force drain failed",
					slog.String("error", err.Error()),
				)
			}
		}
	} else if force {
		h.dispatcher.logger.InfoContext(ctx, "step 2/4: skipping graceful drain (force mode)")
		if drainFunc != nil {
			// Quick force drain
			_ = drainFunc(ctx, 10*time.Second, true)
		}
	} else {
		h.dispatcher.logger.InfoContext(ctx, "step 2/4: no drain function registered, skipping")
	}

	// Step 3: Call shutdown function if registered
	h.dispatcher.logger.InfoContext(ctx, "step 3/4: executing shutdown callback")
	if shutdownFunc != nil {
		if err := shutdownFunc(ctx, force); err != nil {
			h.dispatcher.logger.ErrorContext(ctx, "shutdown callback failed",
				slog.String("error", err.Error()),
			)
			// Continue anyway - we're terminating
		}
	}

	// Step 4: Exit the process if requested
	h.dispatcher.logger.InfoContext(ctx, "step 4/4: termination complete")
	if shouldExit {
		h.dispatcher.logger.InfoContext(ctx, "exiting process")
		os.Exit(0)
	}

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
