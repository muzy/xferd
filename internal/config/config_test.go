package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadValidConfig(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yml")

	configContent := `
server:
  address: "0.0.0.0"
  port: 8080
  temp_dir: /tmp/xferd

directories:
  - name: test
    watch_path: /tmp/test
    recursive: true
    watch:
      mode: hybrid_ultra_low_latency
      reconcile_scan:
        enabled: true
        interval_seconds: 30
    stability:
      confirmation_interval_ms: 100
      required_stable_checks: 2
      max_wait_ms: 1500
    shadow:
      enabled: true
      path: /tmp/shadow
      retention_hours: 48
    outbound:
      url: https://example.com/upload
      auth:
        type: basic
        username: user
        password: pass
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load valid config: %v", err)
	}

	// Validate loaded values
	if cfg.Server.Port != 8080 {
		t.Errorf("Expected port 8080, got %d", cfg.Server.Port)
	}

	if cfg.Server.Address != "0.0.0.0" {
		t.Errorf("Expected address 0.0.0.0, got %s", cfg.Server.Address)
	}

	if len(cfg.Directories) != 1 {
		t.Fatalf("Expected 1 directory, got %d", len(cfg.Directories))
	}

	dir := cfg.Directories[0]
	if dir.Name != "test" {
		t.Errorf("Expected directory name 'test', got '%s'", dir.Name)
	}

	if dir.Watch.Mode != "hybrid_ultra_low_latency" {
		t.Errorf("Expected watch mode 'hybrid_ultra_low_latency', got '%s'", dir.Watch.Mode)
	}

	if dir.Stability.ConfirmationIntervalMs != 100 {
		t.Errorf("Expected confirmation_interval_ms 100, got %d", dir.Stability.ConfirmationIntervalMs)
	}

	if dir.Shadow.RetentionHours != 48 {
		t.Errorf("Expected retention_hours 48, got %d", dir.Shadow.RetentionHours)
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "invalid.yml")

	invalidContent := `
server:
  port: "not a number"
  invalid yaml [[[
`

	if err := os.WriteFile(configPath, []byte(invalidContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("Expected error loading invalid YAML, got nil")
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load("/nonexistent/config.yml")
	if err == nil {
		t.Fatal("Expected error loading missing file, got nil")
	}
}

func TestValidateInvalidPort(t *testing.T) {
	tests := []struct {
		name string
		port int
	}{
		{"zero port", 0},
		{"negative port", -1},
		{"port too large", 65536},
		{"port way too large", 99999},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Server: ServerConfig{
					Port:    tt.port,
					TempDir: "/tmp/test",
				},
				Directories: []DirectoryConfig{
					{
						Name:      "test",
						WatchPath: "/tmp/test",
						Watch: WatchConfig{
							Mode: "hybrid_ultra_low_latency",
						},
						Stability: StabilityConfig{
							ConfirmationIntervalMs: 100,
							RequiredStableChecks:   2,
							MaxWaitMs:              1500,
						},
						Outbound: OutboundConfig{
							URL: "https://example.com",
						},
					},
				},
			}

			err := cfg.Validate()
			if err == nil {
				t.Errorf("Expected validation error for port %d, got nil", tt.port)
			}
		})
	}
}

func TestValidateMissingTempDir(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{
			Port:    8080,
			TempDir: "",
		},
		Directories: []DirectoryConfig{
			{
				Name:      "test",
				WatchPath: "/tmp/test",
				Watch: WatchConfig{
					Mode: "hybrid_ultra_low_latency",
				},
				Stability: StabilityConfig{
					ConfirmationIntervalMs: 100,
					RequiredStableChecks:   2,
					MaxWaitMs:              1500,
				},
				Outbound: OutboundConfig{
					URL: "https://example.com",
				},
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Expected validation error for missing temp_dir, got nil")
	}
}

func TestValidateNoDirectories(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{
			Port:    8080,
			TempDir: "/tmp/test",
		},
		Directories: []DirectoryConfig{},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Expected validation error for no directories, got nil")
	}
}

func TestValidateInvalidWatchMode(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{
			Port:    8080,
			TempDir: "/tmp/test",
		},
		Directories: []DirectoryConfig{
			{
				Name:      "test",
				WatchPath: "/tmp/test",
				Watch: WatchConfig{
					Mode: "invalid_mode",
				},
				Stability: StabilityConfig{
					ConfirmationIntervalMs: 100,
					RequiredStableChecks:   2,
					MaxWaitMs:              1500,
				},
				Outbound: OutboundConfig{
					URL: "https://example.com",
				},
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Expected validation error for invalid watch mode, got nil")
	}
}

func TestValidateAllWatchModes(t *testing.T) {
	modes := []string{
		"event_only",
		"polling_only",
		"hybrid_ultra_low_latency",
	}

	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			cfg := &Config{
				Server: ServerConfig{
					Port:    8080,
					TempDir: "/tmp/test",
				},
				Directories: []DirectoryConfig{
					{
						Name:      "test",
						WatchPath: "/tmp/test",
						Watch: WatchConfig{
							Mode: mode,
						},
						Stability: StabilityConfig{
							ConfirmationIntervalMs: 100,
							RequiredStableChecks:   2,
							MaxWaitMs:              1500,
						},
						Outbound: OutboundConfig{
							URL: "https://example.com",
						},
					},
				},
			}

			err := cfg.Validate()
			if err != nil {
				t.Errorf("Valid watch mode '%s' failed validation: %v", mode, err)
			}
		})
	}
}

func TestValidateInvalidStabilityConfig(t *testing.T) {
	tests := []struct {
		name                   string
		confirmationIntervalMs int
		requiredStableChecks   int
		maxWaitMs              int
	}{
		{"zero confirmation interval", 0, 2, 1500},
		{"negative confirmation interval", -1, 2, 1500},
		{"zero required checks", 100, 0, 1500},
		{"negative required checks", 100, -1, 1500},
		{"zero max wait", 100, 2, 0},
		{"negative max wait", 100, 2, -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Server: ServerConfig{
					Port:    8080,
					TempDir: "/tmp/test",
				},
				Directories: []DirectoryConfig{
					{
						Name:      "test",
						WatchPath: "/tmp/test",
						Watch: WatchConfig{
							Mode: "hybrid_ultra_low_latency",
						},
						Stability: StabilityConfig{
							ConfirmationIntervalMs: tt.confirmationIntervalMs,
							RequiredStableChecks:   tt.requiredStableChecks,
							MaxWaitMs:              tt.maxWaitMs,
						},
						Outbound: OutboundConfig{
							URL: "https://example.com",
						},
					},
				},
			}

			err := cfg.Validate()
			if err == nil {
				t.Errorf("Expected validation error for %s, got nil", tt.name)
			}
		})
	}
}

func TestValidateMissingDirectoryName(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{
			Port:    8080,
			TempDir: "/tmp/test",
		},
		Directories: []DirectoryConfig{
			{
				Name:      "",
				WatchPath: "/tmp/test",
				Watch: WatchConfig{
					Mode: "hybrid_ultra_low_latency",
				},
				Stability: StabilityConfig{
					ConfirmationIntervalMs: 100,
					RequiredStableChecks:   2,
					MaxWaitMs:              1500,
				},
				Outbound: OutboundConfig{
					URL: "https://example.com",
				},
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Expected validation error for missing directory name, got nil")
	}
}

func TestValidateMissingWatchPath(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{
			Port:    8080,
			TempDir: "/tmp/test",
		},
		Directories: []DirectoryConfig{
			{
				Name:      "test",
				WatchPath: "",
				Watch: WatchConfig{
					Mode: "hybrid_ultra_low_latency",
				},
				Stability: StabilityConfig{
					ConfirmationIntervalMs: 100,
					RequiredStableChecks:   2,
					MaxWaitMs:              1500,
				},
				Outbound: OutboundConfig{
					URL: "https://example.com",
				},
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Expected validation error for missing watch_path, got nil")
	}
}

func TestValidateMissingOutboundURL(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{
			Port:    8080,
			TempDir: "/tmp/test",
		},
		Directories: []DirectoryConfig{
			{
				Name:      "test",
				WatchPath: "/tmp/test",
				Watch: WatchConfig{
					Mode: "hybrid_ultra_low_latency",
				},
				Stability: StabilityConfig{
					ConfirmationIntervalMs: 100,
					RequiredStableChecks:   2,
					MaxWaitMs:              1500,
				},
				Outbound: OutboundConfig{
					URL: "",
				},
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Expected validation error for missing outbound URL, got nil")
	}
}

func TestEnvOverrides(t *testing.T) {
	// Set environment variables
	os.Setenv("XFERD_PORT", "9090")
	os.Setenv("XFERD_ADDRESS", "127.0.0.1")
	os.Setenv("XFERD_TEMP_DIR", "/custom/temp")
	defer func() {
		os.Unsetenv("XFERD_PORT")
		os.Unsetenv("XFERD_ADDRESS")
		os.Unsetenv("XFERD_TEMP_DIR")
	}()

	// Create a config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yml")

	configContent := `
server:
  address: "0.0.0.0"
  port: 8080
  temp_dir: /tmp/xferd

directories:
  - name: test
    watch_path: /tmp/test
    watch:
      mode: hybrid_ultra_low_latency
    stability:
      confirmation_interval_ms: 100
      required_stable_checks: 2
      max_wait_ms: 1500
    outbound:
      url: https://example.com/upload
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if cfg.Server.Port != 9090 {
		t.Errorf("Expected port 9090 from env override, got %d", cfg.Server.Port)
	}

	if cfg.Server.Address != "127.0.0.1" {
		t.Errorf("Expected address 127.0.0.1 from env override, got %s", cfg.Server.Address)
	}

	if cfg.Server.TempDir != "/custom/temp" {
		t.Errorf("Expected temp_dir /custom/temp from env override, got %s", cfg.Server.TempDir)
	}
}

func TestMultipleDirectories(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yml")

	configContent := `
server:
  address: "0.0.0.0"
  port: 8080
  temp_dir: /tmp/xferd

directories:
  - name: dir1
    watch_path: /tmp/dir1
    watch:
      mode: event_only
    stability:
      confirmation_interval_ms: 100
      required_stable_checks: 2
      max_wait_ms: 1500
    outbound:
      url: https://example.com/upload1
      auth:
        type: basic
        username: user1
        password: pass1

  - name: dir2
    watch_path: /tmp/dir2
    watch:
      mode: polling_only
    stability:
      confirmation_interval_ms: 200
      required_stable_checks: 3
      max_wait_ms: 3000
    outbound:
      url: https://example.com/upload2
      auth:
        type: bearer
        token: token123

  - name: dir3
    watch_path: /tmp/dir3
    watch:
      mode: hybrid_ultra_low_latency
    stability:
      confirmation_interval_ms: 150
      required_stable_checks: 2
      max_wait_ms: 2000
    outbound:
      url: https://example.com/upload3
      auth:
        type: token
        token: apikey456
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load config with multiple directories: %v", err)
	}

	if len(cfg.Directories) != 3 {
		t.Fatalf("Expected 3 directories, got %d", len(cfg.Directories))
	}

	// Validate first directory
	if cfg.Directories[0].Name != "dir1" {
		t.Errorf("Expected dir1, got %s", cfg.Directories[0].Name)
	}
	if cfg.Directories[0].Watch.Mode != "event_only" {
		t.Errorf("Expected event_only, got %s", cfg.Directories[0].Watch.Mode)
	}
	if cfg.Directories[0].Outbound.Auth.Type != "basic" {
		t.Errorf("Expected basic auth, got %s", cfg.Directories[0].Outbound.Auth.Type)
	}

	// Validate second directory
	if cfg.Directories[1].Name != "dir2" {
		t.Errorf("Expected dir2, got %s", cfg.Directories[1].Name)
	}
	if cfg.Directories[1].Watch.Mode != "polling_only" {
		t.Errorf("Expected polling_only, got %s", cfg.Directories[1].Watch.Mode)
	}
	if cfg.Directories[1].Outbound.Auth.Type != "bearer" {
		t.Errorf("Expected bearer auth, got %s", cfg.Directories[1].Outbound.Auth.Type)
	}

	// Validate third directory
	if cfg.Directories[2].Name != "dir3" {
		t.Errorf("Expected dir3, got %s", cfg.Directories[2].Name)
	}
	if cfg.Directories[2].Watch.Mode != "hybrid_ultra_low_latency" {
		t.Errorf("Expected hybrid_ultra_low_latency, got %s", cfg.Directories[2].Watch.Mode)
	}
	if cfg.Directories[2].Outbound.Auth.Type != "token" {
		t.Errorf("Expected token auth, got %s", cfg.Directories[2].Outbound.Auth.Type)
	}
}

func TestTLSConfiguration(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yml")

	configContent := `
server:
  address: "0.0.0.0"
  port: 8443
  temp_dir: /tmp/xferd
  tls:
    enabled: true
    cert_file: /etc/xferd/cert.pem
    key_file: /etc/xferd/key.pem

directories:
  - name: test
    watch_path: /tmp/test
    watch:
      mode: hybrid_ultra_low_latency
    stability:
      confirmation_interval_ms: 100
      required_stable_checks: 2
      max_wait_ms: 1500
    outbound:
      url: https://example.com/upload
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load config with TLS: %v", err)
	}

	if !cfg.Server.TLS.Enabled {
		t.Error("Expected TLS to be enabled")
	}

	if cfg.Server.TLS.CertFile != "/etc/xferd/cert.pem" {
		t.Errorf("Expected cert_file /etc/xferd/cert.pem, got %s", cfg.Server.TLS.CertFile)
	}

	if cfg.Server.TLS.KeyFile != "/etc/xferd/key.pem" {
		t.Errorf("Expected key_file /etc/xferd/key.pem, got %s", cfg.Server.TLS.KeyFile)
	}
}

func TestReconcileScanConfiguration(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yml")

	configContent := `
server:
  address: "0.0.0.0"
  port: 8080
  temp_dir: /tmp/xferd

directories:
  - name: test
    watch_path: /tmp/test
    watch:
      mode: hybrid_ultra_low_latency
      reconcile_scan:
        enabled: true
        interval_seconds: 60
    stability:
      confirmation_interval_ms: 100
      required_stable_checks: 2
      max_wait_ms: 1500
    outbound:
      url: https://example.com/upload
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if !cfg.Directories[0].Watch.ReconcileScan.Enabled {
		t.Error("Expected reconcile scan to be enabled")
	}

	if cfg.Directories[0].Watch.ReconcileScan.IntervalSeconds != 60 {
		t.Errorf("Expected interval_seconds 60, got %d", cfg.Directories[0].Watch.ReconcileScan.IntervalSeconds)
	}
}

func TestGetDurationHelpers(t *testing.T) {
	stability := StabilityConfig{
		ConfirmationIntervalMs: 100,
		MaxWaitMs:              1500,
	}

	if stability.GetConfirmationInterval().Milliseconds() != 100 {
		t.Errorf("Expected 100ms, got %v", stability.GetConfirmationInterval())
	}

	if stability.GetMaxWait().Milliseconds() != 1500 {
		t.Errorf("Expected 1500ms, got %v", stability.GetMaxWait())
	}

	shadow := ShadowConfig{
		RetentionHours: 48,
	}

	if shadow.GetRetentionDuration().Hours() != 48 {
		t.Errorf("Expected 48 hours, got %v", shadow.GetRetentionDuration())
	}

	reconcile := ReconcileScanConfig{
		IntervalSeconds: 60,
	}

	if reconcile.GetReconcileInterval().Seconds() != 60 {
		t.Errorf("Expected 60 seconds, got %v", reconcile.GetReconcileInterval())
	}
}

func TestGetIngestPath(t *testing.T) {
	tests := []struct {
		name        string
		watchPath   string
		ingestPath  string
		expected    string
		description string
	}{
		{
			name:        "ingest_path not specified",
			watchPath:   "/data/watch",
			ingestPath:  "",
			expected:    "/data/watch",
			description: "should default to watch_path when ingest_path is empty",
		},
		{
			name:        "ingest_path specified",
			watchPath:   "/data/watch",
			ingestPath:  "/data/ingest",
			expected:    "/data/ingest",
			description: "should return ingest_path when specified",
		},
		{
			name:        "ingest_path with different structure",
			watchPath:   "/staging/files",
			ingestPath:  "/processing/documents",
			expected:    "/processing/documents",
			description: "should return ingest_path regardless of path structure",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := DirectoryConfig{
				WatchPath:  tt.watchPath,
				IngestPath: tt.ingestPath,
			}

			result := dir.GetIngestPath()
			if result != tt.expected {
				t.Errorf("GetIngestPath() = %v, expected %v (%s)", result, tt.expected, tt.description)
			}
		})
	}
}

func TestDirectoryConfigValidationWithIngestPath(t *testing.T) {
	tests := []struct {
		name        string
		watchPath   string
		ingestPath  string
		shouldError bool
		description string
	}{
		{
			name:        "valid config without ingest_path",
			watchPath:   "/data/watch",
			ingestPath:  "",
			shouldError: false,
			description: "should accept config without ingest_path",
		},
		{
			name:        "valid config with ingest_path",
			watchPath:   "/data/watch",
			ingestPath:  "/data/ingest",
			shouldError: false,
			description: "should accept config with ingest_path",
		},
		{
			name:        "invalid watch_path",
			watchPath:   "",
			ingestPath:  "/data/ingest",
			shouldError: true,
			description: "should reject config with empty watch_path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := DirectoryConfig{
				Name:       "test",
				WatchPath:  tt.watchPath,
				IngestPath: tt.ingestPath,
				Watch: WatchConfig{
					Mode: "hybrid_ultra_low_latency",
				},
				Stability: StabilityConfig{
					ConfirmationIntervalMs: 100,
					RequiredStableChecks:   2,
					MaxWaitMs:              1500,
				},
				Outbound: OutboundConfig{
					URL: "https://example.com/upload",
				},
			}

			err := dir.Validate()
			if tt.shouldError && err == nil {
				t.Errorf("Validate() should have errored but didn't (%s)", tt.description)
			}
			if !tt.shouldError && err != nil {
				t.Errorf("Validate() errored unexpectedly: %v (%s)", err, tt.description)
			}
		})
	}
}
