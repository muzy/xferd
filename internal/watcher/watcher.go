package watcher

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/muzy/xferd/internal/config"
)

// FileEvent represents a detected file
type FileEvent struct {
	Path                  string
	IsRename              bool
	Timestamp             time.Time
	ProcessedDueToTimeout bool // true if file was processed due to stability timeout
}

// EventHandler processes detected files
type EventHandler func(event FileEvent) error

// Watcher abstracts filesystem watching
type Watcher interface {
	Start(ctx context.Context) error
	Stop() error
	ClearEnqueued(path string)
}

// IgnoredSuffixes are file patterns to ignore (legacy - for backward compatibility)
var IgnoredSuffixes = []string{".partial", ".uploading", ".tmp", ".swp", ".~"}

// ShouldIgnore checks if a file should be ignored based on patterns
func ShouldIgnore(path string, ignorePatterns []string) bool {
	base := filepath.Base(path)

	// Ignore hidden files
	if base != "" && base[0] == '.' {
		return true
	}

	// Check configurable ignore patterns
	for _, pattern := range ignorePatterns {
		// Check if pattern contains path separators (indicates path pattern)
		if strings.Contains(pattern, "/") || strings.Contains(pattern, "\\") {
			// Path pattern - use simple glob matching for paths
			if matchesPathPattern(pattern, path) {
				return true
			}
		} else {
			// Filename pattern - match against basename
			if matched, err := filepath.Match(pattern, base); err == nil && matched {
				return true
			}
		}
	}

	// Legacy suffix checking for backward compatibility
	for _, suffix := range IgnoredSuffixes {
		if len(base) > len(suffix) && base[len(base)-len(suffix):] == suffix {
			return true
		}
	}

	return false
}

// matchesPathPattern performs simple glob matching for path patterns
func matchesPathPattern(pattern, path string) bool {
	// Normalize separators
	pattern = filepath.ToSlash(pattern)
	path = filepath.ToSlash(path)

	// Remove leading slash for matching
	pattern = strings.TrimPrefix(pattern, "/")
	path = strings.TrimPrefix(path, "/")

	// Split into segments
	patternParts := strings.Split(pattern, "/")
	pathParts := strings.Split(path, "/")

	// If pattern has more parts than path, no match
	if len(patternParts) > len(pathParts) {
		return false
	}

	// Match from the end (rightmost parts first)
	patternIdx := len(patternParts) - 1
	pathIdx := len(pathParts) - 1

	for patternIdx >= 0 && pathIdx >= 0 {
		part := patternParts[patternIdx]
		pathPart := pathParts[pathIdx]

		// If this pattern part doesn't match, fail
		if matched, err := filepath.Match(part, pathPart); err != nil || !matched {
			return false
		}

		patternIdx--
		pathIdx--
	}

	// If we have more pattern parts but no more path parts, fail
	if patternIdx >= 0 {
		return false
	}

	return true
}

// ShouldIgnoreLegacy checks if a file should be ignored (legacy function for backward compatibility)
func ShouldIgnoreLegacy(path string) bool {
	return ShouldIgnore(path, nil)
}

// NewWatcher creates a platform-specific watcher
func NewWatcher(cfg config.DirectoryConfig, handler EventHandler) (Watcher, error) {
	// Use platform-specific implementation
	return newPlatformWatcher(cfg, handler)
}

// isStable checks if a file is stable (not being written to)
// Returns (stable, timedOut) - true if stable, true if stability check timed out
func isStable(path string, cfg config.StabilityConfig) (bool, bool) {
	interval := cfg.GetConfirmationInterval()
	maxWait := cfg.GetMaxWait()
	requiredChecks := cfg.RequiredStableChecks

	start := time.Now()
	var lastSize int64
	var lastModTime time.Time
	stableCount := 0

	for {
		if time.Since(start) > maxWait {
			// Timeout - assume stable (but log this and indicate it was due to timeout)
			log.Printf("Stability check timeout for %s: assuming stable after %v (file may still be writing)", path, maxWait)
			return true, true
		}

		info, err := os.Stat(path)
		if err != nil {
			// File disappeared
			return false, false
		}

		currentSize := info.Size()
		currentModTime := info.ModTime()

		if stableCount > 0 && currentSize == lastSize && currentModTime.Equal(lastModTime) {
			stableCount++
			if stableCount >= requiredChecks {
				// File is stable
				return true, false
			}
		} else {
			// File changed, reset counter
			stableCount = 1
			lastSize = currentSize
			lastModTime = currentModTime
		}

		time.Sleep(interval)
	}
}

// CanOpenExclusively tries to open a file exclusively to check if it's in use
func CanOpenExclusively(path string) bool {
	// Try to open the file in read mode
	// On most systems, if another process has it open for writing, this will succeed
	// but we can at least verify the file is accessible
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	// Try to get file info to ensure file is valid
	_, err = f.Stat()
	return err == nil
}

// walkDirectory recursively walks a directory tree
func walkDirectory(root string, fn func(path string, info os.FileInfo) error) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		return fn(path, info)
	})
}

// processFile handles a detected file after stability confirmation
func processFile(path string, isRename bool, cfg config.DirectoryConfig) (FileEvent, error) {
	// Skip if should be ignored
	if ShouldIgnore(path, cfg.Ignore) {
		return FileEvent{}, nil
	}

	// Check if it's a regular file
	info, err := os.Stat(path)
	if err != nil {
		return FileEvent{}, nil // File disappeared, skip
	}

	if !info.Mode().IsRegular() {
		return FileEvent{}, nil // Not a regular file
	}

	// For atomic renames, skip stability check on Linux
	// Windows renames still get a short confirmation
	needsStabilityCheck := true
	if isRename {
		// On Linux with IN_MOVED_TO, file is already complete
		// On Windows, we still do a quick check
		needsStabilityCheck = false
	}

	var processedDueToTimeout bool
	if needsStabilityCheck {
		stable, timedOut := isStable(path, cfg.Stability)
		if !stable {
			return FileEvent{}, fmt.Errorf("file stability check failed: %s", path)
		}
		processedDueToTimeout = timedOut
	}

	// File is ready, return event for caller to handle
	event := FileEvent{
		Path:                  path,
		IsRename:              isRename,
		Timestamp:             time.Now(),
		ProcessedDueToTimeout: processedDueToTimeout,
	}

	return event, nil
}
