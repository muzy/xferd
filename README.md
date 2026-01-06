# Xferd

**Low-latency file movement service for cross-platform file transfers**

Xferd is a production-grade file movement service designed to reliably and efficiently transfer files both into and out of systems. It combines REST API ingress with intelligent filesystem watching to provide sub-second file detection and processing.

## Features

- **Low Latency**: Sub-300ms file detection on local filesystems
- **Cross-Platform**: Native support for Linux and Windows
- **Robust File Watching**: 
  - Atomic rename detection for instant processing
  - Stability confirmation for network filesystems
  - Recursive directory monitoring
  - Periodic reconciliation scans to catch missed files
- **REST API Ingress**: 
  - Streaming multipart uploads with TLS support
  - Optional HTTP Basic Authentication
  - Subdirectory support via URL path (e.g., `/upload/invoices/2025/01`)
  - Path traversal protection (prevents directory escape attacks)
  - Target directory routing with automatic subdirectory creation
- **Shadow Directory**: Optional file archival with retention policies
- **Smart Upload Dispatcher**: 
  - Concurrent uploads with configurable workers
  - Automatic retry on network/server errors
  - Streaming support for large files (≥10 GB)
- **Network Filesystem Support**: Reliable operation on NFS and SMB shares
- **System Service**: Runs as systemd service (Linux) or Windows Service

## Quick Start

### Installation

#### Linux (Debian/Ubuntu)

```bash
# Download the latest .deb package
wget https://github.com/muzy/xferd/releases/latest/download/xferd_linux_amd64.deb

# Install
sudo dpkg -i xferd_linux_amd64.deb

# Edit configuration
sudo nano /etc/xferd/config.yml

# Start service
sudo systemctl start xferd
sudo systemctl enable xferd
```

#### Linux (RHEL/CentOS)

```bash
# Download the latest .rpm package
wget https://github.com/muzy/xferd/releases/latest/download/xferd_linux_amd64.rpm

# Install
sudo rpm -i xferd_linux_amd64.rpm

# Edit configuration
sudo vi /etc/xferd/config.yml

# Start service
sudo systemctl start xferd
sudo systemctl enable xferd
```

#### Windows

**Option 1: MSI Installer (Recommended)**

1. Download the `.msi` installer from [releases](https://github.com/muzy/xferd/releases)
2. Run the installer (installs to `C:\Program Files\Xferd`)
3. Edit the created `config.yml` with your settings
4. Install as service: `xferd-service.exe install`
5. Start service: `xferd-service.exe start`

The MSI installer includes WinSW (Windows Service Wrapper) automatically.

**Option 2: Manual Installation**

1. Download the Windows zip from [releases](https://github.com/muzy/xferd/releases)
2. Extract to `C:\Program Files\Xferd`
3. Download WinSW from https://github.com/winsw/winsw/releases
4. Rename WinSW executable to `xferd-service.exe`
5. Copy `packaging/winsw/xferd.xml` to your installation directory
6. Create `config.yml` from `config.example.yml`
7. Install service: `xferd-service.exe install`
8. Start service: `xferd-service.exe start`

**Service Management Commands:**
```cmd
# All commands require Administrator privileges
xferd-service.exe install     # Install service
xferd-service.exe uninstall   # Uninstall service
xferd-service.exe start       # Start service
xferd-service.exe stop        # Stop service
xferd-service.exe restart     # Restart service
xferd-service.exe status      # Check status
```

**Service Logs:**
- `xferd-service.out.log` - Standard output
- `xferd-service.err.log` - Error output
- Logs rotate automatically (10MB, keeps 8 files)

### Configuration

Create or edit `/etc/xferd/config.yml`:

```yaml
server:
  address: "0.0.0.0"
  port: 8080
  temp_dir: /var/lib/xferd/temp
  tls:
    enabled: true
    cert_file: /etc/xferd/cert.pem
    key_file: /etc/xferd/key.pem
  basic_auth:
    enabled: true
    username: admin
    # Use password_hash instead of password for production
    password_hash: "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"

directories:
  - name: invoices
    watch_path: /data/invoices
    recursive: true
    ignore:
      - "*.tmp"
      - "*.partial"
      - "temp_*"
    watch:
      mode: hybrid_ultra_low_latency
      reconcile_scan:
        enabled: true
        interval_seconds: 30
    stability:
      confirmation_interval_ms: 100
      required_stable_checks: 2
      max_wait_ms: 1500
      # Note: If a file is continuously modified for longer than max_wait_ms,
      # it will be assumed stable and processed immediately. This prevents
      # indefinite waiting for files that are written slowly over long periods.
      # A timeout message will be logged when this occurs, and the source file
      # will NOT be deleted after upload to prevent data loss if writing continues.
      #
      # Additionally, a final stability check is performed before file deletion.
      # If the file changes during upload or shadow copy creation, it will be
      # preserved to prevent data loss.
    shadow:
      enabled: true
      path: /var/lib/xferd/shadow/invoices
      retention_hours: 48
    outbound:
      url: https://esb.example.com/upload
      auth:
        type: basic
        username: user
        password: secret
```

See [config.example.yml](config.example.yml) for a complete example (includes Windows-specific paths when used in MSI builds).

#### Directory Configuration Options

**name**: Unique identifier for the directory (used in upload URLs)

**watch_path**: Absolute path to the directory to monitor for files

**ingest_path** (optional): Absolute path to the directory where HTTP uploads should be placed. If not specified, defaults to `watch_path`. This allows for IN/OUT directory patterns where `watch_path` is the OUT directory (watched for files to upload) and `ingest_path` is the IN directory (where received HTTP uploads are stored for 3rd party software).

**recursive**: Whether to monitor subdirectories recursively (default: false)

**ignore**: Array of glob patterns to exclude files from processing:
- Filename patterns: `*.tmp`, `*.log`, `temp_*`, `backup_*`
- Path patterns: `*/cache/*`, `**/temp/*` (for recursive watching)
- Hidden files are always ignored automatically

**watch**: Configuration for file watching behavior (see Watch Modes section)

**stability**: Configuration for file stability confirmation (see Stability Checks section)

**shadow**: Configuration for shadow directory (see Shadow Directory section)

**outbound**: Configuration for upload destination (see Outbound Configuration section)

### Using Separate Watch and Ingest Directories

The `ingest_path` option allows you to separate directories for incoming HTTP uploads (IN) and outgoing file watching (OUT). This is common when communicating with 3rd party software that expects IN/OUT directory patterns:

**Use Cases:**
- **IN/OUT Directories**: 3rd party software has separate IN and OUT directories
- **File Exchange Patterns**: OUT directory contains files to be pushed/uploaded, IN directory receives files from external systems
- **Workflow Separation**: Keep upload sources separate from download destinations
- **Security Zoning**: Isolate incoming files from outgoing processing areas

**Example Workflow:**
```
3rd Party Software Directories:
/data/integration/out/     <- OUT: files for xferd to watch and upload
/data/integration/in/      <- IN: files received via HTTP for 3rd party software

Xferd Configuration:
watch_path: /data/integration/out    (watch for files to upload)
ingest_path: /data/integration/in    (place HTTP uploads here)

Workflow:
1. 3rd party software writes files to /data/integration/out/ (watch_path)
2. Xferd detects and uploads these files to external APIs
3. External systems send files to xferd via HTTP upload
4. Xferd places received files in /data/integration/in/ (ingest_path)
5. 3rd party software processes files from /data/integration/in/
```

**Important Notes:**
- `watch_path` is where xferd watches for files to upload (OUT directory)
- `ingest_path` is where HTTP uploads are placed (IN directory)
- When `ingest_path` is not specified, both uploads and watching use `watch_path`
- Directory structures are preserved in both directions

## Usage

### Upload Files via REST API

```bash
# Upload a file to the "invoices" directory
curl -X POST \
  -F "file=@invoice.pdf" \
  https://xferd.example.com:8080/upload/invoices

# Upload with basic authentication (if enabled)
curl -X POST \
  -u admin:password \
  -F "file=@invoice.pdf" \
  https://xferd.example.com:8080/upload/invoices

# Upload to different target directories
curl -X POST \
  -u admin:password \
  -F "file=@report.pdf" \
  https://xferd.example.com:8080/upload/reports

# Upload to subdirectories (specify subdirectory in URL path)
curl -X POST \
  -u admin:password \
  -F "file=@invoice.pdf" \
  https://xferd.example.com:8080/upload/invoices/2025/01

# Upload to deeply nested subdirectories
curl -X POST \
  -u admin:password \
  -F "file=@daily_report.pdf" \
  https://xferd.example.com:8080/upload/reports/2025/01/30

# Subdirectories are created automatically if they don't exist
```

**Subdirectory Support:**
- Subdirectories are specified in the URL path after the directory name
- Example: `/upload/invoices/2025/01/30` creates `{watch_path}/2025/01/30/` 
- Subdirectories are automatically created if they don't exist
- All subdirectory paths are validated to prevent directory escape attacks

**Security Notes:**
- The upload endpoint supports optional HTTP Basic Authentication
- All filenames and subdirectory paths are sanitized to prevent path traversal attacks
- Path traversal attempts (e.g., `../../../etc/passwd`) are rejected
- Files are uploaded to the specific directory configured for each route
- TLS encryption is strongly recommended for production use

### Watch Directory for Processing

Simply drop files into configured watch directories. Xferd will:
1. Detect the file (typically within 100-300ms)
2. Confirm it's fully written and stable
3. Store a copy in the shadow directory (if enabled)
4. Upload to the configured endpoint
5. Retry on failures

## Watch Modes

### hybrid_ultra_low_latency (Recommended)

Combines event-based watching with stability confirmation:
- Instant detection via filesystem events
- Atomic rename fast-path (no delay)
- Short stability checks for regular writes
- Periodic reconciliation scans

Best for: Most use cases, provides optimal balance of speed and reliability.

### event_only (Unsafe)

Processes files immediately on filesystem events without stability confirmation.

Best for: Controlled environments where files are always atomically moved.

### polling_only (Fallback)

Pure polling mode that completely ignores filesystem events and relies entirely on periodic reconciliation scans. 
Best for files on filesystems that do not support native events, such as SMB or NFS-like file systems.

**How it works:**
- Ignores all filesystem events (inotify/fsnotify)
- Periodically scans the entire directory tree for new files
- Performs stability checks on discovered files before processing
- Processes files that have been stable for the configured duration

**Example with Stability Checks:**
```
Time: 0s - File copy starts to /data/invoices/invoice.pdf
Time: 5s - File copy completes, but polling scan hasn't run yet
Time: 30s - Reconciliation scan discovers invoice.pdf
         - Stability check begins (confirmation_interval_ms: 100)
         - File size/modification time checked 3 times (required_stable_checks: 3)
         - If stable for 300ms, file is processed and uploaded
         - If still changing after max_wait_ms: 1500ms, file is processed anyway
Time: 35s - File uploaded, source file deleted (unless stability timeout occurred)
```

**Complete Configuration Example:**
```yaml
directories:
  - name: polled_directory
    watch_path: /data/files
    recursive: true
    ignore:
      - "*.tmp"
      - "*/cache/*"
      - "temp_*"
    watch:
      mode: polling_only
      reconcile_scan:
        enabled: true
        interval_seconds: 3          # Scan every 3 seconds
    stability:
      confirmation_interval_ms: 100  # Check stability every 100ms
      required_stable_checks: 3      # File must be stable for 3 consecutive checks (300ms total)
      max_wait_ms: 1500              # Give up waiting after 1.5 seconds
    shadow:
      enabled: true
      path: /var/lib/xferd/shadow/polled
      retention_hours: 48
    outbound:
      url: https://api.example.com/upload
```

**Performance Characteristics:**
- Higher CPU usage due to directory scanning
- Latency depends on scan interval (minimum 30+ seconds)
- Memory usage scales with number of files in watched directories
- No real-time detection - files are only found during scans

**Best for:**
- Network filesystems (NFS/SMB) with unreliable or missing events
- Environments where filesystem events are completely broken
- Legacy filesystems without proper event support
- Situations where real-time detection isn't critical

## Building from Source

### Prerequisites

- **Go 1.22 or later**
- **Make** (optional, for using Makefile)
- **WiX Toolset** (optional, for Windows MSI builds)

### Quick Build

#### Using Make (Recommended)
```bash
# Clone repository
git clone https://github.com/muzy/xferd.git
cd xferd

# Build binary
make build

# Run with example config
make run

# Run tests
make test

# Build for all platforms
make build-all
```

#### Using Go Directly
```bash
# Download dependencies
go mod download

# Build
go build -o xferd ./cmd/xferd

# Run
./xferd -config config.example.yml
```

#### Linux Static Binary
```bash
CGO_ENABLED=0 go build -ldflags="-s -w" -o xferd ./cmd/xferd
```

#### Cross-Platform Builds
```bash
# Windows from Linux
GOOS=windows GOARCH=amd64 go build -o xferd.exe ./cmd/xferd
```

#### Using GoReleaser
```bash
# Install GoReleaser
go install github.com/goreleaser/goreleaser@latest

# Create release packages
make release-snapshot
```

### Windows MSI Installer

Build Windows MSI installers that include WinSW service wrapper:

```bash
# Prerequisites (Ubuntu/Debian)
sudo apt-get install wixl

# Build MSI
make release-msi
```

The MSI includes:
- Xferd executable
- WinSW service wrapper
- Service configuration
- Example configuration
- Installation guide

## Development

### Project Structure

```
xferd/
├── cmd/xferd/           # Main application entry point
├── internal/
│   ├── config/          # Configuration management
│   ├── ingress/         # REST API server
│   ├── watcher/         # File watching (Linux/Windows)
│   ├── uploader/        # Upload dispatcher
│   ├── shadow/          # Shadow directory manager
│   └── service/         # Service orchestration
├── packaging/
│   ├── systemd/         # Linux systemd files
│   └── winsw/           # Windows service files
├── config.example.yml          # Example configuration
├── config.example.windows.yml  # Windows-specific configuration (used for MSI builds)
└── .goreleaser.yml      # Release configuration
```

### Testing

#### Running Tests

```bash
# All unit tests with race detector and coverage
make test

# Quick tests (no race detector)
make test-short

# Integration tests (real filesystem operations)
make test-integration

# All tests (unit + integration + E2E)
make test-all

# Generate HTML coverage report
make test-coverage
```

#### Package-Specific Tests
```bash
make test-config     # Configuration tests
make test-shadow     # Shadow directory tests
make test-watcher    # File watching tests
make test-ingress    # REST API tests
make test-uploader   # Upload dispatcher tests
```

#### What Gets Tested
- **Configuration**: All config options, validation, environment variables
- **Authentication**: Basic auth, bearer tokens, API tokens
- **File Operations**: Upload, watching, stability confirmation, atomic renames
- **Network**: Upload failures, retries, different auth types
- **Platforms**: Cross-platform filesystem behavior
- **Edge Cases**: File disappearance, permission issues, concurrent operations

## Deployment

### Systemd (Linux)

```bash
# View status
sudo systemctl status xferd

# View logs
sudo journalctl -u xferd -f

# Restart
sudo systemctl restart xferd
```

### Windows Service

```cmd
# View status
xferd-service.exe status

# View logs
type C:\Program Files\Xferd\xferd-service.out.log

# Restart
xferd-service.exe restart
```

## Troubleshooting

### Files Not Being Detected

1. Check watch mode configuration
2. Verify file paths and permissions
3. Enable reconciliation scans
4. Check logs for errors

### High Latency

1. Reduce `confirmation_interval_ms`
2. Reduce `required_stable_checks` (carefully)
3. Use `hybrid_ultra_low_latency` mode
4. Ensure files are atomically renamed when possible

### Files Processed Before Writing Completes

If you see log messages like "Stability check timeout for /path/file: assuming stable after 2000ms",
this indicates that a file was continuously modified for longer than `max_wait_ms`. Xferd assumes
the file is stable to prevent indefinite waiting. This commonly occurs with:

- Slow network copies (e.g., `scp`, `rsync`)
- Streaming writes (e.g., `dd`, `tar`)
- Files written over long periods

**Data Protection:** Even if a file is processed due to timeout, Xferd performs a final stability
check before deletion. If the file changes during upload or shadow copy creation, the source file
is preserved to prevent data loss.

**Solutions:**
1. Increase `max_wait_ms` for directories with slow writes
2. Use atomic renames when possible (`mv` instead of `cp`)
3. For streaming operations, ensure the write completes within your `max_wait_ms` window

**Note:** Files processed due to stability timeout will be uploaded but the source file will be preserved (not deleted) to prevent data loss if writing continues.
4. Check for "file changed during processing, keeping source" messages in logs

### Upload Failures

1. Check network connectivity
2. Verify authentication credentials
3. Review upload endpoint logs
4. Check shadow directory for archived files

## Security Considerations

### Authentication

#### HTTP Basic Authentication
The REST API supports optional HTTP Basic Authentication with secure password storage:

**Configuration Options:**
```yaml
server:
  basic_auth:
    enabled: true
    username: your_username
    # Option 1: Secure password hash (recommended for production)
    password_hash: "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"
    # Option 2: Plaintext password (development only)
    # password: your_plaintext_password
```

**Generate Secure Password Hashes:**
```bash
xferd-hashpw
# Enter password when prompted (input is hidden)
# Copy the generated bcrypt hash to your config.yml
```

**Security Benefits:**
- Bcrypt hashing with unique salts (production)
- Constant-time password comparison prevents timing attacks
- Failed authentication attempts are logged with source IP
- Health endpoint remains accessible without authentication

### Path Traversal Protection

Multiple defense layers prevent path traversal attacks:

**Defense Layer 1: Go's multipart library**
- Automatically extracts basename from Content-Disposition headers

**Defense Layer 2: Filename Sanitization**
- Rejects paths with `/`, `\`, `..`, or null bytes
- Prevents directory escape attempts like `../../../etc/passwd`

**Defense Layer 3: Directory Isolation**
- Each configured directory is isolated by name
- Files written only to specified watch paths
- Atomic rename ensures files appear in correct locations

### TLS Encryption

**Configuration:**
```yaml
server:
  tls:
    enabled: true
    cert_file: /etc/xferd/cert.pem
    key_file: /etc/xferd/key.pem
```

**Features:**
- TLS 1.2 minimum version enforced
- Protects credentials and file data in transit
- Strongly recommended for production deployments

### File Upload Security

**Atomic Operations:**
- Files uploaded to temporary directory with `.partial` suffix
- Atomic rename prevents processing incomplete uploads
- Reduces race conditions and ensures data integrity

**Streaming Support:**
- Large files streamed to disk (no memory buffering)
- Prevents memory exhaustion attacks
- Supports files ≥10 GB

### File System Security

- **Configuration Security**: Store config files with `chmod 600`
- **User Isolation**: Run as dedicated user with minimal permissions
- **Directory Permissions**: Set appropriate ownership and permissions
- **Shadow Directory**: Implement retention policies for archived files

### Security Best Practices

1. **Always use `password_hash` in production** - never store plaintext passwords
2. **Generate unique hashes** for each environment (dev/staging/production)
3. **Use strong passwords** - minimum 12 characters, mixed case, numbers, symbols
4. **Rotate credentials regularly** - generate new hashes when changing passwords
5. **Enable TLS** - use certificates from trusted CAs
6. **Secure config files** - `chmod 600 /etc/xferd/config.yml`
7. **Run as dedicated user** - create xferd user with minimal permissions
8. **Monitor logs** - watch for failed authentication and rejected filenames
9. **Limit network access** - firewall ingress port appropriately
10. **Keep software updated** - monitor for security releases

### Monitoring & Auditing

Monitor logs for security events:
```bash
# Check for failed authentication attempts
sudo journalctl -u xferd | grep "Failed authentication"

# Check for rejected unsafe filenames
sudo journalctl -u xferd | grep "Rejected unsafe filename"
```

Set up alerts for:
- Repeated authentication failures
- Unusual upload patterns
- Configuration file changes

## License

MIT License - see [LICENSE](LICENSE) for details

## Support

- **Issues**: https://github.com/muzy/xferd/issues
- **Discussions**: https://github.com/muzy/xferd/discussions

## Contributing

Contributions welcome! Please:
1. Fork the repository
2. Create a feature branch
3. Add tests for new functionality
4. Submit a pull request

## Acknowledgments

Built with:
- [fsnotify](https://github.com/fsnotify/fsnotify) - Cross-platform filesystem notifications
- [yaml.v3](https://github.com/go-yaml/yaml) - YAML parsing

