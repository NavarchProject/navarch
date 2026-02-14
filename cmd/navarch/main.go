package main

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
)

var (
	controlPlaneAddr string
	outputFormat     string
	requestTimeout   time.Duration
	insecure         bool
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "navarch",
		Short: "Navarch GPU fleet management CLI",
		Long:  `Navarch provisions and maintains GPU instances across cloud providers.`,
	}

	defaultAddr := "http://localhost:50051"
	if envAddr := os.Getenv("NAVARCH_SERVER"); envAddr != "" {
		defaultAddr = envAddr
	}

	rootCmd.PersistentFlags().StringVarP(&controlPlaneAddr, "server", "s", defaultAddr, "Control plane address (env: NAVARCH_SERVER)")
	rootCmd.PersistentFlags().StringVarP(&outputFormat, "output", "o", "table", "Output format (table, json)")
	rootCmd.PersistentFlags().DurationVar(&requestTimeout, "timeout", 30*time.Second, "Request timeout")
	rootCmd.PersistentFlags().BoolVar(&insecure, "insecure", false, "Skip TLS certificate verification")

	rootCmd.AddCommand(listCmd())
	rootCmd.AddCommand(getCmd())
	rootCmd.AddCommand(cordonCmd())
	rootCmd.AddCommand(drainCmd())
	rootCmd.AddCommand(uncordonCmd())
	rootCmd.AddCommand(runCmd())
	rootCmd.AddCommand(devCmd())
	rootCmd.AddCommand(versionCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
