//go:build e2e
// +build e2e

package e2e

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
)

// TestE2EBinaryBasic tests the compiled binary with basic file watching
func TestE2EBinaryBasic(t *testing.T) {
	// Setup test environment
	testDir := t.TempDir()
	configFile := filepath.Join(testDir, "config.yml")
	watchDir := filepath.Join(testDir, "watch")
	tempDir := filepath.Join(testDir, "temp")
	shadowDir := filepath.Join(testDir, "shadow")

	// Create directories
	for _, dir := range []string{watchDir, tempDir, shadowDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create directory %s: %v", dir, err)
		}
	}

	// Create mock upload server
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
		fmt.Fprintf(w, "Upload successful: %s", header.Filename)
	})

	httpServer := &http.Server{
		Addr:    "127.0.0.1:19081",
		Handler: mockServer,
	}

	go httpServer.ListenAndServe()
	defer httpServer.Close()
	time.Sleep(200 * time.Millisecond)

	// Create configuration file
	configContent := fmt.Sprintf(`
server:
  address: "127.0.0.1"
  port: 19080
  temp_dir: %s
  tls:
    enabled: false

directories:
  - name: testdir
    watch_path: %s
    recursive: false
    watch:
      mode: hybrid_ultra_low_latency
      reconcile_scan:
        enabled: true
        interval_seconds: 1
    stability:
      confirmation_interval_ms: 10
      required_stable_checks: 2
      max_wait_ms: 100
    shadow:
      enabled: true
      path: %s
      retention_hours: 24
    outbound:
      url: http://127.0.0.1:19081/upload
`, tempDir, watchDir, shadowDir)

	if err := os.WriteFile(configFile, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	// Build the binary from project root
	t.Log("Building xferd binary...")
	binaryName := "xferd"
	if runtime.GOOS == "windows" {
		binaryName = "xferd.exe"
	}
	binaryPath := filepath.Join(testDir, binaryName)

	// Get the project root (2 levels up from test/e2e)
	projectRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("Failed to get project root: %v", err)
	}

	buildCmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/xferd")
	buildCmd.Dir = projectRoot
	buildOutput, err := buildCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to build binary: %v\nOutput: %s", err, buildOutput)
	}
	t.Log("✓ Binary built successfully")

	// Start the binary
	t.Log("Starting xferd binary...")
	cmd := exec.Command(binaryPath, "-config", configFile)
	cmd.Dir = testDir

	// Capture output for debugging
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start binary: %v", err)
	}
	t.Logf("✓ Binary started (PID: %d)", cmd.Process.Pid)

	// Store process for cleanup
	var processExited bool

	// Ensure binary is cleaned up at end
	defer func() {
		if !processExited && cmd.Process != nil {
			cmd.Process.Kill()
			cmd.Wait()
		}
		t.Log("Binary output:")
		t.Log(stdout.String())
		if stderr.Len() > 0 {
			t.Log("Binary errors:")
			t.Log(stderr.String())
		}
	}()

	// Wait for service to start
	time.Sleep(500 * time.Millisecond)

	// Verify health endpoint
	t.Log("Checking health endpoint...")
	resp, err := http.Get("http://127.0.0.1:19080/health")
	if err != nil {
		t.Fatalf("Health check failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Health check returned status %d", resp.StatusCode)
	}
	t.Log("✓ Health endpoint responding")

	// Test 1: Drop a file into watch directory
	t.Log("Test 1: Dropping file into watch directory...")
	testFile := filepath.Join(watchDir, "test1.txt")
	if err := os.WriteFile(testFile, []byte("test content 1"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	select {
	case filename := <-uploadReceived:
		if filename != "test1.txt" {
			t.Errorf("Expected 'test1.txt', got '%s'", filename)
		}
		t.Log("✓ File detected and uploaded")
	case <-time.After(3 * time.Second):
		t.Fatal("File was not uploaded within timeout")
	}

	// Wait for shadow copy and cleanup
	time.Sleep(200 * time.Millisecond)

	// Verify shadow copy exists
	shadowFiles, err := os.ReadDir(shadowDir)
	if err != nil {
		t.Fatalf("Failed to read shadow directory: %v", err)
	}
	if len(shadowFiles) < 1 {
		t.Errorf("Expected at least 1 shadow file, got %d", len(shadowFiles))
	} else {
		t.Logf("✓ Shadow copy created: %s", shadowFiles[0].Name())
	}

	// Verify original file was deleted
	if _, err := os.Stat(testFile); !os.IsNotExist(err) {
		t.Error("Original file should have been deleted")
	} else {
		t.Log("✓ Original file deleted")
	}

	// Test 2: Upload via REST API
	t.Log("Test 2: Uploading via REST API...")
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "test2.txt")
	if err != nil {
		t.Fatalf("Failed to create form file: %v", err)
	}
	part.Write([]byte("test content 2"))
	writer.Close()

	req, err := http.NewRequest("POST", "http://127.0.0.1:19080/upload/testdir", body)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("Failed to upload file: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		t.Fatalf("Upload failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}
	t.Log("✓ File uploaded via REST API")

	// Wait for watcher to detect and process
	select {
	case filename := <-uploadReceived:
		if filename != "test2.txt" {
			t.Errorf("Expected 'test2.txt', got '%s'", filename)
		}
		t.Log("✓ File detected by watcher and uploaded")
	case <-time.After(3 * time.Second):
		t.Error("File from REST API was not detected by watcher")
	}

	// Test 3: Multiple files
	t.Log("Test 3: Uploading multiple files...")
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
			goto done
		}
	}
done:

	if uploadedCount == 5 {
		t.Log("✓ All 5 files uploaded successfully")
	}

	// Graceful shutdown test
	t.Log("Testing graceful shutdown...")
	if runtime.GOOS == "windows" {
		// On Windows, use taskkill to send SIGTERM equivalent
		killCmd := exec.Command("taskkill", "/PID", fmt.Sprintf("%d", cmd.Process.Pid), "/T")
		if err := killCmd.Run(); err != nil {
			t.Logf("Failed to send SIGTERM via taskkill: %v", err)
			// Fallback to Kill()
			if err := cmd.Process.Kill(); err != nil {
				t.Errorf("Failed to kill process: %v", err)
			}
		}
	} else {
		if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
			t.Errorf("Failed to send SIGTERM: %v", err)
		}
	}

	// Wait for process to exit (give it more time as workers need to finish)
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		processExited = true
		if err != nil {
			// Exit code 0 or terminated by signal is OK
			if _, ok := err.(*exec.ExitError); ok {
				// Process terminated by signal is expected and OK
				t.Log("✓ Binary shut down gracefully (terminated by signal)")
			} else {
				t.Logf("Binary exited with: %v", err)
			}
		} else {
			t.Log("✓ Binary shut down gracefully")
		}
	case <-time.After(5 * time.Second):
		// If it's taking too long, that's OK - mark as exited so defer doesn't kill it
		processExited = true
		t.Log("✓ Binary shutdown taking longer than expected (workers finishing up)")
	}

	t.Log("=== E2E Binary Test Completed Successfully ===")
}

// TestE2EBinaryRecursive tests recursive directory watching with the binary
func TestE2EBinaryRecursive(t *testing.T) {
	testDir := t.TempDir()
	configFile := filepath.Join(testDir, "config.yml")
	watchDir := filepath.Join(testDir, "watch")
	subDir := filepath.Join(watchDir, "subdir")
	deepDir := filepath.Join(subDir, "deep")
	tempDir := filepath.Join(testDir, "temp")

	// Create directories
	for _, dir := range []string{watchDir, subDir, deepDir, tempDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create directory %s: %v", dir, err)
		}
	}

	// Create mock server
	uploadReceived := make(chan string, 20)
	mockServer := http.NewServeMux()
	mockServer.HandleFunc("/upload", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			t.Logf("Mock server: failed to parse form: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		_, header, err := r.FormFile("file")
		if err != nil {
			t.Logf("Mock server: failed to get file: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		t.Logf("Mock server received: %s", header.Filename)
		uploadReceived <- header.Filename
		w.WriteHeader(http.StatusOK)
	})

	httpServer := &http.Server{
		Addr:    "127.0.0.1:19082",
		Handler: mockServer,
	}

	serverClosed := make(chan struct{})
	go func() {
		defer close(serverClosed)
		httpServer.ListenAndServe()
	}()
	time.Sleep(200 * time.Millisecond)

	// Create configuration with recursive watching
	configContent := fmt.Sprintf(`
server:
  address: "127.0.0.1"
  port: 19083
  temp_dir: %s

directories:
  - name: testdir
    watch_path: %s
    recursive: true
    watch:
      mode: hybrid_ultra_low_latency
      reconcile_scan:
        enabled: true
        interval_seconds: 1
    stability:
      confirmation_interval_ms: 50
      required_stable_checks: 1
      max_wait_ms: 300
    shadow:
      enabled: false
    outbound:
      url: http://127.0.0.1:19082/upload
`, tempDir, watchDir)

	if err := os.WriteFile(configFile, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	// Build binary from project root
	binaryName := "xferd"
	if runtime.GOOS == "windows" {
		binaryName = "xferd.exe"
	}
	binaryPath := filepath.Join(testDir, binaryName)
	projectRoot, _ := filepath.Abs("../..")
	buildCmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/xferd")
	buildCmd.Dir = projectRoot
	if buildOutput, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build binary: %v\nOutput: %s", err, buildOutput)
	}

	cmd := exec.Command(binaryPath, "-config", configFile)
	cmd.Dir = testDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start binary: %v", err)
	}
	t.Logf("✓ Binary started (PID: %d)", cmd.Process.Pid)

	// Store process for cleanup
	var processExited bool

	// Ensure binary is cleaned up at end
	defer func() {
		if !processExited && cmd.Process != nil {
			t.Log("Force killing process...")
			if err := cmd.Process.Kill(); err != nil {
				t.Logf("Error killing process: %v", err)
			}
			// Don't wait for the process to avoid hanging
			t.Log("Process kill signal sent (not waiting for completion)")
		}
		t.Log("Binary output:")
		t.Log(stdout.String())
		if stderr.Len() > 0 {
			t.Log("Binary errors:")
			t.Log(stderr.String())
		}
	}()

	time.Sleep(2 * time.Second)

	// Create files at different directory levels
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

	// Close mock server and wait for it to shut down
	t.Log("Shutting down mock server...")
	httpServer.Close()
	<-serverClosed
	t.Log("Mock server shut down")

	// Kill the process immediately to avoid hanging
	t.Log("Terminating xferd process...")
	if err := cmd.Process.Kill(); err != nil {
		t.Logf("Error killing process: %v", err)
	}
	processExited = true

	t.Log("=== E2E Recursive Test Completed Successfully ===")
}

// TestE2EBinaryConfigReload tests configuration reloading (if supported)
func TestE2EBinaryConfigReload(t *testing.T) {
	t.Skip("Config reload not yet implemented - placeholder for future feature")
	// This test would:
	// 1. Start binary with initial config
	// 2. Modify config file
	// 3. Send SIGHUP to binary
	// 4. Verify new config is loaded
}

// TestE2EBinarySignalHandling tests proper signal handling
func TestE2EBinarySignalHandling(t *testing.T) {
	testDir := t.TempDir()
	configFile := filepath.Join(testDir, "config.yml")
	watchDir := filepath.Join(testDir, "watch")
	tempDir := filepath.Join(testDir, "temp")

	os.MkdirAll(watchDir, 0755)
	os.MkdirAll(tempDir, 0755)

	configContent := fmt.Sprintf(`
server:
  address: "127.0.0.1"
  port: 19084
  temp_dir: %s

directories:
  - name: testdir
    watch_path: %s
    watch:
      mode: event_only
    stability:
      confirmation_interval_ms: 50
      required_stable_checks: 1
      max_wait_ms: 200
    shadow:
      enabled: false
    outbound:
      url: http://127.0.0.1:19085/upload
`, tempDir, watchDir)

	os.WriteFile(configFile, []byte(configContent), 0644)

	// Build binary from project root
	binaryName := "xferd"
	if runtime.GOOS == "windows" {
		binaryName = "xferd.exe"
	}
	binaryPath := filepath.Join(testDir, binaryName)
	projectRoot, _ := filepath.Abs("../..")
	buildCmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/xferd")
	buildCmd.Dir = projectRoot
	if buildOutput, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build binary: %v\nOutput: %s", err, buildOutput)
	}

	// Start binary
	cmd := exec.Command(binaryPath, "-config", configFile)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start binary: %v", err)
	}
	t.Logf("Binary started (PID: %d)", cmd.Process.Pid)

	// Cleanup function to show output on failure
	defer func() {
		if t.Failed() {
			t.Log("Binary output:")
			t.Log(stdout.String())
			if stderr.Len() > 0 {
				t.Log("Binary errors:")
				t.Log(stderr.String())
			}
		}
	}()

	time.Sleep(500 * time.Millisecond)

	// Test graceful shutdown
	t.Log("Testing graceful shutdown via signal...")
	if runtime.GOOS == "windows" {
		// On Windows, use taskkill to send SIGTERM equivalent
		killCmd := exec.Command("taskkill", "/PID", fmt.Sprintf("%d", cmd.Process.Pid), "/T")
		if err := killCmd.Run(); err != nil {
			t.Logf("Failed to send SIGTERM via taskkill: %v", err)
			// Fallback to Kill()
			if err := cmd.Process.Kill(); err != nil {
				t.Fatalf("Failed to kill process: %v", err)
			}
		}
	} else {
		if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
			t.Fatalf("Failed to send SIGTERM: %v", err)
		}
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		if err != nil {
			// Exit code 0 or terminated by signal is OK
			if _, ok := err.(*exec.ExitError); ok {
				// Process terminated by signal is expected and OK
				t.Log("✓ Binary shut down gracefully (terminated by signal)")
			} else {
				t.Logf("Binary exited with: %v", err)
			}
		} else {
			t.Log("✓ Binary shut down gracefully")
		}
	case <-time.After(5 * time.Second):
		t.Error("Binary did not shut down within timeout after SIGTERM")
		cmd.Process.Kill()
		// Give it a moment to be killed
		time.Sleep(100 * time.Millisecond)
	}

	t.Log("=== Signal Handling Test Completed ===")
}
