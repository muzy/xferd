//go:build linux

package watcher

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/muzy/xferd/internal/config"
)

// LinuxWatcher implements recursive watching for Linux using inotify
type LinuxWatcher struct {
	config          config.DirectoryConfig
	handler         EventHandler
	watcher         *fsnotify.Watcher
	watchedDirs     map[string]bool
	processingFiles sync.Map // tracks files currently being processed for stability
	enqueuedFiles   sync.Map // tracks files that have been enqueued for upload
	mu              sync.Mutex
	ctx             context.Context
	cancel          context.CancelFunc
	wg              sync.WaitGroup
}

// newPlatformWatcher creates a Linux-specific watcher
func newPlatformWatcher(cfg config.DirectoryConfig, handler EventHandler) (Watcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create fsnotify watcher: %w", err)
	}

	return &LinuxWatcher{
		config:      cfg,
		handler:     handler,
		watcher:     w,
		watchedDirs: make(map[string]bool),
	}, nil
}

// Start begins watching the configured directory
func (w *LinuxWatcher) Start(ctx context.Context) error {
	w.ctx, w.cancel = context.WithCancel(ctx)

	// Initial directory tree walk and watch setup
	if err := w.setupWatches(); err != nil {
		return fmt.Errorf("failed to setup watches: %w", err)
	}

	// Perform startup reconciliation scan if enabled
	if w.config.Watch.IsStartupReconcileScanEnabled() {
		log.Printf("Performing startup reconciliation scan for: %s", w.config.WatchPath)
		w.performReconciliationScan()
	}

	// Start event processing goroutine
	w.wg.Add(1)
	go w.processEvents()

	// Start reconciliation scan if enabled
	if w.config.Watch.ReconcileScan.Enabled {
		w.wg.Add(1)
		go w.reconciliationScan()
	}

	log.Printf("Linux watcher started for: %s (recursive: %v)", w.config.WatchPath, w.config.Recursive)
	return nil
}

// Stop stops the watcher
func (w *LinuxWatcher) Stop() error {
	if w.cancel != nil {
		w.cancel()
	}

	if w.watcher != nil {
		w.watcher.Close()
	}

	w.wg.Wait()
	log.Printf("Linux watcher stopped for: %s", w.config.WatchPath)
	return nil
}

// setupWatches walks the directory tree and sets up watches
func (w *LinuxWatcher) setupWatches() error {
	return filepath.Walk(w.config.WatchPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return w.addWatch(path)
		}

		return nil
	})
}

// addWatch adds a directory to the watch list
func (w *LinuxWatcher) addWatch(dir string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.watchedDirs[dir] {
		return nil // Already watching
	}

	if err := w.watcher.Add(dir); err != nil {
		return fmt.Errorf("failed to add watch for %s: %w", dir, err)
	}

	w.watchedDirs[dir] = true
	log.Printf("Added watch: %s", dir)
	return nil
}

// processEvents processes filesystem events
func (w *LinuxWatcher) processEvents() {
	defer w.wg.Done()

	for {
		select {
		case <-w.ctx.Done():
			return

		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}

			w.handleEvent(event)

		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("Watcher error: %v", err)
		}
	}
}

// handleEvent processes a single filesystem event
func (w *LinuxWatcher) handleEvent(event fsnotify.Event) {
	path := event.Name

	// Handle directory creation (for recursive watching)
	if event.Op&fsnotify.Create != 0 {
		info, err := os.Stat(path)
		if err == nil && info.IsDir() && w.config.Recursive {
			// New directory created, add watch
			if err := w.addWatch(path); err != nil {
				log.Printf("Failed to add watch for new directory %s: %v", path, err)
			}
			return
		}
	}

	// Handle file events based on mode
	switch w.config.Watch.Mode {
	case "hybrid_ultra_low_latency":
		w.handleHybridEvent(event)
	case "event_only":
		w.handleEventOnly(event)
	case "polling_only":
		// Polling is handled by reconciliation scan
		return
	}
}

// handleHybridEvent handles events in hybrid mode (default)
func (w *LinuxWatcher) handleHybridEvent(event fsnotify.Event) {
	path := event.Name

	// Check if this file has already been enqueued
	_, alreadyEnqueued := w.enqueuedFiles.Load(path)
	if alreadyEnqueued {
		// Already enqueued this file, skip
		return
	}

	// IN_MOVED_TO (rename into watched directory) - fastest path
	if event.Op&fsnotify.Rename != 0 || event.Op&fsnotify.Create != 0 {
		// Check if this is actually a rename completion
		info, err := os.Stat(path)
		if err != nil {
			return // File doesn't exist
		}

		if !info.Mode().IsRegular() {
			return // Not a file
		}

		// For atomic rename (MOVED_TO), process immediately
		isRename := event.Op&fsnotify.Rename != 0

		// Process file and get event
		event, err := processFile(path, isRename, w.config)
		if err != nil {
			log.Printf("Error processing file %s: %v", path, err)
			return
		}

		// Check if event is valid (processFile returns empty event for ignored/disappeared files)
		if event.Path == "" {
			return
		}

		// Mark as enqueued
		w.enqueuedFiles.Store(path, true)

		if err := w.handler(event); err != nil {
			log.Printf("Error handling file %s: %v", path, err)
			w.enqueuedFiles.Delete(path) // Remove on failure
		}
	}

	// WRITE events - confirm stability synchronously before enqueuing
	if event.Op&fsnotify.Write != 0 {
		// Check if we're already processing this file
		_, alreadyProcessing := w.processingFiles.LoadOrStore(path, true)
		if alreadyProcessing {
			// Already processing this file, skip
			return
		}

		// File being written - confirm stability synchronously
		go func() {
			defer w.processingFiles.Delete(path) // Clean up when done

			// Process file and get event
			event, err := processFile(path, false, w.config)
			if err != nil {
				log.Printf("Error processing file %s: %v", path, err)
				return
			}

			// Check if event is valid (processFile returns empty event for ignored/disappeared files)
			if event.Path == "" {
				return
			}

			// Mark as enqueued
			w.enqueuedFiles.Store(path, true)

			if err := w.handler(event); err != nil {
				log.Printf("Error handling file %s: %v", path, err)
				w.enqueuedFiles.Delete(path) // Remove on failure
			}
		}()
	}
}

// handleEventOnly handles events in event-only mode (unsafe)
func (w *LinuxWatcher) handleEventOnly(event fsnotify.Event) {
	if event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Rename) != 0 {
		// Process immediately without stability check
		fileEvent := FileEvent{
			Path:      event.Name,
			IsRename:  event.Op&fsnotify.Rename != 0,
			Timestamp: time.Now(),
		}

		if err := w.handler(fileEvent); err != nil {
			log.Printf("Error processing file %s: %v", event.Name, err)
		}
	}
}

// reconciliationScan periodically scans the directory for missed files
func (w *LinuxWatcher) reconciliationScan() {
	defer w.wg.Done()

	interval := w.config.Watch.ReconcileScan.GetReconcileInterval()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-w.ctx.Done():
			return
		case <-ticker.C:
			w.performReconciliationScan()
		}
	}
}

// ClearEnqueued removes a file from the enqueued tracking
func (w *LinuxWatcher) ClearEnqueued(path string) {
	w.enqueuedFiles.Delete(path)
}

// performReconciliationScan scans for files that may have been missed
func (w *LinuxWatcher) performReconciliationScan() {
	log.Printf("Performing reconciliation scan for: %s", w.config.WatchPath)

	err := filepath.Walk(w.config.WatchPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}

		if !info.Mode().IsRegular() {
			return nil
		}

		if ShouldIgnore(path, w.config.Ignore) {
			return nil
		}

		// Check if this file has already been enqueued
		_, alreadyEnqueued := w.enqueuedFiles.Load(path)
		if alreadyEnqueued {
			return nil // Already processed
		}

		// Check if we're already processing this file
		_, alreadyProcessing := w.processingFiles.LoadOrStore(path, true)
		if alreadyProcessing {
			return nil // Already being processed
		}

		// Check if file is stable and process
		if stable, _ := isStable(path, w.config.Stability); stable {
			// Process file and get event
			event, err := processFile(path, false, w.config)
			if err != nil {
				log.Printf("Reconciliation: error processing %s: %v", path, err)
				w.processingFiles.Delete(path)
				return nil
			}

			// Check if event is valid (processFile returns empty event for ignored/disappeared files)
			if event.Path == "" {
				w.processingFiles.Delete(path)
				return nil
			}

			// Mark as enqueued
			w.enqueuedFiles.Store(path, true)

			if err := w.handler(event); err != nil {
				log.Printf("Reconciliation: error handling file %s: %v", path, err)
				w.enqueuedFiles.Delete(path) // Remove on failure
			}
			w.processingFiles.Delete(path)
		} else {
			w.processingFiles.Delete(path)
		}

		return nil
	})

	if err != nil {
		log.Printf("Reconciliation scan error: %v", err)
	}
}
