package shadow

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/muzy/xferd/internal/config"
)

// Manager handles shadow directory operations
type Manager struct {
	config config.ShadowConfig
	mu     sync.Mutex
}

// NewManager creates a new shadow directory manager
func NewManager(cfg config.ShadowConfig) (*Manager, error) {
	if !cfg.Enabled {
		return &Manager{config: cfg}, nil
	}

	// Ensure shadow directory exists
	if err := os.MkdirAll(cfg.Path, 0755); err != nil {
		return nil, fmt.Errorf("failed to create shadow directory: %w", err)
	}

	return &Manager{
		config: cfg,
	}, nil
}

// Store copies a file to the shadow directory
func (m *Manager) Store(sourcePath string) error {
	if !m.config.Enabled {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Generate shadow path maintaining relative structure
	shadowPath := m.getShadowPath(sourcePath)

	// Ensure parent directory exists
	shadowDir := filepath.Dir(shadowPath)
	if err := os.MkdirAll(shadowDir, 0755); err != nil {
		return fmt.Errorf("failed to create shadow subdirectory: %w", err)
	}

	// Create a real copy of the file
	if err := m.copyFile(sourcePath, shadowPath); err != nil {
		return fmt.Errorf("failed to copy to shadow: %w", err)
	}
	log.Printf("Shadow: copied %s -> %s", sourcePath, shadowPath)

	return nil
}

// Cleanup removes files older than retention period
func (m *Manager) Cleanup() error {
	if !m.config.Enabled {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	retention := m.config.GetRetentionDuration()
	cutoff := time.Now().Add(-retention)

	log.Printf("Shadow cleanup: removing files older than %v", retention)

	removed := 0
	err := filepath.Walk(m.config.Path, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}

		if info.IsDir() {
			return nil // Skip directories
		}

		if info.ModTime().Before(cutoff) {
			if err := os.Remove(path); err != nil {
				log.Printf("Shadow cleanup: failed to remove %s: %v", path, err)
			} else {
				removed++
			}
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("shadow cleanup failed: %w", err)
	}

	log.Printf("Shadow cleanup: removed %d files", removed)
	return nil
}

// StartCleanupRoutine starts periodic cleanup
func (m *Manager) StartCleanupRoutine(stopCh <-chan struct{}) {
	if !m.config.Enabled {
		return
	}

	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			if err := m.Cleanup(); err != nil {
				log.Printf("Shadow cleanup error: %v", err)
			}
		}
	}
}

// getShadowPath generates the shadow path for a source file
func (m *Manager) getShadowPath(sourcePath string) string {
	// Add timestamp to avoid conflicts
	base := filepath.Base(sourcePath)
	timestamp := time.Now().Format("20060102-150405.000000")
	shadowName := fmt.Sprintf("%s-%s", timestamp, base)
	
	return filepath.Join(m.config.Path, shadowName)
}

// copyFile copies a file from src to dst
func (m *Manager) copyFile(src, dst string) error {
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()

	destination, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destination.Close()

	// Stream copy to handle large files
	if _, err := io.Copy(destination, source); err != nil {
		return err
	}

	// Sync to disk
	return destination.Sync()
}

