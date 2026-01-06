# Building Windows MSI Installers for Xferd

This directory contains the WiX configuration for building Windows MSI installer packages that include the latest version of WinSW (Windows Service Wrapper).

## Overview

The MSI installer includes:
- Xferd executable (`xferd.exe`)
- WinSW service wrapper (`xferd-service.exe`) - automatically downloaded from the latest release
- Service configuration (`xferd.xml`)
- Example configuration (`config.example.yml`)
- Service installation guide

## Prerequisites

### On Linux/macOS

Install `wixl`:

```bash
# Ubuntu/Debian
sudo apt-get install wixl

# macOS
brew install wixl
```

### On Windows

Install WiX Toolset v3.11 or later:
- Download from: https://wixtoolset.org/
- Or use Chocolatey: `choco install wixtoolset`

## Building MSI Packages

### Automated Build (Recommended)

Use the Makefile targets:

```bash
# Download latest WinSW
make download-winsw

# Build snapshot release and create MSI
make release-msi
```

### Manual Build

1. **Download WinSW:**
   ```bash
   ./scripts/download-winsw.sh
   ```

2. **Build binaries with GoReleaser:**
   ```bash
   goreleaser release --snapshot --clean --skip=publish
   ```

3. **Build MSI packages:**
   
   On Linux/macOS:
   ```bash
   ./scripts/build-msi.sh
   ```
   
   On Windows:
   ```powershell
   .\scripts\build-msi.ps1
   ```

The MSI files will be created in the `dist/` directory:
- `xferd_<version>_windows_amd64.msi`

## MSI Installation

The MSI installer is a **silent installer** that runs without user interaction dialogs. It automatically installs to the default location without prompting for confirmation.

### What the MSI Does

When installed, the MSI:
1. Installs Xferd to `C:\Program Files\Xferd\` (64-bit Program Files)
2. Includes WinSW pre-configured as `xferd-service.exe`
3. Includes service configuration (`xferd.xml`)
4. Includes Windows-specific example config (`config.example.yml` with Windows paths)
5. Includes WinSW license file (`WinSW-LICENSE.txt`)
6. Creates Start Menu shortcuts to the install folder and setup guide

### License Information

Before installing, please review the license terms:
- **MIT License**: See `license.rtf` in the MSI package or visit the project repository
- The license agreement is not displayed during silent installation

## Installation Instructions

### Installing the MSI

The MSI is a silent installer that can be installed using any of these methods:

```cmd
# Double-click the MSI file in Windows Explorer
# Or use the command line:
msiexec /i xferd_<version>_windows_amd64.msi /quiet

# Or install with PowerShell:
Start-Process -FilePath "xferd_<version>_windows_amd64.msi" -ArgumentList "/quiet" -Wait
```

**Note**: The installation runs silently without showing any dialogs. Please review the license before installation.

### Post-Installation Steps

After installing the MSI, users need to:

1. **Create a configuration file:**
   ```cmd
   cd "C:\Program Files\Xferd"
   copy config.example.yml config.yml
   notepad config.yml
   ```

   The `config.example.yml` is already configured with Windows-appropriate paths.

2. **Install the Windows service:**
   ```cmd
   cd "C:\Program Files\Xferd"
   xferd-service.exe install
   ```

3. **Start the service:**
   ```cmd
   xferd-service.exe start
   ```

See `SERVICE_README.md` in the installation directory for complete instructions.

## WinSW Updates

The MSI build process automatically downloads the latest WinSW release from:
https://github.com/winsw/winsw/releases

The downloaded version is recorded in `packaging/winsw/bin/winsw-version.txt` and included in the release artifacts.

To update to a newer WinSW version, simply run:
```bash
make download-winsw
```

## Customizing the Installer

### Change Installation Directory

Edit `packaging/wix/xferd.wxs` and modify the `INSTALLFOLDER` name.

### Change Product GUID

If you fork this project, generate new GUIDs using:
```bash
uuidgen  # Linux/macOS
```
or PowerShell on Windows:
```powershell
[guid]::NewGuid()
```

Update the GUIDs in `xferd.wxs` for:
- Product `Id`
- Product `UpgradeCode`
- Each Component `Guid`

### Modify License

Replace `license.rtf` with your own license file in RTF format.

## Testing

Test the MSI in a clean Windows environment:

1. **Install the MSI** (runs silently):
   ```cmd
   msiexec /i xferd_<version>_windows_amd64.msi /quiet
   ```

2. **Verify installation**:
   - Files should be in `C:\Program Files\Xferd\`
   - Check that `xferd` is in PATH: `where xferd`

3. **Test service setup**:
   ```cmd
   cd "C:\Program Files\Xferd"
   xferd-service.exe install
   ```

4. **Uninstall**:
   ```cmd
   msiexec /x xferd_<version>_windows_amd64.msi /quiet
   ```
   Or use Windows Settings > Apps > Xferd > Uninstall

## Troubleshooting

### wixl: error: cannot open file

Ensure all referenced files exist in the build directory. The build script should copy all necessary files.

### Version mismatch errors

The MSI `Version` attribute must be in the format `X.Y.Z` (no `v` prefix, no pre-release tags). The build scripts handle this automatically.

### Permission denied on Windows

Run the build script as Administrator or ensure you have write permissions to the dist directory.

## CI/CD Integration

For automated builds in CI/CD pipelines:

```yaml
# Example GitHub Actions workflow
- name: Download WinSW
  run: ./scripts/download-winsw.sh

- name: Build release
  uses: goreleaser/goreleaser-action@v5
  with:
    version: latest
    args: release --clean

- name: Build MSI
  run: ./scripts/build-msi.sh
```

See `.github/workflows/release.yml` for a complete example.

## Support

For issues with MSI building, open an issue at: https://github.com/muzy/xferd/issues

