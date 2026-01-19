package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/NavarchProject/navarch/pkg/controlplane"
	"github.com/NavarchProject/navarch/pkg/controlplane/db"
	"github.com/NavarchProject/navarch/proto/protoconnect"
)

func main() {
	// Parse command-line flags
	addr := flag.String("addr", ":50051", "HTTP server address")
	healthCheckInterval := flag.Int("health-check-interval", 60, "Default health check interval in seconds")
	heartbeatInterval := flag.Int("heartbeat-interval", 30, "Default heartbeat interval in seconds")
	flag.Parse()

	log.Println("Navarch Control Plane")
	log.Printf("Server address: %s", *addr)

	// Create in-memory database
	database := db.NewInMemDB()
	defer database.Close()

	log.Println("Using in-memory database")

	// Create control plane server
	cfg := controlplane.Config{
		HealthCheckIntervalSeconds: int32(*healthCheckInterval),
		HeartbeatIntervalSeconds:   int32(*heartbeatInterval),
		EnabledHealthChecks:        []string{"boot", "nvml", "xid"},
	}
	srv := controlplane.NewServer(database, cfg)

	// Create HTTP mux and register Connect handler
	mux := http.NewServeMux()
	path, handler := protoconnect.NewControlPlaneServiceHandler(srv)
	mux.Handle(path, handler)

	log.Printf("Control plane listening on %s", *addr)
	fmt.Println("Control plane started successfully")

	// Create HTTP server with h2c support (HTTP/2 without TLS)
	httpServer := &http.Server{
		Addr:    *addr,
		Handler: h2c.NewHandler(mux, &http2.Server{}),
	}

	// Start serving in a goroutine
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to serve: %v", err)
		}
	}()

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	<-sigChan
	log.Println("Shutting down control plane...")
	if err := httpServer.Close(); err != nil {
		log.Printf("Error during shutdown: %v", err)
	}
	log.Println("Control plane stopped")
}
