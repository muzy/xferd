package watcher

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/muzy/xferd/internal/config"
)

func TestShouldIgnoreHiddenFiles(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{"hidden file", "/tmp/.hidden", true},
		{"hidden file with extension", "/tmp/.hidden.txt", true},
		{"regular file", "/tmp/regular.txt", false},
		{"path with hidden dir", "/tmp/.hidden/file.txt", false}, // only checks basename
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ShouldIgnore(tt.path, nil)
			if result != tt.expected {
				t.Errorf("ShouldIgnore(%s) = %v, expected %v", tt.path, result, tt.expected)
			}
		})
	}
}

func TestShouldIgnoreSuffixes(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{"partial suffix", "/tmp/file.partial", true},
		{"uploading suffix", "/tmp/file.uploading", true},
		{"tmp suffix", "/tmp/file.tmp", true},
		{"swp suffix", "/tmp/file.swp", true},
		{"tilde suffix", "/tmp/file.~", true},
		{"regular file", "/tmp/file.txt", false},
		{"partial in middle", "/tmp/file.partial.txt", false}, // only checks suffix
		{"doc file", "/tmp/document.pdf", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ShouldIgnore(tt.path, nil)
			if result != tt.expected {
				t.Errorf("ShouldIgnore(%s) = %v, expected %v", tt.path, result, tt.expected)
			}
		})
	}
}

func TestShouldIgnoreEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{"just dot", "/tmp/.", true},
		{"just double dot", "/tmp/..", true},
		{"empty basename", "", true}, // filepath.Base("") returns "." which is hidden
		{"just suffix", ".partial", true},
		{"suffix without dot", "partial", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ShouldIgnore(tt.path, nil)
			if result != tt.expected {
				t.Errorf("ShouldIgnore(%s) = %v, expected %v", tt.path, result, tt.expected)
			}
		})
	}
}

func TestShouldIgnoreWithPatterns(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		ignorePatterns []string
		expected       bool
	}{
		// Filename patterns
		{"exact match", "/tmp/test.tmp", []string{"*.tmp"}, true},
		{"wildcard match", "/tmp/file.log", []string{"*.log"}, true},
		{"no match", "/tmp/file.txt", []string{"*.log"}, false},
		{"prefix match", "/tmp/temp_file.txt", []string{"temp_*"}, true},
		{"suffix match", "/tmp/file_backup", []string{"*_backup"}, true},

		// Path patterns (for recursive watching)
		{"path pattern match", "/data/cache/temp.txt", []string{"*/cache/*"}, true},
		{"path pattern no match", "/data/files/temp.txt", []string{"*/cache/*"}, false},
		{"deep path match", "/home/user/docs/temp/.DS_Store", []string{"**/.DS_Store"}, true},
		{"deep path no match", "/home/user/docs/temp/file.txt", []string{"**/.DS_Store"}, false},

		// Mixed patterns
		{"multiple patterns match first", "/tmp/test.tmp", []string{"*.tmp", "*.log"}, true},
		{"multiple patterns match second", "/tmp/test.log", []string{"*.tmp", "*.log"}, true},
		{"multiple patterns no match", "/tmp/test.txt", []string{"*.tmp", "*.log"}, false},

		// Edge cases
		{"empty patterns", "/tmp/test.txt", []string{}, false},
		{"hidden file with patterns", "/tmp/.hidden", []string{"*.tmp"}, true},        // still ignores hidden
		{"legacy suffix with patterns", "/tmp/test.partial", []string{"*.log"}, true}, // still ignores legacy
		{"complex glob", "/tmp/backup_2023_01_01.zip", []string{"backup_*.zip"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ShouldIgnore(tt.path, tt.ignorePatterns)
			if result != tt.expected {
				t.Errorf("ShouldIgnore(%s, %v) = %v, expected %v", tt.path, tt.ignorePatterns, result, tt.expected)
			}
		})
	}
}

func TestProcessFileWithIgnorePatterns(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := config.DirectoryConfig{
		Stability: config.StabilityConfig{
			ConfirmationIntervalMs: 10,
			RequiredStableChecks:   1,
			MaxWaitMs:              100,
		},
		Ignore: []string{"*.tmp", "temp_*", "*/cache/*"},
	}

	handlerCalled := false
	handler := func(event FileEvent) error {
		handlerCalled = true
		return nil
	}

	tests := []struct {
		name     string
		filename string
		expected bool // true if handler should be called
	}{
		{"regular file", "regular.txt", true},
		{"tmp file", "file.tmp", false},
		{"temp prefix", "temp_file.txt", false},
		{"cache path", filepath.Join("cache", "file.txt"), false},
		{"nested cache path", filepath.Join("subdir", "cache", "file.txt"), false},
		{"regular in subdir", filepath.Join("subdir", "regular.txt"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testFile := filepath.Join(tmpDir, tt.filename)

			// Create parent directories if needed
			if dir := filepath.Dir(testFile); dir != tmpDir {
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatalf("Failed to create directories: %v", err)
				}
			}

			if err := os.WriteFile(testFile, []byte("content"), 0644); err != nil {
				t.Fatalf("Failed to create test file: %v", err)
			}

			handlerCalled = false
			event, err := processFile(testFile, false, cfg)

			if err != nil {
				t.Errorf("processFile should not error: %v", err)
			}

			if tt.expected {
				// Should process the file
				if event.Path == "" {
					t.Error("Expected file to be processed but got empty event")
				} else {
					// File was processed, call handler
					if err := handler(event); err != nil {
						t.Errorf("Handler error: %v", err)
					}
					if !handlerCalled {
						t.Error("Handler should have been called")
					}
				}
			} else {
				// Should ignore the file
				if event.Path != "" {
					t.Error("Expected file to be ignored but got event")
				}
				if handlerCalled {
					t.Error("Handler should not have been called for ignored file")
				}
			}
		})
	}
}

func TestCanOpenExclusively(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")

	// Create a test file
	content := []byte("test content")
	if err := os.WriteFile(testFile, content, 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Should be able to open it
	if !CanOpenExclusively(testFile) {
		t.Error("Should be able to open test file exclusively")
	}

	// Test nonexistent file
	nonexistent := filepath.Join(tmpDir, "nonexistent.txt")
	if CanOpenExclusively(nonexistent) {
		t.Error("Should not be able to open nonexistent file")
	}

	// Test directory
	if CanOpenExclusively(tmpDir) {
		// On some systems this may succeed, so we just test it runs
		t.Log("Can open directory (platform-specific behavior)")
	}
}

func TestIsStableQuickStability(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "stable.txt")

	// Create a stable file
	content := []byte("stable content")
	if err := os.WriteFile(testFile, content, 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Wait a bit to ensure file is definitely stable
	time.Sleep(50 * time.Millisecond)

	cfg := config.StabilityConfig{
		ConfirmationIntervalMs: 10,
		RequiredStableChecks:   2,
		MaxWaitMs:              500,
	}

	start := time.Now()
	stable, _ := isStable(testFile, cfg)
	elapsed := time.Since(start)

	if !stable {
		t.Error("File should be detected as stable")
	}

	// Should detect stability quickly (within max wait time)
	if elapsed > time.Duration(cfg.MaxWaitMs)*time.Millisecond {
		t.Errorf("Stability check took too long: %v", elapsed)
	}
}

func TestIsStableFileModifiedDuringCheck(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "modified.txt")

	// Create initial file
	content := []byte("initial content")
	if err := os.WriteFile(testFile, content, 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	cfg := config.StabilityConfig{
		ConfirmationIntervalMs: 50,
		RequiredStableChecks:   3,
		MaxWaitMs:              500,
	}

	// Start stability check in goroutine
	done := make(chan bool)
	var stable bool
	go func() {
		stable, _ = isStable(testFile, cfg)
		done <- true
	}()

	// Modify file during stability check
	time.Sleep(100 * time.Millisecond)
	newContent := []byte("modified content")
	os.WriteFile(testFile, newContent, 0644)

	<-done

	// Due to the modification, it should eventually time out and return true (assumes stable)
	// or detect instability early
	t.Logf("File stability after modification: %v", stable)
}

func TestIsStableFileDisappears(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "disappearing.txt")

	// Create file
	content := []byte("temporary content")
	if err := os.WriteFile(testFile, content, 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	cfg := config.StabilityConfig{
		ConfirmationIntervalMs: 50,
		RequiredStableChecks:   3,
		MaxWaitMs:              500,
	}

	// Start stability check in goroutine
	done := make(chan bool)
	var stable bool
	go func() {
		stable, _ = isStable(testFile, cfg)
		done <- true
	}()

	// Delete file during stability check (after first check completes)
	time.Sleep(60 * time.Millisecond)
	os.Remove(testFile)

	<-done

	// File disappeared during check, should return false
	if stable {
		t.Error("File should not be stable after disappearing")
	}
}

func TestIsStableTimeout(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "timeout.txt")

	// Create file
	content := []byte("content")
	if err := os.WriteFile(testFile, content, 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Configure with very high required checks to force timeout
	cfg := config.StabilityConfig{
		ConfirmationIntervalMs: 50,
		RequiredStableChecks:   100, // Will never reach this
		MaxWaitMs:              200, // But will timeout here
	}

	start := time.Now()
	stable, _ := isStable(testFile, cfg)
	elapsed := time.Since(start)

	// Should timeout and return true (assumes stable)
	if !stable {
		t.Error("Should assume stable after timeout")
	}

	// Should take approximately maxWaitMs
	expectedDuration := time.Duration(cfg.MaxWaitMs) * time.Millisecond
	if elapsed < expectedDuration || elapsed > expectedDuration+100*time.Millisecond {
		t.Logf("Stability check duration: %v (expected ~%v)", elapsed, expectedDuration)
	}
}

func TestProcessFileIgnoredFiles(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := config.DirectoryConfig{
		Stability: config.StabilityConfig{
			ConfirmationIntervalMs: 10,
			RequiredStableChecks:   1,
			MaxWaitMs:              100,
		},
	}

	handlerCalled := false
	handler := func(event FileEvent) error {
		handlerCalled = true
		return nil
	}

	tests := []struct {
		name     string
		filename string
	}{
		{"hidden file", ".hidden.txt"},
		{"partial file", "file.partial"},
		{"tmp file", "file.tmp"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testFile := filepath.Join(tmpDir, tt.filename)
			if err := os.WriteFile(testFile, []byte("content"), 0644); err != nil {
				t.Fatalf("Failed to create test file: %v", err)
			}

			handlerCalled = false
			event, err := processFile(testFile, false, cfg)

			if err != nil {
				t.Errorf("processFile should not error on ignored files: %v", err)
			}

			if event.Path != "" {
				// File was not ignored, call handler
				if err := handler(event); err != nil {
					t.Errorf("Handler error: %v", err)
				}
			}

			if handlerCalled {
				t.Error("Handler should not be called for ignored files")
			}
		})
	}
}

func TestProcessFileRegularFile(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "regular.txt")

	if err := os.WriteFile(testFile, []byte("content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Wait for file to be stable
	time.Sleep(50 * time.Millisecond)

	cfg := config.DirectoryConfig{
		Stability: config.StabilityConfig{
			ConfirmationIntervalMs: 10,
			RequiredStableChecks:   2,
			MaxWaitMs:              200,
		},
	}

	var receivedEvent FileEvent
	handlerCalled := false
	handler := func(event FileEvent) error {
		handlerCalled = true
		receivedEvent = event
		return nil
	}

	event, err := processFile(testFile, false, cfg)
	if err != nil {
		t.Fatalf("processFile failed: %v", err)
	}

	if err := handler(event); err != nil {
		t.Fatalf("Handler failed: %v", err)
	}

	if !handlerCalled {
		t.Fatal("Handler should have been called")
	}

	if receivedEvent.Path != testFile {
		t.Errorf("Event path mismatch. Expected %s, got %s", testFile, receivedEvent.Path)
	}

	if receivedEvent.IsRename {
		t.Error("IsRename should be false for regular file")
	}
}

func TestProcessFileRename(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping rename test on Windows (different behavior)")
	}

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "renamed.txt")

	if err := os.WriteFile(testFile, []byte("content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	cfg := config.DirectoryConfig{
		Stability: config.StabilityConfig{
			ConfirmationIntervalMs: 10,
			RequiredStableChecks:   2,
			MaxWaitMs:              200,
		},
	}

	var receivedEvent FileEvent
	handlerCalled := false
	handler := func(event FileEvent) error {
		handlerCalled = true
		receivedEvent = event
		return nil
	}

	// Process as rename (isRename = true)
	event, err := processFile(testFile, true, cfg)
	if err != nil {
		t.Fatalf("processFile failed: %v", err)
	}

	if err := handler(event); err != nil {
		t.Fatalf("Handler failed: %v", err)
	}

	if !handlerCalled {
		t.Fatal("Handler should have been called")
	}

	if !receivedEvent.IsRename {
		t.Error("IsRename should be true for renamed file")
	}
}

func TestProcessFileDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "subdir")

	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatalf("Failed to create subdirectory: %v", err)
	}

	cfg := config.DirectoryConfig{
		Stability: config.StabilityConfig{
			ConfirmationIntervalMs: 10,
			RequiredStableChecks:   1,
			MaxWaitMs:              100,
		},
	}

	handlerCalled := false
	handler := func(event FileEvent) error {
		handlerCalled = true
		return nil
	}

	event, err := processFile(subDir, false, cfg)
	if err != nil {
		t.Errorf("processFile should not error on directories: %v", err)
	}

	if event.Path != "" {
		// File was not ignored, call handler
		if err := handler(event); err != nil {
			t.Errorf("Handler error: %v", err)
		}
	}

	if handlerCalled {
		t.Error("Handler should not be called for directories")
	}
}

func TestProcessFileNonexistent(t *testing.T) {
	tmpDir := t.TempDir()
	nonexistent := filepath.Join(tmpDir, "nonexistent.txt")

	cfg := config.DirectoryConfig{
		Stability: config.StabilityConfig{
			ConfirmationIntervalMs: 10,
			RequiredStableChecks:   1,
			MaxWaitMs:              100,
		},
	}

	handlerCalled := false
	handler := func(event FileEvent) error {
		handlerCalled = true
		return nil
	}

	event, err := processFile(nonexistent, false, cfg)
	if err != nil {
		t.Errorf("processFile should not error on nonexistent files: %v", err)
	}

	if event.Path != "" {
		// File was not ignored, call handler
		if err := handler(event); err != nil {
			t.Errorf("Handler error: %v", err)
		}
	}

	if handlerCalled {
		t.Error("Handler should not be called for nonexistent files")
	}
}

func TestFileEventStruct(t *testing.T) {
	now := time.Now()
	event := FileEvent{
		Path:      "/tmp/test.txt",
		IsRename:  true,
		Timestamp: now,
	}

	if event.Path != "/tmp/test.txt" {
		t.Errorf("Path mismatch. Expected /tmp/test.txt, got %s", event.Path)
	}

	if !event.IsRename {
		t.Error("IsRename should be true")
	}

	if !event.Timestamp.Equal(now) {
		t.Errorf("Timestamp mismatch. Expected %v, got %v", now, event.Timestamp)
	}
}

func TestStabilityConfigEdgeCases(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")

	if err := os.WriteFile(testFile, []byte("content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Very aggressive stability check (should pass quickly)
	cfg := config.StabilityConfig{
		ConfirmationIntervalMs: 1,
		RequiredStableChecks:   1,
		MaxWaitMs:              100,
	}

	start := time.Now()
	stable, _ := isStable(testFile, cfg)
	elapsed := time.Since(start)

	if !stable {
		t.Error("File should be stable")
	}

	if elapsed > 50*time.Millisecond {
		t.Errorf("Stability check should complete quickly, took %v", elapsed)
	}
}

func TestIgnoredSuffixesList(t *testing.T) {
	// Verify the expected suffixes are in the ignored list
	expectedSuffixes := []string{".partial", ".uploading", ".tmp", ".swp", ".~"}

	if len(IgnoredSuffixes) != len(expectedSuffixes) {
		t.Errorf("Expected %d ignored suffixes, got %d", len(expectedSuffixes), len(IgnoredSuffixes))
	}

	for _, suffix := range expectedSuffixes {
		found := false
		for _, ignored := range IgnoredSuffixes {
			if ignored == suffix {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected suffix %s not found in IgnoredSuffixes", suffix)
		}
	}
}

func TestWalkDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a directory structure
	subDir := filepath.Join(tmpDir, "subdir")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatalf("Failed to create subdirectory: %v", err)
	}

	file1 := filepath.Join(tmpDir, "file1.txt")
	file2 := filepath.Join(subDir, "file2.txt")

	if err := os.WriteFile(file1, []byte("content1"), 0644); err != nil {
		t.Fatalf("Failed to create file1: %v", err)
	}

	if err := os.WriteFile(file2, []byte("content2"), 0644); err != nil {
		t.Fatalf("Failed to create file2: %v", err)
	}

	// Walk directory and count files
	fileCount := 0
	err := walkDirectory(tmpDir, func(path string, info os.FileInfo) error {
		if !info.IsDir() {
			fileCount++
		}
		return nil
	})

	if err != nil {
		t.Fatalf("walkDirectory failed: %v", err)
	}

	if fileCount != 2 {
		t.Errorf("Expected 2 files, found %d", fileCount)
	}
}

func TestWalkDirectoryEmpty(t *testing.T) {
	tmpDir := t.TempDir()

	err := walkDirectory(tmpDir, func(path string, info os.FileInfo) error {
		return nil
	})

	if err != nil {
		t.Fatalf("walkDirectory failed on empty directory: %v", err)
	}
}

func TestWalkDirectoryNonexistent(t *testing.T) {
	err := walkDirectory("/nonexistent/path", func(path string, info os.FileInfo) error {
		return nil
	})

	if err == nil {
		t.Fatal("Expected error walking nonexistent directory")
	}
}
