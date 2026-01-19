package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/NavarchProject/navarch/pkg/simulator"
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

var (
	verbose bool
	debug   bool
)

var rootCmd = &cobra.Command{
	Use:   "simulator",
	Short: "Navarch fleet simulator for testing and development",
	Long: `The Navarch simulator creates a simulated GPU fleet and control plane
for testing health checks, failure scenarios, and command flows.

Run scenarios from YAML files or use interactive mode to manually
inject failures and observe system behavior.`,
}

var runCmd = &cobra.Command{
	Use:   "run <scenario.yaml>",
	Short: "Run a simulation scenario",
	Long: `Run a simulation scenario from a YAML file.

The scenario defines the fleet configuration, events to execute,
and assertions to verify at the end.

Example:
  simulator run scenarios/gpu-failure.yaml`,
	Args: cobra.ExactArgs(1),
	RunE: runScenario,
}

var interactiveCmd = &cobra.Command{
	Use:   "interactive",
	Short: "Start an interactive simulation session",
	Long: `Start an interactive simulation session where you can manually
control the fleet and inject failures in real-time.

This starts a control plane and a default fleet, then presents
an interactive prompt for commands.`,
	RunE: runInteractive,
}

var validateCmd = &cobra.Command{
	Use:   "validate <scenario.yaml>",
	Short: "Validate a scenario file without running it",
	Args:  cobra.ExactArgs(1),
	RunE:  validateScenario,
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output")
	rootCmd.PersistentFlags().BoolVar(&debug, "debug", false, "Enable debug output")

	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(interactiveCmd)
	rootCmd.AddCommand(validateCmd)
}

func setupLogger() *slog.Logger {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	} else if verbose {
		level = slog.LevelInfo
	} else {
		level = slog.LevelWarn
	}

	opts := &slog.HandlerOptions{
		Level: level,
	}
	handler := slog.NewTextHandler(os.Stdout, opts)
	return slog.New(handler)
}

func runScenario(cmd *cobra.Command, args []string) error {
	logger := setupLogger()

	scenario, err := simulator.LoadScenario(args[0])
	if err != nil {
		return fmt.Errorf("failed to load scenario: %w", err)
	}

	logger.Info("loaded scenario",
		slog.String("name", scenario.Name),
		slog.String("description", scenario.Description),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle interrupt
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		logger.Info("received interrupt, shutting down...")
		cancel()
	}()

	runner := simulator.NewRunner(scenario, simulator.WithLogger(logger))
	if err := runner.Run(ctx); err != nil {
		return fmt.Errorf("scenario failed: %w", err)
	}

	return nil
}

func runInteractive(cmd *cobra.Command, args []string) error {
	logger := setupLogger()

	// Create a default fleet for interactive mode
	scenario := &simulator.Scenario{
		Name:        "interactive",
		Description: "Interactive simulation session",
		Fleet: []simulator.NodeSpec{
			{
				ID:           "node-1",
				Provider:     "gcp",
				Region:       "us-central1",
				Zone:         "us-central1-a",
				InstanceType: "a3-highgpu-8g",
				GPUCount:     8,
				GPUType:      "NVIDIA H100 80GB HBM3",
			},
			{
				ID:           "node-2",
				Provider:     "gcp",
				Region:       "us-central1",
				Zone:         "us-central1-b",
				InstanceType: "a3-highgpu-8g",
				GPUCount:     8,
				GPUType:      "NVIDIA H100 80GB HBM3",
			},
		},
		Events: []simulator.Event{
			{At: simulator.Duration(0), Action: "start_fleet"},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle interrupt
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		logger.Info("received interrupt, shutting down...")
		cancel()
	}()

	runner := simulator.NewRunner(scenario, simulator.WithLogger(logger), simulator.WithWaitForCancel())

	// Start the simulation
	go func() {
		if err := runner.Run(ctx); err != nil && ctx.Err() == nil {
			logger.Error("simulation error", slog.String("error", err.Error()))
		}
	}()

	fmt.Println()
	fmt.Println("Navarch Fleet Simulator - Interactive Mode")
	fmt.Println("==========================================")
	fmt.Println()
	fmt.Println("Control plane running at http://localhost:8080")
	fmt.Println("Fleet nodes: node-1, node-2")
	fmt.Println()
	fmt.Println("Use the navarch CLI to interact with the fleet:")
	fmt.Println("  navarch list -s http://localhost:8080")
	fmt.Println("  navarch get node-1 -s http://localhost:8080")
	fmt.Println("  navarch cordon node-1 -s http://localhost:8080")
	fmt.Println()
	fmt.Println("Press Ctrl+C to stop the simulation")
	fmt.Println()

	// Wait for context cancellation
	<-ctx.Done()
	return nil
}

func validateScenario(cmd *cobra.Command, args []string) error {
	scenario, err := simulator.LoadScenario(args[0])
	if err != nil {
		return err
	}

	fmt.Printf("Scenario: %s\n", scenario.Name)
	fmt.Printf("Description: %s\n", scenario.Description)
	fmt.Printf("Fleet size: %d nodes\n", len(scenario.Fleet))
	fmt.Printf("Events: %d\n", len(scenario.Events))
	fmt.Printf("Assertions: %d\n", len(scenario.Assertions))
	fmt.Println()
	fmt.Println("Fleet:")
	for _, node := range scenario.Fleet {
		fmt.Printf("  - %s (%s, %s, %d GPUs)\n", node.ID, node.Provider, node.InstanceType, node.GPUCount)
	}
	fmt.Println()
	fmt.Println("Events:")
	for _, event := range scenario.Events {
		fmt.Printf("  - %s: %s", event.At.Duration(), event.Action)
		if event.Target != "" {
			fmt.Printf(" -> %s", event.Target)
		}
		fmt.Println()
	}
	fmt.Println()
	fmt.Println("Scenario is valid.")

	return nil
}

