package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/NavarchProject/navarch/pkg/node"
)

func main() {
	controlPlaneAddr := flag.String("server", "http://localhost:50051", "Control plane address")
	nodeID := flag.String("node-id", "", "Node ID (defaults to hostname)")
	provider := flag.String("provider", "gcp", "Cloud provider")
	region := flag.String("region", "", "Cloud region")
	zone := flag.String("zone", "", "Cloud zone")
	instanceType := flag.String("instance-type", "", "Instance type")
	authToken := flag.String("auth-token", "", "Authentication token (or use NAVARCH_AUTH_TOKEN env)")
	flag.Parse()

	// Get auth token from flag or environment
	token := *authToken
	if token == "" {
		token = os.Getenv("NAVARCH_AUTH_TOKEN")
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	if *nodeID == "" {
		hostname, err := os.Hostname()
		if err != nil {
			logger.Error("failed to get hostname", slog.String("error", err.Error()))
			os.Exit(1)
		}
		*nodeID = hostname
	}

	logger.Info("starting Navarch Node Daemon",
		slog.String("node_id", *nodeID),
		slog.String("control_plane", *controlPlaneAddr),
	)

	cfg := node.Config{
		ControlPlaneAddr: *controlPlaneAddr,
		NodeID:           *nodeID,
		Provider:         *provider,
		Region:           *region,
		Zone:             *zone,
		InstanceType:     *instanceType,
		AuthToken:        token,
	}

	n, err := node.New(cfg, logger)
	if err != nil {
		logger.Error("failed to create node", slog.String("error", err.Error()))
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := n.Start(ctx); err != nil {
		logger.Error("failed to start node", slog.String("error", err.Error()))
		os.Exit(1)
	}

	logger.Info("node daemon started successfully")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	<-sigChan
	logger.Info("shutting down node daemon")
	cancel()

	if err := n.Stop(); err != nil {
		logger.Error("error during shutdown", slog.String("error", err.Error()))
	}

	logger.Info("node daemon stopped")
}
