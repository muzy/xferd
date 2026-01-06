package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config represents the entire xferd configuration
type Config struct {
	Server      ServerConfig      `yaml:"server"`
	Directories []DirectoryConfig `yaml:"directories"`
}

// ServerConfig defines REST ingress settings
type ServerConfig struct {
	Address   string          `yaml:"address"`
	Port      int             `yaml:"port"`
	TLS       TLSConfig       `yaml:"tls"`
	TempDir   string          `yaml:"temp_dir"`
	BasicAuth BasicAuthConfig `yaml:"basic_auth"`
}

// BasicAuthConfig defines optional basic authentication
type BasicAuthConfig struct {
	Enabled      bool   `yaml:"enabled"`
	Username     string `yaml:"username"`
	Password     string `yaml:"password"`      // Plaintext password (not recommended for production)
	PasswordHash string `yaml:"password_hash"` // Bcrypt hash of password (recommended)
}

// TLSConfig defines TLS settings
type TLSConfig struct {
	Enabled  bool   `yaml:"enabled"`
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
}

// DirectoryConfig represents a single watched directory configuration
type DirectoryConfig struct {
	Name       string          `yaml:"name"`
	WatchPath  string          `yaml:"watch_path"`
	IngestPath string          `yaml:"ingest_path,omitempty"` // Optional: defaults to watch_path
	Recursive  bool            `yaml:"recursive"`
	Ignore     []string        `yaml:"ignore"`
	Watch      WatchConfig     `yaml:"watch"`
	Stability  StabilityConfig `yaml:"stability"`
	Shadow     ShadowConfig    `yaml:"shadow"`
	Outbound   OutboundConfig  `yaml:"outbound"`
}

// WatchConfig defines watching behavior
type WatchConfig struct {
	Mode                 string              `yaml:"mode"`
	StartupReconcileScan *bool               `yaml:"startup_reconcile_scan"`
	ReconcileScan        ReconcileScanConfig `yaml:"reconcile_scan"`
}

// ReconcileScanConfig defines periodic reconciliation
type ReconcileScanConfig struct {
	Enabled         bool `yaml:"enabled"`
	IntervalSeconds int  `yaml:"interval_seconds"`
}

// StabilityConfig defines file stability confirmation settings
type StabilityConfig struct {
	ConfirmationIntervalMs int `yaml:"confirmation_interval_ms"`
	RequiredStableChecks   int `yaml:"required_stable_checks"`
	MaxWaitMs              int `yaml:"max_wait_ms"`
}

// ShadowConfig defines shadow directory settings
type ShadowConfig struct {
	Enabled        bool   `yaml:"enabled"`
	Path           string `yaml:"path"`
	RetentionHours int    `yaml:"retention_hours"`
}

// OutboundConfig defines upload destination settings
type OutboundConfig struct {
	URL  string     `yaml:"url"`
	Auth AuthConfig `yaml:"auth"`
}

// AuthConfig defines authentication settings
type AuthConfig struct {
	Type     string `yaml:"type"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	Token    string `yaml:"token"`
}

// Load reads and parses the configuration file
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Apply environment variable overrides
	applyEnvOverrides(&cfg)

	// Set defaults
	setDefaults(&cfg)

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &cfg, nil
}

// Validate checks the configuration for errors
func (c *Config) Validate() error {
	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		return fmt.Errorf("invalid server port: %d", c.Server.Port)
	}

	if c.Server.TempDir == "" {
		return fmt.Errorf("temp_dir is required")
	}

	// Validate basic auth config
	if c.Server.BasicAuth.Enabled {
		if c.Server.BasicAuth.Username == "" {
			return fmt.Errorf("basic_auth.username is required when basic_auth is enabled")
		}
		if c.Server.BasicAuth.Password == "" && c.Server.BasicAuth.PasswordHash == "" {
			return fmt.Errorf("either basic_auth.password or basic_auth.password_hash is required when basic_auth is enabled")
		}
		if c.Server.BasicAuth.Password != "" && c.Server.BasicAuth.PasswordHash != "" {
			return fmt.Errorf("cannot specify both basic_auth.password and basic_auth.password_hash")
		}
	}

	if len(c.Directories) == 0 {
		return fmt.Errorf("at least one directory must be configured")
	}

	for i := range c.Directories {
		dir := &c.Directories[i]
		if err := dir.Validate(); err != nil {
			return fmt.Errorf("directory[%d] (%s): %w", i, dir.Name, err)
		}
	}

	return nil
}

// Validate checks a directory configuration
func (d *DirectoryConfig) Validate() error {
	if d.Name == "" {
		return fmt.Errorf("name is required")
	}

	if d.WatchPath == "" {
		return fmt.Errorf("watch_path is required")
	}

	// If ingest_path is not specified, it defaults to watch_path, so no validation needed
	// (The above check is not needed as the condition is always false)

	// Validate watch mode
	validModes := map[string]bool{
		"event_only":               true,
		"polling_only":             true,
		"hybrid_ultra_low_latency": true,
	}
	if !validModes[d.Watch.Mode] {
		return fmt.Errorf("invalid watch mode: %s", d.Watch.Mode)
	}

	// Validate stability config
	if d.Stability.ConfirmationIntervalMs <= 0 {
		return fmt.Errorf("confirmation_interval_ms must be positive")
	}
	if d.Stability.RequiredStableChecks <= 0 {
		return fmt.Errorf("required_stable_checks must be positive")
	}
	if d.Stability.MaxWaitMs <= 0 {
		return fmt.Errorf("max_wait_ms must be positive")
	}

	// Validate outbound config
	if d.Outbound.URL == "" {
		return fmt.Errorf("outbound.url is required")
	}

	return nil
}

// GetConfirmationInterval returns the stability confirmation interval
func (s *StabilityConfig) GetConfirmationInterval() time.Duration {
	return time.Duration(s.ConfirmationIntervalMs) * time.Millisecond
}

// GetMaxWait returns the maximum wait time for stability
func (s *StabilityConfig) GetMaxWait() time.Duration {
	return time.Duration(s.MaxWaitMs) * time.Millisecond
}

// GetRetentionDuration returns the shadow retention duration
func (s *ShadowConfig) GetRetentionDuration() time.Duration {
	return time.Duration(s.RetentionHours) * time.Hour
}

// GetReconcileInterval returns the reconciliation scan interval
func (r *ReconcileScanConfig) GetReconcileInterval() time.Duration {
	return time.Duration(r.IntervalSeconds) * time.Second
}

// IsStartupReconcileScanEnabled returns whether startup reconciliation scan is enabled
func (w *WatchConfig) IsStartupReconcileScanEnabled() bool {
	if w.StartupReconcileScan == nil {
		return true // Default to enabled
	}
	return *w.StartupReconcileScan
}

// GetIngestPath returns the ingest path, defaulting to watch_path if not specified
func (d *DirectoryConfig) GetIngestPath() string {
	if d.IngestPath != "" {
		return d.IngestPath
	}
	return d.WatchPath
}

// setDefaults applies default values to the configuration
func setDefaults(cfg *Config) {
	for i := range cfg.Directories {
		// Enable startup reconciliation scan by default
		if cfg.Directories[i].Watch.StartupReconcileScan == nil {
			defaultValue := true
			cfg.Directories[i].Watch.StartupReconcileScan = &defaultValue
		}
	}
}

// applyEnvOverrides applies environment variable overrides to the config
func applyEnvOverrides(cfg *Config) {
	if port := os.Getenv("XFERD_PORT"); port != "" {
		_, _ = fmt.Sscanf(port, "%d", &cfg.Server.Port)
	}
	if addr := os.Getenv("XFERD_ADDRESS"); addr != "" {
		cfg.Server.Address = addr
	}
	if tempDir := os.Getenv("XFERD_TEMP_DIR"); tempDir != "" {
		cfg.Server.TempDir = tempDir
	}
}
