package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/NavarchProject/navarch/pkg/node"
)

func main() {
	// Parse command-line flags
	controlPlaneAddr := flag.String("control-plane", "localhost:50051", "Control plane gRPC address")
	nodeID := flag.String("node-id", "", "Node ID (defaults to hostname)")
	provider := flag.String("provider", "gcp", "Cloud provider")
	region := flag.String("region", "", "Cloud region")
	zone := flag.String("zone", "", "Cloud zone")
	instanceType := flag.String("instance-type", "", "Instance type")
	flag.Parse()

	// Default node ID to hostname if not specified
	if *nodeID == "" {
		hostname, err := os.Hostname()
		if err != nil {
			log.Fatalf("Failed to get hostname: %v", err)
		}
		*nodeID = hostname
	}

	log.Println("Navarch Node Daemon")
	log.Printf("Node ID: %s", *nodeID)
	log.Printf("Control Plane: %s", *controlPlaneAddr)

	// Create node configuration
	cfg := node.Config{
		ControlPlaneAddr: *controlPlaneAddr,
		NodeID:           *nodeID,
		Provider:         *provider,
		Region:           *region,
		Zone:             *zone,
		InstanceType:     *instanceType,
	}

	// Create and start node
	n, err := node.New(cfg)
	if err != nil {
		log.Fatalf("Failed to create node: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := n.Start(ctx); err != nil {
		log.Fatalf("Failed to start node: %v", err)
	}

	log.Println("Node daemon started successfully")

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	<-sigChan
	log.Println("Shutting down node daemon...")
	cancel()

	if err := n.Stop(); err != nil {
		log.Printf("Error during shutdown: %v", err)
	}

	log.Println("Node daemon stopped")
}
