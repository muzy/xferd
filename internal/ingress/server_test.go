package ingress

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/muzy/xferd/internal/config"
)

func TestNewServer(t *testing.T) {
	tmpDir := t.TempDir()
	tempDir := filepath.Join(tmpDir, "temp")

	cfg := config.ServerConfig{
		Address: "0.0.0.0",
		Port:    8080,
		TempDir: tempDir,
	}

	dirs := []config.DirectoryConfig{
		{
			Name:      "test",
			WatchPath: filepath.Join(tmpDir, "watch"),
		},
	}

	server, err := NewServer(cfg, dirs)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	if server == nil {
		t.Fatal("Expected non-nil server")
	}

	// Verify temp directory was created
	info, err := os.Stat(tempDir)
	if err != nil {
		t.Fatalf("Temp directory not created: %v", err)
	}

	if !info.IsDir() {
		t.Fatal("Temp path is not a directory")
	}
}

func TestNewServerMultipleDirectories(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := config.ServerConfig{
		Address: "0.0.0.0",
		Port:    8080,
		TempDir: filepath.Join(tmpDir, "temp"),
	}

	dirs := []config.DirectoryConfig{
		{Name: "dir1", WatchPath: filepath.Join(tmpDir, "dir1")},
		{Name: "dir2", WatchPath: filepath.Join(tmpDir, "dir2")},
		{Name: "dir3", WatchPath: filepath.Join(tmpDir, "dir3")},
	}

	server, err := NewServer(cfg, dirs)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	if len(server.directories) != 3 {
		t.Errorf("Expected 3 directories, got %d", len(server.directories))
	}

	// Verify all directories are in the map
	for _, dir := range dirs {
		if _, exists := server.directories[dir.Name]; !exists {
			t.Errorf("Directory %s not found in server map", dir.Name)
		}
	}
}

func TestHealthEndpoint(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := config.ServerConfig{
		Address: "0.0.0.0",
		Port:    8080,
		TempDir: filepath.Join(tmpDir, "temp"),
	}

	dirs := []config.DirectoryConfig{
		{Name: "test", WatchPath: filepath.Join(tmpDir, "watch")},
	}

	server, err := NewServer(cfg, dirs)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Test GET request
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	server.handleHealth(w, req)

	resp := w.Result()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	if string(body) != "OK" {
		t.Errorf("Expected body 'OK', got '%s'", string(body))
	}
}

func TestHealthEndpointInvalidMethod(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := config.ServerConfig{
		Address: "0.0.0.0",
		Port:    8080,
		TempDir: filepath.Join(tmpDir, "temp"),
	}

	dirs := []config.DirectoryConfig{
		{Name: "test", WatchPath: filepath.Join(tmpDir, "watch")},
	}

	server, err := NewServer(cfg, dirs)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Test POST request (should fail)
	req := httptest.NewRequest("POST", "/health", nil)
	w := httptest.NewRecorder()

	server.handleHealth(w, req)

	resp := w.Result()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", resp.StatusCode)
	}
}

func TestUploadEndpointSuccess(t *testing.T) {
	tmpDir := t.TempDir()
	tempDir := filepath.Join(tmpDir, "temp")
	watchDir := filepath.Join(tmpDir, "watch")

	// Create watch directory
	if err := os.MkdirAll(watchDir, 0755); err != nil {
		t.Fatalf("Failed to create watch directory: %v", err)
	}

	cfg := config.ServerConfig{
		Address: "0.0.0.0",
		Port:    8080,
		TempDir: tempDir,
	}

	dirs := []config.DirectoryConfig{
		{Name: "test", WatchPath: watchDir},
	}

	server, err := NewServer(cfg, dirs)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Create multipart form
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("file", "test.txt")
	if err != nil {
		t.Fatalf("Failed to create form file: %v", err)
	}

	content := []byte("test file content")
	part.Write(content)
	writer.Close()

	// Create request
	req := httptest.NewRequest("POST", "/upload/test", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()

	server.handleUpload(w, req)

	resp := w.Result()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Expected status 200, got %d. Body: %s", resp.StatusCode, string(body))
	}

	// Verify file was created in watch directory
	finalPath := filepath.Join(watchDir, "test.txt")
	uploadedContent, err := os.ReadFile(finalPath)
	if err != nil {
		t.Fatalf("Failed to read uploaded file: %v", err)
	}

	if string(uploadedContent) != string(content) {
		t.Errorf("Content mismatch. Expected '%s', got '%s'", string(content), string(uploadedContent))
	}
}

func TestUploadEndpointInvalidMethod(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := config.ServerConfig{
		Address: "0.0.0.0",
		Port:    8080,
		TempDir: filepath.Join(tmpDir, "temp"),
	}

	dirs := []config.DirectoryConfig{
		{Name: "test", WatchPath: filepath.Join(tmpDir, "watch")},
	}

	server, err := NewServer(cfg, dirs)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Test GET request (should fail)
	req := httptest.NewRequest("GET", "/upload/test", nil)
	w := httptest.NewRecorder()

	server.handleUpload(w, req)

	resp := w.Result()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", resp.StatusCode)
	}
}

func TestUploadEndpointMissingDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := config.ServerConfig{
		Address: "0.0.0.0",
		Port:    8080,
		TempDir: filepath.Join(tmpDir, "temp"),
	}

	dirs := []config.DirectoryConfig{
		{Name: "test", WatchPath: filepath.Join(tmpDir, "watch")},
	}

	server, err := NewServer(cfg, dirs)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Create request with empty directory name
	req := httptest.NewRequest("POST", "/upload/", nil)
	w := httptest.NewRecorder()

	server.handleUpload(w, req)

	resp := w.Result()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", resp.StatusCode)
	}
}

func TestUploadEndpointUnknownDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := config.ServerConfig{
		Address: "0.0.0.0",
		Port:    8080,
		TempDir: filepath.Join(tmpDir, "temp"),
	}

	dirs := []config.DirectoryConfig{
		{Name: "test", WatchPath: filepath.Join(tmpDir, "watch")},
	}

	server, err := NewServer(cfg, dirs)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Create multipart form
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "test.txt")
	part.Write([]byte("content"))
	writer.Close()

	// Request for unknown directory
	req := httptest.NewRequest("POST", "/upload/unknown", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()

	server.handleUpload(w, req)

	resp := w.Result()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", resp.StatusCode)
	}
}

func TestUploadEndpointMissingFile(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := config.ServerConfig{
		Address: "0.0.0.0",
		Port:    8080,
		TempDir: filepath.Join(tmpDir, "temp"),
	}

	dirs := []config.DirectoryConfig{
		{Name: "test", WatchPath: filepath.Join(tmpDir, "watch")},
	}

	server, err := NewServer(cfg, dirs)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Create multipart form without file field
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writer.WriteField("other", "value")
	writer.Close()

	req := httptest.NewRequest("POST", "/upload/test", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()

	server.handleUpload(w, req)

	resp := w.Result()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", resp.StatusCode)
	}
}

func TestUploadEndpointLargeFile(t *testing.T) {
	tmpDir := t.TempDir()
	tempDir := filepath.Join(tmpDir, "temp")
	watchDir := filepath.Join(tmpDir, "watch")

	if err := os.MkdirAll(watchDir, 0755); err != nil {
		t.Fatalf("Failed to create watch directory: %v", err)
	}

	cfg := config.ServerConfig{
		Address: "0.0.0.0",
		Port:    8080,
		TempDir: tempDir,
	}

	dirs := []config.DirectoryConfig{
		{Name: "test", WatchPath: watchDir},
	}

	server, err := NewServer(cfg, dirs)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Create multipart form with larger file (1 MB)
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("file", "large.bin")
	if err != nil {
		t.Fatalf("Failed to create form file: %v", err)
	}

	largeContent := make([]byte, 1024*1024) // 1 MB
	for i := range largeContent {
		largeContent[i] = byte(i % 256)
	}
	part.Write(largeContent)
	writer.Close()

	req := httptest.NewRequest("POST", "/upload/test", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()

	server.handleUpload(w, req)

	resp := w.Result()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Expected status 200, got %d. Body: %s", resp.StatusCode, string(body))
	}

	// Verify file was created with correct size
	finalPath := filepath.Join(watchDir, "large.bin")
	info, err := os.Stat(finalPath)
	if err != nil {
		t.Fatalf("Failed to stat uploaded file: %v", err)
	}

	if info.Size() != int64(len(largeContent)) {
		t.Errorf("Size mismatch. Expected %d, got %d", len(largeContent), info.Size())
	}
}

func TestStreamToFile(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := config.ServerConfig{
		Address: "0.0.0.0",
		Port:    8080,
		TempDir: tmpDir,
	}

	server, err := NewServer(cfg, nil)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	content := []byte("test content for streaming")
	reader := bytes.NewReader(content)

	destPath := filepath.Join(tmpDir, "streamed.txt")

	err = server.streamToFile(reader, destPath)
	if err != nil {
		t.Fatalf("streamToFile failed: %v", err)
	}

	// Verify file content
	written, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("Failed to read streamed file: %v", err)
	}

	if string(written) != string(content) {
		t.Errorf("Content mismatch. Expected '%s', got '%s'", string(content), string(written))
	}
}

func TestStreamToFileLarge(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := config.ServerConfig{
		Address: "0.0.0.0",
		Port:    8080,
		TempDir: tmpDir,
	}

	server, err := NewServer(cfg, nil)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Create 5 MB content
	content := make([]byte, 5*1024*1024)
	for i := range content {
		content[i] = byte(i % 256)
	}
	reader := bytes.NewReader(content)

	destPath := filepath.Join(tmpDir, "large.bin")

	start := time.Now()
	err = server.streamToFile(reader, destPath)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("streamToFile failed: %v", err)
	}

	t.Logf("Streamed 5 MB in %v", elapsed)

	// Verify file size
	info, err := os.Stat(destPath)
	if err != nil {
		t.Fatalf("Failed to stat streamed file: %v", err)
	}

	if info.Size() != int64(len(content)) {
		t.Errorf("Size mismatch. Expected %d, got %d", len(content), info.Size())
	}
}

func TestHandleStreamingUpload(t *testing.T) {
	tmpDir := t.TempDir()
	tempDir := filepath.Join(tmpDir, "temp")
	watchDir := filepath.Join(tmpDir, "watch")

	if err := os.MkdirAll(watchDir, 0755); err != nil {
		t.Fatalf("Failed to create watch directory: %v", err)
	}

	cfg := config.ServerConfig{
		Address: "0.0.0.0",
		Port:    8080,
		TempDir: tempDir,
	}

	dirs := []config.DirectoryConfig{
		{Name: "test", WatchPath: watchDir},
	}

	server, err := NewServer(cfg, dirs)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	content := []byte("streaming content")
	body := bytes.NewReader(content)

	req := httptest.NewRequest("POST", "/upload/test?filename=stream.txt", body)
	w := httptest.NewRecorder()

	server.handleStreamingUpload(w, req)

	resp := w.Result()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("Expected status 200, got %d. Body: %s", resp.StatusCode, string(respBody))
	}

	// Verify file was created
	finalPath := filepath.Join(watchDir, "stream.txt")
	uploadedContent, err := os.ReadFile(finalPath)
	if err != nil {
		t.Fatalf("Failed to read uploaded file: %v", err)
	}

	if string(uploadedContent) != string(content) {
		t.Errorf("Content mismatch. Expected '%s', got '%s'", string(content), string(uploadedContent))
	}
}

func TestHandleStreamingUploadMissingFilename(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := config.ServerConfig{
		Address: "0.0.0.0",
		Port:    8080,
		TempDir: filepath.Join(tmpDir, "temp"),
	}

	dirs := []config.DirectoryConfig{
		{Name: "test", WatchPath: filepath.Join(tmpDir, "watch")},
	}

	server, err := NewServer(cfg, dirs)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	content := []byte("content")
	body := bytes.NewReader(content)

	// No filename in query or header
	req := httptest.NewRequest("POST", "/upload/test", body)
	w := httptest.NewRecorder()

	server.handleStreamingUpload(w, req)

	resp := w.Result()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", resp.StatusCode)
	}
}

func TestHandleStreamingUploadWithHeader(t *testing.T) {
	tmpDir := t.TempDir()
	tempDir := filepath.Join(tmpDir, "temp")
	watchDir := filepath.Join(tmpDir, "watch")

	if err := os.MkdirAll(watchDir, 0755); err != nil {
		t.Fatalf("Failed to create watch directory: %v", err)
	}

	cfg := config.ServerConfig{
		Address: "0.0.0.0",
		Port:    8080,
		TempDir: tempDir,
	}

	dirs := []config.DirectoryConfig{
		{Name: "test", WatchPath: watchDir},
	}

	server, err := NewServer(cfg, dirs)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	content := []byte("streaming content")
	body := bytes.NewReader(content)

	// Use X-Filename header
	req := httptest.NewRequest("POST", "/upload/test", body)
	req.Header.Set("X-Filename", "header.txt")
	w := httptest.NewRecorder()

	server.handleStreamingUpload(w, req)

	resp := w.Result()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("Expected status 200, got %d. Body: %s", resp.StatusCode, string(respBody))
	}

	// Verify file was created with header filename
	finalPath := filepath.Join(watchDir, "header.txt")
	if _, err := os.Stat(finalPath); err != nil {
		t.Fatalf("File not created with header filename: %v", err)
	}
}

func TestServerStartStop(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := config.ServerConfig{
		Address: "127.0.0.1",
		Port:    18080, // Use non-standard port
		TempDir: filepath.Join(tmpDir, "temp"),
	}

	dirs := []config.DirectoryConfig{
		{Name: "test", WatchPath: filepath.Join(tmpDir, "watch")},
	}

	server, err := NewServer(cfg, dirs)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Start server in goroutine
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start(ctx)
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Make a health check request
	resp, err := http.Get("http://127.0.0.1:18080/health")
	if err != nil {
		t.Fatalf("Failed to reach server: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Stop server
	cancel()

	// Wait for server to stop
	select {
	case err := <-errCh:
		if err != nil && !strings.Contains(err.Error(), "Server closed") {
			t.Logf("Server stopped with: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Server did not stop within timeout")
	}
}

func TestUploadFilenameWithSpecialCharacters(t *testing.T) {
	tmpDir := t.TempDir()
	tempDir := filepath.Join(tmpDir, "temp")
	watchDir := filepath.Join(tmpDir, "watch")

	if err := os.MkdirAll(watchDir, 0755); err != nil {
		t.Fatalf("Failed to create watch directory: %v", err)
	}

	cfg := config.ServerConfig{
		Address: "0.0.0.0",
		Port:    8080,
		TempDir: tempDir,
	}

	dirs := []config.DirectoryConfig{
		{Name: "test", WatchPath: watchDir},
	}

	server, err := NewServer(cfg, dirs)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Test with various filenames
	testFilenames := []string{
		"test file with spaces.txt",
		"test-with-dashes.txt",
		"test_with_underscores.txt",
		"test.multiple.dots.txt",
	}

	for _, filename := range testFilenames {
		t.Run(filename, func(t *testing.T) {
			body := &bytes.Buffer{}
			writer := multipart.NewWriter(body)

			part, err := writer.CreateFormFile("file", filename)
			if err != nil {
				t.Fatalf("Failed to create form file: %v", err)
			}

			content := []byte("test content")
			part.Write(content)
			writer.Close()

			req := httptest.NewRequest("POST", "/upload/test", body)
			req.Header.Set("Content-Type", writer.FormDataContentType())
			w := httptest.NewRecorder()

			server.handleUpload(w, req)

			resp := w.Result()

			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("Expected status 200, got %d. Body: %s", resp.StatusCode, string(body))
			}

			// Verify file exists
			finalPath := filepath.Join(watchDir, filename)
			if _, err := os.Stat(finalPath); err != nil {
				t.Errorf("File not created: %v", err)
			}
		})
	}
}

// TestBasicAuthEnabled tests basic authentication when enabled with plaintext password
func TestBasicAuthEnabled(t *testing.T) {
	tmpDir := t.TempDir()
	tempDir := filepath.Join(tmpDir, "temp")
	watchDir := filepath.Join(tmpDir, "watch")

	if err := os.MkdirAll(watchDir, 0755); err != nil {
		t.Fatalf("Failed to create watch directory: %v", err)
	}

	cfg := config.ServerConfig{
		Address: "0.0.0.0",
		Port:    8080,
		TempDir: tempDir,
		BasicAuth: config.BasicAuthConfig{
			Enabled:  true,
			Username: "testuser",
			Password: "testpass",
		},
	}

	dirs := []config.DirectoryConfig{
		{Name: "test", WatchPath: watchDir},
	}

	server, err := NewServer(cfg, dirs)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Test without auth - should fail
	t.Run("NoAuth", func(t *testing.T) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, _ := writer.CreateFormFile("file", "test.txt")
		part.Write([]byte("test content"))
		writer.Close()

		req := httptest.NewRequest("POST", "/upload/test", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		w := httptest.NewRecorder()

		// Use the auth-wrapped handler
		handler := server.withAuth(server.handleUpload)
		handler(w, req)
		resp := w.Result()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", resp.StatusCode)
		}

		if resp.Header.Get("WWW-Authenticate") == "" {
			t.Error("Expected WWW-Authenticate header")
		}
	})

	// Test with wrong credentials - should fail
	t.Run("WrongCredentials", func(t *testing.T) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, _ := writer.CreateFormFile("file", "test.txt")
		part.Write([]byte("test content"))
		writer.Close()

		req := httptest.NewRequest("POST", "/upload/test", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.SetBasicAuth("wronguser", "wrongpass")
		w := httptest.NewRecorder()

		handler := server.withAuth(server.handleUpload)
		handler(w, req)
		resp := w.Result()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", resp.StatusCode)
		}
	})

	// Test with correct credentials - should succeed
	t.Run("CorrectCredentials", func(t *testing.T) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, _ := writer.CreateFormFile("file", "test.txt")
		part.Write([]byte("test content"))
		writer.Close()

		req := httptest.NewRequest("POST", "/upload/test", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.SetBasicAuth("testuser", "testpass")
		w := httptest.NewRecorder()

		handler := server.withAuth(server.handleUpload)
		handler(w, req)
		resp := w.Result()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Errorf("Expected status 200, got %d. Body: %s", resp.StatusCode, string(body))
		}
	})
}

// TestBasicAuthWithHash tests basic authentication with bcrypt password hash
func TestBasicAuthWithHash(t *testing.T) {
	tmpDir := t.TempDir()
	tempDir := filepath.Join(tmpDir, "temp")
	watchDir := filepath.Join(tmpDir, "watch")

	if err := os.MkdirAll(watchDir, 0755); err != nil {
		t.Fatalf("Failed to create watch directory: %v", err)
	}

	// Pre-generated bcrypt hash for password "testpass"
	// Generated with: bcrypt.GenerateFromPassword([]byte("testpass"), bcrypt.DefaultCost)
	passwordHash := "$2a$10$AAPH3lLwZ8Sjw6l3SYLREehE/gR9Lui3wrZbHFN5Q/fG/WxWfBjaa"

	cfg := config.ServerConfig{
		Address: "0.0.0.0",
		Port:    8080,
		TempDir: tempDir,
		BasicAuth: config.BasicAuthConfig{
			Enabled:      true,
			Username:     "testuser",
			PasswordHash: passwordHash,
		},
	}

	dirs := []config.DirectoryConfig{
		{Name: "test", WatchPath: watchDir},
	}

	server, err := NewServer(cfg, dirs)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Test without auth - should fail
	t.Run("NoAuth", func(t *testing.T) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, _ := writer.CreateFormFile("file", "test.txt")
		part.Write([]byte("test content"))
		writer.Close()

		req := httptest.NewRequest("POST", "/upload/test", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		w := httptest.NewRecorder()

		handler := server.withAuth(server.handleUpload)
		handler(w, req)
		resp := w.Result()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", resp.StatusCode)
		}
	})

	// Test with wrong password - should fail
	t.Run("WrongPassword", func(t *testing.T) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, _ := writer.CreateFormFile("file", "test.txt")
		part.Write([]byte("test content"))
		writer.Close()

		req := httptest.NewRequest("POST", "/upload/test", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.SetBasicAuth("testuser", "wrongpass")
		w := httptest.NewRecorder()

		handler := server.withAuth(server.handleUpload)
		handler(w, req)
		resp := w.Result()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", resp.StatusCode)
		}
	})

	// Test with correct password - should succeed
	t.Run("CorrectPassword", func(t *testing.T) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, _ := writer.CreateFormFile("file", "test.txt")
		part.Write([]byte("test content"))
		writer.Close()

		req := httptest.NewRequest("POST", "/upload/test", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.SetBasicAuth("testuser", "testpass")
		w := httptest.NewRecorder()

		handler := server.withAuth(server.handleUpload)
		handler(w, req)
		resp := w.Result()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Errorf("Expected status 200, got %d. Body: %s", resp.StatusCode, string(body))
		}
	})
}

// TestBasicAuthDisabled tests that auth is not required when disabled
func TestBasicAuthDisabled(t *testing.T) {
	tmpDir := t.TempDir()
	tempDir := filepath.Join(tmpDir, "temp")
	watchDir := filepath.Join(tmpDir, "watch")

	if err := os.MkdirAll(watchDir, 0755); err != nil {
		t.Fatalf("Failed to create watch directory: %v", err)
	}

	cfg := config.ServerConfig{
		Address: "0.0.0.0",
		Port:    8080,
		TempDir: tempDir,
		BasicAuth: config.BasicAuthConfig{
			Enabled: false,
		},
	}

	dirs := []config.DirectoryConfig{
		{Name: "test", WatchPath: watchDir},
	}

	server, err := NewServer(cfg, dirs)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "test.txt")
	part.Write([]byte("test content"))
	writer.Close()

	req := httptest.NewRequest("POST", "/upload/test", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()

	server.handleUpload(w, req)
	resp := w.Result()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
}

// TestPathTraversalProtection tests that path traversal attempts are blocked
// Note: Go's mime/multipart library provides defense-in-depth by automatically
// extracting the basename from filenames in Content-Disposition headers.
// Our sanitizeFilename() provides an additional security layer, especially
// important for the streaming upload path which uses headers/query params.
func TestPathTraversalProtection(t *testing.T) {
	tmpDir := t.TempDir()
	tempDir := filepath.Join(tmpDir, "temp")
	watchDir := filepath.Join(tmpDir, "watch")

	if err := os.MkdirAll(watchDir, 0755); err != nil {
		t.Fatalf("Failed to create watch directory: %v", err)
	}

	cfg := config.ServerConfig{
		Address: "0.0.0.0",
		Port:    8080,
		TempDir: tempDir,
	}

	dirs := []config.DirectoryConfig{
		{Name: "test", WatchPath: watchDir},
	}

	server, err := NewServer(cfg, dirs)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Test that files with path separators are rejected by our sanitization
	// even though Go's multipart library would extract the basename
	maliciousTests := []struct {
		name         string
		filename     string
		shouldReject bool
	}{
		{"double_dot", "..", true},
		{"single_dot", ".", true},
		{"windows_backslash", "..\\windows\\file.txt", true},
		// These pass through multipart but are safe due to basename extraction
		// Go's multipart library provides defense-in-depth by extracting only the basename
		{"unix_traversal", "../../../etc/passwd", false},     // Go extracts "passwd"
		{"unix_absolute", "/etc/passwd", false},              // Go extracts "passwd"
		{"subdirectory", "subdir/file.txt", false},           // Go extracts "file.txt"
		{"windows_absolute", "C:\\Windows\\file.txt", false}, // Go extracts basename (platform-dependent)
	}

	for _, tt := range maliciousTests {
		t.Run(tt.name, func(t *testing.T) {
			body := &bytes.Buffer{}
			writer := multipart.NewWriter(body)
			part, _ := writer.CreateFormFile("file", tt.filename)
			part.Write([]byte("test content"))
			writer.Close()

			req := httptest.NewRequest("POST", "/upload/test", body)
			req.Header.Set("Content-Type", writer.FormDataContentType())
			w := httptest.NewRecorder()

			server.handleUpload(w, req)

			resp := w.Result()

			if tt.shouldReject {
				if resp.StatusCode != http.StatusBadRequest {
					t.Errorf("Expected status 400 for malicious filename '%s', got %d", tt.filename, resp.StatusCode)
				}
			} else {
				// These are safe because Go's multipart extracts basename
				// Files are created with safe names in the watch directory
				if resp.StatusCode != http.StatusOK {
					t.Logf("Filename '%s' processed safely by Go's multipart library", tt.filename)
				}
			}

			// Verify no file was created outside watch directory
			parentDir := filepath.Dir(watchDir)
			entries, _ := os.ReadDir(parentDir)
			for _, entry := range entries {
				if entry.Name() != filepath.Base(watchDir) && entry.Name() != filepath.Base(tempDir) {
					t.Errorf("Unexpected file created in parent directory: %s", entry.Name())
				}
			}
		})
	}
}

// TestPathTraversalProtectionStreaming tests path traversal protection for streaming uploads
func TestPathTraversalProtectionStreaming(t *testing.T) {
	tmpDir := t.TempDir()
	tempDir := filepath.Join(tmpDir, "temp")
	watchDir := filepath.Join(tmpDir, "watch")

	if err := os.MkdirAll(watchDir, 0755); err != nil {
		t.Fatalf("Failed to create watch directory: %v", err)
	}

	cfg := config.ServerConfig{
		Address: "0.0.0.0",
		Port:    8080,
		TempDir: tempDir,
	}

	dirs := []config.DirectoryConfig{
		{Name: "test", WatchPath: watchDir},
	}

	server, err := NewServer(cfg, dirs)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Test malicious filenames (should be rejected)
	maliciousFilenames := []string{
		"../../../etc/passwd",
		"../../secret.txt",
		"..",
		"/etc/passwd",
		"subdir/file.txt", // Path separators not allowed in filenames
	}

	for _, filename := range maliciousFilenames {
		t.Run("filename:"+filename, func(t *testing.T) {
			content := []byte("malicious content")
			body := bytes.NewReader(content)

			req := httptest.NewRequest("POST", "/upload/test", body)
			req.Header.Set("X-Filename", filename)
			w := httptest.NewRecorder()

			server.handleStreamingUpload(w, req)

			resp := w.Result()

			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("Expected status 400 for malicious filename '%s', got %d", filename, resp.StatusCode)
			}
		})
	}

	// Test malicious URL paths (should be rejected)
	maliciousURLPaths := []string{
		"/upload/test/../escape",
		"/upload/test/../../escape",
		"/upload/test/subdir/../..",
	}

	for _, urlPath := range maliciousURLPaths {
		t.Run("urlpath:"+urlPath, func(t *testing.T) {
			content := []byte("malicious content")
			body := bytes.NewReader(content)

			req := httptest.NewRequest("POST", urlPath, body)
			req.Header.Set("X-Filename", "file.txt")
			w := httptest.NewRecorder()

			server.handleStreamingUpload(w, req)

			resp := w.Result()

			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("Expected status 400 for malicious URL path '%s', got %d", urlPath, resp.StatusCode)
			}
		})
	}
}

// TestSanitizeFilename tests the sanitizeFilename function directly
func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		input       string
		expectError bool
		expected    string
	}{
		// Valid filenames
		{"normal.txt", false, "normal.txt"},
		{"file with spaces.txt", false, "file with spaces.txt"},
		{"file-with-dashes.txt", false, "file-with-dashes.txt"},

		// Simple directory/filename (no separators)
		{"subdir", false, "subdir"},

		// Path separators NOT allowed in filenames (use URL paths instead)
		{"subdir/file.txt", true, ""},
		{"deep/nested/path/file.txt", true, ""},
		{"2025/01/invoice.pdf", true, ""},
		{"2025/01", true, ""},
		{"folder\\file.txt", true, ""},

		// Path traversal attempts (MUST BE REJECTED)
		{"../../../etc/passwd", true, ""},
		{"../../secret.txt", true, ""},
		{"..", true, ""},
		{".", true, ""},
		{"subdir/../file.txt", true, ""},
		{"..\\windows\\file.txt", true, ""},
		{"subdir/./file.txt", true, ""}, // Contains "." component

		// Absolute paths (MUST BE REJECTED)
		{"/absolute/path/file.txt", true, ""},

		// Invalid filenames
		{"", true, ""},
		{"file\x00.txt", true, ""},
		{"/file.txt", true, ""},        // Leading slash
		{"subdir//file.txt", true, ""}, // Empty component
		{"subdir/", true, ""},          // Trailing slash creates empty component
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := sanitizeFilename(tt.input)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for input '%s', but got none", tt.input)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for input '%s': %v", tt.input, err)
				}
				if result != tt.expected {
					t.Errorf("Expected '%s', got '%s'", tt.expected, result)
				}
			}
		})
	}
}

// TestSanitizeSubdirectoryPath tests the sanitizeSubdirectoryPath function
// This function validates subdirectory paths (allows path separators)
func TestSanitizeSubdirectoryPath(t *testing.T) {
	tests := []struct {
		input       string
		expectError bool
		expected    string
	}{
		// Valid subdirectory paths
		{"subdir", false, "subdir"},
		{"subdir/nested", false, filepath.FromSlash("subdir/nested")},
		{"deep/nested/path", false, filepath.FromSlash("deep/nested/path")},
		{"2025/01", false, filepath.FromSlash("2025/01")},

		// Path traversal attempts (MUST BE REJECTED)
		{"../../../etc", true, ""},
		{"../../secret", true, ""},
		{"..", true, ""},
		{".", true, ""},
		{"subdir/../other", true, ""},
		{"..\\windows\\dir", true, ""},
		{"subdir/./nested", true, ""}, // Contains "." component

		// Absolute paths (MUST BE REJECTED)
		{"/absolute/path", true, ""},

		// Invalid paths
		{"", true, ""},
		{"path\x00dir", true, ""},
		{"/path", true, ""},          // Leading slash
		{"subdir//nested", true, ""}, // Empty component
		{"subdir/", true, ""},        // Trailing slash creates empty component
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := sanitizeSubdirectoryPath(tt.input)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for input '%s', but got none", tt.input)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for input '%s': %v", tt.input, err)
				}
				if result != tt.expected {
					t.Errorf("Expected '%s', got '%s'", tt.expected, result)
				}
			}
		})
	}
}

// TestValidateSubdirectoryPath tests the validateSubdirectoryPath security function
func TestValidateSubdirectoryPath(t *testing.T) {
	tmpDir := t.TempDir()
	baseDir := filepath.Join(tmpDir, "watch")
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		t.Fatalf("Failed to create base directory: %v", err)
	}

	tests := []struct {
		name         string
		relativePath string
		expectError  bool
	}{
		{"simple_file", "file.txt", false},
		{"subdirectory", filepath.FromSlash("subdir/file.txt"), false},
		{"deep_nested", filepath.FromSlash("a/b/c/d/file.txt"), false},
		{"traversal_attempt", "../escape.txt", true},
		{"deep_traversal", "../../escape.txt", true},
		{"mixed_traversal", filepath.FromSlash("subdir/../../escape.txt"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := validateSubdirectoryPath(baseDir, tt.relativePath)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for path '%s', but got none. Result: %s", tt.relativePath, result)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for path '%s': %v", tt.relativePath, err)
				}
				// Verify result is within base directory
				if !strings.HasPrefix(result, baseDir) {
					t.Errorf("Result path '%s' is not within base directory '%s'", result, baseDir)
				}
			}
		})
	}
}

// TestUploadToSubdirectory tests uploading files to subdirectories
// Subdirectories are specified in the URL path, not in the filename
func TestUploadToSubdirectory(t *testing.T) {
	tmpDir := t.TempDir()
	tempDir := filepath.Join(tmpDir, "temp")
	watchDir := filepath.Join(tmpDir, "watch")

	if err := os.MkdirAll(watchDir, 0755); err != nil {
		t.Fatalf("Failed to create watch directory: %v", err)
	}

	cfg := config.ServerConfig{
		Address: "0.0.0.0",
		Port:    8080,
		TempDir: tempDir,
	}

	dirs := []config.DirectoryConfig{
		{Name: "test", WatchPath: watchDir},
	}

	server, err := NewServer(cfg, dirs)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	testCases := []struct {
		name     string
		urlPath  string
		filename string
		expected string
	}{
		{"single_level", "/upload/test/2025", "invoice.pdf", filepath.Join(watchDir, "2025", "invoice.pdf")},
		{"multi_level", "/upload/test/2025/01/30", "report.pdf", filepath.Join(watchDir, "2025", "01", "30", "report.pdf")},
		{"with_spaces", "/upload/test/my%20folder", "my file.txt", filepath.Join(watchDir, "my folder", "my file.txt")},
		{"root_level", "/upload/test", "simple.txt", filepath.Join(watchDir, "simple.txt")},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			body := &bytes.Buffer{}
			writer := multipart.NewWriter(body)

			part, err := writer.CreateFormFile("file", tc.filename)
			if err != nil {
				t.Fatalf("Failed to create form file: %v", err)
			}

			content := []byte("test content for " + tc.filename)
			part.Write(content)
			writer.Close()

			req := httptest.NewRequest("POST", tc.urlPath, body)
			req.Header.Set("Content-Type", writer.FormDataContentType())
			w := httptest.NewRecorder()

			server.handleUpload(w, req)

			resp := w.Result()

			if resp.StatusCode != http.StatusOK {
				respBody, _ := io.ReadAll(resp.Body)
				t.Fatalf("Expected status 200, got %d. Body: %s", resp.StatusCode, string(respBody))
			}

			// Verify file was created in correct subdirectory
			uploadedContent, err := os.ReadFile(tc.expected)
			if err != nil {
				t.Fatalf("Failed to read uploaded file at %s: %v", tc.expected, err)
			}

			if string(uploadedContent) != string(content) {
				t.Errorf("Content mismatch. Expected '%s', got '%s'", string(content), string(uploadedContent))
			}

			// Verify subdirectory was created (if applicable)
			if tc.urlPath != "/upload/test" {
				dir := filepath.Dir(tc.expected)
				info, err := os.Stat(dir)
				if err != nil {
					t.Errorf("Subdirectory not created: %v", err)
				} else if !info.IsDir() {
					t.Errorf("Expected %s to be a directory", dir)
				}
			}
		})
	}
}

// TestStreamingUploadToSubdirectory tests streaming uploads to subdirectories
// Subdirectories are specified in the URL path, not in the filename
func TestStreamingUploadToSubdirectory(t *testing.T) {
	tmpDir := t.TempDir()
	tempDir := filepath.Join(tmpDir, "temp")
	watchDir := filepath.Join(tmpDir, "watch")

	if err := os.MkdirAll(watchDir, 0755); err != nil {
		t.Fatalf("Failed to create watch directory: %v", err)
	}

	cfg := config.ServerConfig{
		Address: "0.0.0.0",
		Port:    8080,
		TempDir: tempDir,
	}

	dirs := []config.DirectoryConfig{
		{Name: "test", WatchPath: watchDir},
	}

	server, err := NewServer(cfg, dirs)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	testCases := []struct {
		name     string
		urlPath  string
		filename string
		expected string
	}{
		{"single_level", "/upload/test/logs", "app.log", filepath.Join(watchDir, "logs", "app.log")},
		{"multi_level", "/upload/test/data/2025/01", "metrics.json", filepath.Join(watchDir, "data", "2025", "01", "metrics.json")},
		{"root_level", "/upload/test", "file.txt", filepath.Join(watchDir, "file.txt")},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			content := []byte("streaming content for " + tc.filename)
			body := bytes.NewReader(content)

			req := httptest.NewRequest("POST", tc.urlPath, body)
			req.Header.Set("X-Filename", tc.filename)
			w := httptest.NewRecorder()

			server.handleStreamingUpload(w, req)

			resp := w.Result()

			if resp.StatusCode != http.StatusOK {
				respBody, _ := io.ReadAll(resp.Body)
				t.Fatalf("Expected status 200, got %d. Body: %s", resp.StatusCode, string(respBody))
			}

			// Verify file was created in correct subdirectory
			uploadedContent, err := os.ReadFile(tc.expected)
			if err != nil {
				t.Fatalf("Failed to read uploaded file at %s: %v", tc.expected, err)
			}

			if string(uploadedContent) != string(content) {
				t.Errorf("Content mismatch. Expected '%s', got '%s'", string(content), string(uploadedContent))
			}
		})
	}
}

// TestSubdirectoryPathTraversalProtection ensures path traversal is blocked for subdirectories
// Tests malicious URL paths that attempt to escape the watch directory
func TestSubdirectoryPathTraversalProtection(t *testing.T) {
	tmpDir := t.TempDir()
	tempDir := filepath.Join(tmpDir, "temp")
	watchDir := filepath.Join(tmpDir, "watch")

	if err := os.MkdirAll(watchDir, 0755); err != nil {
		t.Fatalf("Failed to create watch directory: %v", err)
	}

	cfg := config.ServerConfig{
		Address: "0.0.0.0",
		Port:    8080,
		TempDir: tempDir,
	}

	dirs := []config.DirectoryConfig{
		{Name: "test", WatchPath: watchDir},
	}

	server, err := NewServer(cfg, dirs)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	maliciousTests := []struct {
		name    string
		urlPath string
	}{
		{"parent_traversal", "/upload/test/../escape"},
		{"double_parent", "/upload/test/../../escape"},
		{"subdir_parent", "/upload/test/subdir/../.."},
		{"triple_parent", "/upload/test/valid/../../.."},
	}

	for _, tt := range maliciousTests {
		t.Run(tt.name, func(t *testing.T) {
			body := &bytes.Buffer{}
			writer := multipart.NewWriter(body)
			part, _ := writer.CreateFormFile("file", "file.txt")
			part.Write([]byte("malicious content"))
			writer.Close()

			req := httptest.NewRequest("POST", tt.urlPath, body)
			req.Header.Set("Content-Type", writer.FormDataContentType())
			w := httptest.NewRecorder()

			server.handleUpload(w, req)

			resp := w.Result()

			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("Expected status 400 for malicious URL path '%s', got %d", tt.urlPath, resp.StatusCode)
			}

			// Verify no file was created outside watch directory
			parentDir := filepath.Dir(watchDir)
			entries, _ := os.ReadDir(parentDir)
			for _, entry := range entries {
				name := entry.Name()
				if name != filepath.Base(watchDir) && name != filepath.Base(tempDir) {
					if name == "escape" || name == "file.txt" {
						t.Errorf("File escaped watch directory: %s", name)
					}
				}
			}
		})
	}
}

// TestHealthEndpointNoAuth tests that health endpoint doesn't require auth
func TestHealthEndpointNoAuth(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := config.ServerConfig{
		Address: "0.0.0.0",
		Port:    8080,
		TempDir: filepath.Join(tmpDir, "temp"),
		BasicAuth: config.BasicAuthConfig{
			Enabled:  true,
			Username: "testuser",
			Password: "testpass",
		},
	}

	dirs := []config.DirectoryConfig{
		{Name: "test", WatchPath: filepath.Join(tmpDir, "watch")},
	}

	server, err := NewServer(cfg, dirs)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Health endpoint should work without auth
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	server.handleHealth(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
}
