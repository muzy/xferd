# Binary-Based E2E Tests

## Overview

These tests compile and run the actual `xferd` binary as an external process, then test it by interacting with it through:
- HTTP API calls
- Filesystem operations
- Signal handling
- Configuration files

This is the **most realistic** testing approach - testing exactly what users run in production.

## Test Philosophy

### Three-Level Testing Strategy

1. **Unit Tests** (`internal/*/`*_test.go`)
   - Test individual functions and packages
   - Fast, isolated, good debugging
   - Run with: `make test`

2. **Integration Tests** (`internal/service/service_test.go`)
   - Test components working together programmatically
   - Import packages, test integration
   - Run with: `make test-integration`

3. **E2E Binary Tests** (`test/e2e/*_test.go`) ← **You are here**
   - Compile and run actual binary
   - Test as external process
   - Most realistic, tests complete system
   - Run with: `make test-e2e`

## Running E2E Tests

```bash
# Run all E2E binary tests
make test-e2e

# Run with verbose output
make test-e2e-verbose

# Run specific test
go test -v -tags=e2e -run TestE2EBinaryBasic ./test/e2e

# Run complete test suite (all three levels)
make test-complete
```

## What These Tests Do

### TestE2EBinaryBasic
1. Compiles the xferd binary
2. Creates a test configuration file
3. Starts the binary as a subprocess
4. Tests:
   - Health endpoint responds
   - File watching works
   - REST API uploads work
   - Shadow copies are created
   - Multiple file handling
   - Graceful shutdown (SIGTERM)

### TestE2EBinaryRecursive
1. Compiles and starts xferd
2. Tests recursive directory watching
3. Creates files in nested directories
4. Verifies all files are detected and uploaded

### TestE2EBinarySignalHandling
1. Tests SIGTERM handling
2. Verifies graceful shutdown
3. Tests cleanup on exit

## Requirements

- Go 1.22+ installed
- Ability to compile binaries
- Ports 19080-19085 available
- Standard filesystem permissions

## Test Structure

```
test/e2e/
├── e2e_binary_test.go    # Main E2E binary tests
└── README.md             # This file
```

## Build Tags

Tests use the `e2e` build tag to separate them from faster unit tests:

```go
//go:build e2e
// +build e2e
```

This allows running only E2E tests when needed:
```bash
go test -tags=e2e ./test/e2e/...
```

### Manual Testing
You can run the test binary manually:
```bash
# Build from test
cd /tmp/TestE2EBinaryBasic*/
./xferd -config config.yml

# In another terminal
curl http://127.0.0.1:19080/health
```

### Common Issues

**Port Already in Use**
```
bind: address already in use
```
Solution: Change port in test or kill process using port

**Binary Won't Compile**
```
Failed to build binary
```
Solution: Check Go installation, verify code compiles manually

**Timeout Waiting for Upload**
```
File was not uploaded within timeout
```
Solution: Check logs, verify watcher is running, check file permissions

## CI/CD Integration

### GitHub Actions
```yaml
name: E2E Tests
on: [push, pull_request]
jobs:
  e2e:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
        with:
          go-version: '1.22'
      - name: Run E2E tests
        run: make test-e2e
        timeout-minutes: 10
```

### GitLab CI
```yaml
e2e-tests:
  stage: test
  image: golang:1.22
  script:
    - make test-e2e
  timeout: 10m
```
