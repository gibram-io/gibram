// GibRAM Server
package main

import (
	"compress/gzip"
	"context"
	"flag"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/gibram-io/gibram/pkg/backup"
	"github.com/gibram-io/gibram/pkg/config"
	"github.com/gibram-io/gibram/pkg/engine"
	"github.com/gibram-io/gibram/pkg/logging"
	"github.com/gibram-io/gibram/pkg/memory"
	"github.com/gibram-io/gibram/pkg/metrics"
	"github.com/gibram-io/gibram/pkg/server"
	"github.com/gibram-io/gibram/pkg/shutdown"
	"github.com/gibram-io/gibram/pkg/version"
)

// Version can be overridden at build time via -ldflags "-X main.Version=...".
var Version = "dev"

func main() {
	// Command line flags
	configFile := flag.String("config", "", "Config file path (YAML)")
	addr := flag.String("addr", "", "Server address (override config)")
	dataDir := flag.String("data", "", "Data directory (override config)")
	vectorDim := flag.Int("dim", 0, "Vector dimension (override config)")
	insecure := flag.Bool("insecure", false, "Run in insecure mode (no TLS, no auth) - DEV ONLY")
	logLevel := flag.String("log-level", "", "Log level (override config)")
	sessionCleanupInterval := flag.Duration("session-cleanup-interval", 60*time.Second, "Session cleanup interval")
	flag.Parse()

	// Load configuration
	var cfg *config.Config
	var err error

	if *configFile != "" {
		cfg, err = config.LoadConfig(*configFile)
		if err != nil {
			// Use default logger before init
			logging.Error("Failed to load config: %v", err)
			os.Exit(1)
		}
	} else {
		cfg = config.DefaultConfig()
	}

	// Apply CLI overrides
	cfg.ApplyOverrides(config.CLIOverrides{
		Addr:      *addr,
		DataDir:   *dataDir,
		VectorDim: *vectorDim,
		LogLevel:  *logLevel,
	})

	// Initialize logger from config
	err = logging.Init(logging.Config{
		Level:  cfg.Logging.Level,
		Format: cfg.Logging.Format,
		Output: cfg.Logging.Output,
		File:   cfg.Logging.File,
	})
	if err != nil {
		logging.Error("Failed to initialize logger: %v", err)
		os.Exit(1)
	}

	log := logging.WithPrefix("main")

	// Print startup info
	startVersion := Version
	if startVersion == "" || startVersion == "dev" {
		startVersion = version.Version
	}
	log.Info("GibRAM v%s starting...", startVersion)
	log.Info("  Address:    %s", cfg.Server.Addr)
	log.Info("  Data dir:   %s", cfg.Server.DataDir)
	log.Info("  Vector dim: %d", cfg.Server.VectorDim)
	log.Info("  Log level:  %s", cfg.Logging.Level)
	log.Info("  Protocol:   GibRAM Protocol v1 (proto3)")
	if *insecure {
		log.Warn("Running in INSECURE mode (no TLS, no auth)")
		// Clear auth/TLS config in insecure mode
		cfg.Auth.Keys = nil
		cfg.TLS.CertFile = ""
		cfg.TLS.KeyFile = ""
		cfg.TLS.AutoCert = false // Disable auto-cert
	}

	// Create engine (in-memory for now, can add persistence later)
	eng := engine.NewEngine(cfg.Server.VectorDim)

	// Start session cleanup goroutine
	eng.StartSessionCleanup(*sessionCleanupInterval)

	// Initialize metrics collector and profiler
	metricsCollector := metrics.NewCollector()
	profiler := metrics.NewProfiler(metricsCollector)
	profiler.Start()
	log.Info("  Metrics:    enabled")

	// Initialize memory tracker (1GB max memory, adjust as needed)
	maxMemoryBytes := int64(1 * 1024 * 1024 * 1024) // 1GB
	memTracker := memory.NewTracker(maxMemoryBytes)
	memTracker.SetAlertCallback(func(level string, usedBytes, maxBytes int64) {
		usedMB := usedBytes / (1024 * 1024)
		maxMB := maxBytes / (1024 * 1024)
		memLog := logging.WithPrefix("memory")
		switch level {
		case "critical":
			memLog.Error("CRITICAL: Memory usage %dMB / %dMB (%.1f%%)", usedMB, maxMB, float64(usedBytes)/float64(maxBytes)*100)
			memTracker.ForceGC()
		case "warning":
			memLog.Warn("Memory usage %dMB / %dMB (%.1f%%)", usedMB, maxMB, float64(usedBytes)/float64(maxBytes)*100)
		}
		// Record to metrics
		metricsCollector.Gauge("memory.used_bytes", usedBytes)
		metricsCollector.Gauge("memory.max_bytes", maxBytes)
	})

	// Start memory monitoring goroutine
	memStopCh := make(chan struct{})
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-memStopCh:
				return
			case <-ticker.C:
				usedBytes, _ := memTracker.Check()
				metricsCollector.Gauge("memory.used_bytes", usedBytes)
			}
		}
	}()
	log.Info("  Memory:     monitoring enabled (max %dMB)", maxMemoryBytes/(1024*1024))

	// Initialize backup system
	var wal *backup.WAL
	var recovery *backup.Recovery
	walDir := filepath.Join(cfg.Server.DataDir, "wal")
	snapshotDir := filepath.Join(cfg.Server.DataDir, "snapshots")

	// Create directories
	if err := os.MkdirAll(walDir, 0755); err != nil {
		log.Warn("WAL dir create failed: %v (backup disabled)", err)
	}
	if err := os.MkdirAll(snapshotDir, 0755); err != nil {
		log.Warn("Snapshot dir create failed: %v (snapshots disabled)", err)
	}

	wal, err = backup.NewWAL(walDir, backup.SyncPeriodic)
	if err != nil {
		log.Warn("WAL init failed: %v (backup disabled)", err)
	} else {
		log.Info("  WAL:        %s", walDir)
	}

	recovery = backup.NewRecovery(cfg.Server.DataDir)
	log.Info("  Snapshots:  %s", snapshotDir)

	// Create and start Protobuf server with config
	srv := server.NewServerWithConfig(eng, cfg)

	// Wire WAL to server for WAL commands
	if wal != nil {
		srv.SetWAL(wal)
	}

	// Setup snapshot callback - Production-grade implementation
	srv.SetSnapshotCallback(func(path string) error {
		if path == "" {
			path = filepath.Join(snapshotDir, backup.GenerateSnapshotName("gibram"))
		}

		// Create snapshot file
		f, err := os.Create(path)
		if err != nil {
			return err
		}
		defer func() {
			if err := f.Close(); err != nil {
				log.Warn("Failed to close snapshot file: %v", err)
			}
		}()

		// Use gzip compression
		gw := gzip.NewWriter(f)
		defer func() {
			if err := gw.Close(); err != nil {
				log.Warn("Failed to close snapshot gzip writer: %v", err)
			}
		}()

		// Serialize engine state
		info := eng.Info()
		log.Info("Snapshot starting: %d docs, %d textunits, %d entities, %d rels, %d communities",
			info.DocumentCount, info.TextUnitCount, info.EntityCount,
			info.RelationshipCount, info.CommunityCount)

		if err := eng.Snapshot(gw); err != nil {
			return err
		}

		// Record WAL LSN for recovery point
		if wal != nil {
			lsn := wal.CurrentLSN()
			log.Info("Snapshot completed: %s (WAL LSN: %d)", path, lsn)
		} else {
			log.Info("Snapshot completed: %s", path)
		}

		return nil
	})

	// Setup restore callback - Production-grade implementation
	srv.SetRestoreCallback(func(path string) error {
		// Open snapshot file
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() {
			if err := f.Close(); err != nil {
				log.Warn("Failed to close snapshot file: %v", err)
			}
		}()

		// Detect if gzipped
		var reader io.Reader
		buf := make([]byte, 2)
		if _, err := f.Read(buf); err != nil {
			return err
		}
		if _, err := f.Seek(0, 0); err != nil {
			return err
		}

		if buf[0] == 0x1f && buf[1] == 0x8b {
			// Gzip magic number
			gr, err := gzip.NewReader(f)
			if err != nil {
				return err
			}
			defer func() {
				if err := gr.Close(); err != nil {
					log.Warn("Failed to close snapshot gzip reader: %v", err)
				}
			}()
			reader = gr
		} else {
			reader = f
		}

		log.Info("Restoring snapshot from %s", path)

		// Restore engine state
		if err := eng.Restore(reader); err != nil {
			return err
		}

		info := eng.Info()
		log.Info("Restore completed: %d docs, %d textunits, %d entities, %d rels, %d communities",
			info.DocumentCount, info.TextUnitCount, info.EntityCount,
			info.RelationshipCount, info.CommunityCount)

		return nil
	})

	if err := srv.Start(cfg.Server.Addr); err != nil {
		log.Error("Failed to start server: %v", err)
		os.Exit(1)
	}

	// Print info
	info := eng.Info()
	log.Info("Server ready!")
	log.Info("  Documents:     %d", info.DocumentCount)
	log.Info("  TextUnits:     %d", info.TextUnitCount)
	log.Info("  Entities:      %d", info.EntityCount)
	log.Info("  Relationships: %d", info.RelationshipCount)
	log.Info("  Communities:   %d", info.CommunityCount)

	// Setup graceful shutdown
	shutdownHandler := shutdown.NewHandler()
	shutdownHandler.SetTimeout(30 * time.Second)

	// Register shutdown hooks in order
	shutdownHandler.Register("server", 10, func(ctx context.Context) error {
		srv.Stop()
		return nil
	})

	shutdownHandler.Register("session-cleanup", 20, func(ctx context.Context) error {
		eng.StopSessionCleanup()
		return nil
	})

	shutdownHandler.Register("profiler", 30, func(ctx context.Context) error {
		profiler.Stop()
		return nil
	})

	shutdownHandler.Register("memory-tracker", 35, func(ctx context.Context) error {
		close(memStopCh)
		// Final memory check
		usedBytes, _ := memTracker.Check()
		log.Info("Final memory usage: %dMB", usedBytes/(1024*1024))
		return nil
	})

	shutdownHandler.Register("wal", 40, func(ctx context.Context) error {
		if wal != nil {
			return wal.Close()
		}
		return nil
	})

	shutdownHandler.Register("metrics-snapshot", 50, func(ctx context.Context) error {
		snap := metricsCollector.Snapshot()
		log.Info("Final metrics: %s", metrics.FormatStats(snap))
		return nil
	})

	// Start listening for signals
	shutdownHandler.Start()

	// Wait for shutdown
	shutdownHandler.Wait()

	log.Info("Server stopped")

	// Suppress unused variable warnings
	_ = recovery
}
