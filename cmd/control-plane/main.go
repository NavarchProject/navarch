package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/NavarchProject/navarch/pkg/config"
	"github.com/NavarchProject/navarch/pkg/controlplane"
	"github.com/NavarchProject/navarch/pkg/controlplane/db"
	"github.com/NavarchProject/navarch/pkg/pool"
	"github.com/NavarchProject/navarch/pkg/provider"
	"github.com/NavarchProject/navarch/pkg/provider/fake"
	"github.com/NavarchProject/navarch/pkg/provider/gcp"
	"github.com/NavarchProject/navarch/pkg/provider/lambda"
	"github.com/NavarchProject/navarch/proto/protoconnect"
)

func main() {
	configPath := flag.String("config", "", "Path to configuration file")
	flag.Parse()

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

	srv := controlplane.NewServer(database, controlplane.Config{
		HealthCheckIntervalSeconds: int32(cfg.Server.HealthCheckInterval.Seconds()),
		HeartbeatIntervalSeconds:   int32(cfg.Server.HeartbeatInterval.Seconds()),
		EnabledHealthChecks:        []string{"boot", "nvml", "xid"},
	}, logger)

	var poolManager *controlplane.PoolManager
	if len(cfg.Pools) > 0 {
		poolManager, err = initPoolManager(cfg, database, logger)
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

	logger.Info("control plane ready", slog.String("addr", cfg.Server.Address))

	httpServer := &http.Server{
		Addr:    cfg.Server.Address,
		Handler: h2c.NewHandler(mux, &http2.Server{}),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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

func initPoolManager(cfg *config.Config, database db.DB, logger *slog.Logger) (*controlplane.PoolManager, error) {
	metricsSource := controlplane.NewDBMetricsSource(database, logger)
	pm := controlplane.NewPoolManager(controlplane.PoolManagerConfig{
		EvaluationInterval: cfg.Server.AutoscaleInterval,
	}, metricsSource, logger)

	providers := make(map[string]provider.Provider)
	controlPlaneAddr := fmt.Sprintf("http://localhost%s", cfg.Server.Address)
	if len(cfg.Server.Address) > 0 && cfg.Server.Address[0] != ':' {
		controlPlaneAddr = "http://" + cfg.Server.Address
	}

	for poolName, poolCfg := range cfg.Pools {
		var poolProviders []pool.ProviderConfig

		if poolCfg.Provider != "" {
			prov, err := getOrCreateProvider(poolCfg.Provider, cfg, providers, controlPlaneAddr, logger)
			if err != nil {
				return nil, fmt.Errorf("pool %s: %w", poolName, err)
			}
			poolProviders = append(poolProviders, pool.ProviderConfig{
				Name:     poolCfg.Provider,
				Provider: prov,
			})
		} else {
			for _, pe := range poolCfg.Providers {
				prov, err := getOrCreateProvider(pe.Name, cfg, providers, controlPlaneAddr, logger)
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

		labels := make(map[string]string)
		if poolCfg.Labels != nil {
			for k, v := range poolCfg.Labels {
				labels[k] = v
			}
		}
		// Ensure pool name is always in labels for metrics aggregation
		labels["pool"] = poolName

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
				UnhealthyThreshold: getUnhealthyThreshold(poolCfg.Health),
				AutoReplace:        getAutoReplace(poolCfg.Health),
				Labels:             labels,
			},
			Providers:        poolProviders,
			ProviderStrategy: poolCfg.Strategy,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create pool %s: %w", poolName, err)
		}

		var autoscaler pool.Autoscaler
		if poolCfg.Autoscaling != nil {
			autoscaler, err = buildAutoscaler(poolCfg.Autoscaling)
			if err != nil {
				return nil, fmt.Errorf("pool %s: %w", poolName, err)
			}
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

func getUnhealthyThreshold(h *config.HealthCfg) int {
	if h == nil || h.UnhealthyAfter == 0 {
		return 2
	}
	return h.UnhealthyAfter
}

func getAutoReplace(h *config.HealthCfg) bool {
	if h == nil {
		return true
	}
	return h.AutoReplace
}

func buildAutoscaler(cfg *config.AutoscalingCfg) (pool.Autoscaler, error) {
	switch cfg.Type {
	case "reactive":
		scaleUp := 80.0
		scaleDown := 20.0
		if cfg.ScaleUpAt != nil {
			scaleUp = float64(*cfg.ScaleUpAt)
		}
		if cfg.ScaleDownAt != nil {
			scaleDown = float64(*cfg.ScaleDownAt)
		}
		return pool.NewReactiveAutoscaler(scaleUp, scaleDown), nil

	case "queue":
		jobsPerNode := 10
		if cfg.JobsPerNode != nil {
			jobsPerNode = *cfg.JobsPerNode
		}
		return pool.NewQueueBasedAutoscaler(jobsPerNode), nil

	case "scheduled":
		var entries []pool.ScheduleEntry
		for _, s := range cfg.Schedule {
			entries = append(entries, pool.ScheduleEntry{
				DaysOfWeek: parseDaysOfWeek(s.Days),
				StartHour:  s.Start,
				EndHour:    s.End,
				MinNodes:   s.MinNodes,
				MaxNodes:   s.MaxNodes,
			})
		}
		var fallback pool.Autoscaler
		if cfg.Fallback != nil {
			var err error
			fallback, err = buildAutoscaler(cfg.Fallback)
			if err != nil {
				return nil, err
			}
		}
		return pool.NewScheduledAutoscaler(entries, fallback), nil

	case "predictive":
		lookback := 10
		growth := 1.2
		if cfg.LookbackWindow != nil {
			lookback = *cfg.LookbackWindow
		}
		if cfg.GrowthFactor != nil {
			growth = *cfg.GrowthFactor
		}
		var fallback pool.Autoscaler
		if cfg.Fallback != nil {
			var err error
			fallback, err = buildAutoscaler(cfg.Fallback)
			if err != nil {
				return nil, err
			}
		}
		return pool.NewPredictiveAutoscaler(lookback, growth, fallback), nil

	case "composite":
		var autoscalers []pool.Autoscaler
		for _, a := range cfg.Autoscalers {
			as, err := buildAutoscaler(&a)
			if err != nil {
				return nil, err
			}
			autoscalers = append(autoscalers, as)
		}
		mode := pool.ModeMax
		if cfg.Mode == "min" {
			mode = pool.ModeMin
		} else if cfg.Mode == "avg" {
			mode = pool.ModeAvg
		}
		return pool.NewCompositeAutoscaler(mode, autoscalers...), nil

	default:
		return nil, fmt.Errorf("unknown autoscaler type: %s", cfg.Type)
	}
}

func parseDaysOfWeek(days []string) []time.Weekday {
	dayMap := map[string]time.Weekday{
		"sunday":    time.Sunday,
		"monday":    time.Monday,
		"tuesday":   time.Tuesday,
		"wednesday": time.Wednesday,
		"thursday":  time.Thursday,
		"friday":    time.Friday,
		"saturday":  time.Saturday,
	}
	var result []time.Weekday
	for _, d := range days {
		if wd, ok := dayMap[strings.ToLower(d)]; ok {
			result = append(result, wd)
		}
	}
	return result
}
