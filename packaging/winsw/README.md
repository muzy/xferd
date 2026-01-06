# Xferd Windows Service Installation

This directory contains the Windows Service configuration for running Xferd as a Windows service using WinSW (Windows Service Wrapper).

## Prerequisites

- Windows Server 2016+ or Windows 10+
- Administrative privileges

## Installation Steps

### 1. Download WinSW

Download the latest WinSW executable from:
https://github.com/winsw/winsw/releases

Choose the appropriate version:
- `WinSW-x64.exe` for 64-bit Windows
- `WinSW-x86.exe` for 32-bit Windows

### 2. Setup Directory Structure

Create a directory for Xferd, for example: `C:\Program Files\Xferd`

Copy the following files to this directory:
- `xferd.exe` (the Xferd binary)
- `xferd.xml` (this service configuration)
- `config.yml` (your Xferd configuration)
- Rename the downloaded WinSW executable to `xferd-service.exe`

### 3. Configure Xferd

Edit `config.yml` with your desired settings:
- Set appropriate watch directories
- Configure upload endpoints
- Adjust stability and performance settings

### 4. Install the Service

Open Command Prompt or PowerShell **as Administrator** and run:

```cmd
cd "C:\Program Files\Xferd"
xferd-service.exe install
```

### 5. Start the Service

```cmd
xferd-service.exe start
```

Or use Windows Services manager:
- Press `Win + R`, type `services.msc`
- Find "Xferd File Movement Service"
- Right-click and select "Start"

## Management Commands

All commands must be run from the Xferd installation directory as Administrator:

- **Install service**: `xferd-service.exe install`
- **Uninstall service**: `xferd-service.exe uninstall`
- **Start service**: `xferd-service.exe start`
- **Stop service**: `xferd-service.exe stop`
- **Restart service**: `xferd-service.exe restart`
- **Check status**: `xferd-service.exe status`

## Viewing Logs

Logs are written to the same directory as the service:
- `xferd-service.out.log` - Standard output
- `xferd-service.err.log` - Error output

Logs rotate automatically when they reach 10MB, keeping the last 8 log files.

## Service Configuration

The service is configured via `xferd.xml`. Key settings:

- **Automatic startup**: Service starts automatically on system boot
- **Restart on failure**: Automatically restarts after crashes
- **LocalSystem account**: Runs with full system privileges (change if needed)

## Troubleshooting

### Service won't start
1. Check that `config.yml` is valid YAML
2. Verify all paths in `config.yml` exist and are accessible
3. Check `xferd-service.err.log` for errors

### Permission issues
- Ensure the service account has read/write access to:
  - Watch directories
  - Temp directory
  - Shadow directories

### High CPU usage
- Reduce reconciliation scan frequency
- Check for very large directory trees
- Consider using `polling_only` mode on network filesystems

## Uninstallation

1. Stop the service: `xferd-service.exe stop`
2. Uninstall the service: `xferd-service.exe uninstall`
3. Delete the Xferd directory

## Support

For issues and support, visit: https://github.com/muzy/xferd

