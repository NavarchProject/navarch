package main

import (
	"context"
	"flag"
	"log/slog"
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

	// Set up structured logging
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	logger.Info("starting Navarch Control Plane", slog.String("addr", *addr))

	// Create in-memory database
	database := db.NewInMemDB()
	defer database.Close()

	logger.Info("using in-memory database")

	// Create control plane server
	cfg := controlplane.Config{
		HealthCheckIntervalSeconds: int32(*healthCheckInterval),
		HeartbeatIntervalSeconds:   int32(*heartbeatInterval),
		EnabledHealthChecks:        []string{"boot", "nvml", "xid"},
	}
	srv := controlplane.NewServer(database, cfg, logger)

	// Create HTTP mux and register Connect handler
	mux := http.NewServeMux()
	path, handler := protoconnect.NewControlPlaneServiceHandler(srv)
	mux.Handle(path, handler)

	// Health endpoints
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		// Check if database is accessible
		ctx := r.Context()
		if _, err := database.ListNodes(ctx); err != nil {
			logger.Warn("readiness check failed", slog.String("error", err.Error()))
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("database not ready"))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ready"))
	})

	logger.Info("control plane ready", slog.String("addr", *addr))

	// Create HTTP server with h2c support (HTTP/2 without TLS)
	httpServer := &http.Server{
		Addr:    *addr,
		Handler: h2c.NewHandler(mux, &http2.Server{}),
	}

	// Start serving in a goroutine
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server failed", slog.String("error", err.Error()))
			os.Exit(1)
		}
	}()

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	<-sigChan
	logger.Info("shutting down control plane")
	if err := httpServer.Shutdown(context.Background()); err != nil {
		logger.Error("error during shutdown", slog.String("error", err.Error()))
	}
	logger.Info("control plane stopped")
}
