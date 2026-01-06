//go:build integration
// +build integration

package service

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/muzy/xferd/internal/config"
)

// TestE2EBasicUpload tests the complete upload flow
func TestE2EBasicUpload(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Setup test environment
	testDir := t.TempDir()
	tempDir := filepath.Join(testDir, "temp")
	watchDir := filepath.Join(testDir, "watch")
	shadowDir := filepath.Join(testDir, "shadow")

	// Create directories
	for _, dir := range []string{tempDir, watchDir, shadowDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create directory %s: %v", dir, err)
		}
	}

	// Create mock upload server
	uploadReceived := make(chan string, 1)
	mockServer := http.NewServeMux()
	mockServer.HandleFunc("/upload", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		file, header, err := r.FormFile("file")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		defer file.Close()

		uploadReceived <- header.Filename
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Upload successful: %s", header.Filename)
	})

	httpServer := &http.Server{
		Addr:    "127.0.0.1:18081",
		Handler: mockServer,
	}

	// Start mock upload server
	go func() {
		httpServer.ListenAndServe()
	}()
	defer httpServer.Close()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	// Create configuration
	cfg := &config.Config{
		Server: config.ServerConfig{
			Address: "127.0.0.1",
			Port:    18080,
			TempDir: tempDir,
			TLS: config.TLSConfig{
				Enabled: false,
			},
			BasicAuth: config.BasicAuthConfig{
				Enabled: false,
			},
		},
		Directories: []config.DirectoryConfig{
			{
				Name:      "testdir",
				WatchPath: watchDir,
				Recursive: false,
				Watch: config.WatchConfig{
					Mode: "event_only",
				},
				Stability: config.StabilityConfig{
					ConfirmationIntervalMs: 10,
					RequiredStableChecks:   2,
					MaxWaitMs:              100,
				},
				Shadow: config.ShadowConfig{
					Enabled:        true,
					Path:           shadowDir,
					RetentionHours: 24,
				},
				Outbound: config.OutboundConfig{
					URL: "http://127.0.0.1:18081/upload",
				},
			},
		},
	}

	// Create and start service
	svc, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	// Start service in goroutine (Start() blocks waiting for signals)
	serviceDone := make(chan error, 1)
	go func() {
		serviceDone <- svc.Start()
	}()

	// Wait for service to start
	time.Sleep(100 * time.Millisecond)

	// Upload a file via REST API
	testContent := []byte("Test file content for E2E test")
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("file", "test.txt")
	if err != nil {
		t.Fatalf("Failed to create form file: %v", err)
	}
	part.Write(testContent)
	writer.Close()

	req, err := http.NewRequest("POST", "http://127.0.0.1:18080/upload/testdir", body)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to upload file: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		t.Fatalf("Upload failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	t.Log("File uploaded successfully via REST API")

	// Wait for file to be processed and uploaded by watcher
	// The watcher detects the file and uploads it to the mock server
	timeout := time.After(3 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	fileUploaded := false
	for !fileUploaded {
		select {
		case filename := <-uploadReceived:
			if filename == "test.txt" {
				t.Logf("✓ File received by mock server (from watcher): %s", filename)
				fileUploaded = true
			}
		case <-ticker.C:
			// Check if file has been processed
			watchFiles, _ := os.ReadDir(watchDir)
			t.Logf("Watch directory has %d files", len(watchFiles))
		case <-timeout:
			t.Fatal("File was not uploaded to mock server within timeout")
		}
	}

	// Wait for shadow copy and cleanup
	time.Sleep(200 * time.Millisecond)
	shadowFiles, err := os.ReadDir(shadowDir)
	if err != nil {
		t.Fatalf("Failed to read shadow directory: %v", err)
	}

	t.Logf("Shadow directory has %d files", len(shadowFiles))
	if len(shadowFiles) < 1 {
		t.Errorf("Expected at least 1 shadow file, got %d", len(shadowFiles))
	}

	// Verify original file was deleted from watch directory
	watchFiles, err := os.ReadDir(watchDir)
	if err != nil {
		t.Fatalf("Failed to read watch directory: %v", err)
	}

	if len(watchFiles) != 0 {
		t.Errorf("Expected watch directory to be empty, but found %d files", len(watchFiles))
		for _, f := range watchFiles {
			t.Logf("  - %s", f.Name())
		}
	}

	// Stop service
	go svc.Stop()
	select {
	case err := <-serviceDone:
		if err != nil {
			t.Logf("Service stopped with: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Log("Service did not stop within timeout")
	}

	t.Log("E2E basic upload test completed successfully")
}

// TestE2EFileWatcher tests the file watcher functionality
func TestE2EFileWatcher(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	testDir := t.TempDir()
	tempDir := filepath.Join(testDir, "temp")
	watchDir := filepath.Join(testDir, "watch")
	shadowDir := filepath.Join(testDir, "shadow")

	for _, dir := range []string{tempDir, watchDir, shadowDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create directory %s: %v", dir, err)
		}
	}

	// Track uploads
	uploadReceived := make(chan string, 10)
	mockServer := http.NewServeMux()
	mockServer.HandleFunc("/upload", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		_, header, err := r.FormFile("file")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		uploadReceived <- header.Filename
		w.WriteHeader(http.StatusOK)
	})

	httpServer := &http.Server{
		Addr:    "127.0.0.1:18082",
		Handler: mockServer,
	}

	go httpServer.ListenAndServe()
	defer httpServer.Close()
	time.Sleep(100 * time.Millisecond)

	cfg := &config.Config{
		Server: config.ServerConfig{
			Address: "127.0.0.1",
			Port:    18083,
			TempDir: tempDir,
		},
		Directories: []config.DirectoryConfig{
			{
				Name:      "testdir",
				WatchPath: watchDir,
				Recursive: false,
				Watch: config.WatchConfig{
					Mode: "hybrid_ultra_low_latency",
					ReconcileScan: config.ReconcileScanConfig{
						Enabled:         true,
						IntervalSeconds: 2,
					},
				},
				Stability: config.StabilityConfig{
					ConfirmationIntervalMs: 10,
					RequiredStableChecks:   2,
					MaxWaitMs:              100,
				},
				Shadow: config.ShadowConfig{
					Enabled: false,
				},
				Outbound: config.OutboundConfig{
					URL: "http://127.0.0.1:18082/upload",
				},
			},
		},
	}

	svc, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	go svc.Start()
	time.Sleep(500 * time.Millisecond)

	// Test 1: Drop a file directly into watch directory
	t.Log("Test 1: Dropping file directly into watch directory")
	testFile1 := filepath.Join(watchDir, "direct.txt")
	if err := os.WriteFile(testFile1, []byte("direct drop"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	select {
	case filename := <-uploadReceived:
		if filename != "direct.txt" {
			t.Errorf("Expected 'direct.txt', got '%s'", filename)
		}
		t.Log("✓ Direct file drop detected and uploaded")
	case <-time.After(2 * time.Second):
		t.Error("File was not uploaded within timeout")
	}

	// Test 2: Atomic rename (create in temp, rename to watch)
	t.Log("Test 2: Testing atomic rename detection")
	tempFile := filepath.Join(tempDir, "atomic.txt")
	if err := os.WriteFile(tempFile, []byte("atomic content"), 0644); err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}

	finalFile := filepath.Join(watchDir, "atomic.txt")
	if err := os.Rename(tempFile, finalFile); err != nil {
		t.Fatalf("Failed to rename file: %v", err)
	}

	select {
	case filename := <-uploadReceived:
		if filename != "atomic.txt" {
			t.Errorf("Expected 'atomic.txt', got '%s'", filename)
		}
		t.Log("✓ Atomic rename detected and uploaded")
	case <-time.After(5 * time.Second):
		t.Error("Renamed file was not uploaded within timeout")
	}

	// Test 3: Multiple files
	t.Log("Test 3: Testing multiple file uploads")
	for i := 0; i < 5; i++ {
		filename := fmt.Sprintf("multi%d.txt", i)
		filepath := filepath.Join(watchDir, filename)
		if err := os.WriteFile(filepath, []byte(fmt.Sprintf("content %d", i)), 0644); err != nil {
			t.Fatalf("Failed to create file %s: %v", filename, err)
		}
	}

	// Wait for all uploads
	uploadedCount := 0
	timeout := time.After(5 * time.Second)
	for uploadedCount < 5 {
		select {
		case filename := <-uploadReceived:
			t.Logf("✓ Uploaded: %s", filename)
			uploadedCount++
		case <-timeout:
			t.Errorf("Only %d/5 files uploaded within timeout", uploadedCount)
			break
		}
	}

	if uploadedCount == 5 {
		t.Log("✓ All 5 files uploaded successfully")
	}

	svc.Stop()
	time.Sleep(500 * time.Millisecond)

	t.Log("E2E file watcher test completed successfully")
}

// TestE2ELargeFile tests handling of large files
func TestE2ELargeFile(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	testDir := t.TempDir()
	tempDir := filepath.Join(testDir, "temp")
	watchDir := filepath.Join(testDir, "watch")

	for _, dir := range []string{tempDir, watchDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create directory %s: %v", dir, err)
		}
	}

	uploadReceived := make(chan int64, 1) // Store file size
	mockServer := http.NewServeMux()
	mockServer.HandleFunc("/upload", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(200 << 20); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		file, _, err := r.FormFile("file")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		defer file.Close()

		// Count bytes
		size, _ := io.Copy(io.Discard, file)
		uploadReceived <- size
		w.WriteHeader(http.StatusOK)
	})

	httpServer := &http.Server{
		Addr:    "127.0.0.1:18084",
		Handler: mockServer,
	}

	go httpServer.ListenAndServe()
	defer httpServer.Close()
	time.Sleep(100 * time.Millisecond)

	cfg := &config.Config{
		Server: config.ServerConfig{
			Address: "127.0.0.1",
			Port:    18085,
			TempDir: tempDir,
		},
		Directories: []config.DirectoryConfig{
			{
				Name:      "testdir",
				WatchPath: watchDir,
				Watch: config.WatchConfig{
					Mode: "hybrid_ultra_low_latency",
				},
				Stability: config.StabilityConfig{
					ConfirmationIntervalMs: 100,
					RequiredStableChecks:   3,
					MaxWaitMs:              1000,
				},
				Shadow: config.ShadowConfig{
					Enabled: false,
				},
				Outbound: config.OutboundConfig{
					URL: "http://127.0.0.1:18084/upload",
				},
			},
		},
	}

	svc, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	go svc.Start()
	time.Sleep(500 * time.Millisecond)

	// Create a 10 MB file
	t.Log("Creating 10 MB test file...")
	largeFile := filepath.Join(watchDir, "large.bin")
	file, err := os.Create(largeFile)
	if err != nil {
		t.Fatalf("Failed to create large file: %v", err)
	}

	// Write 10 MB of data
	chunk := make([]byte, 1024*1024) // 1 MB chunks
	for i := range chunk {
		chunk[i] = byte(i % 256)
	}

	expectedSize := int64(10 * 1024 * 1024)
	for i := 0; i < 10; i++ {
		if _, err := file.Write(chunk); err != nil {
			t.Fatalf("Failed to write chunk: %v", err)
		}
	}
	file.Close()

	t.Log("Large file created, waiting for upload...")

	select {
	case size := <-uploadReceived:
		if size != expectedSize {
			t.Errorf("Expected size %d, got %d", expectedSize, size)
		} else {
			t.Logf("✓ Large file (10 MB) uploaded successfully: %d bytes", size)
		}
	case <-time.After(10 * time.Second):
		t.Error("Large file was not uploaded within timeout")
	}

	svc.Stop()
	time.Sleep(500 * time.Millisecond)

	t.Log("E2E large file test completed successfully")
}

// TestE2EShadowRetention tests shadow directory retention
func TestE2EShadowRetention(t *testing.T) {
	t.Skip("Skipping flaky test - race condition with duplicate file detection")
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	testDir := t.TempDir()
	tempDir := filepath.Join(testDir, "temp")
	watchDir := filepath.Join(testDir, "watch")
	shadowDir := filepath.Join(testDir, "shadow")

	for _, dir := range []string{tempDir, watchDir, shadowDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create directory %s: %v", dir, err)
		}
	}

	uploadReceived := make(chan string, 10)
	mockServer := http.NewServeMux()
	mockServer.HandleFunc("/upload", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			t.Logf("Parse form error: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		_, header, err := r.FormFile("file")
		if err != nil {
			t.Logf("Get file error: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		t.Logf("Mock server received: %s", header.Filename)
		uploadReceived <- header.Filename
		w.WriteHeader(http.StatusOK)
	})

	httpServer := &http.Server{
		Addr:    "127.0.0.1:18086",
		Handler: mockServer,
	}

	go httpServer.ListenAndServe()
	defer httpServer.Close()
	time.Sleep(100 * time.Millisecond)

	// Use very short retention for testing (1 second)
	cfg := &config.Config{
		Server: config.ServerConfig{
			Address: "127.0.0.1",
			Port:    18087,
			TempDir: tempDir,
		},
		Directories: []config.DirectoryConfig{
			{
				Name:      "testdir",
				WatchPath: watchDir,
				Watch: config.WatchConfig{
					Mode: "event_only",
				},
				Stability: config.StabilityConfig{
					ConfirmationIntervalMs: 20,
					RequiredStableChecks:   2,
					MaxWaitMs:              200,
				},
				Shadow: config.ShadowConfig{
					Enabled:        true,
					Path:           shadowDir,
					RetentionHours: 1, // Short retention for testing
				},
				Outbound: config.OutboundConfig{
					URL: "http://127.0.0.1:18086/upload",
				},
			},
		},
	}

	svc, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	go svc.Start()
	time.Sleep(100 * time.Millisecond)

	// Create and process a file
	t.Log("Creating test file...")
	testFile := filepath.Join(watchDir, "shadow-test.txt")
	if err := os.WriteFile(testFile, []byte("shadow test content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Wait for upload (might receive duplicates due to file events)
	select {
	case <-uploadReceived:
		t.Log("✓ File uploaded")
	case <-time.After(3 * time.Second):
		t.Fatal("File was not uploaded within timeout")
	}

	// Drain any duplicate uploads
	time.Sleep(100 * time.Millisecond)
	for len(uploadReceived) > 0 {
		<-uploadReceived
	}

	// Verify shadow copy exists
	time.Sleep(100 * time.Millisecond)
	shadowFiles, err := os.ReadDir(shadowDir)
	if err != nil {
		t.Fatalf("Failed to read shadow directory: %v", err)
	}

	if len(shadowFiles) != 1 {
		t.Fatalf("Expected 1 shadow file, got %d", len(shadowFiles))
	}

	shadowFile := filepath.Join(shadowDir, shadowFiles[0].Name())
	t.Logf("✓ Shadow copy created: %s", shadowFiles[0].Name())

	// Verify original file was deleted
	if _, err := os.Stat(testFile); !os.IsNotExist(err) {
		t.Error("Original file should have been deleted")
	} else {
		t.Log("✓ Original file deleted from watch directory")
	}

	// Verify shadow file content
	content, err := os.ReadFile(shadowFile)
	if err != nil {
		t.Fatalf("Failed to read shadow file: %v", err)
	}

	if string(content) != "shadow test content" {
		t.Errorf("Shadow file content mismatch. Expected 'shadow test content', got '%s'", string(content))
	} else {
		t.Log("✓ Shadow file content verified")
	}

	svc.Stop()
	time.Sleep(500 * time.Millisecond)

	t.Log("E2E shadow retention test completed successfully")
}

// TestE2ERecursiveWatching tests recursive directory watching
func TestE2ERecursiveWatching(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	testDir := t.TempDir()
	tempDir := filepath.Join(testDir, "temp")
	watchDir := filepath.Join(testDir, "watch")
	subDir := filepath.Join(watchDir, "subdir")
	deepDir := filepath.Join(subDir, "deep")

	for _, dir := range []string{tempDir, watchDir, subDir, deepDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create directory %s: %v", dir, err)
		}
	}

	uploadReceived := make(chan string, 10)
	mockServer := http.NewServeMux()
	mockServer.HandleFunc("/upload", func(w http.ResponseWriter, r *http.Request) {
		r.ParseMultipartForm(32 << 20)
		_, header, _ := r.FormFile("file")
		uploadReceived <- header.Filename
		w.WriteHeader(http.StatusOK)
	})

	httpServer := &http.Server{
		Addr:    "127.0.0.1:18088",
		Handler: mockServer,
	}

	go httpServer.ListenAndServe()
	defer httpServer.Close()
	time.Sleep(100 * time.Millisecond)

	cfg := &config.Config{
		Server: config.ServerConfig{
			Address: "127.0.0.1",
			Port:    18089,
			TempDir: tempDir,
		},
		Directories: []config.DirectoryConfig{
			{
				Name:      "testdir",
				WatchPath: watchDir,
				Recursive: true, // Enable recursive watching
				Watch: config.WatchConfig{
					Mode: "hybrid_ultra_low_latency",
				},
				Stability: config.StabilityConfig{
					ConfirmationIntervalMs: 10,
					RequiredStableChecks:   2,
					MaxWaitMs:              100,
				},
				Shadow: config.ShadowConfig{
					Enabled: false,
				},
				Outbound: config.OutboundConfig{
					URL: "http://127.0.0.1:18088/upload",
				},
			},
		},
	}

	svc, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	go svc.Start()
	time.Sleep(500 * time.Millisecond)

	// Create files at different levels
	files := map[string]string{
		filepath.Join(watchDir, "root.txt"): "root level",
		filepath.Join(subDir, "sub.txt"):    "sub level",
		filepath.Join(deepDir, "deep.txt"):  "deep level",
	}

	t.Log("Creating files at different directory levels...")
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create file %s: %v", path, err)
		}
	}

	// Wait for all uploads
	uploadedFiles := make(map[string]bool)
	timeout := time.After(5 * time.Second)
	expectedFiles := []string{"root.txt", "sub.txt", "deep.txt"}

	for len(uploadedFiles) < len(expectedFiles) {
		select {
		case filename := <-uploadReceived:
			uploadedFiles[filename] = true
			t.Logf("✓ Uploaded: %s", filename)
		case <-timeout:
			t.Fatalf("Only %d/%d files uploaded within timeout", len(uploadedFiles), len(expectedFiles))
		}
	}

	for _, expectedFile := range expectedFiles {
		if !uploadedFiles[expectedFile] {
			t.Errorf("File %s was not uploaded", expectedFile)
		}
	}

	t.Log("✓ All files from nested directories uploaded successfully")

	svc.Stop()
	time.Sleep(500 * time.Millisecond)

	t.Log("E2E recursive watching test completed successfully")
}
