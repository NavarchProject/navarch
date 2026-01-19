package main

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

var (
	// Set via ldflags at build time
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

func versionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("navarch version %s\n", version)
			fmt.Printf("  commit:     %s\n", commit)
			fmt.Printf("  built:      %s\n", buildDate)
			fmt.Printf("  go version: %s\n", runtime.Version())
			fmt.Printf("  platform:   %s/%s\n", runtime.GOOS, runtime.GOARCH)
		},
	}

	return cmd
}

