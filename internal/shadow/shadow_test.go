package shadow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/muzy/xferd/internal/config"
)

func TestNewManagerDisabled(t *testing.T) {
	cfg := config.ShadowConfig{
		Enabled: false,
	}

	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("Failed to create disabled shadow manager: %v", err)
	}

	if mgr == nil {
		t.Fatal("Expected non-nil manager")
	}
}

func TestNewManagerEnabled(t *testing.T) {
	tmpDir := t.TempDir()
	shadowPath := filepath.Join(tmpDir, "shadow")

	cfg := config.ShadowConfig{
		Enabled:        true,
		Path:           shadowPath,
		RetentionHours: 24,
	}

	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("Failed to create shadow manager: %v", err)
	}

	if mgr == nil {
		t.Fatal("Expected non-nil manager")
	}

	// Verify shadow directory was created
	info, err := os.Stat(shadowPath)
	if err != nil {
		t.Fatalf("Shadow directory not created: %v", err)
	}

	if !info.IsDir() {
		t.Fatal("Shadow path is not a directory")
	}
}

func TestStoreFileDisabled(t *testing.T) {
	cfg := config.ShadowConfig{
		Enabled: false,
	}

	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Create a test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	content := []byte("test content")
	if err := os.WriteFile(testFile, content, 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Store should succeed but do nothing
	err = mgr.Store(testFile)
	if err != nil {
		t.Fatalf("Store failed for disabled manager: %v", err)
	}
}

func TestStoreFileEnabled(t *testing.T) {
	tmpDir := t.TempDir()
	shadowPath := filepath.Join(tmpDir, "shadow")
	sourcePath := filepath.Join(tmpDir, "source")

	// Create source directory
	if err := os.MkdirAll(sourcePath, 0755); err != nil {
		t.Fatalf("Failed to create source directory: %v", err)
	}

	cfg := config.ShadowConfig{
		Enabled:        true,
		Path:           shadowPath,
		RetentionHours: 24,
	}

	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Create a test file
	testFile := filepath.Join(sourcePath, "test.txt")
	content := []byte("test content")
	if err := os.WriteFile(testFile, content, 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Store the file
	err = mgr.Store(testFile)
	if err != nil {
		t.Fatalf("Failed to store file: %v", err)
	}

	// Verify shadow file exists (with timestamp prefix)
	files, err := os.ReadDir(shadowPath)
	if err != nil {
		t.Fatalf("Failed to read shadow directory: %v", err)
	}

	if len(files) != 1 {
		t.Fatalf("Expected 1 file in shadow directory, got %d", len(files))
	}

	// Read shadow file and verify content
	shadowFile := filepath.Join(shadowPath, files[0].Name())
	shadowContent, err := os.ReadFile(shadowFile)
	if err != nil {
		t.Fatalf("Failed to read shadow file: %v", err)
	}

	if string(shadowContent) != string(content) {
		t.Errorf("Shadow file content mismatch. Expected '%s', got '%s'", string(content), string(shadowContent))
	}
}

func TestStoreMultipleFiles(t *testing.T) {
	tmpDir := t.TempDir()
	shadowPath := filepath.Join(tmpDir, "shadow")
	sourcePath := filepath.Join(tmpDir, "source")

	if err := os.MkdirAll(sourcePath, 0755); err != nil {
		t.Fatalf("Failed to create source directory: %v", err)
	}

	cfg := config.ShadowConfig{
		Enabled:        true,
		Path:           shadowPath,
		RetentionHours: 24,
	}

	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Create and store multiple files
	for i := 0; i < 5; i++ {
		testFile := filepath.Join(sourcePath, "test.txt")
		content := []byte("test content")
		if err := os.WriteFile(testFile, content, 0644); err != nil {
			t.Fatalf("Failed to create test file %d: %v", i, err)
		}

		// Small delay to ensure unique timestamps
		time.Sleep(2 * time.Millisecond)

		if err := mgr.Store(testFile); err != nil {
			t.Fatalf("Failed to store file %d: %v", i, err)
		}
	}

	// Verify all files exist
	files, err := os.ReadDir(shadowPath)
	if err != nil {
		t.Fatalf("Failed to read shadow directory: %v", err)
	}

	if len(files) != 5 {
		t.Fatalf("Expected 5 files in shadow directory, got %d", len(files))
	}
}

func TestCleanupDisabled(t *testing.T) {
	cfg := config.ShadowConfig{
		Enabled: false,
	}

	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Cleanup should succeed but do nothing
	err = mgr.Cleanup()
	if err != nil {
		t.Fatalf("Cleanup failed for disabled manager: %v", err)
	}
}

func TestCleanupOldFiles(t *testing.T) {
	tmpDir := t.TempDir()
	shadowPath := filepath.Join(tmpDir, "shadow")

	cfg := config.ShadowConfig{
		Enabled:        true,
		Path:           shadowPath,
		RetentionHours: 1, // 1 hour retention
	}

	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Create an old file (modified 2 hours ago)
	oldFile := filepath.Join(shadowPath, "old-file.txt")
	if err := os.WriteFile(oldFile, []byte("old content"), 0644); err != nil {
		t.Fatalf("Failed to create old file: %v", err)
	}

	twoHoursAgo := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(oldFile, twoHoursAgo, twoHoursAgo); err != nil {
		t.Fatalf("Failed to set old file timestamp: %v", err)
	}

	// Create a recent file
	recentFile := filepath.Join(shadowPath, "recent-file.txt")
	if err := os.WriteFile(recentFile, []byte("recent content"), 0644); err != nil {
		t.Fatalf("Failed to create recent file: %v", err)
	}

	// Run cleanup
	err = mgr.Cleanup()
	if err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}

	// Verify old file was removed
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Error("Old file should have been removed")
	}

	// Verify recent file still exists
	if _, err := os.Stat(recentFile); err != nil {
		t.Error("Recent file should still exist")
	}
}

func TestCleanupEmptyDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	shadowPath := filepath.Join(tmpDir, "shadow")

	cfg := config.ShadowConfig{
		Enabled:        true,
		Path:           shadowPath,
		RetentionHours: 1,
	}

	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Cleanup on empty directory should succeed
	err = mgr.Cleanup()
	if err != nil {
		t.Fatalf("Cleanup failed on empty directory: %v", err)
	}
}

func TestCleanupWithSubdirectories(t *testing.T) {
	tmpDir := t.TempDir()
	shadowPath := filepath.Join(tmpDir, "shadow")
	subDir := filepath.Join(shadowPath, "subdir")

	cfg := config.ShadowConfig{
		Enabled:        true,
		Path:           shadowPath,
		RetentionHours: 1,
	}

	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Create subdirectory
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("Failed to create subdirectory: %v", err)
	}

	// Create an old file in subdirectory
	oldFile := filepath.Join(subDir, "old-file.txt")
	if err := os.WriteFile(oldFile, []byte("old content"), 0644); err != nil {
		t.Fatalf("Failed to create old file: %v", err)
	}

	twoHoursAgo := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(oldFile, twoHoursAgo, twoHoursAgo); err != nil {
		t.Fatalf("Failed to set old file timestamp: %v", err)
	}

	// Run cleanup
	err = mgr.Cleanup()
	if err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}

	// Verify old file was removed
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Error("Old file in subdirectory should have been removed")
	}

	// Subdirectory should still exist (cleanup doesn't remove directories)
	if _, err := os.Stat(subDir); err != nil {
		t.Error("Subdirectory should still exist")
	}
}

func TestGetShadowPath(t *testing.T) {
	tmpDir := t.TempDir()
	shadowPath := filepath.Join(tmpDir, "shadow")

	cfg := config.ShadowConfig{
		Enabled:        true,
		Path:           shadowPath,
		RetentionHours: 24,
	}

	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	sourcePath := "/tmp/source/test.txt"
	shadowFilePath := mgr.getShadowPath(sourcePath)

	// Should be in shadow directory
	if filepath.Dir(shadowFilePath) != shadowPath {
		t.Errorf("Shadow file should be in shadow directory. Got: %s", shadowFilePath)
	}

	// Should contain timestamp
	filename := filepath.Base(shadowFilePath)
	if len(filename) < 20 { // timestamp is at least 20 chars
		t.Errorf("Shadow filename should contain timestamp. Got: %s", filename)
	}

	// Should contain original filename
	if !strings.HasPrefix(filename, "202") { // timestamp starts with year 202x
		t.Errorf("Shadow filename should start with timestamp. Got: %s", filename)
	}
}

func TestCopyFile(t *testing.T) {
	tmpDir := t.TempDir()
	shadowPath := filepath.Join(tmpDir, "shadow")

	cfg := config.ShadowConfig{
		Enabled:        true,
		Path:           shadowPath,
		RetentionHours: 24,
	}

	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Create source file
	sourceFile := filepath.Join(tmpDir, "source.txt")
	content := []byte("test content for copying")
	if err := os.WriteFile(sourceFile, content, 0644); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	// Copy file
	destFile := filepath.Join(shadowPath, "dest.txt")
	err = mgr.copyFile(sourceFile, destFile)
	if err != nil {
		t.Fatalf("Failed to copy file: %v", err)
	}

	// Verify destination file exists and has correct content
	destContent, err := os.ReadFile(destFile)
	if err != nil {
		t.Fatalf("Failed to read destination file: %v", err)
	}

	if string(destContent) != string(content) {
		t.Errorf("Content mismatch. Expected '%s', got '%s'", string(content), string(destContent))
	}
}

func TestCopyLargeFile(t *testing.T) {
	tmpDir := t.TempDir()
	shadowPath := filepath.Join(tmpDir, "shadow")

	cfg := config.ShadowConfig{
		Enabled:        true,
		Path:           shadowPath,
		RetentionHours: 24,
	}

	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Create a large source file (10 MB)
	sourceFile := filepath.Join(tmpDir, "large.bin")
	content := make([]byte, 10*1024*1024)
	for i := range content {
		content[i] = byte(i % 256)
	}
	if err := os.WriteFile(sourceFile, content, 0644); err != nil {
		t.Fatalf("Failed to create large file: %v", err)
	}

	// Copy file
	destFile := filepath.Join(shadowPath, "large-copy.bin")
	err = mgr.copyFile(sourceFile, destFile)
	if err != nil {
		t.Fatalf("Failed to copy large file: %v", err)
	}

	// Verify file size
	info, err := os.Stat(destFile)
	if err != nil {
		t.Fatalf("Failed to stat destination file: %v", err)
	}

	if info.Size() != int64(len(content)) {
		t.Errorf("Size mismatch. Expected %d, got %d", len(content), info.Size())
	}
}

func TestCopyNonexistentFile(t *testing.T) {
	tmpDir := t.TempDir()
	shadowPath := filepath.Join(tmpDir, "shadow")

	cfg := config.ShadowConfig{
		Enabled:        true,
		Path:           shadowPath,
		RetentionHours: 24,
	}

	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Try to copy nonexistent file
	sourceFile := filepath.Join(tmpDir, "nonexistent.txt")
	destFile := filepath.Join(shadowPath, "dest.txt")

	err = mgr.copyFile(sourceFile, destFile)
	if err == nil {
		t.Fatal("Expected error copying nonexistent file, got nil")
	}
}

func TestRetentionDurationCalculation(t *testing.T) {
	tests := []struct {
		hours    int
		expected time.Duration
	}{
		{1, 1 * time.Hour},
		{24, 24 * time.Hour},
		{48, 48 * time.Hour},
		{168, 168 * time.Hour}, // 1 week
	}

	for _, tt := range tests {
		t.Run(string(rune(tt.hours)), func(t *testing.T) {
			cfg := config.ShadowConfig{
				RetentionHours: tt.hours,
			}

			duration := cfg.GetRetentionDuration()
			if duration != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, duration)
			}
		})
	}
}

func TestConcurrentStores(t *testing.T) {
	tmpDir := t.TempDir()
	shadowPath := filepath.Join(tmpDir, "shadow")
	sourcePath := filepath.Join(tmpDir, "source")

	if err := os.MkdirAll(sourcePath, 0755); err != nil {
		t.Fatalf("Failed to create source directory: %v", err)
	}

	cfg := config.ShadowConfig{
		Enabled:        true,
		Path:           shadowPath,
		RetentionHours: 24,
	}

	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Create and store files concurrently
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(index int) {
			defer func() { done <- true }()

			testFile := filepath.Join(sourcePath, "test.txt")
			content := []byte("test content")
			if err := os.WriteFile(testFile, content, 0644); err != nil {
				t.Errorf("Failed to create test file %d: %v", index, err)
				return
			}

			// Small delay to ensure unique timestamps
			time.Sleep(2 * time.Millisecond)

			if err := mgr.Store(testFile); err != nil {
				t.Errorf("Failed to store file %d: %v", index, err)
			}
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify files exist
	files, err := os.ReadDir(shadowPath)
	if err != nil {
		t.Fatalf("Failed to read shadow directory: %v", err)
	}

	if len(files) != 10 {
		t.Errorf("Expected 10 files in shadow directory, got %d", len(files))
	}
}
