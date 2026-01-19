package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	controlPlaneAddr string
	outputFormat     string
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "navarch",
		Short: "Navarch GPU fleet management CLI",
		Long:  `Navarch provisions and maintains GPU instances across cloud providers.`,
	}

	// Get default control plane address from env var if set
	defaultAddr := "http://localhost:50051"
	if envAddr := os.Getenv("NAVARCH_CONTROL_PLANE"); envAddr != "" {
		defaultAddr = envAddr
	}

	rootCmd.PersistentFlags().StringVar(&controlPlaneAddr, "control-plane", defaultAddr, "Control plane address (env: NAVARCH_CONTROL_PLANE)")
	rootCmd.PersistentFlags().StringVarP(&outputFormat, "output", "o", "table", "Output format (table, json)")

	rootCmd.AddCommand(listCmd())
	rootCmd.AddCommand(getCmd())
	rootCmd.AddCommand(cordonCmd())
	rootCmd.AddCommand(drainCmd())
	rootCmd.AddCommand(uncordonCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
