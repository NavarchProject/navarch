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

	"github.com/NavarchProject/navarch/pkg/auth"
	"github.com/NavarchProject/navarch/pkg/config"
	"github.com/NavarchProject/navarch/pkg/controlplane"
	"github.com/NavarchProject/navarch/pkg/controlplane/db"
	"github.com/NavarchProject/navarch/pkg/health"
	"github.com/NavarchProject/navarch/pkg/pool"
	"github.com/NavarchProject/navarch/pkg/provider"
	"github.com/NavarchProject/navarch/pkg/provider/fake"
	"github.com/NavarchProject/navarch/pkg/provider/gcp"
	"github.com/NavarchProject/navarch/pkg/provider/lambda"
	"github.com/NavarchProject/navarch/proto/protoconnect"
)

func main() {
	configPath := flag.String("config", "", "Path to configuration file")
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

	var cfg *config.Config
	var err error

	if *configPath != "" {
		cfg, err = config.Load(*configPath)
		if err != nil {
			logger.Error("failed to load config", slog.String("error", err.Error()))
			os.Exit(1)
		}
	} else {
		cfg = defaultConfig()
	}

	logger.Info("starting Navarch Control Plane",
		slog.String("addr", cfg.Server.Address),
	)

	database := db.NewInMemDB()

	// Create the instance manager for tracking cloud instance lifecycle
	instanceManager := controlplane.NewInstanceManager(
		database,
		controlplane.DefaultInstanceManagerConfig(),
		logger.With(slog.String("component", "instance-manager")),
	)

	// Load health policy
	var healthPolicy *health.Policy
	if cfg.Server.HealthPolicy != "" {
		var err error
		healthPolicy, err = health.LoadPolicy(cfg.Server.HealthPolicy)
		if err != nil {
			logger.Error("failed to load health policy", slog.String("path", cfg.Server.HealthPolicy), slog.String("error", err.Error()))
			os.Exit(1)
		}
		logger.Info("loaded health policy", slog.String("path", cfg.Server.HealthPolicy), slog.Int("rules", len(healthPolicy.Rules)))
	}

	srv := controlplane.NewServer(database, controlplane.Config{
		HealthCheckIntervalSeconds: int32(cfg.Server.HealthCheckInterval.Seconds()),
		HeartbeatIntervalSeconds:   int32(cfg.Server.HeartbeatInterval.Seconds()),
		EnabledHealthChecks:        []string{"boot", "nvml", "xid"},
		HealthPolicy:               healthPolicy,
	}, instanceManager, logger)

	var poolManager *controlplane.PoolManager
	if len(cfg.Pools) > 0 {
		poolManager, err = initPoolManager(cfg, database, instanceManager, logger)
		if err != nil {
			logger.Error("failed to initialize pool manager", slog.String("error", err.Error()))
			os.Exit(1)
		}
		// Wire pool manager to receive health notifications for auto-replacement
		srv.SetHealthObserver(poolManager)
	}

	mux := http.NewServeMux()
	path, handler := protoconnect.NewControlPlaneServiceHandler(srv)
	mux.Handle(path, handler)
	mux.HandleFunc("/healthz", healthzHandler)
	mux.HandleFunc("/readyz", readyzHandler(database, logger))

	// Setup authentication middleware
	var httpHandler http.Handler = mux
	if token != "" {
		logger.Info("authentication enabled")
		authenticator := auth.NewBearerTokenAuthenticator(token, "system:authenticated", nil)
		middleware := auth.NewMiddleware(authenticator,
			auth.WithExcludedPaths("/healthz", "/readyz", "/metrics"),
		)
		httpHandler = middleware.Wrap(mux)
	}

	logger.Info("control plane ready", slog.String("addr", cfg.Server.Address))

	httpServer := &http.Server{
		Addr:    cfg.Server.Address,
		Handler: h2c.NewHandler(httpHandler, &http2.Server{}),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the instance manager for background stale instance detection
	instanceManager.Start(ctx)

	if poolManager != nil {
		poolManager.Start(ctx)
	}

	serverErrChan := make(chan error, 1)
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server failed", slog.String("error", err.Error()))
			serverErrChan <- err
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	select {
	case sig := <-sigChan:
		logger.Info("received shutdown signal", slog.String("signal", sig.String()))
	case err := <-serverErrChan:
		logger.Error("server error triggered shutdown", slog.String("error", err.Error()))
	}

	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if poolManager != nil {
		poolManager.Stop()
	}

	instanceManager.Stop()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("error shutting down HTTP server", slog.String("error", err.Error()))
	}
	if err := database.Close(); err != nil {
		logger.Error("error closing database", slog.String("error", err.Error()))
	}

	logger.Info("control plane stopped")
}

func defaultConfig() *config.Config {
	return &config.Config{
		Server: config.ServerConfig{
			Address:             ":50051",
			HeartbeatInterval:   30 * time.Second,
			HealthCheckInterval: 60 * time.Second,
			AutoscaleInterval:   30 * time.Second,
		},
		Providers: make(map[string]config.ProviderCfg),
		Pools:     make(map[string]config.PoolCfg),
	}
}

func healthzHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func readyzHandler(database db.DB, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, err := database.ListNodes(r.Context()); err != nil {
			logger.Warn("readiness check failed", slog.String("error", err.Error()))
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("database not ready"))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ready"))
	}
}

func initPoolManager(cfg *config.Config, database db.DB, instanceManager *controlplane.InstanceManager, logger *slog.Logger) (*controlplane.PoolManager, error) {
	metricsSource := controlplane.NewDBMetricsSource(database, logger)
	pm := controlplane.NewPoolManager(controlplane.PoolManagerConfig{
		EvaluationInterval: cfg.Server.AutoscaleInterval,
	}, metricsSource, instanceManager, logger)

	providers := make(map[string]provider.Provider)
	controlPlaneAddr := fmt.Sprintf("http://localhost%s", cfg.Server.Address)
	if len(cfg.Server.Address) > 0 && cfg.Server.Address[0] != ':' {
		controlPlaneAddr = "http://" + cfg.Server.Address
	}

	for poolName, poolCfg := range cfg.Pools {
		poolProviders, err := buildPoolProviders(poolName, poolCfg, cfg, providers, controlPlaneAddr, logger)
		if err != nil {
			return nil, err
		}

		labels := buildPoolLabels(poolName, poolCfg.Labels)

		p, err := pool.NewWithOptions(pool.NewPoolOptions{
			Config: pool.Config{
				Name:               poolName,
				InstanceType:       poolCfg.InstanceType,
				Region:             poolCfg.Region,
				Zones:              poolCfg.Zones,
				SSHKeyNames:        poolCfg.SSHKeys,
				MinNodes:           poolCfg.MinNodes,
				MaxNodes:           poolCfg.MaxNodes,
				CooldownPeriod:     poolCfg.Cooldown,
				UnhealthyThreshold: config.GetUnhealthyThreshold(poolCfg.Health),
				AutoReplace:        config.GetAutoReplace(poolCfg.Health),
				Labels:             labels,
			},
			Providers:        poolProviders,
			ProviderStrategy: poolCfg.Strategy,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create pool %s: %w", poolName, err)
		}

		autoscaler, err := config.BuildAutoscaler(poolCfg.Autoscaling)
		if err != nil {
			return nil, fmt.Errorf("pool %s: %w", poolName, err)
		}

		if err := pm.AddPool(p, autoscaler); err != nil {
			return nil, err
		}

		logger.Info("pool configured",
			slog.String("pool", poolName),
			slog.String("instance_type", poolCfg.InstanceType),
			slog.Int("min_nodes", poolCfg.MinNodes),
			slog.Int("max_nodes", poolCfg.MaxNodes),
		)
	}

	return pm, nil
}

func buildPoolProviders(poolName string, poolCfg config.PoolCfg, cfg *config.Config, cache map[string]provider.Provider, controlPlaneAddr string, logger *slog.Logger) ([]pool.ProviderConfig, error) {
	var poolProviders []pool.ProviderConfig

	if poolCfg.Provider != "" {
		prov, err := getOrCreateProvider(poolCfg.Provider, cfg, cache, controlPlaneAddr, logger)
		if err != nil {
			return nil, fmt.Errorf("pool %s: %w", poolName, err)
		}
		poolProviders = append(poolProviders, pool.ProviderConfig{
			Name:     poolCfg.Provider,
			Provider: prov,
		})
	} else {
		for _, pe := range poolCfg.Providers {
			prov, err := getOrCreateProvider(pe.Name, cfg, cache, controlPlaneAddr, logger)
			if err != nil {
				return nil, fmt.Errorf("pool %s: %w", poolName, err)
			}
			poolProviders = append(poolProviders, pool.ProviderConfig{
				Name:         pe.Name,
				Provider:     prov,
				Priority:     pe.Priority,
				Weight:       pe.Weight,
				Regions:      pe.Regions,
				InstanceType: pe.InstanceType,
			})
		}
	}

	return poolProviders, nil
}

func buildPoolLabels(poolName string, labels map[string]string) map[string]string {
	result := make(map[string]string)
	for k, v := range labels {
		result[k] = v
	}
	// Ensure pool name is always in labels for metrics aggregation
	result["pool"] = poolName
	return result
}

func getOrCreateProvider(name string, cfg *config.Config, cache map[string]provider.Provider, controlPlaneAddr string, logger *slog.Logger) (provider.Provider, error) {
	if prov, ok := cache[name]; ok {
		return prov, nil
	}

	provCfg, ok := cfg.Providers[name]
	if !ok {
		return nil, fmt.Errorf("unknown provider: %s", name)
	}

	var prov provider.Provider
	var err error

	switch provCfg.Type {
	case "fake":
		gpuCount := 8
		if provCfg.GPUCount > 0 {
			gpuCount = provCfg.GPUCount
		}
		prov, err = fake.New(fake.Config{
			ControlPlaneAddr: controlPlaneAddr,
			GPUCount:         gpuCount,
			Logger:           logger,
		})

	case "lambda":
		apiKey := os.Getenv("LAMBDA_API_KEY")
		if provCfg.APIKeyEnv != "" {
			apiKey = os.Getenv(provCfg.APIKeyEnv)
		}
		if apiKey == "" {
			return nil, fmt.Errorf("LAMBDA_API_KEY environment variable is required")
		}
		prov, err = lambda.New(lambda.Config{APIKey: apiKey})

	case "gcp":
		zone := provCfg.Zone
		if zone == "" {
			zone = "us-central1-a" // Default zone
		}
		prov, err = gcp.New(gcp.Config{
			Project: provCfg.Project,
			Zone:    zone,
		})

	case "aws":
		return nil, fmt.Errorf("provider %s is not yet implemented", provCfg.Type)

	default:
		return nil, fmt.Errorf("unknown provider type: %s", provCfg.Type)
	}

	if err != nil {
		return nil, err
	}

	cache[name] = prov
	return prov, nil
}

