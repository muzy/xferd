package uploader

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/muzy/xferd/internal/config"
	"github.com/muzy/xferd/internal/shadow"
)

// Uploader handles outbound file uploads
type Uploader struct {
	config config.OutboundConfig
	client *http.Client
}

// NewUploader creates a new uploader
func NewUploader(cfg config.OutboundConfig) *Uploader {
	return &Uploader{
		config: cfg,
		client: &http.Client{
			Timeout: 5 * time.Minute, // Long timeout for large files
		},
	}
}

// Upload sends a file to the configured endpoint
func (u *Uploader) Upload(ctx context.Context, filePath string) error {
	// Open the file
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// Get file info
	fileInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	// Prepare multipart upload
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Create form file
	part, err := writer.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return fmt.Errorf("failed to create form file: %w", err)
	}

	// Copy file content
	if _, err := io.Copy(part, file); err != nil {
		return fmt.Errorf("failed to copy file content: %w", err)
	}

	// Close multipart writer
	if err := writer.Close(); err != nil {
		return fmt.Errorf("failed to close multipart writer: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", u.config.URL, body)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())

	// Add authentication
	u.addAuth(req)

	// Execute request with retries
	return u.executeWithRetry(req, filePath, fileInfo.Size())
}

// UploadStream uploads using streaming to handle large files efficiently
func (u *Uploader) UploadStream(ctx context.Context, filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	// Create a pipe for streaming
	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)

	// Write multipart data in a goroutine
	go func() {
		defer pw.Close()
		defer writer.Close()

		part, err := writer.CreateFormFile("file", filepath.Base(filePath))
		if err != nil {
			pw.CloseWithError(err)
			return
		}

		if _, err := io.Copy(part, file); err != nil {
			pw.CloseWithError(err)
			return
		}
	}()

	// Create request with pipe reader
	req, err := http.NewRequestWithContext(ctx, "POST", u.config.URL, pr)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	u.addAuth(req)

	// Execute request
	return u.executeWithRetry(req, filePath, fileInfo.Size())
}

// addAuth adds authentication to the request
func (u *Uploader) addAuth(req *http.Request) {
	switch u.config.Auth.Type {
	case "basic":
		req.SetBasicAuth(u.config.Auth.Username, u.config.Auth.Password)
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+u.config.Auth.Token)
	case "token":
		req.Header.Set("Authorization", "Token "+u.config.Auth.Token)
	}
}

// executeWithRetry executes the request with retry logic
func (u *Uploader) executeWithRetry(req *http.Request, filePath string, fileSize int64) error {
	maxRetries := 3
	backoff := time.Second

	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			log.Printf("Upload retry %d/%d for %s", attempt, maxRetries, filePath)

			// Check if context is cancelled before sleeping
			select {
			case <-req.Context().Done():
				return fmt.Errorf("upload cancelled: %w", req.Context().Err())
			case <-time.After(backoff):
				// Continue with retry
			}
			backoff *= 2
		}

		resp, err := u.client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("request failed: %w", err)
			// Check if this is a context cancellation error
			if req.Context().Err() != nil {
				return fmt.Errorf("upload cancelled: %w", req.Context().Err())
			}
			continue
		}

		// Read and close response body
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		// Check status code
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			log.Printf("Upload successful: %s (size: %d bytes, status: %d)",
				filePath, fileSize, resp.StatusCode)
			return nil
		}

		// 4xx errors - don't retry (client error)
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			return fmt.Errorf("client error (no retry): %d - %s", resp.StatusCode, string(body))
		}

		// 5xx errors - retry (server error)
		lastErr = fmt.Errorf("server error: %d - %s", resp.StatusCode, string(body))
	}

	return fmt.Errorf("upload failed after %d attempts: %w", maxRetries+1, lastErr)
}

// Dispatcher manages upload queue and concurrency
type Dispatcher struct {
	uploader           *Uploader
	shadowManager      *shadow.Manager
	workQueue          chan fileEvent
	maxWorkers         int
	onSuccessfulUpload func(path string) // callback for successful uploads
	ctx                context.Context
	cancel             context.CancelFunc
	stopped            bool
	stopMu             sync.Mutex
	wg                 sync.WaitGroup // track worker goroutines
}

// SetOnSuccessfulUpload sets the callback for successful uploads
func (d *Dispatcher) SetOnSuccessfulUpload(callback func(path string)) {
	d.onSuccessfulUpload = callback
}

// fileEvent represents a file to be uploaded with metadata
type fileEvent struct {
	path                  string
	processedDueToTimeout bool
}

// NewDispatcher creates a new upload dispatcher
func NewDispatcher(cfg config.OutboundConfig, shadowMgr *shadow.Manager, maxWorkers int) *Dispatcher {
	return &Dispatcher{
		uploader:      NewUploader(cfg),
		shadowManager: shadowMgr,
		workQueue:     make(chan fileEvent, 100),
		maxWorkers:    maxWorkers,
	}
}

// Start starts the dispatcher workers
func (d *Dispatcher) Start(ctx context.Context) {
	d.ctx, d.cancel = context.WithCancel(ctx)

	// Start worker goroutines
	for i := 0; i < d.maxWorkers; i++ {
		d.wg.Add(1)
		go d.worker(i)
	}

	log.Printf("Upload dispatcher started with %d workers", d.maxWorkers)
}

// Stop stops the dispatcher and waits for all workers to finish
func (d *Dispatcher) Stop() {
	d.stopMu.Lock()
	if d.stopped {
		d.stopMu.Unlock()
		return
	}
	d.stopped = true
	d.stopMu.Unlock()

	// Cancel context to signal workers to stop
	if d.cancel != nil {
		d.cancel()
	}

	// Close work queue to unblock workers waiting on queue
	close(d.workQueue)

	// Wait for all workers to finish processing
	d.wg.Wait()
	log.Printf("All upload workers stopped")
}

// Enqueue adds a file to the upload queue
func (d *Dispatcher) Enqueue(filePath string, processedDueToTimeout bool) {
	event := fileEvent{
		path:                  filePath,
		processedDueToTimeout: processedDueToTimeout,
	}

	select {
	case d.workQueue <- event:
		log.Printf("Enqueued for upload: %s", filePath)
	case <-d.ctx.Done():
		log.Printf("Dispatcher stopped, cannot enqueue: %s", filePath)
	default:
		log.Printf("Upload queue full, dropping: %s", filePath)
	}
}

// worker processes files from the queue
func (d *Dispatcher) worker(id int) {
	defer d.wg.Done()
	log.Printf("Upload worker %d started", id)

	for {
		select {
		case <-d.ctx.Done():
			log.Printf("Upload worker %d stopped", id)
			return

		case event, ok := <-d.workQueue:
			if !ok {
				log.Printf("Upload worker %d stopped (queue closed)", id)
				return
			}

			filePath := event.path

			// Upload the file (use streaming for large files)
			fileInfo, err := os.Stat(filePath)
			if err != nil {
				log.Printf("Worker %d: failed to stat %s: %v", id, filePath, err)
				continue
			}

			// Use streaming for files larger than 100MB
			if fileInfo.Size() > 100*1024*1024 {
				err = d.uploader.UploadStream(d.ctx, filePath)
			} else {
				err = d.uploader.Upload(d.ctx, filePath)
			}

			if err != nil {
				log.Printf("Worker %d: upload failed for %s: %v", id, filePath, err)
			} else {
				log.Printf("Worker %d: upload completed: %s", id, filePath)

				// Call success callback if provided
				if d.onSuccessfulUpload != nil {
					d.onSuccessfulUpload(filePath)
				}

				// If file was processed due to timeout, it may still be writing - don't delete
				if event.processedDueToTimeout {
					log.Printf("Worker %d: keeping source file %s (processed due to stability timeout)", id, filePath)
					continue
				}

				// Get file info before shadow copy for final stability check
				info, err := os.Stat(filePath)
				if err != nil {
					log.Printf("Worker %d: failed to stat file before shadow copy %s: %v", id, filePath, err)
					log.Printf("Worker %d: keeping source file due to stat failure", id)
					continue
				}
				preShadowSize := info.Size()
				preShadowModTime := info.ModTime()

				// Create shadow copy
				if err := d.shadowManager.Store(filePath); err != nil {
					log.Printf("Worker %d: failed to create shadow copy for %s: %v", id, filePath, err)
					log.Printf("Worker %d: keeping source file due to shadow copy failure", id)
					continue
				}

				// Final stability check before deletion
				// If file changed during upload/shadow process, don't delete it
				if info, err := os.Stat(filePath); err != nil {
					log.Printf("Worker %d: file disappeared before deletion check: %s", id, filePath)
				} else if info.Size() != preShadowSize || !info.ModTime().Equal(preShadowModTime) {
					log.Printf("Worker %d: file changed during processing, keeping source: %s", id, filePath)
					log.Printf("Worker %d: size before: %d, after: %d", id, preShadowSize, info.Size())
				} else {
					// File is still stable, safe to delete source
					if err := os.Remove(filePath); err != nil {
						log.Printf("Worker %d: failed to delete source file %s: %v", id, filePath, err)
					} else {
						log.Printf("Worker %d: deleted source file: %s", id, filePath)
					}
				}
			}
		}
	}
}
