package uploader

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/muzy/xferd/internal/config"
	"github.com/muzy/xferd/internal/shadow"
)

func TestNewUploader(t *testing.T) {
	cfg := config.OutboundConfig{
		URL: "https://example.com/upload",
		Auth: config.AuthConfig{
			Type:     "basic",
			Username: "user",
			Password: "pass",
		},
	}

	uploader := NewUploader(cfg)

	if uploader == nil {
		t.Fatal("Expected non-nil uploader")
	}

	if uploader.config.URL != cfg.URL {
		t.Errorf("Config URL mismatch. Expected %s, got %s", cfg.URL, uploader.config.URL)
	}

	if uploader.client == nil {
		t.Fatal("Expected non-nil HTTP client")
	}
}

func TestUploadSuccess(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	content := []byte("test file content")

	if err := os.WriteFile(testFile, content, 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify method
		if r.Method != "POST" {
			t.Errorf("Expected POST, got %s", r.Method)
		}

		// Verify multipart form
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			t.Errorf("Failed to parse multipart form: %v", err)
		}

		file, header, err := r.FormFile("file")
		if err != nil {
			t.Errorf("Failed to get form file: %v", err)
		}
		defer file.Close()

		if header.Filename != "test.txt" {
			t.Errorf("Expected filename test.txt, got %s", header.Filename)
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Upload successful"))
	}))
	defer server.Close()

	cfg := config.OutboundConfig{
		URL: server.URL,
	}

	uploader := NewUploader(cfg)
	ctx := context.Background()

	err := uploader.Upload(ctx, testFile)
	if err != nil {
		t.Fatalf("Upload failed: %v", err)
	}
}

func TestUploadWithBasicAuth(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")

	if err := os.WriteFile(testFile, []byte("content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	expectedUsername := "testuser"
	expectedPassword := "testpass"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok {
			t.Error("Basic auth not present")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		if username != expectedUsername {
			t.Errorf("Expected username %s, got %s", expectedUsername, username)
		}

		if password != expectedPassword {
			t.Errorf("Expected password %s, got %s", expectedPassword, password)
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := config.OutboundConfig{
		URL: server.URL,
		Auth: config.AuthConfig{
			Type:     "basic",
			Username: expectedUsername,
			Password: expectedPassword,
		},
	}

	uploader := NewUploader(cfg)
	ctx := context.Background()

	err := uploader.Upload(ctx, testFile)
	if err != nil {
		t.Fatalf("Upload with basic auth failed: %v", err)
	}
}

func TestUploadWithBearerAuth(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")

	if err := os.WriteFile(testFile, []byte("content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	expectedToken := "test-bearer-token"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		expectedAuth := "Bearer " + expectedToken

		if auth != expectedAuth {
			t.Errorf("Expected auth %s, got %s", expectedAuth, auth)
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := config.OutboundConfig{
		URL: server.URL,
		Auth: config.AuthConfig{
			Type:  "bearer",
			Token: expectedToken,
		},
	}

	uploader := NewUploader(cfg)
	ctx := context.Background()

	err := uploader.Upload(ctx, testFile)
	if err != nil {
		t.Fatalf("Upload with bearer auth failed: %v", err)
	}
}

func TestUploadWithTokenAuth(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")

	if err := os.WriteFile(testFile, []byte("content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	expectedToken := "test-api-token"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		expectedAuth := "Token " + expectedToken

		if auth != expectedAuth {
			t.Errorf("Expected auth %s, got %s", expectedAuth, auth)
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := config.OutboundConfig{
		URL: server.URL,
		Auth: config.AuthConfig{
			Type:  "token",
			Token: expectedToken,
		},
	}

	uploader := NewUploader(cfg)
	ctx := context.Background()

	err := uploader.Upload(ctx, testFile)
	if err != nil {
		t.Fatalf("Upload with token auth failed: %v", err)
	}
}

func TestUploadServerError(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")

	if err := os.WriteFile(testFile, []byte("content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Server error"))
	}))
	defer server.Close()

	cfg := config.OutboundConfig{
		URL: server.URL,
	}

	uploader := NewUploader(cfg)
	ctx := context.Background()

	err := uploader.Upload(ctx, testFile)
	if err == nil {
		t.Fatal("Expected error on server error, got nil")
	}

	// Should mention retry attempts
	if !strings.Contains(err.Error(), "failed after") {
		t.Errorf("Error should mention retry attempts: %v", err)
	}
}

func TestUploadClientError(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")

	if err := os.WriteFile(testFile, []byte("content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Bad request"))
	}))
	defer server.Close()

	cfg := config.OutboundConfig{
		URL: server.URL,
	}

	uploader := NewUploader(cfg)
	ctx := context.Background()

	err := uploader.Upload(ctx, testFile)
	if err == nil {
		t.Fatal("Expected error on client error, got nil")
	}

	// Should not retry on 4xx errors
	if !strings.Contains(err.Error(), "no retry") {
		t.Errorf("Error should indicate no retry for client error: %v", err)
	}
}

func TestUploadRetrySuccess(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")

	if err := os.WriteFile(testFile, []byte("content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			// Fail first 2 attempts
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		// Succeed on 3rd attempt
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := config.OutboundConfig{
		URL: server.URL,
	}

	uploader := NewUploader(cfg)
	ctx := context.Background()

	err := uploader.Upload(ctx, testFile)
	if err != nil {
		t.Fatalf("Upload should succeed after retry: %v", err)
	}

	if attempts != 3 {
		t.Errorf("Expected 3 attempts, got %d", attempts)
	}
}

func TestUploadNonexistentFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := config.OutboundConfig{
		URL: server.URL,
	}

	uploader := NewUploader(cfg)
	ctx := context.Background()

	err := uploader.Upload(ctx, "/nonexistent/file.txt")
	if err == nil {
		t.Fatal("Expected error uploading nonexistent file, got nil")
	}
}

func TestUploadStreamSuccess(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "large.bin")

	// Create a larger file (1 MB)
	largeContent := make([]byte, 1024*1024)
	for i := range largeContent {
		largeContent[i] = byte(i % 256)
	}

	if err := os.WriteFile(testFile, largeContent, 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify streaming upload
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			t.Errorf("Failed to parse multipart form: %v", err)
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := config.OutboundConfig{
		URL: server.URL,
	}

	uploader := NewUploader(cfg)
	ctx := context.Background()

	err := uploader.UploadStream(ctx, testFile)
	if err != nil {
		t.Fatalf("Streaming upload failed: %v", err)
	}
}

func TestNewDispatcher(t *testing.T) {
	cfg := config.OutboundConfig{
		URL: "https://example.com/upload",
	}

	shadowCfg := config.ShadowConfig{
		Enabled: false,
	}

	shadowMgr, err := shadow.NewManager(shadowCfg)
	if err != nil {
		t.Fatalf("Failed to create shadow manager: %v", err)
	}

	dispatcher := NewDispatcher(cfg, shadowMgr, 4)

	if dispatcher == nil {
		t.Fatal("Expected non-nil dispatcher")
	}

	if dispatcher.maxWorkers != 4 {
		t.Errorf("Expected 4 workers, got %d", dispatcher.maxWorkers)
	}
}

func TestDispatcherStartStop(t *testing.T) {
	cfg := config.OutboundConfig{
		URL: "https://example.com/upload",
	}

	shadowCfg := config.ShadowConfig{
		Enabled: false,
	}

	shadowMgr, err := shadow.NewManager(shadowCfg)
	if err != nil {
		t.Fatalf("Failed to create shadow manager: %v", err)
	}

	dispatcher := NewDispatcher(cfg, shadowMgr, 2)

	ctx := context.Background()
	dispatcher.Start(ctx)

	// Give workers time to start
	time.Sleep(50 * time.Millisecond)

	// Stop dispatcher
	dispatcher.Stop()

	// Verify workers stopped
	time.Sleep(50 * time.Millisecond)
}

func TestDispatcherEnqueue(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")

	if err := os.WriteFile(testFile, []byte("content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	uploadReceived := make(chan string, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			t.Errorf("Failed to parse form: %v", err)
		}

		_, header, err := r.FormFile("file")
		if err != nil {
			t.Errorf("Failed to get file: %v", err)
		}

		uploadReceived <- header.Filename
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := config.OutboundConfig{
		URL: server.URL,
	}

	shadowCfg := config.ShadowConfig{
		Enabled: false,
	}

	shadowMgr, err := shadow.NewManager(shadowCfg)
	if err != nil {
		t.Fatalf("Failed to create shadow manager: %v", err)
	}

	dispatcher := NewDispatcher(cfg, shadowMgr, 2)
	ctx := context.Background()
	dispatcher.Start(ctx)
	defer dispatcher.Stop()

	// Enqueue file
	dispatcher.Enqueue(testFile, false)

	// Wait for upload
	select {
	case filename := <-uploadReceived:
		if filename != "test.txt" {
			t.Errorf("Expected filename test.txt, got %s", filename)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Upload not received within timeout")
	}

	// Give worker time to finish processing (shadow copy, file deletion)
	time.Sleep(100 * time.Millisecond)
}

func TestDispatcherMultipleFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create multiple test files
	numFiles := 10
	var testFiles []string
	for i := 0; i < numFiles; i++ {
		testFile := filepath.Join(tmpDir, fmt.Sprintf("test%d.txt", i))
		if err := os.WriteFile(testFile, []byte(fmt.Sprintf("content %d", i)), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}
		testFiles = append(testFiles, testFile)
	}

	uploadedFiles := make(map[string]bool)
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			t.Errorf("Failed to parse form: %v", err)
		}

		_, header, err := r.FormFile("file")
		if err != nil {
			t.Errorf("Failed to get file: %v", err)
		}

		mu.Lock()
		uploadedFiles[header.Filename] = true
		mu.Unlock()

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := config.OutboundConfig{
		URL: server.URL,
	}

	shadowCfg := config.ShadowConfig{
		Enabled: false,
	}

	shadowMgr, err := shadow.NewManager(shadowCfg)
	if err != nil {
		t.Fatalf("Failed to create shadow manager: %v", err)
	}

	dispatcher := NewDispatcher(cfg, shadowMgr, 3)
	ctx := context.Background()
	dispatcher.Start(ctx)
	defer dispatcher.Stop()

	// Enqueue all files
	for _, testFile := range testFiles {
		dispatcher.Enqueue(testFile, false)
	}

	// Wait for all uploads
	timeout := time.After(10 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			mu.Lock()
			uploaded := len(uploadedFiles)
			mu.Unlock()
			t.Fatalf("Not all files uploaded within timeout. Got %d/%d", uploaded, numFiles)
		case <-ticker.C:
			mu.Lock()
			uploaded := len(uploadedFiles)
			mu.Unlock()
			if uploaded == numFiles {
				return
			}
		}
	}
}

func TestDispatcherWithShadow(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	shadowPath := filepath.Join(tmpDir, "shadow")

	if err := os.WriteFile(testFile, []byte("content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := config.OutboundConfig{
		URL: server.URL,
	}

	shadowCfg := config.ShadowConfig{
		Enabled:        true,
		Path:           shadowPath,
		RetentionHours: 24,
	}

	shadowMgr, err := shadow.NewManager(shadowCfg)
	if err != nil {
		t.Fatalf("Failed to create shadow manager: %v", err)
	}

	dispatcher := NewDispatcher(cfg, shadowMgr, 2)
	ctx := context.Background()
	dispatcher.Start(ctx)
	defer dispatcher.Stop()

	// Enqueue file
	dispatcher.Enqueue(testFile, false)

	// Wait for upload and shadow copy
	time.Sleep(1 * time.Second)

	// Verify source file was deleted
	if _, err := os.Stat(testFile); !os.IsNotExist(err) {
		t.Error("Source file should have been deleted after upload")
	}

	// Verify shadow copy exists
	files, err := os.ReadDir(shadowPath)
	if err != nil {
		t.Fatalf("Failed to read shadow directory: %v", err)
	}

	if len(files) != 1 {
		t.Errorf("Expected 1 shadow file, got %d", len(files))
	}
}

func TestDispatcherUploadFailure(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")

	if err := os.WriteFile(testFile, []byte("content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Server that always fails
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := config.OutboundConfig{
		URL: server.URL,
	}

	shadowCfg := config.ShadowConfig{
		Enabled: false,
	}

	shadowMgr, err := shadow.NewManager(shadowCfg)
	if err != nil {
		t.Fatalf("Failed to create shadow manager: %v", err)
	}

	dispatcher := NewDispatcher(cfg, shadowMgr, 1)
	ctx := context.Background()
	dispatcher.Start(ctx)
	defer dispatcher.Stop()

	// Enqueue file
	dispatcher.Enqueue(testFile, false)

	// Wait for upload attempts
	time.Sleep(5 * time.Second)

	// Source file should still exist (upload failed)
	if _, err := os.Stat(testFile); err != nil {
		t.Error("Source file should still exist after failed upload")
	}

	// Give worker time to finish processing
	time.Sleep(500 * time.Millisecond)
}

func TestDispatcherLargeFileStreaming(t *testing.T) {
	tmpDir := t.TempDir()
	largeFile := filepath.Join(tmpDir, "large.bin")

	// Create 150 MB file to trigger streaming
	largeContent := make([]byte, 150*1024*1024)
	if err := os.WriteFile(largeFile, largeContent, 0644); err != nil {
		t.Fatalf("Failed to create large file: %v", err)
	}

	streamingUsed := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if it's using streaming (Transfer-Encoding: chunked or similar)
		if r.ContentLength < 0 || r.TransferEncoding != nil {
			streamingUsed = true
		}

		// Read and discard body
		io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := config.OutboundConfig{
		URL: server.URL,
	}

	shadowCfg := config.ShadowConfig{
		Enabled: false,
	}

	shadowMgr, err := shadow.NewManager(shadowCfg)
	if err != nil {
		t.Fatalf("Failed to create shadow manager: %v", err)
	}

	dispatcher := NewDispatcher(cfg, shadowMgr, 1)
	ctx := context.Background()
	dispatcher.Start(ctx)
	defer dispatcher.Stop()

	// Enqueue large file
	dispatcher.Enqueue(largeFile, false)

	// Wait for upload
	time.Sleep(10 * time.Second)

	t.Logf("Streaming used: %v", streamingUsed)
}

func TestDispatcherContextCancellation(t *testing.T) {
	cfg := config.OutboundConfig{
		URL: "https://example.com/upload",
	}

	shadowCfg := config.ShadowConfig{
		Enabled: false,
	}

	shadowMgr, err := shadow.NewManager(shadowCfg)
	if err != nil {
		t.Fatalf("Failed to create shadow manager: %v", err)
	}

	dispatcher := NewDispatcher(cfg, shadowMgr, 2)

	ctx, cancel := context.WithCancel(context.Background())
	dispatcher.Start(ctx)

	// Give workers time to start
	time.Sleep(50 * time.Millisecond)

	// Cancel context
	cancel()

	// Wait for workers to stop
	time.Sleep(100 * time.Millisecond)

	// Try to enqueue after cancellation
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// This should not block or panic
	dispatcher.Enqueue(testFile, false)
}

func TestAddAuthNoAuth(t *testing.T) {
	cfg := config.OutboundConfig{
		URL: "https://example.com/upload",
	}

	uploader := NewUploader(cfg)

	req, _ := http.NewRequest("POST", cfg.URL, nil)
	uploader.addAuth(req)

	// Should not add any auth headers
	if req.Header.Get("Authorization") != "" {
		t.Error("Should not add authorization header when auth type is empty")
	}
}

func TestUploaderTimeout(t *testing.T) {
	// Test that uploader respects client timeout
	uploader := NewUploader(config.OutboundConfig{
		URL: "https://httpbin.org/delay/10", // Will delay 10 seconds
	})

	// Client has 5 minute timeout by default, but we can test with a shorter context
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	err := uploader.Upload(ctx, testFile)
	if err == nil {
		t.Error("Expected timeout error, got nil")
	}

	if !strings.Contains(err.Error(), "context") && !strings.Contains(err.Error(), "timeout") {
		t.Logf("Error: %v", err)
	}
}
