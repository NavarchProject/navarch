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

	rootCmd.PersistentFlags().StringVar(&controlPlaneAddr, "control-plane", "http://localhost:50051", "Control plane address")
	rootCmd.PersistentFlags().StringVarP(&outputFormat, "output", "o", "table", "Output format (table, json, yaml)")

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
