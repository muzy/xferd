//go:build windows

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

// WindowsWatcher implements watching for Windows
type WindowsWatcher struct {
	config          config.DirectoryConfig
	handler         EventHandler
	watcher         *fsnotify.Watcher
	processingFiles sync.Map // tracks files currently being processed for stability
	enqueuedFiles   sync.Map // tracks files that have been enqueued for upload
	ctx             context.Context
	cancel          context.CancelFunc
	wg              sync.WaitGroup
}

// newPlatformWatcher creates a Windows-specific watcher
func newPlatformWatcher(cfg config.DirectoryConfig, handler EventHandler) (Watcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create fsnotify watcher: %w", err)
	}

	return &WindowsWatcher{
		config:  cfg,
		handler: handler,
		watcher: w,
	}, nil
}

// Start begins watching the configured directory
func (w *WindowsWatcher) Start(ctx context.Context) error {
	w.ctx, w.cancel = context.WithCancel(ctx)

	// Add watch for the root directory
	// On Windows, fsnotify supports recursive watching natively
	if err := w.watcher.Add(w.config.WatchPath); err != nil {
		return fmt.Errorf("failed to add watch: %w", err)
	}

	// Perform startup reconciliation scan if enabled
	if w.config.Watch.IsStartupReconcileScanEnabled() {
		log.Printf("Performing startup reconciliation scan for: %s", w.config.WatchPath)
		w.performReconciliationScan()
	}

	// Start event processing
	w.wg.Add(1)
	go w.processEvents()

	// Start reconciliation scan if enabled
	if w.config.Watch.ReconcileScan.Enabled {
		w.wg.Add(1)
		go w.reconciliationScan()
	}

	log.Printf("Windows watcher started for: %s (recursive: %v)", w.config.WatchPath, w.config.Recursive)
	return nil
}

// Stop stops the watcher
func (w *WindowsWatcher) Stop() error {
	if w.cancel != nil {
		w.cancel()
	}

	if w.watcher != nil {
		w.watcher.Close()
	}

	w.wg.Wait()
	log.Printf("Windows watcher stopped for: %s", w.config.WatchPath)
	return nil
}

// processEvents processes filesystem events
func (w *WindowsWatcher) processEvents() {
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

// handleEvent processes a filesystem event
func (w *WindowsWatcher) handleEvent(event fsnotify.Event) {
	path := event.Name

	// Check if it's a file (not directory)
	info, err := os.Stat(path)
	if err != nil {
		return // File doesn't exist or error
	}

	if !info.Mode().IsRegular() {
		return // Not a regular file
	}

	// Handle based on watch mode
	switch w.config.Watch.Mode {
	case "hybrid_ultra_low_latency":
		w.handleHybridEvent(event)
	case "event_only":
		w.handleEventOnly(event)
	case "polling_only":
		// Pure polling mode - ignore events
		return
	}
}

// handleHybridEvent handles events in hybrid mode
func (w *WindowsWatcher) handleHybridEvent(event fsnotify.Event) {
	// RENAME_NEW_NAME on Windows indicates file completion
	// Still do a short stability check
	if event.Op&(fsnotify.Rename|fsnotify.Create|fsnotify.Write) != 0 {
		path := event.Name

		// Check if this file has already been enqueued
		_, alreadyEnqueued := w.enqueuedFiles.Load(path)
		if alreadyEnqueued {
			// Already enqueued this file, skip
			return
		}

		isRename := event.Op&fsnotify.Rename != 0

		// Check if we're already processing this file
		_, alreadyProcessing := w.processingFiles.LoadOrStore(path, true)
		if alreadyProcessing {
			// Already processing this file, skip
			return
		}

		go func() {
			defer w.processingFiles.Delete(path) // Clean up when done

			// Process file and get event
			event, err := processFile(path, isRename, w.config)
			if err != nil {
				log.Printf("Error processing file %s: %v", path, err)
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

// handleEventOnly handles events in event-only mode
func (w *WindowsWatcher) handleEventOnly(event fsnotify.Event) {
	if event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Rename) != 0 {
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
func (w *WindowsWatcher) reconciliationScan() {
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
func (w *WindowsWatcher) ClearEnqueued(path string) {
	w.enqueuedFiles.Delete(path)
}

// performReconciliationScan scans for files that may have been missed
func (w *WindowsWatcher) performReconciliationScan() {
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
