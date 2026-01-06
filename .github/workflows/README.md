# GitHub Workflows

This directory contains GitHub Actions workflows for continuous integration, testing, and release management.

## Workflows

### 1. CI Workflow (`ci.yml`)

**Triggers:**
- Push to `main`, `master`, or `develop` branches
- Pull requests to `main`, `master`, or `develop` branches
- Manual dispatch

**Jobs:**

#### Lint
- Runs code formatting checks (`go fmt`)
- Runs `go vet` for static analysis
- Runs `golangci-lint` for comprehensive linting
- OS: Ubuntu latest
- Go versions: 1.22

#### Test
- Runs unit tests with race detector
- Generates coverage reports
- Uploads coverage to Codecov (optional)
- **Matrix:** Ubuntu, Windows, macOS × Go 1.21, 1.22
- **Purpose:** Ensure code works across all supported platforms

#### Integration Tests
- Runs integration tests with filesystem interactions
- **Matrix:** Ubuntu, Windows, macOS
- Go version: 1.22

#### E2E Tests
- Runs end-to-end binary tests
- Compiles and tests actual binary behavior
- **Matrix:** Ubuntu, Windows, macOS
- Go version: 1.22

#### Build
- Builds binaries for all target platforms
- **Matrix:** Linux, macOS, Windows × amd64, arm64
- Uploads build artifacts
- **Dependencies:** Requires lint, test, integration, and E2E tests to pass

### 2. Release Workflow (`release.yml`)

**Triggers:**
- Push of version tags (`v*`)
- Manual dispatch

**Jobs:**

#### Test
- Runs all tests before releasing
- Unit tests, integration tests, and E2E tests
- Ensures only tested code is released

#### GoReleaser Build
- Builds multi-platform binaries
- Creates packages (DEB, RPM, archives)
- Downloads WinSW for Windows service support
- **Dependencies:** Requires tests to pass

#### Build MSI
- Creates Windows MSI installers
- Uses WinSW for Windows service integration
- Uploads MSI packages to GitHub releases