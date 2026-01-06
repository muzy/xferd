package service

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/muzy/xferd/internal/config"
	"github.com/muzy/xferd/internal/ingress"
	"github.com/muzy/xferd/internal/shadow"
	"github.com/muzy/xferd/internal/uploader"
	"github.com/muzy/xferd/internal/watcher"
)

// Service represents the main xferd service
type Service struct {
	config       *config.Config
	server       *ingress.Server
	watchers     []watcher.Watcher
	dispatchers  []*uploader.Dispatcher
	shadows      []*shadow.Manager
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	shadowStopCh chan struct{} // Channel to stop shadow cleanup routines
	stopOnce     sync.Once     // Ensure Stop() is idempotent
}

// New creates a new xferd service
func New(cfg *config.Config) (*Service, error) {
	// Create REST ingress server
	server, err := ingress.NewServer(cfg.Server, cfg.Directories)
	if err != nil {
		return nil, fmt.Errorf("failed to create server: %w", err)
	}

	svc := &Service{
		config:      cfg,
		server:      server,
		watchers:    make([]watcher.Watcher, 0, len(cfg.Directories)),
		dispatchers: make([]*uploader.Dispatcher, 0, len(cfg.Directories)),
		shadows:     make([]*shadow.Manager, 0, len(cfg.Directories)),
	}

	// Create watchers, dispatchers, and shadow managers for each directory
	for _, dirCfg := range cfg.Directories {
		// Create shadow manager
		shadowMgr, err := shadow.NewManager(dirCfg.Shadow)
		if err != nil {
			return nil, fmt.Errorf("failed to create shadow manager for %s: %w", dirCfg.Name, err)
		}
		svc.shadows = append(svc.shadows, shadowMgr)

		// Create upload dispatcher
		dispatcher := uploader.NewDispatcher(dirCfg.Outbound, shadowMgr, 4) // 4 workers per directory
		svc.dispatchers = append(svc.dispatchers, dispatcher)

		// Create file event handler
		handler := svc.createFileHandler(dirCfg.Name, shadowMgr, dispatcher)

		// Create watcher
		w, err := watcher.NewWatcher(dirCfg, handler)
		if err != nil {
			return nil, fmt.Errorf("failed to create watcher for %s: %w", dirCfg.Name, err)
		}
		svc.watchers = append(svc.watchers, w)
	}

	// Now that all watchers are created, set the callbacks on dispatchers
	for i := range svc.dispatchers {
		// Create callback to clear enqueued files from all watchers after successful upload
		onSuccessfulUpload := func(path string) {
			for _, watcher := range svc.watchers {
				watcher.ClearEnqueued(path)
			}
		}

		svc.dispatchers[i].SetOnSuccessfulUpload(onSuccessfulUpload)
	}

	return svc, nil
}

// createFileHandler creates a file event handler for a directory
func (s *Service) createFileHandler(dirName string, shadowMgr *shadow.Manager, dispatcher *uploader.Dispatcher) watcher.EventHandler {
	return func(event watcher.FileEvent) error {
		log.Printf("[%s] File detected: %s (rename: %v)", dirName, event.Path, event.IsRename)

		// Enqueue for upload (shadow copy will be created after successful upload)
		dispatcher.Enqueue(event.Path, event.ProcessedDueToTimeout)

		return nil
	}
}

// Start starts the xferd service
func (s *Service) Start() error {
	s.ctx, s.cancel = context.WithCancel(context.Background())

	log.Println("Starting xferd service...")

	// Start upload dispatchers
	for i, dispatcher := range s.dispatchers {
		dispatcher.Start(s.ctx)
		log.Printf("Started dispatcher for directory %d", i)
	}

	// Start watchers
	for i, w := range s.watchers {
		if err := w.Start(s.ctx); err != nil {
			return fmt.Errorf("failed to start watcher %d: %w", i, err)
		}
	}

	// Start shadow cleanup routines
	s.shadowStopCh = make(chan struct{})
	for i, shadowMgr := range s.shadows {
		s.wg.Add(1)
		go func(idx int, mgr *shadow.Manager) {
			defer s.wg.Done()
			log.Printf("Starting shadow cleanup routine %d", idx)
			mgr.StartCleanupRoutine(s.shadowStopCh)
		}(i, shadowMgr)
	}

	// Start REST ingress server
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		if err := s.server.Start(s.ctx); err != nil && err != http.ErrServerClosed {
			log.Printf("Server error: %v", err)
		}
	}()

	log.Println("Xferd service started successfully")

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		log.Printf("Received signal: %v, shutting down...", sig)
	case <-s.ctx.Done():
		log.Println("Context cancelled, shutting down...")
	}

	// Stop all components
	return s.Stop()
}

// Stop stops the xferd service gracefully
func (s *Service) Stop() error {
	var err error
	s.stopOnce.Do(func() {
		log.Println("Stopping xferd service...")

		// Cancel context to stop all goroutines
		if s.cancel != nil {
			s.cancel()
		}

		// Stop REST server
		if s.server != nil {
			if serverErr := s.server.Stop(); serverErr != nil {
				log.Printf("Error stopping server: %v", serverErr)
				err = serverErr
			}
		}

		// Stop all watchers
		for i, w := range s.watchers {
			if watcherErr := w.Stop(); watcherErr != nil {
				log.Printf("Error stopping watcher %d: %v", i, watcherErr)
				if err == nil {
					err = watcherErr
				}
			}
		}

		// Stop all dispatchers
		for i, dispatcher := range s.dispatchers {
			dispatcher.Stop()
			log.Printf("Stopped dispatcher %d", i)
		}

		// Stop shadow cleanup routines
		if s.shadowStopCh != nil {
			close(s.shadowStopCh)
		}

		// Wait for all goroutines to finish
		s.wg.Wait()

		log.Println("Xferd service stopped")
	})
	return err
}

// Run loads config and runs the service
func Run(configPath string) error {
	// Load configuration
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	log.Printf("Configuration loaded: %d directories", len(cfg.Directories))

	// Log configuration details
	logConfiguration(cfg)

	// Create and start service
	svc, err := New(cfg)
	if err != nil {
		return fmt.Errorf("failed to create service: %w", err)
	}

	return svc.Start()
}

// logConfiguration logs the current configuration details on startup
func logConfiguration(cfg *config.Config) {
	log.Println("=== XFERD CONFIGURATION ===")

	// Server configuration
	log.Printf("Server: %s:%d", cfg.Server.Address, cfg.Server.Port)
	log.Printf("  Temp Directory: %s", cfg.Server.TempDir)
	if cfg.Server.TLS.Enabled {
		log.Printf("  TLS: enabled (cert: %s, key: %s)", cfg.Server.TLS.CertFile, cfg.Server.TLS.KeyFile)
	} else {
		log.Println("  TLS: disabled")
	}
	if cfg.Server.BasicAuth.Enabled {
		log.Printf("  Basic Auth: enabled (user: %s)", cfg.Server.BasicAuth.Username)
	} else {
		log.Println("  Basic Auth: disabled")
	}

	// Directory configurations
	log.Printf("Directories: %d configured", len(cfg.Directories))
	for i, dir := range cfg.Directories {
		log.Printf("Directory %d: %s", i+1, dir.Name)
		recursiveStr := ""
		if !dir.Recursive {
			recursiveStr = "non-"
		}
		log.Printf("  Watching: %s (%srecursive)", dir.WatchPath, recursiveStr)
		if dir.IngestPath != "" && dir.IngestPath != dir.WatchPath {
			log.Printf("  Ingest: %s", dir.IngestPath)
		}

		// Explain what happens and when
		switch dir.Watch.Mode {
		case "hybrid_ultra_low_latency":
			log.Printf("  Detection Method: Instant event-based detection with smart stability checks")
			log.Printf("    → Files detected immediately via filesystem events (<50-200ms latency)")
			log.Printf("    → Atomic renames processed instantly (no stability delay)")
			log.Printf("    → Regular file writes confirmed stable after %d checks every %dms (up to %dms total)",
				dir.Stability.RequiredStableChecks, dir.Stability.ConfirmationIntervalMs, dir.Stability.MaxWaitMs)
			if dir.Watch.ReconcileScan.Enabled {
				log.Printf("    → Every %d seconds: Full directory scan catches any missed files", dir.Watch.ReconcileScan.IntervalSeconds)
			}
			if dir.Watch.IsStartupReconcileScanEnabled() {
				log.Printf("    → On startup: Immediate full directory scan")
			}

		case "event_only":
			log.Printf("  Detection Method: Raw filesystem events only (UNSAFE - no stability checks)")
			log.Printf("    → Files processed immediately when filesystem events occur")
			log.Printf("    → No waiting for file writes to complete - may process incomplete files")
			log.Printf("    → Best for: Controlled environments with atomic file placement")
			if dir.Watch.ReconcileScan.Enabled {
				log.Printf("    → Every %d seconds: Full directory scan catches any missed events", dir.Watch.ReconcileScan.IntervalSeconds)
			}

		case "polling_only":
			log.Printf("  Detection Method: Periodic directory scanning only (high latency)")
			log.Printf("    → No real-time detection - relies entirely on scheduled scans")
			if dir.Watch.ReconcileScan.Enabled {
				log.Printf("    → Every %d seconds: Scans entire directory tree for new files", dir.Watch.ReconcileScan.IntervalSeconds)
				log.Printf("    → When found: Waits up to %dms checking stability every %dms (%d checks total)",
					dir.Stability.MaxWaitMs, dir.Stability.ConfirmationIntervalMs, dir.Stability.RequiredStableChecks)
			}
			if dir.Watch.IsStartupReconcileScanEnabled() {
				log.Printf("    → On startup: Immediate full directory scan")
			}
		}

		// Shadow directory explanation
		if dir.Shadow.Enabled {
			log.Printf("  Processing: Files copied to shadow directory during upload")
			log.Printf("    → Shadow Path: %s", dir.Shadow.Path)
			log.Printf("    → Cleanup: Files older than %d hours are automatically deleted", dir.Shadow.RetentionHours)
		} else {
			log.Printf("  Processing: Files deleted after successful upload (no archiving)")
		}

		// Upload explanation
		log.Printf("  Outbound Upload: Files sent to %s", dir.Outbound.URL)
		if dir.Outbound.Auth.Type == "basic" {
			log.Printf("    → Authentication: HTTP Basic Auth (%s)", dir.Outbound.Auth.Username)
		} else if dir.Outbound.Auth.Type == "bearer" {
			log.Printf("    → Authentication: Bearer token")
		} else {
			log.Printf("    → Authentication: none")
		}
		log.Printf("    → Method: Concurrent uploads with automatic retry on failure")

		// REST API ingest endpoint
		protocol := "http"
		if cfg.Server.TLS.Enabled {
			protocol = "https"
		}
		baseURL := fmt.Sprintf("%s://%s:%d", protocol, cfg.Server.Address, cfg.Server.Port)
		uploadEndpoint := fmt.Sprintf("%s/upload/%s", baseURL, dir.Name)
		log.Printf("  REST API Ingest: %s", uploadEndpoint)
		log.Printf("    → Example: curl -X POST -F \"file=@example.pdf\" %s", uploadEndpoint)
		if cfg.Server.BasicAuth.Enabled {
			log.Printf("    → Requires authentication: Basic Auth (%s)", cfg.Server.BasicAuth.Username)
		} else {
			log.Printf("    → No authentication required")
		}
		log.Printf("    → Supports subdirectories: %s/2025/01/30", uploadEndpoint)

		log.Println()
	}

	log.Println("=== SERVICE STARTING ===")
}
