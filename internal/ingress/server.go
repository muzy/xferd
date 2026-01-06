package ingress

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/muzy/xferd/internal/config"
	"golang.org/x/crypto/bcrypt"
)

// Server handles REST ingress for file uploads
type Server struct {
	config      config.ServerConfig
	directories map[string]config.DirectoryConfig // name -> config
	httpServer  *http.Server
	mu          sync.RWMutex
}

// NewServer creates a new REST ingress server
func NewServer(cfg config.ServerConfig, directories []config.DirectoryConfig) (*Server, error) {
	// Create temp directory if it doesn't exist
	if err := os.MkdirAll(cfg.TempDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}

	// Build directory map
	dirMap := make(map[string]config.DirectoryConfig)
	for _, dir := range directories {
		dirMap[dir.Name] = dir
	}

	s := &Server{
		config:      cfg,
		directories: dirMap,
	}

	// Setup HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/upload/", s.withAuth(s.handleUpload))
	mux.HandleFunc("/health", s.handleHealth)

	addr := fmt.Sprintf("%s:%d", cfg.Address, cfg.Port)
	s.httpServer = &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  30 * time.Minute, // Long timeout for large file uploads
		WriteTimeout: 30 * time.Minute,
	}

	return s, nil
}

// Start starts the HTTP server
func (s *Server) Start(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		s.httpServer.Shutdown(shutdownCtx)
	}()

	addr := s.httpServer.Addr
	if s.config.TLS.Enabled {
		log.Printf("Starting HTTPS ingress server on %s", addr)

		// Load TLS certificate
		cert, err := tls.LoadX509KeyPair(s.config.TLS.CertFile, s.config.TLS.KeyFile)
		if err != nil {
			return fmt.Errorf("failed to load TLS certificate: %w", err)
		}

		s.httpServer.TLSConfig = &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		}

		return s.httpServer.ListenAndServeTLS("", "")
	}

	log.Printf("Starting HTTP ingress server on %s", addr)
	return s.httpServer.ListenAndServe()
}

// Stop stops the server
func (s *Server) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return s.httpServer.Shutdown(ctx)
}

// withAuth wraps a handler with basic authentication if enabled
func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.config.BasicAuth.Enabled {
			next(w, r)
			return
		}

		username, password, ok := r.BasicAuth()
		if !ok {
			w.Header().Set("WWW-Authenticate", `Basic realm="xferd"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Use constant-time comparison for username
		usernameMatch := subtle.ConstantTimeCompare([]byte(username), []byte(s.config.BasicAuth.Username)) == 1

		var passwordMatch bool
		if s.config.BasicAuth.PasswordHash != "" {
			// Compare against bcrypt hash
			err := bcrypt.CompareHashAndPassword([]byte(s.config.BasicAuth.PasswordHash), []byte(password))
			passwordMatch = err == nil
		} else {
			// Compare against plaintext password (not recommended for production)
			passwordMatch = subtle.ConstantTimeCompare([]byte(password), []byte(s.config.BasicAuth.Password)) == 1
		}

		if !usernameMatch || !passwordMatch {
			w.Header().Set("WWW-Authenticate", `Basic realm="xferd"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			log.Printf("Failed authentication attempt from %s (username: %s)", r.RemoteAddr, username)
			return
		}

		next(w, r)
	}
}

// sanitizeFilename validates a filename (no path separators allowed)
func sanitizeFilename(filename string) (string, error) {
	// Check for null bytes first
	if strings.Contains(filename, "\x00") {
		return "", fmt.Errorf("filename contains null byte")
	}

	// Check for empty filename
	if filename == "" {
		return "", fmt.Errorf("filename is empty")
	}

	// Check for path traversal attempts
	if strings.Contains(filename, "..") {
		return "", fmt.Errorf("filename contains path traversal attempt")
	}

	// Filenames should not contain path separators
	if strings.Contains(filename, "/") || strings.Contains(filename, "\\") {
		return "", fmt.Errorf("filename contains path separator")
	}

	// Check for special names
	if filename == "." || filename == ".." {
		return "", fmt.Errorf("invalid filename")
	}

	// Clean the filename
	cleaned := filepath.Clean(filename)
	if cleaned != filename {
		return "", fmt.Errorf("filename normalization mismatch")
	}

	return filename, nil
}

// sanitizeSubdirectoryPath validates a subdirectory path (allows path separators)
func sanitizeSubdirectoryPath(subdir string) (string, error) {
	// Check for null bytes first
	if strings.Contains(subdir, "\x00") {
		return "", fmt.Errorf("path contains null byte")
	}

	// Check for empty path
	if subdir == "" {
		return "", fmt.Errorf("path is empty")
	}

	// Convert backslashes to forward slashes for consistent handling
	normalized := filepath.ToSlash(subdir)

	// Check for path traversal attempts (..)
	if strings.Contains(normalized, "..") {
		return "", fmt.Errorf("path contains traversal attempt")
	}

	// Check for absolute paths
	if strings.HasPrefix(normalized, "/") || filepath.IsAbs(subdir) {
		return "", fmt.Errorf("absolute paths not allowed")
	}

	// Split into components and validate each
	parts := strings.Split(normalized, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("invalid path component: %s", part)
		}
	}

	// Clean the path - this handles any remaining edge cases
	cleaned := filepath.Clean(normalized)

	// Ensure cleaning didn't introduce path traversal
	if strings.Contains(cleaned, "..") || strings.HasPrefix(cleaned, "/") {
		return "", fmt.Errorf("path normalization resulted in unsafe path")
	}

	// Convert back to OS-specific separators
	safePath := filepath.FromSlash(cleaned)

	return safePath, nil
}

// validateSubdirectoryPath ensures the final destination path is within the base directory
// This is a critical security check to prevent directory escape attacks
func validateSubdirectoryPath(baseDir, relativePath string) (string, error) {
	// Get absolute path of base directory
	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		return "", fmt.Errorf("failed to resolve base directory: %w", err)
	}

	// Join and get absolute path of final destination
	finalPath := filepath.Join(absBase, relativePath)
	absFinal, err := filepath.Abs(finalPath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve final path: %w", err)
	}

	// Ensure the final path is within the base directory
	// This prevents attacks like ../../../../etc/passwd
	if !strings.HasPrefix(absFinal, absBase+string(filepath.Separator)) &&
		absFinal != absBase {
		return "", fmt.Errorf("path escapes base directory")
	}

	return absFinal, nil
}

// handleHealth handles health check requests
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

// handleUpload handles file upload requests
// URL format: /upload/{directory_name}[/subdirectory/path]
// Example: /upload/invoices/2025/01/30
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract path after /upload/
	uploadPath := r.URL.Path[len("/upload/"):]
	if uploadPath == "" {
		http.Error(w, "Directory name required", http.StatusBadRequest)
		return
	}

	// Split into directory name and subdirectory path
	// First component is the directory name, rest is subdirectory
	pathParts := strings.SplitN(uploadPath, "/", 2)
	dirName := pathParts[0]
	var subdirPath string
	if len(pathParts) > 1 {
		subdirPath = pathParts[1]
	}

	// Lookup directory config
	s.mu.RLock()
	dirConfig, exists := s.directories[dirName]
	s.mu.RUnlock()

	if !exists {
		http.Error(w, "Unknown directory", http.StatusNotFound)
		return
	}

	// Parse multipart form
	if err := r.ParseMultipartForm(32 << 20); err != nil { // 32MB memory limit
		http.Error(w, fmt.Sprintf("Failed to parse form: %v", err), http.StatusBadRequest)
		return
	}

	file, handler, err := r.FormFile("file")
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get file: %v", err), http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Get filename from multipart (Go extracts basename automatically)
	filename := handler.Filename
	if filename == "" {
		http.Error(w, "Filename is required", http.StatusBadRequest)
		return
	}

	// Sanitize the filename (no path separators allowed in filename itself)
	safeFilename, err := sanitizeFilename(filename)
	if err != nil {
		http.Error(w, fmt.Sprintf("Invalid filename: %v", err), http.StatusBadRequest)
		log.Printf("Rejected unsafe filename from %s: %s", r.RemoteAddr, filename)
		return
	}

	// Build the target path: subdirectory from URL + filename from multipart
	var targetRelPath string
	if subdirPath != "" {
		// Sanitize subdirectory path (allows path separators)
		safeSubdir, err := sanitizeSubdirectoryPath(subdirPath)
		if err != nil {
			http.Error(w, fmt.Sprintf("Invalid subdirectory path: %v", err), http.StatusBadRequest)
			log.Printf("Rejected unsafe subdirectory from %s: %s", r.RemoteAddr, subdirPath)
			return
		}
		targetRelPath = filepath.Join(safeSubdir, safeFilename)
	} else {
		targetRelPath = safeFilename
	}

	// Validate that the final path is within the ingest directory
	finalPath, err := validateSubdirectoryPath(dirConfig.GetIngestPath(), targetRelPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Invalid path: %v", err), http.StatusBadRequest)
		log.Printf("Rejected path escape attempt from %s: %s", r.RemoteAddr, targetRelPath)
		return
	}

	// Create subdirectories if needed
	finalDir := filepath.Dir(finalPath)
	if err := os.MkdirAll(finalDir, 0755); err != nil {
		http.Error(w, fmt.Sprintf("Failed to create directory: %v", err), http.StatusInternalServerError)
		log.Printf("Directory creation failed for %s: %v", handler.Filename, err)
		return
	}

	// Stream file to temp location with .partial suffix
	// Use a unique temp name to avoid collisions
	tempPath := filepath.Join(s.config.TempDir, filepath.Base(safeFilename)+".partial")

	if err := s.streamToFile(file, tempPath); err != nil {
		http.Error(w, fmt.Sprintf("Failed to write file: %v", err), http.StatusInternalServerError)
		log.Printf("Upload failed for %s: %v", handler.Filename, err)
		return
	}

	// Atomic rename into watched directory
	if err := os.Rename(tempPath, finalPath); err != nil {
		os.Remove(tempPath) // Cleanup on error
		http.Error(w, fmt.Sprintf("Failed to finalize file: %v", err), http.StatusInternalServerError)
		log.Printf("Rename failed for %s: %v", handler.Filename, err)
		return
	}

	log.Printf("Upload complete: %s -> %s (%d bytes)", safeFilename, dirConfig.Name, handler.Size)
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Upload successful: %s\n", safeFilename)
}

// streamToFile streams data to a file efficiently
func (s *Server) streamToFile(src io.Reader, destPath string) error {
	// Create temp file
	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer f.Close()

	// Stream copy
	if _, err := io.Copy(f, src); err != nil {
		return fmt.Errorf("failed to copy data: %w", err)
	}

	// Sync to disk before atomic rename
	if err := f.Sync(); err != nil {
		return fmt.Errorf("failed to sync file: %w", err)
	}

	return nil
}

// StreamingHandler provides an alternative streaming upload handler
// This version streams directly without buffering in memory
// URL format: /upload/{directory_name}[/subdirectory/path]
func (s *Server) handleStreamingUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract path after /upload/
	uploadPath := r.URL.Path[len("/upload/"):]
	if uploadPath == "" {
		http.Error(w, "Directory name required", http.StatusBadRequest)
		return
	}

	// Split into directory name and subdirectory path
	pathParts := strings.SplitN(uploadPath, "/", 2)
	dirName := pathParts[0]
	var subdirPath string
	if len(pathParts) > 1 {
		subdirPath = pathParts[1]
	}

	s.mu.RLock()
	dirConfig, exists := s.directories[dirName]
	s.mu.RUnlock()

	if !exists {
		http.Error(w, "Unknown directory", http.StatusNotFound)
		return
	}

	// Get filename from header or query param
	filename := r.URL.Query().Get("filename")
	if filename == "" {
		filename = r.Header.Get("X-Filename")
	}
	if filename == "" {
		http.Error(w, "Filename required", http.StatusBadRequest)
		return
	}

	// Sanitize filename (no path separators allowed in filename itself)
	safeFilename, err := sanitizeFilename(filename)
	if err != nil {
		http.Error(w, fmt.Sprintf("Invalid filename: %v", err), http.StatusBadRequest)
		log.Printf("Rejected unsafe filename from %s: %s", r.RemoteAddr, filename)
		return
	}

	// Build the target path: subdirectory from URL + filename from parameter
	var targetRelPath string
	if subdirPath != "" {
		// Sanitize subdirectory path (allows path separators)
		safeSubdir, err := sanitizeSubdirectoryPath(subdirPath)
		if err != nil {
			http.Error(w, fmt.Sprintf("Invalid subdirectory path: %v", err), http.StatusBadRequest)
			log.Printf("Rejected unsafe subdirectory from %s: %s", r.RemoteAddr, subdirPath)
			return
		}
		targetRelPath = filepath.Join(safeSubdir, safeFilename)
	} else {
		targetRelPath = safeFilename
	}

	// Validate that the final path is within the ingest directory
	finalPath, err := validateSubdirectoryPath(dirConfig.GetIngestPath(), targetRelPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Invalid path: %v", err), http.StatusBadRequest)
		log.Printf("Rejected path escape attempt from %s: %s", r.RemoteAddr, targetRelPath)
		return
	}

	// Create subdirectories if needed
	finalDir := filepath.Dir(finalPath)
	if err := os.MkdirAll(finalDir, 0755); err != nil {
		http.Error(w, fmt.Sprintf("Failed to create directory: %v", err), http.StatusInternalServerError)
		log.Printf("Directory creation failed for %s: %v", filename, err)
		return
	}

	// Stream directly from request body
	tempPath := filepath.Join(s.config.TempDir, filepath.Base(safeFilename)+".partial")

	if err := s.streamToFile(r.Body, tempPath); err != nil {
		http.Error(w, fmt.Sprintf("Failed to write file: %v", err), http.StatusInternalServerError)
		log.Printf("Streaming upload failed for %s: %v", safeFilename, err)
		return
	}

	// Atomic rename
	if err := os.Rename(tempPath, finalPath); err != nil {
		os.Remove(tempPath)
		http.Error(w, fmt.Sprintf("Failed to finalize file: %v", err), http.StatusInternalServerError)
		log.Printf("Rename failed for %s: %v", safeFilename, err)
		return
	}

	log.Printf("Streaming upload complete: %s -> %s", safeFilename, dirConfig.Name)
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Upload successful: %s\n", safeFilename)
}
