# PowerShell script to download the latest WinSW release for MSI packaging

$ErrorActionPreference = "Stop"

$WinswRepo = "winsw/winsw"
$OutputDir = "packaging/winsw/bin"

# Create output directory
New-Item -ItemType Directory -Force -Path $OutputDir | Out-Null

Write-Host "Fetching latest WinSW release..."

# Get the latest release info from GitHub API
$LatestRelease = Invoke-RestMethod -Uri "https://api.github.com/repos/$WinswRepo/releases/latest"

# Extract the version tag
$Version = $LatestRelease.tag_name
Write-Host "Latest WinSW version: $Version"

# Find download URLs for the binaries we need
$WinswX64Asset = $LatestRelease.assets | Where-Object { $_.name -eq "WinSW-x64.exe" }
$WinswX86Asset = $LatestRelease.assets | Where-Object { $_.name -eq "WinSW-x86.exe" }

if (-not $WinswX64Asset) {
    Write-Error "Could not find WinSW-x64.exe in the latest release"
    exit 1
}

if (-not $WinswX86Asset) {
    Write-Error "Could not find WinSW-x86.exe in the latest release"
    exit 1
}

# Download WinSW binaries
Write-Host "Downloading WinSW-x64.exe..."
Invoke-WebRequest -Uri $WinswX64Asset.browser_download_url -OutFile "$OutputDir/WinSW-x64.exe"

Write-Host "Downloading WinSW-x86.exe..."
Invoke-WebRequest -Uri $WinswX86Asset.browser_download_url -OutFile "$OutputDir/WinSW-x86.exe"

# Download the LICENSE file
Write-Host "Downloading WinSW LICENSE..."
$WinswLicenseUrl = "https://raw.githubusercontent.com/$WinswRepo/$Version/LICENSE.txt"
Invoke-WebRequest -Uri $WinswLicenseUrl -OutFile "$OutputDir/WinSW-LICENSE.txt"

# Create a version file
Set-Content -Path "$OutputDir/winsw-version.txt" -Value $Version

Write-Host "WinSW binaries downloaded successfully to $OutputDir"
Write-Host "  - WinSW-x64.exe"
Write-Host "  - WinSW-x86.exe"
Write-Host "  - WinSW-LICENSE.txt"
Write-Host "  - winsw-version.txt"

