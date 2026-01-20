package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/NavarchProject/navarch/pkg/controlplane"
	"github.com/NavarchProject/navarch/pkg/controlplane/db"
	"github.com/NavarchProject/navarch/pkg/pool"
	"github.com/NavarchProject/navarch/pkg/provider"
	"github.com/NavarchProject/navarch/pkg/provider/fake"
	"github.com/NavarchProject/navarch/pkg/provider/lambda"
	"github.com/NavarchProject/navarch/proto/protoconnect"
)

func healthzHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func readyzHandler(database db.DB, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if _, err := database.ListNodes(ctx); err != nil {
			if logger != nil {
				logger.Warn("readiness check failed", slog.String("error", err.Error()))
			}
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("database not ready"))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ready"))
	}
}

func main() {
	addr := flag.String("addr", ":50051", "HTTP server address")
	healthCheckInterval := flag.Int("health-check-interval", 60, "Default health check interval in seconds")
	heartbeatInterval := flag.Int("heartbeat-interval", 30, "Default heartbeat interval in seconds")
	shutdownTimeout := flag.Int("shutdown-timeout", 30, "Graceful shutdown timeout in seconds")
	poolsConfig := flag.String("pools-config", "", "Path to pools configuration YAML file")
	autoscaleInterval := flag.Int("autoscale-interval", 30, "Autoscaler evaluation interval in seconds")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	logger.Info("starting Navarch Control Plane",
		slog.String("addr", *addr),
		slog.Int("shutdown_timeout_seconds", *shutdownTimeout),
	)

	database := db.NewInMemDB()
	logger.Info("using in-memory database")

	cfg := controlplane.Config{
		HealthCheckIntervalSeconds: int32(*healthCheckInterval),
		HeartbeatIntervalSeconds:   int32(*heartbeatInterval),
		EnabledHealthChecks:        []string{"boot", "nvml", "xid"},
	}
	srv := controlplane.NewServer(database, cfg, logger)

	// Initialize pool manager
	var poolManager *controlplane.PoolManager
	if *poolsConfig != "" {
		controlPlaneURL := fmt.Sprintf("http://localhost%s", *addr)
		if (*addr)[0] != ':' {
			controlPlaneURL = "http://" + *addr
		}
		var err error
		poolManager, err = initPoolManager(*poolsConfig, *autoscaleInterval, controlPlaneURL, logger)
		if err != nil {
			logger.Error("failed to initialize pool manager", slog.String("error", err.Error()))
			os.Exit(1)
		}
	}

	mux := http.NewServeMux()
	path, handler := protoconnect.NewControlPlaneServiceHandler(srv)
	mux.Handle(path, handler)

	mux.HandleFunc("/healthz", healthzHandler)
	mux.HandleFunc("/readyz", readyzHandler(database, logger))

	logger.Info("control plane ready", slog.String("addr", *addr))

	// HTTP/2 without TLS via h2c
	httpServer := &http.Server{
		Addr:    *addr,
		Handler: h2c.NewHandler(mux, &http2.Server{}),
	}

	// Start pool manager if configured
	ctx, cancel := context.WithCancel(context.Background())
	if poolManager != nil {
		poolManager.Start(ctx)
	}

	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server failed", slog.String("error", err.Error()))
			os.Exit(1)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	sig := <-sigChan
	logger.Info("received shutdown signal",
		slog.String("signal", sig.String()),
		slog.Int("timeout_seconds", *shutdownTimeout),
	)
	cancel()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Duration(*shutdownTimeout)*time.Second)
	defer cancel()

	if poolManager != nil {
		logger.Info("stopping pool manager")
		poolManager.Stop()
	}

	logger.Info("stopping HTTP server")
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("error shutting down HTTP server", slog.String("error", err.Error()))
	} else {
		logger.Info("HTTP server stopped cleanly")
	}

	logger.Info("closing database connections")
	if err := database.Close(); err != nil {
		logger.Error("error closing database", slog.String("error", err.Error()))
	} else {
		logger.Info("database closed cleanly")
	}

	logger.Info("control plane stopped")
}

func initPoolManager(configPath string, intervalSec int, controlPlaneAddr string, logger *slog.Logger) (*controlplane.PoolManager, error) {
	poolsCfg, err := controlplane.LoadPoolsConfig(configPath)
	if err != nil {
		return nil, err
	}

	pm := controlplane.NewPoolManager(controlplane.PoolManagerConfig{
		EvaluationInterval: time.Duration(intervalSec) * time.Second,
	}, nil, logger)

	providers := make(map[string]provider.Provider)

	for _, poolYAML := range poolsCfg.Pools {
		prov, ok := providers[poolYAML.Provider]
		if !ok {
			var err error
			prov, err = createProvider(poolYAML.Provider, poolsCfg.Providers, controlPlaneAddr, logger)
			if err != nil {
				return nil, fmt.Errorf("failed to create provider %s: %w", poolYAML.Provider, err)
			}
			providers[poolYAML.Provider] = prov
		}

		poolCfg, err := poolYAML.ToPoolConfig(poolsCfg.Global)
		if err != nil {
			return nil, fmt.Errorf("invalid pool config %s: %w", poolYAML.Name, err)
		}

		p, err := pool.New(poolCfg, prov)
		if err != nil {
			return nil, fmt.Errorf("failed to create pool %s: %w", poolYAML.Name, err)
		}

		autoscaler, err := controlplane.BuildAutoscaler(poolYAML.Scaling.Autoscaler)
		if err != nil {
			return nil, fmt.Errorf("failed to build autoscaler for pool %s: %w", poolYAML.Name, err)
		}

		if err := pm.AddPool(p, autoscaler); err != nil {
			return nil, err
		}

		logger.Info("pool configured",
			slog.String("pool", poolYAML.Name),
			slog.String("provider", poolYAML.Provider),
			slog.String("instance_type", poolYAML.InstanceType),
			slog.Int("min_nodes", poolYAML.Scaling.MinNodes),
			slog.Int("max_nodes", poolYAML.Scaling.MaxNodes),
		)
	}

	return pm, nil
}

func createProvider(name string, configs map[string]controlplane.ProviderConfig, controlPlaneAddr string, logger *slog.Logger) (provider.Provider, error) {
	cfg, ok := configs[name]
	if !ok && name != "fake" {
		return nil, fmt.Errorf("no configuration for provider %s", name)
	}

	switch name {
	case "fake":
		gpuCount := 8
		if cfg.GPUCount > 0 {
			gpuCount = cfg.GPUCount
		}
		return fake.New(fake.Config{
			ControlPlaneAddr: controlPlaneAddr,
			GPUCount:         gpuCount,
			Logger:           logger,
		})

	case "lambda":
		apiKey := os.Getenv("LAMBDA_API_KEY")
		if apiKey == "" && cfg.APIKeySecret != "" {
			apiKey = os.Getenv(cfg.APIKeySecret)
		}
		if apiKey == "" {
			return nil, fmt.Errorf("LAMBDA_API_KEY environment variable is required")
		}
		return lambda.New(lambda.Config{APIKey: apiKey})

	case "gcp", "aws":
		return nil, fmt.Errorf("provider %s is not yet implemented", name)

	default:
		return nil, fmt.Errorf("unknown provider: %s", name)
	}
}
