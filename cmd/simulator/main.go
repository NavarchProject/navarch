package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

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
	verbose   bool
	debug     bool
	seed      int64
	keepAlive bool
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

For stress test scenarios, the scenario file includes a 'stress' section
that configures fleet generation, chaos injection, and test duration.

Examples:
  # Run a regular scenario
  simulator run scenarios/gpu-failure.yaml

  # Run a stress test
  simulator run scenarios/stress/1000-node-chaos.yaml -v

  # Run with reproducible randomness
  simulator run scenarios/stress/1000-node-chaos.yaml --seed 12345`,
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

	runCmd.Flags().Int64Var(&seed, "seed", 0, "Random seed for reproducible stress tests (0 = random)")
	runCmd.Flags().BoolVar(&keepAlive, "keep-alive", false, "Keep server running after scenario completes")

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

	handler := NewSimulatorHandler(os.Stdout, level)
	return slog.New(handler)
}

func runScenario(cmd *cobra.Command, args []string) error {
	logger := setupLogger()

	scenario, err := simulator.LoadScenario(args[0])
	if err != nil {
		return fmt.Errorf("failed to load scenario: %w", err)
	}

	// Log scenario info
	if scenario.IsStressTest() {
		logger.Info("loaded stress test scenario",
			slog.String("name", scenario.Name),
			slog.Duration("duration", scenario.GetEffectiveDuration()),
		)
		if scenario.Stress.FleetGen != nil {
			logger.Info("fleet configuration",
				slog.Int("total_nodes", scenario.Stress.FleetGen.TotalNodes),
				slog.Int("templates", len(scenario.Stress.FleetGen.Templates)),
			)
		}
		if scenario.Stress.Chaos != nil && scenario.Stress.Chaos.Enabled {
			logger.Info("chaos configuration",
				slog.Float64("failure_rate", scenario.Stress.Chaos.FailureRate),
				slog.Bool("cascading", scenario.Stress.Chaos.Cascading != nil && scenario.Stress.Chaos.Cascading.Enabled),
			)
		}
	} else {
		logger.Info("loaded scenario",
			slog.String("name", scenario.Name),
			slog.String("description", scenario.Description),
		)
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

	// Create runner with options
	opts := []simulator.RunnerOption{
		simulator.WithLogger(logger),
	}

	if keepAlive {
		opts = append(opts, simulator.WithWaitForCancel())
	}

	// Use seed from flag or scenario
	effectiveSeed := seed
	if effectiveSeed == 0 && scenario.Stress != nil && scenario.Stress.Seed != 0 {
		effectiveSeed = scenario.Stress.Seed
	}
	if effectiveSeed != 0 {
		opts = append(opts, simulator.WithSeed(effectiveSeed))
		logger.Info("using seed", slog.Int64("seed", effectiveSeed))
	}

	runner := simulator.NewRunner(scenario, opts...)
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

	// Wait for server to start
	time.Sleep(500 * time.Millisecond)

	// Get actual address (may differ from 8080 if port was in use)
	addr := runner.ControlPlaneAddr()

	fmt.Println()
	fmt.Println("Navarch Fleet Simulator - Interactive Mode")
	fmt.Println("==========================================")
	fmt.Println()
	fmt.Printf("Control plane running at %s\n", addr)
	fmt.Println("Fleet nodes: node-1, node-2")
	fmt.Println()
	fmt.Println("Use the navarch CLI to interact with the fleet:")
	fmt.Printf("  navarch list -s %s\n", addr)
	fmt.Printf("  navarch get node-1 -s %s\n", addr)
	fmt.Printf("  navarch cordon node-1 -s %s\n", addr)
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
	fmt.Println()

	// Check if this is a stress test
	if scenario.IsStressTest() {
		fmt.Println("Type: STRESS TEST")
		fmt.Printf("Duration: %s\n", scenario.GetEffectiveDuration())
		fmt.Println()

		stress := scenario.Stress
		if stress.FleetGen != nil {
			fmt.Println("Fleet Generation:")
			fmt.Printf("  Total Nodes: %d\n", stress.FleetGen.TotalNodes)
			fmt.Printf("  Templates: %d\n", len(stress.FleetGen.Templates))
			for _, t := range stress.FleetGen.Templates {
				fmt.Printf("    - %s (weight: %d, %d GPUs, %s)\n", t.Name, t.Weight, t.GPUCount, t.GPUType)
			}
			if len(stress.FleetGen.Providers) > 0 {
				fmt.Printf("  Providers: %v\n", stress.FleetGen.Providers)
			}
			if len(stress.FleetGen.Regions) > 0 {
				fmt.Printf("  Regions: %v\n", stress.FleetGen.Regions)
			}
			if stress.FleetGen.Startup.Pattern != "" {
				fmt.Printf("  Startup: %s over %s\n", stress.FleetGen.Startup.Pattern, stress.FleetGen.Startup.Duration.Duration())
			}
			fmt.Println()
		}

		if stress.Chaos != nil && stress.Chaos.Enabled {
			fmt.Println("Chaos Configuration:")
			fmt.Printf("  Failure Rate: %.1f per minute per 1000 nodes\n", stress.Chaos.FailureRate)
			if len(stress.Chaos.XIDDistribution) > 0 {
				fmt.Printf("  XID Codes: %d types configured\n", len(stress.Chaos.XIDDistribution))
			}
			if len(stress.Chaos.FailureTypes) > 0 {
				fmt.Printf("  Failure Types: ")
				for i, ft := range stress.Chaos.FailureTypes {
					if i > 0 {
						fmt.Printf(", ")
					}
					fmt.Printf("%s(%d)", ft.Type, ft.Weight)
				}
				fmt.Println()
			}
			if stress.Chaos.Cascading != nil && stress.Chaos.Cascading.Enabled {
				fmt.Printf("  Cascading: enabled (%.0f%% probability, max depth %d, scope: %s)\n",
					stress.Chaos.Cascading.Probability*100,
					stress.Chaos.Cascading.MaxDepth,
					stress.Chaos.Cascading.Scope)
			}
			if stress.Chaos.Recovery != nil && stress.Chaos.Recovery.Enabled {
				fmt.Printf("  Recovery: enabled (%.0f%% probability, mean: %s)\n",
					stress.Chaos.Recovery.Probability*100,
					stress.Chaos.Recovery.MeanTime.Duration())
			}
			if len(stress.Chaos.ScheduledOutages) > 0 {
				fmt.Printf("  Scheduled Outages: %d\n", len(stress.Chaos.ScheduledOutages))
				for _, o := range stress.Chaos.ScheduledOutages {
					fmt.Printf("    - %s at %s for %s (%s: %s)\n", o.Name, o.StartTime.Duration(), o.Duration.Duration(), o.Scope, o.Target)
				}
			}
			fmt.Println()
		}

		if stress.ReportFile != "" {
			fmt.Printf("Report: %s\n", stress.ReportFile)
			fmt.Println()
		}
	} else {
		fmt.Println("Type: Standard Scenario")
		fmt.Printf("Fleet size: %d nodes\n", len(scenario.Fleet))
		fmt.Println()
		fmt.Println("Fleet:")
		for _, node := range scenario.Fleet {
			fmt.Printf("  - %s (%s, %s, %d GPUs)\n", node.ID, node.Provider, node.InstanceType, node.GPUCount)
		}
		fmt.Println()
	}

	fmt.Printf("Events: %d\n", len(scenario.Events))
	for _, event := range scenario.Events {
		fmt.Printf("  - %s: %s", event.At.Duration(), event.Action)
		if event.Target != "" {
			fmt.Printf(" -> %s", event.Target)
		}
		fmt.Println()
	}
	fmt.Println()

	if len(scenario.Assertions) > 0 {
		fmt.Printf("Assertions: %d\n", len(scenario.Assertions))
		for _, a := range scenario.Assertions {
			fmt.Printf("  - %s: %s = %s\n", a.Type, a.Target, a.Expected)
		}
		fmt.Println()
	}

	fmt.Println("Scenario is valid.")

	return nil
}

