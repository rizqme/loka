package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/rizqme/loka/internal/config"
	"github.com/rizqme/loka/internal/controlplane"
	"github.com/rizqme/loka/internal/controlplane/api"
	"github.com/rizqme/loka/internal/controlplane/image"
	"github.com/rizqme/loka/internal/controlplane/ha"
	"github.com/rizqme/loka/internal/controlplane/scheduler"
	"github.com/rizqme/loka/internal/controlplane/session"
	"github.com/rizqme/loka/internal/controlplane/worker"
	localobjstore "github.com/rizqme/loka/internal/objstore/local"
	"github.com/rizqme/loka/internal/worker/vm"
	"github.com/rizqme/loka/internal/provider"
	provaws "github.com/rizqme/loka/internal/provider/aws"
	provazure "github.com/rizqme/loka/internal/provider/azure"
	provdo "github.com/rizqme/loka/internal/provider/digitalocean"
	provgcp "github.com/rizqme/loka/internal/provider/gcp"
	provlocal "github.com/rizqme/loka/internal/provider/local"
	provovh "github.com/rizqme/loka/internal/provider/ovh"
	provsm "github.com/rizqme/loka/internal/provider/selfmanaged"
	"github.com/rizqme/loka/internal/store"
	"github.com/rizqme/loka/pkg/version"

	// Register store drivers.
	_ "github.com/rizqme/loka/internal/store/postgres"
	_ "github.com/rizqme/loka/internal/store/sqlite"

	// Register coordinator drivers.
	_ "github.com/rizqme/loka/internal/store/redis"
)

func main() {
	// Load config first so we can configure logging from it.
	var cfg config.ControlPlaneConfig
	if configPath := os.Getenv("LOKA_CONFIG"); configPath != "" {
		if err := config.Load(configPath, &cfg); err != nil {
			fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
			os.Exit(1)
		}
	}
	cfg.Defaults()

	// Configure logging.
	logLevel := slog.LevelInfo
	switch cfg.Logging.Level {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	}
	var logHandler slog.Handler
	if cfg.Logging.Format == "json" {
		logHandler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel})
	} else {
		logHandler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel})
	}
	logger := slog.New(logHandler)
	slog.SetDefault(logger)

	logger.Info("starting lokad", "version", version.Version, "explore", version.Commit)

	// Initialize store via factory.
	db, err := store.Open(store.Config{
		Driver: cfg.Database.Driver,
		DSN:    cfg.Database.DSN,
	})
	if err != nil {
		logger.Error("failed to open database", "driver", cfg.Database.Driver, "error", err)
		os.Exit(1)
	}
	defer db.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := db.Migrate(ctx); err != nil {
		logger.Error("failed to migrate database", "error", err)
		os.Exit(1)
	}

	logger.Info("database ready", "driver", cfg.Database.Driver)

	// Initialize coordinator via factory.
	coordinator, err := ha.Open(ha.Config{
		Type:           cfg.Coordinator.Type,
		Address:        cfg.Coordinator.Address,
		Password:       cfg.Coordinator.Password,
		SentinelMaster: cfg.Coordinator.SentinelMaster,
	})
	if err != nil {
		logger.Error("failed to create coordinator", "type", cfg.Coordinator.Type, "error", err)
		os.Exit(1)
	}
	defer coordinator.Close()

	logger.Info("coordinator ready", "type", cfg.Coordinator.Type)

	// Initialize object store.
	objStore, err := localobjstore.New(cfg.ObjectStore.Path)
	if err != nil {
		logger.Error("failed to create object store", "error", err)
		os.Exit(1)
	}

	// Initialize provider registry.
	providerRegistry := provider.NewRegistry()
	providerRegistry.Register(provlocal.New())
	providerRegistry.Register(provsm.New(db))
	providerRegistry.Register(provaws.New(provaws.Config{}, logger))
	providerRegistry.Register(provgcp.New(provgcp.Config{}, logger))
	providerRegistry.Register(provazure.New(provazure.Config{}, logger))
	providerRegistry.Register(provovh.New(provovh.Config{}, logger))
	providerRegistry.Register(provdo.New(provdo.Config{}, logger))
	logger.Info("providers registered", "count", len(providerRegistry.List()))

	// Initialize image manager (Docker images → Firecracker rootfs).
	imgMgr := image.NewManager(objStore, cfg.ObjectStore.Path, logger)

	// Initialize worker registry.
	registry := worker.NewRegistry(db, logger)

	// Initialize scheduler.
	sched := scheduler.New(registry, scheduler.Strategy(cfg.Scheduler.Strategy))

	// Initialize session manager.
	sm := session.NewManager(db, registry, sched, imgMgr, logger)

	// Initialize drainer with migration callback.
	drainer := worker.NewDrainer(registry, db, sm.MigrateSession, logger)

	// Start worker health monitor (only on leader in HA mode).
	monitor := worker.NewMonitor(registry, db, sm.MigrateSession, worker.DefaultMonitorConfig(), logger)

	if cfg.Mode == "ha" {
		// In HA mode, only the leader runs the monitor.
		go coordinator.ElectLeader(ctx, "control-plane", func(leaderCtx context.Context) {
			logger.Info("this instance is the leader")
			monitor.Start(leaderCtx)
		})
	} else {
		// Single mode — always run the monitor.
		go monitor.Start(ctx)
	}

	// Firecracker configuration.
	fcConfig := vm.FirecrackerConfig{
		BinaryPath: envOrDefault("LOKA_FIRECRACKER_BIN", "/usr/local/bin/firecracker"),
		KernelPath: envOrDefault("LOKA_KERNEL_PATH", cfg.ObjectStore.Path+"/kernel/vmlinux"),
		RootfsPath: envOrDefault("LOKA_ROOTFS_PATH", cfg.ObjectStore.Path+"/rootfs/rootfs.ext4"),
		DataDir:    cfg.ObjectStore.Path + "/worker-data",
	}

	// Start embedded local worker — all execution goes through Firecracker VMs.
	dataDir := cfg.ObjectStore.Path + "/worker-data"
	localWorker, err := controlplane.NewLocalWorker(registry, sm, objStore, dataDir, fcConfig, logger)
	if err != nil {
		logger.Error("failed to create local worker", "error", err)
		os.Exit(1)
	}
	localWorker.Start(ctx)

	// Initialize API server.
	srv := api.NewServer(sm, registry, providerRegistry, imgMgr, drainer, db, logger, api.ServerOpts{
		APIKey: cfg.Auth.APIKey,
	})

	httpServer := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: srv.Handler(),
	}

	// Graceful shutdown.
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		logger.Info("shutting down...")
		cancel()
		httpServer.Shutdown(context.Background())
	}()

	logger.Info(fmt.Sprintf("listening on %s (mode=%s, db=%s, coordinator=%s)",
		cfg.ListenAddr, cfg.Mode, cfg.Database.Driver, cfg.Coordinator.Type))
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("server error", "error", err)
		os.Exit(1)
	}
}

func envOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
